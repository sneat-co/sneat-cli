package commands

import (
	"errors"
	"testing"

	"github.com/sneat-co/sneat-cli/internal/config"
)

func TestContactDelete_ResolveSpaceIDError(t *testing.T) {
	env := writerEnv(&fakeContactWriter{}, true, nil)
	env.Store = &fakeStore{} // no session -> Store.Load fails
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "delete", "--id", "c1"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected resolveSpaceID error")
	}
}

func TestContactDelete_ContactWriterConstructorError(t *testing.T) {
	env := writerEnv(&fakeContactWriter{}, true, nil)
	writerErr := errors.New("ctor boom")
	env.NewContactWriter = func(config.Config) (ContactWriter, error) { return nil, writerErr }
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "delete", "--space", "private", "--id", "c1"})
	if err := root.Execute(); !errors.Is(err, writerErr) {
		t.Fatalf("Execute error = %v, want %v", err, writerErr)
	}
}

func TestContactDelete_DeleteContactError(t *testing.T) {
	deleteErr := errors.New("delete boom")
	env := writerEnv(&fakeContactWriter{}, true, nil)
	env.NewContactWriter = func(config.Config) (ContactWriter, error) {
		return &errContactWriter{deleteErr: deleteErr}, nil
	}
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "delete", "--space", "private", "--id", "c1"})
	if err := root.Execute(); !errors.Is(err, deleteErr) {
		t.Fatalf("Execute error = %v, want %v", err, deleteErr)
	}
}
