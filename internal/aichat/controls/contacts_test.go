package controls

import (
	"strings"
	"testing"

	"github.com/strongo/aichat/ai/session"
)

// TestNewContactCard_NoRawIDs covers S8/S9: a contact card shows the
// display name, never the raw contactID/spaceID keys a generic field dump
// would surface.
func TestNewContactCard_NoRawIDs(t *testing.T) {
	ref := session.EntityRef{Type: "contact", Title: "Alice Smith",
		Keys: map[string]string{"spaceID": "sp1", "contactID": "c1"}}
	b := NewContactCard("Alice Smith", ref)
	view := plain(b.View(60, false))
	if !strings.Contains(view, "Alice Smith") {
		t.Fatalf("view = %q, want the contact's name", view)
	}
	if strings.Contains(view, "c1") || strings.Contains(view, "contactID") || strings.Contains(view, "sp1") {
		t.Fatalf("view = %q, must not show raw entity keys", view)
	}
}

func TestNewContactCard_EmptyNameFallsBackToLabel(t *testing.T) {
	b := NewContactCard("", session.EntityRef{Type: "contact"})
	if b.Title != "Contact" {
		t.Fatalf("Title = %q, want the fallback label", b.Title)
	}
}
