package commands

import (
	"context"
	"testing"

	"github.com/sneat-co/contactus/backend/dto4contactus"
	"github.com/sneat-co/sneat-cli/internal/config"
)

type fakeContactWriter struct {
	createReq *dto4contactus.CreateContactRequest
	deleteReq *dto4contactus.ContactRequest
}

func (f *fakeContactWriter) CreateContact(_ context.Context, req dto4contactus.CreateContactRequest) (map[string]any, error) {
	f.createReq = &req
	return map[string]any{"contact": map[string]any{"id": "new"}}, nil
}

func (f *fakeContactWriter) DeleteContact(_ context.Context, req dto4contactus.ContactRequest) error {
	f.deleteReq = &req
	return nil
}

// writerEnv builds an Env with a contact writer, spaces resolver, terminal, and
// an optional interactive form.
func writerEnv(w *fakeContactWriter, isTTY bool, form func(*contactInput) error) Env {
	env := contactsEnv(&fakeContactsReader{})
	env.NewContactWriter = func(config.Config) (ContactWriter, error) { return w, nil }
	env.IsTerminal = func() bool { return isTTY }
	env.RunContactForm = form
	return env
}

func TestContactAdd_NonInteractive(t *testing.T) {
	w := &fakeContactWriter{}
	env := writerEnv(w, true, nil)
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "add", "--name", "Jane Doe", "--gender", "female", "--email", "j@x.com", "--role", "member"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	req := w.createReq
	if req == nil {
		t.Fatal("CreateContact not called")
	}
	if string(req.SpaceID) != "famID" {
		t.Fatalf("space = %q, want famID (default)", req.SpaceID)
	}
	if req.Person == nil || req.Person.Names == nil || req.Person.Names.FullName != "Jane Doe" {
		t.Fatalf("name not set: %+v", req.Person)
	}
	if string(req.Person.Gender) != "female" {
		t.Fatalf("gender = %q", req.Person.Gender)
	}
	if _, ok := req.Emails["j@x.com"]; !ok {
		t.Fatalf("email not set: %+v", req.Emails)
	}
	if len(req.Roles) != 1 || req.Roles[0] != "member" {
		t.Fatalf("roles = %+v", req.Roles)
	}
}

func TestContactAdd_InteractiveWhenNoFields(t *testing.T) {
	w := &fakeContactWriter{}
	form := func(in *contactInput) error { in.Name = "Formed Person"; return nil }
	env := writerEnv(w, true, form)
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "add"}) // no field flags -> form
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if w.createReq == nil || w.createReq.Person.Names.FullName != "Formed Person" {
		t.Fatalf("form values not used: %+v", w.createReq)
	}
}

func TestContactAdd_NonTTYNoFieldsErrors(t *testing.T) {
	env := writerEnv(&fakeContactWriter{}, false, func(*contactInput) error { return nil })
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "add"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error: no fields and not a terminal")
	}
}

func TestContactAdd_RequiresName(t *testing.T) {
	env := writerEnv(&fakeContactWriter{}, true, nil)
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "add", "--gender", "male"}) // a field, but no name
	if err := root.Execute(); err == nil {
		t.Fatal("expected error: name required")
	}
}

func TestContactDelete(t *testing.T) {
	w := &fakeContactWriter{}
	env := writerEnv(w, true, nil)
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "delete", "--space", "private", "--id", "c1"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if w.deleteReq == nil || w.deleteReq.ContactID != "c1" || string(w.deleteReq.SpaceID) != "privID" {
		t.Fatalf("delete req = %+v", w.deleteReq)
	}
}
