package firestoredb

import (
	"testing"

	"github.com/sneat-co/sneat-cli/internal/config"
)

// TestContactsCollectionRef_PointsAtSpaceContactusContacts is a pure,
// no-I/O regression for the path this reader queries against
// (contactsCollectionRef's own doc comment): spaces/{spaceID}/ext/
// contactus/contacts.
func TestContactsCollectionRef_PointsAtSpaceContactusContacts(t *testing.T) {
	ref := contactsCollectionRef("sp1")
	want := "spaces/sp1/ext/contactus/contacts"
	if got := ref.Path(); got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
}

// TestNewContactsReader_WrapsALazySession is m4/m6: NewContactsReader (kept
// for cmd/sneat/main.go's own single-reader use -- see its own doc comment)
// must build its reader over a Session rather than open a client eagerly,
// so constructing it and closing it without ever reading from it is a safe
// no-op, the same contract TestSession_Close_NoOpWhenNeverOpened already
// covers for Session itself.
func TestNewContactsReader_WrapsALazySession(t *testing.T) {
	r := NewContactsReader(config.Config{Project: "p1"}, nil)
	if r == nil {
		t.Fatal("NewContactsReader returned nil")
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close() on a never-read reader = %v, want nil (lazy open, nothing to close)", err)
	}
}

// TestNewContactsReaderFromSession_SharesTheGivenSession is m6: two readers
// built from the SAME Session must forward Close() to that one shared
// Session (idempotent), not each open/close their own.
func TestNewContactsReaderFromSession_SharesTheGivenSession(t *testing.T) {
	session := NewSession(config.Config{Project: "p1"}, nil)
	a := NewContactsReaderFromSession(session)
	b := NewContactsReaderFromSession(session)
	if a.session != session || b.session != session {
		t.Fatalf("a.session=%p b.session=%p, want both to be the SAME shared session %p", a.session, b.session, session)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("a.Close() = %v, want nil", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("b.Close() (session already closed by a) = %v, want nil (idempotent)", err)
	}
}
