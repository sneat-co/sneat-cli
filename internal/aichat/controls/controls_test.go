package controls

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/strongo/aichat/ai/session"
)

func plain(s string) string { return ansi.Strip(s) }

func TestListBlock_View(t *testing.T) {
	items := []Item{
		{Title: "Dentist", Subtitle: "16:00", Ref: session.EntityRef{Type: "happening"}},
		{Title: "Standup", Subtitle: "10:00", Ref: session.EntityRef{Type: "happening"}},
	}
	b := NewListBlock("Today", items)
	if !b.Focusable() {
		t.Fatal("expected a non-empty list to be focusable")
	}
	view := plain(b.View(40, false))
	if !strings.Contains(view, "Today") || !strings.Contains(view, "Dentist") || !strings.Contains(view, "Standup") {
		t.Fatalf("view = %q", view)
	}
	if got := b.Current(); got == nil || got.Type != "happening" {
		t.Fatalf("Current() = %v, want the cursor's (first) item", got)
	}
}

func TestListBlock_EmptyIsNotFocusable(t *testing.T) {
	b := NewListBlock("Todos", nil)
	if b.Focusable() {
		t.Fatal("an empty list must not be focusable")
	}
	if b.Current() != nil {
		t.Fatal("Current() on an empty list must be nil")
	}
	view := plain(b.View(40, false))
	if !strings.Contains(view, "nothing") {
		t.Fatalf("view = %q, want an explicit empty message", view)
	}
}

func TestListBlock_CursorMovesWithArrowKeys(t *testing.T) {
	items := []Item{
		{Title: "A", Ref: session.EntityRef{Type: "happening", Keys: map[string]string{"id": "a"}}},
		{Title: "B", Ref: session.EntityRef{Type: "happening", Keys: map[string]string{"id": "b"}}},
	}
	b := NewListBlock("List", items)
	if b.Current().Keys["id"] != "a" {
		t.Fatalf("initial cursor should be the first item")
	}
	blk, cmd := b.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if cmd != nil {
		t.Fatal("ListBlock.Update must not return a command")
	}
	if blk.(*ListBlock).Current().Keys["id"] != "b" {
		t.Fatalf("cursor did not move down")
	}
}

func TestCardBlock_View(t *testing.T) {
	ref := session.EntityRef{Type: "contact", Keys: map[string]string{"contactID": "c1"}}
	b := NewCardBlock("Alice", ref, [2]string{"Email", "alice@example.com"})
	if !b.Focusable() {
		t.Fatal("a card must be focusable")
	}
	if b.Current() == nil || b.Current().Keys["contactID"] != "c1" {
		t.Fatal("Current() must report the card's own entity")
	}
	view := plain(b.View(40, true))
	if !strings.Contains(view, "Alice") || !strings.Contains(view, "alice@example.com") {
		t.Fatalf("view = %q", view)
	}
}

func TestNewContactsGrid_BuildsPositionalRows(t *testing.T) {
	contacts := []Contact{
		{Name: "Alice", Ref: session.EntityRef{Type: "contact", Keys: map[string]string{"contactID": "c1"}}},
		{Name: "Bob", Ref: session.EntityRef{Type: "contact", Keys: map[string]string{"contactID": "c2"}}},
	}
	g := NewContactsGrid("Contacts", contacts)
	if len(g.Rows()) != 2 {
		t.Fatalf("rows = %d, want 2", len(g.Rows()))
	}
	if g.Rows()[0].Values[0] != "Alice" || g.Rows()[1].Values[0] != "Bob" {
		t.Fatalf("rows = %+v", g.Rows())
	}
	if g.Rows()[0].Ref == nil || g.Rows()[0].Ref.Keys["contactID"] != "c1" {
		t.Fatalf("row Ref = %+v", g.Rows()[0].Ref)
	}
}
