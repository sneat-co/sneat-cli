package chatapp

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/bots-go-framework/bots-go-core/botkb"
)

// TestButtonsBlock_CurrentButton_OutOfBounds covers currentButton's own
// bounds guards directly, unreachable through Update alone (Update never
// lets row/col go negative or past the last row/col on its own).
func TestButtonsBlock_CurrentButton_OutOfBounds(t *testing.T) {
	kb := botkb.NewMessageKeyboard(botkb.KeyboardTypeInline, []botkb.Button{botkb.NewDataButton("A", "a")})
	b := newButtonsBlock(kb)

	b.row = -1
	if b.currentButton() != nil {
		t.Fatal("row=-1 should yield no current button")
	}
	b.row = 5
	if b.currentButton() != nil {
		t.Fatal("row past the end should yield no current button")
	}
	b.row = 0
	b.col = -1
	if b.currentButton() != nil {
		t.Fatal("col=-1 should yield no current button")
	}
	b.col = 5
	if b.currentButton() != nil {
		t.Fatal("col past the end should yield no current button")
	}
}

// TestButtonsBlock_ClampCol_ShrinksOnShorterRow covers clampCol's own
// shrink-to-fit branch: moving from a longer row to a shorter one clamps
// col into range instead of leaving it dangling.
func TestButtonsBlock_ClampCol_ShrinksOnShorterRow(t *testing.T) {
	kb := botkb.NewMessageKeyboard(botkb.KeyboardTypeInline,
		[]botkb.Button{botkb.NewDataButton("A", "a"), botkb.NewDataButton("B", "b"), botkb.NewDataButton("C", "c")},
		[]botkb.Button{botkb.NewDataButton("D", "d")},
	)
	b := newButtonsBlock(kb)
	b.col = 2 // last button of row 0 (C)
	b.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if b.row != 1 || b.col != 0 {
		t.Fatalf("row=%d col=%d, want row 1 col clamped to 0 (row 1 has only one button)", b.row, b.col)
	}
}

// TestButtonsBlock_Update_UpDownLeftRightAtBounds covers every no-op guard:
// up at row 0, down at the last row, left at col 0, right at the last col,
// and a non-KeyPressMsg passthrough.
func TestButtonsBlock_Update_UpDownLeftRightAtBounds(t *testing.T) {
	kb := botkb.NewMessageKeyboard(botkb.KeyboardTypeInline,
		[]botkb.Button{botkb.NewDataButton("A", "a"), botkb.NewDataButton("B", "b")},
	)
	b := newButtonsBlock(kb)

	if _, cmd := b.Update(tea.KeyPressMsg{Code: tea.KeyUp}); cmd != nil {
		t.Fatal("up at row 0 should be a no-op")
	}
	if b.row != 0 {
		t.Fatalf("row = %d, want unchanged 0", b.row)
	}

	if _, cmd := b.Update(tea.KeyPressMsg{Code: tea.KeyDown}); cmd != nil {
		t.Fatal("down at the only/last row should be a no-op")
	}

	if _, cmd := b.Update(tea.KeyPressMsg{Code: tea.KeyLeft}); cmd != nil {
		t.Fatal("left at col 0 should be a no-op")
	}
	if b.col != 0 {
		t.Fatalf("col = %d, want unchanged 0", b.col)
	}

	b.col = 1
	if _, cmd := b.Update(tea.KeyPressMsg{Code: tea.KeyRight}); cmd != nil {
		t.Fatal("right at the last col should be a no-op")
	}
	if b.col != 1 {
		t.Fatalf("col = %d, want unchanged 1", b.col)
	}

	if nb, cmd := b.Update(struct{ tea.Msg }{}); nb != b || cmd != nil {
		t.Fatal("a non-KeyPressMsg should be a pure no-op")
	}

	if _, cmd := b.Update(tea.KeyPressMsg{Code: 'x'}); cmd != nil {
		t.Fatal("an unmapped key should be a no-op")
	}
}

// TestButtonsBlock_View_FocusedCursorReversed covers View's focused-cursor
// reverse-style branch and the between-rows newline.
func TestButtonsBlock_View_FocusedCursorReversed(t *testing.T) {
	kb := botkb.NewMessageKeyboard(botkb.KeyboardTypeInline,
		[]botkb.Button{botkb.NewDataButton("A", "a")},
		[]botkb.Button{botkb.NewDataButton("B", "b")},
	)
	b := newButtonsBlock(kb)
	got := plain(b.View(80, true))
	if !strings.Contains(got, "A") || !strings.Contains(got, "B") {
		t.Fatalf("View(focused) = %q", got)
	}
}

// TestClampWidth_Buttons covers both the width<=0 passthrough and the
// positive-width clamp branch for chatapp's own clampWidth (a deliberate
// duplicate of controls' -- see its own doc comment).
func TestClampWidth_Buttons(t *testing.T) {
	if got := clampWidth("hello", 0); got != "hello" {
		t.Fatalf("clampWidth(width=0) = %q, want unchanged", got)
	}
	got := clampWidth("hello world this is long", 5)
	if !strings.Contains(got, "hello") {
		t.Fatalf("clampWidth(width=5) = %q", got)
	}
}
