package firestoredb

import (
	"context"
	"errors"
	"testing"

	"github.com/dal-go/dalgo/dal"
	"github.com/dal-go/record"
	"github.com/sneat-co/contactus/backend/dbo4contactus"
	"github.com/sneat-co/sneat-cli/internal/config"
	"golang.org/x/oauth2"
)

// TestNewContactRecord builds an incomplete-key envelope with the ContactDbo
// shape ListContacts' query decodes into.
func TestNewContactRecord(t *testing.T) {
	rec := newContactRecord()
	if rec == nil {
		t.Fatal("newContactRecord() = nil")
	}
	if _, ok := rec.Data().(*dbo4contactus.ContactDbo); !ok {
		t.Fatalf("Data() = %T, want *dbo4contactus.ContactDbo", rec.Data())
	}
}

// sessionWithFakeConn builds a Session whose Session.DB(ctx) hands back the
// given fake connection/runner via the newFirestoreConn seam, restoring the
// real seam when the test ends.
func sessionWithFakeConn(t *testing.T, runner *fakeRunner) *Session {
	t.Helper()
	orig := newFirestoreConn
	newFirestoreConn = func(ctx context.Context, cfg config.Config, ts oauth2.TokenSource) (firestoreConn, readOnlyRunner, error) {
		return &fakeConn{}, runner, nil
	}
	t.Cleanup(func() { newFirestoreConn = orig })
	return NewSession(config.Config{Project: "p1"}, nil)
}

// TestListContacts_ReturnsActiveContacts drives ListContacts end to end
// through the newFirestoreConn seam and a fake query reader.
func TestListContacts_ReturnsActiveContacts(t *testing.T) {
	key := record.NewKeyWithParentAndID(
		record.NewKeyWithParentAndID(record.NewKeyWithID("spaces", "sp1"), "ext", "contactus"),
		"contacts", "c1",
	)
	rec := record.NewRecordWithData(key, &dbo4contactus.ContactDbo{})
	tx := &fakeReadTransaction{queryReaderFn: func(ctx context.Context, query dal.Query) (dal.RecordsReader, error) {
		return dal.NewRecordsReader([]record.Record{rec}), nil
	}}
	session := sessionWithFakeConn(t, &fakeRunner{tx: tx})

	contacts, err := NewContactsReaderFromSession(session).ListContacts(context.Background(), "sp1")
	if err != nil {
		t.Fatalf("ListContacts() = %v, want nil", err)
	}
	if len(contacts) != 1 || contacts[0].ID != "c1" {
		t.Fatalf("contacts = %+v", contacts)
	}
}

// TestListContacts_SessionError propagates a failed Session.DB open.
func TestListContacts_SessionError(t *testing.T) {
	wantErr := errors.New("open failed")
	orig := newFirestoreConn
	newFirestoreConn = func(ctx context.Context, cfg config.Config, ts oauth2.TokenSource) (firestoreConn, readOnlyRunner, error) {
		return nil, nil, wantErr
	}
	t.Cleanup(func() { newFirestoreConn = orig })
	session := NewSession(config.Config{Project: "p1"}, nil)

	_, err := NewContactsReaderFromSession(session).ListContacts(context.Background(), "sp1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("ListContacts() = %v, want %v", err, wantErr)
	}
}

// TestListContacts_QueryError propagates a transaction/query failure.
func TestListContacts_QueryError(t *testing.T) {
	wantErr := errors.New("query failed")
	session := sessionWithFakeConn(t, &fakeRunner{runErr: wantErr})

	_, err := NewContactsReaderFromSession(session).ListContacts(context.Background(), "sp1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("ListContacts() = %v, want %v", err, wantErr)
	}
}

// TestGetContact covers found, not-found, session-error and read-error.
func TestGetContact(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		tx := &fakeReadTransaction{getFn: func(ctx context.Context, rec record.Record) error {
			rec.SetError(nil)
			return nil
		}}
		session := sessionWithFakeConn(t, &fakeRunner{tx: tx})
		got, err := NewContactsReaderFromSession(session).GetContact(context.Background(), "sp1", "c1")
		if err != nil {
			t.Fatalf("GetContact() = %v, want nil", err)
		}
		if got.ID != "c1" || got.Contact == nil {
			t.Fatalf("got = %+v", got)
		}
	})

	t.Run("not found", func(t *testing.T) {
		tx := &fakeReadTransaction{getFn: func(ctx context.Context, rec record.Record) error {
			rec.SetError(record.ErrRecordNotFound)
			return nil
		}}
		session := sessionWithFakeConn(t, &fakeRunner{tx: tx})
		_, err := NewContactsReaderFromSession(session).GetContact(context.Background(), "sp1", "c1")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("GetContact() = %v, want %v", err, ErrNotFound)
		}
	})

	t.Run("session error", func(t *testing.T) {
		wantErr := errors.New("open failed")
		orig := newFirestoreConn
		newFirestoreConn = func(ctx context.Context, cfg config.Config, ts oauth2.TokenSource) (firestoreConn, readOnlyRunner, error) {
			return nil, nil, wantErr
		}
		t.Cleanup(func() { newFirestoreConn = orig })
		session := NewSession(config.Config{Project: "p1"}, nil)

		_, err := NewContactsReaderFromSession(session).GetContact(context.Background(), "sp1", "c1")
		if !errors.Is(err, wantErr) {
			t.Fatalf("GetContact() = %v, want %v", err, wantErr)
		}
	})

	t.Run("read error", func(t *testing.T) {
		wantErr := errors.New("read failed")
		session := sessionWithFakeConn(t, &fakeRunner{runErr: wantErr})
		_, err := NewContactsReaderFromSession(session).GetContact(context.Background(), "sp1", "c1")
		if !errors.Is(err, wantErr) {
			t.Fatalf("GetContact() = %v, want %v", err, wantErr)
		}
	})
}

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
