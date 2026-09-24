package data

import (
	"context"
	"errors"
	"testing"

	"github.com/sneat-co/contactus/backend/dbo4contactus"
	"github.com/sneat-co/sneat-cli/internal/firestoredb"
)

// TestFirestoreContacts_List drives List/FindByName through the
// contactsReader seam.
func TestFirestoreContacts_List(t *testing.T) {
	fake := &fakeContactsReader{listFn: func(ctx context.Context, spaceID string) ([]firestoredb.Contact, error) {
		return []firestoredb.Contact{
			{ID: "c1", Contact: &dbo4contactus.ContactDbo{Title: "Alice"}},
			{ID: "c2", Contact: &dbo4contactus.ContactDbo{Title: "Bob"}},
		}, nil
	}}
	fc := &firestoreContacts{r: fake}

	got, err := fc.List(context.Background(), "sp1")
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	if len(got) != 2 || got[0].Name != "Alice" {
		t.Fatalf("got = %+v", got)
	}
}

func TestFirestoreContacts_List_Error(t *testing.T) {
	wantErr := errors.New("list failed")
	fake := &fakeContactsReader{listFn: func(ctx context.Context, spaceID string) ([]firestoredb.Contact, error) {
		return nil, wantErr
	}}
	fc := &firestoreContacts{r: fake}
	if _, err := fc.List(context.Background(), "sp1"); !errors.Is(err, wantErr) {
		t.Fatalf("List() = %v, want %v", err, wantErr)
	}
	if _, err := fc.FindByName(context.Background(), "sp1", "a"); !errors.Is(err, wantErr) {
		t.Fatalf("FindByName() = %v, want %v", err, wantErr)
	}
}

func TestFirestoreContacts_FindByName(t *testing.T) {
	fake := &fakeContactsReader{listFn: func(ctx context.Context, spaceID string) ([]firestoredb.Contact, error) {
		return []firestoredb.Contact{
			{ID: "c1", Contact: &dbo4contactus.ContactDbo{Title: "Alice"}},
			{ID: "c2", Contact: &dbo4contactus.ContactDbo{Title: "Bob"}},
		}, nil
	}}
	fc := &firestoreContacts{r: fake}

	got, err := fc.FindByName(context.Background(), "sp1", "ali")
	if err != nil {
		t.Fatalf("FindByName() = %v, want nil", err)
	}
	if len(got) != 1 || got[0].ID != "c1" {
		t.Fatalf("got = %+v", got)
	}
}

func TestFirestoreContacts_Get(t *testing.T) {
	fake := &fakeContactsReader{getFn: func(ctx context.Context, spaceID, contactID string) (firestoredb.Contact, error) {
		return firestoredb.Contact{ID: contactID, Contact: &dbo4contactus.ContactDbo{Title: "Alice"}}, nil
	}}
	fc := &firestoreContacts{r: fake}

	got, err := fc.Get(context.Background(), "sp1", "c1")
	if err != nil {
		t.Fatalf("Get() = %v, want nil", err)
	}
	if got.Name != "Alice" {
		t.Fatalf("got = %+v", got)
	}

	wantErr := errors.New("get failed")
	fake2 := &fakeContactsReader{getFn: func(ctx context.Context, spaceID, contactID string) (firestoredb.Contact, error) {
		return firestoredb.Contact{}, wantErr
	}}
	fc2 := &firestoreContacts{r: fake2}
	if _, err := fc2.Get(context.Background(), "sp1", "c1"); !errors.Is(err, wantErr) {
		t.Fatalf("Get() = %v, want %v", err, wantErr)
	}
}

func TestFirestoreContacts_Close(t *testing.T) {
	fake := &fakeContactsReader{}
	fc := &firestoreContacts{r: fake}
	if err := fc.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
	if fake.closed != 1 {
		t.Fatalf("closed = %d, want 1", fake.closed)
	}
}
