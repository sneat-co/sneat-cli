package chatapp

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/bots-go-framework/bots-go-core/botkb"
)

func plain(s string) string { return ansi.Strip(s) }

func TestNewButtonsBlock_NilForEmptyOrWrongType(t *testing.T) {
	if newButtonsBlock(nil) != nil {
		t.Fatal("nil Keyboard must yield a nil block")
	}
	empty := botkb.NewMessageKeyboard(botkb.KeyboardTypeInline)
	if newButtonsBlock(empty) != nil {
		t.Fatal("a Keyboard with no rows must yield a nil block")
	}
}

func TestButtonsBlock_ViewAndNavigation(t *testing.T) {
	kb := botkb.NewMessageKeyboard(botkb.KeyboardTypeInline,
		[]botkb.Button{botkb.NewDataButton("Home", "space?id=sp1")},
		[]botkb.Button{botkb.NewDataButton("Work", "space?id=sp2")},
	)
	b := newButtonsBlock(kb)
	if b == nil {
		t.Fatal("expected a block")
	}
	if !b.Focusable() {
		t.Fatal("expected a non-empty buttons block to be focusable")
	}
	view := plain(b.View(60, false))
	if !strings.Contains(view, "Home") || !strings.Contains(view, "Work") {
		t.Fatalf("view = %q", view)
	}

	// Cursor starts on the first row's first button.
	blk, cmd := b.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a command for Enter")
	}
	msg := cmd()
	activated, ok := msg.(buttonActivatedMsg)
	if !ok {
		t.Fatalf("msg = %#v, want buttonActivatedMsg", msg)
	}
	if activated.Button.GetText() != "Home" {
		t.Fatalf("activated = %+v, want the first button (Home)", activated.Button)
	}
	if blk != b {
		t.Fatal("Update must return the same block for Enter")
	}

	// Down moves to the second row.
	blk, _ = b.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, cmd = blk.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	activated = cmd().(buttonActivatedMsg)
	if activated.Button.GetText() != "Work" {
		t.Fatalf("activated = %+v, want the second row's button (Work)", activated.Button)
	}
}

func TestButtonsBlock_LeftRightMoveWithinRow(t *testing.T) {
	kb := botkb.NewMessageKeyboard(botkb.KeyboardTypeInline,
		[]botkb.Button{botkb.NewDataButton("A", "a"), botkb.NewDataButton("B", "b")},
	)
	b := newButtonsBlock(kb)
	b.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	_, cmd := b.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	activated := cmd().(buttonActivatedMsg)
	if activated.Button.GetText() != "B" {
		t.Fatalf("activated = %+v, want B after Right", activated.Button)
	}
}
