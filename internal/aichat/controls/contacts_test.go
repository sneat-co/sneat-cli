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
	b := NewContactCard(ContactRow{Ref: ref, Name: "Alice Smith"})
	view := plain(b.View(60, false))
	if !strings.Contains(view, "Alice Smith") {
		t.Fatalf("view = %q, want the contact's name", view)
	}
	if strings.Contains(view, "c1") || strings.Contains(view, "contactID") || strings.Contains(view, "sp1") {
		t.Fatalf("view = %q, must not show raw entity keys", view)
	}
}

func TestNewContactCard_EmptyNameFallsBackToLabel(t *testing.T) {
	b := NewContactCard(ContactRow{Ref: session.EntityRef{Type: "contact"}})
	if b.Title != "Contact" {
		t.Fatalf("Title = %q, want the fallback label", b.Title)
	}
}

// TestNewContactCard_ShowsRelationshipAndOtherFields is m11: a ContactRow's
// human-readable fields (relationship, birthday, emails, phones) render as
// label/value lines, still with no raw entity keys.
func TestNewContactCard_ShowsRelationshipAndOtherFields(t *testing.T) {
	ref := session.EntityRef{Type: "contact", Title: "Bob",
		Keys: map[string]string{"spaceID": "sp1", "contactID": "c1"}}
	b := NewContactCard(ContactRow{
		Ref: ref, Name: "Bob", RelatedAs: "spouse", DoB: "1990-01-02",
		Emails: []string{"bob@example.com"}, Phones: []string{"+1 555 0100"},
	})
	view := plain(b.View(60, false))
	for _, want := range []string{"Bob", "spouse", "1990-01-02", "bob@example.com", "+1 555 0100"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view = %q, want it to contain %q", view, want)
		}
	}
	if strings.Contains(view, "c1") || strings.Contains(view, "contactID") || strings.Contains(view, "sp1") {
		t.Fatalf("view = %q, must not show raw entity keys", view)
	}
}
