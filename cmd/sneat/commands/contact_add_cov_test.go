package commands

import (
	"errors"
	"testing"

	"github.com/sneat-co/sneat-cli/internal/config"
)

func TestContactAdd_ResolveSpaceIDError(t *testing.T) {
	env := writerEnv(&fakeContactWriter{}, true, nil)
	env.Store = &fakeStore{} // no session -> Store.Load fails
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "add", "--name", "Jane Doe"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected resolveSpaceID error")
	}
}

func TestContactAdd_FormError(t *testing.T) {
	formErr := errors.New("form boom")
	env := writerEnv(&fakeContactWriter{}, true, func(*contactInput) error { return formErr })
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "add"}) // no fields -> interactive form
	if err := root.Execute(); !errors.Is(err, formErr) {
		t.Fatalf("Execute error = %v, want %v", err, formErr)
	}
}

func TestContactAdd_ContactWriterConstructorError(t *testing.T) {
	env := writerEnv(&fakeContactWriter{}, true, nil)
	writerErr := errors.New("ctor boom")
	env.NewContactWriter = func(config.Config) (ContactWriter, error) { return nil, writerErr }
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "add", "--name", "Jane Doe"})
	if err := root.Execute(); !errors.Is(err, writerErr) {
		t.Fatalf("Execute error = %v, want %v", err, writerErr)
	}
}

func TestContactAdd_CreateContactError(t *testing.T) {
	createErr := errors.New("create boom")
	env := writerEnv(&fakeContactWriter{}, true, nil)
	env.NewContactWriter = func(config.Config) (ContactWriter, error) {
		return &errContactWriter{createErr: createErr}, nil
	}
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "add", "--name", "Jane Doe"})
	if err := root.Execute(); !errors.Is(err, createErr) {
		t.Fatalf("Execute error = %v, want %v", err, createErr)
	}
}
