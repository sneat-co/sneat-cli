package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/firestoredb"
	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/sneat-co/sneat-cli/internal/sneatauth"
)

type fakeContactsReader struct {
	list       []firestoredb.Contact
	one        firestoredb.Contact
	err        error
	gotSpace   string
	gotContact string
}

func (f *fakeContactsReader) ListContacts(_ context.Context, spaceID string) ([]firestoredb.Contact, error) {
	f.gotSpace = spaceID
	return f.list, f.err
}

func (f *fakeContactsReader) GetContact(_ context.Context, spaceID, contactID string) (firestoredb.Contact, error) {
	f.gotSpace, f.gotContact = spaceID, contactID
	return f.one, f.err
}

func contactsEnv(reader ContactsReader) Env {
	env := testEnv(&fakeStore{load: &session.Session{UID: "u1"}}, sneatauth.Result{})
	env.NewContactsReader = func(config.Config) (ContactsReader, error) { return reader, nil }
	env.NewSpacesReader = func(config.Config) (SpacesReader, error) {
		return &fakeSpacesReader{spaces: map[string]any{
			"famID":  map[string]any{"type": "family"},
			"privID": map[string]any{"type": "private"},
		}}, nil
	}
	return env
}

func TestContactList_PrintsContactsForSpace(t *testing.T) {
	reader := &fakeContactsReader{list: []firestoredb.Contact{{ID: "at"}}}
	env := contactsEnv(reader)
	root := Root(env)
	root.AddCommand(Contact(env))
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"contact", "list", "--space", "vaoyj", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if reader.gotSpace != "vaoyj" {
		t.Fatalf("space = %q", reader.gotSpace)
	}
	var got []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output not JSON: %v (%q)", err, buf.String())
	}
	if len(got) != 1 || got[0]["id"] != "at" {
		t.Fatalf("contacts = %+v", got)
	}
}

func TestContacts_AliasListsContacts(t *testing.T) {
	reader := &fakeContactsReader{list: []firestoredb.Contact{{ID: "at"}}}
	env := contactsEnv(reader)
	root := Root(env)
	root.AddCommand(Contacts(env))
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"contacts", "--space", "vaoyj"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if reader.gotSpace != "vaoyj" {
		t.Fatalf("space = %q", reader.gotSpace)
	}
}

func TestContactList_DefaultsToFamilySpace(t *testing.T) {
	reader := &fakeContactsReader{}
	env := contactsEnv(reader)
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "list"}) // no --space -> family
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if reader.gotSpace != "famID" {
		t.Fatalf("default space = %q, want famID", reader.gotSpace)
	}
}

func TestContactList_PseudoPrivateSpace(t *testing.T) {
	reader := &fakeContactsReader{}
	env := contactsEnv(reader)
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "list", "--space", "private"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if reader.gotSpace != "privID" {
		t.Fatalf("private space = %q, want privID", reader.gotSpace)
	}
}

func TestContactList_CurrentSpaceUsedWhenNoFlag(t *testing.T) {
	reader := &fakeContactsReader{}
	env := contactsEnv(reader)
	env.Store = &fakeStore{load: &session.Session{UID: "u1", CurrentSpace: "chosenID"}}
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "list"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if reader.gotSpace != "chosenID" {
		t.Fatalf("space = %q, want chosenID (current space)", reader.gotSpace)
	}
}

func TestContactGet_PrintsContact(t *testing.T) {
	reader := &fakeContactsReader{one: firestoredb.Contact{ID: "at"}}
	env := contactsEnv(reader)
	root := Root(env)
	root.AddCommand(Contact(env))
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"contact", "get", "--space", "vaoyj", "--id", "at"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if reader.gotContact != "at" {
		t.Fatalf("contactID = %q", reader.gotContact)
	}
}

func TestContactGet_Error(t *testing.T) {
	env := contactsEnv(&fakeContactsReader{err: errors.New("boom")})
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "get", "--space", "vaoyj", "--id", "nope"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected error")
	}
}
