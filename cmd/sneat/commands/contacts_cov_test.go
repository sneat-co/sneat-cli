package commands

import (
	"errors"
	"testing"

	"github.com/sneat-co/contactus/backend/dbo4contactus"
	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/firestoredb"
	"github.com/sneat-co/sneat-ext-contracts/contactus/contactusmodels/briefs4contactus"
	"github.com/sneat-co/sneat-go-core/models/dbmodels"
)

func TestContactListCmd_ContactsReaderConstructorError(t *testing.T) {
	env := contactsEnv(&fakeContactsReader{})
	readerErr := errors.New("ctor boom")
	env.NewContactsReader = func(config.Config) (ContactsReader, error) { return nil, readerErr }
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "list", "--space", "vaoyj"}) // real id -> skip resolveSpaceID's reader
	if err := root.Execute(); !errors.Is(err, readerErr) {
		t.Fatalf("Execute error = %v, want %v", err, readerErr)
	}
}

func TestContactListCmd_ListContactsError(t *testing.T) {
	listErr := errors.New("list boom")
	env := contactsEnv(&fakeContactsReader{err: listErr})
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "list", "--space", "vaoyj"})
	if err := root.Execute(); !errors.Is(err, listErr) {
		t.Fatalf("Execute error = %v, want %v", err, listErr)
	}
}

func TestContactRow_NonNilContact(t *testing.T) {
	d := &dbo4contactus.ContactDbo{}
	d.Title = "Jane Doe"
	d.Type = briefs4contactus.ContactType("person")
	d.Gender = dbmodels.Gender("female")
	d.Status = dbmodels.Status("active")
	d.Roles = []string{"member", "admin"}
	row := contactRow(firestoredb.Contact{ID: "c1", Contact: d})
	want := []string{"c1", "Jane Doe", "person", "female", "active", "member,admin"}
	if len(row) != len(want) {
		t.Fatalf("row = %+v, want %+v", row, want)
	}
	for i := range want {
		if row[i] != want[i] {
			t.Fatalf("row[%d] = %q, want %q (row=%+v)", i, row[i], want[i], row)
		}
	}
}

func TestContactGet_ResolveSpaceIDError(t *testing.T) {
	env := contactsEnv(&fakeContactsReader{})
	env.Store = &fakeStore{} // no session -> Store.Load fails
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "get", "--id", "c1"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected resolveSpaceID error")
	}
}

func TestContactGet_ContactsReaderConstructorError(t *testing.T) {
	env := contactsEnv(&fakeContactsReader{})
	readerErr := errors.New("ctor boom")
	env.NewContactsReader = func(config.Config) (ContactsReader, error) { return nil, readerErr }
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "get", "--space", "vaoyj", "--id", "c1"})
	if err := root.Execute(); !errors.Is(err, readerErr) {
		t.Fatalf("Execute error = %v, want %v", err, readerErr)
	}
}
