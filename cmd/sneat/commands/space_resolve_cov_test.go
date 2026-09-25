package commands

import (
	"errors"
	"testing"

	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/session"
)

// These drive resolveSpaceID/spaceIDByType through the `contact list`
// command (any resolveSpaceID caller works; contact list is the simplest),
// which also exercises that command's own error-forwarding branch.

func TestResolveSpaceID_StoreLoadError(t *testing.T) {
	env := contactsEnv(&fakeContactsReader{})
	env.Store = &fakeStore{} // no session -> Load fails
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "list"}) // no --space -> needs the session
	if err := root.Execute(); err == nil {
		t.Fatal("expected Store.Load error")
	}
}

func TestResolveSpaceID_SpacesReaderConstructorError(t *testing.T) {
	env := contactsEnv(&fakeContactsReader{})
	readerErr := errors.New("ctor boom")
	env.NewSpacesReader = func(config.Config) (SpacesReader, error) { return nil, readerErr }
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "list"}) // default space -> family -> needs reader
	if err := root.Execute(); !errors.Is(err, readerErr) {
		t.Fatalf("Execute error = %v, want %v", err, readerErr)
	}
}

func TestResolveSpaceID_ListSpacesError(t *testing.T) {
	env := contactsEnv(&fakeContactsReader{})
	listErr := errors.New("list boom")
	env.NewSpacesReader = func(config.Config) (SpacesReader, error) {
		return &fakeSpacesReader{err: listErr}, nil
	}
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "list"})
	if err := root.Execute(); !errors.Is(err, listErr) {
		t.Fatalf("Execute error = %v, want %v", err, listErr)
	}
}

func TestResolveSpaceID_NoMatchingSpace(t *testing.T) {
	env := contactsEnv(&fakeContactsReader{})
	env.NewSpacesReader = func(config.Config) (SpacesReader, error) {
		return &fakeSpacesReader{spaces: map[string]any{
			"famID": map[string]any{"type": "family"},
		}}, nil
	}
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "list", "--space", "private"}) // no private space exists
	err := root.Execute()
	if err == nil {
		t.Fatal("expected 'no private space found' error")
	}
	if got := err.Error(); got != "no private space found for the current user; pass --space <id>" {
		t.Fatalf("error = %q", got)
	}
}

func TestResolveSpaceID_MultipleMatchingSpaces(t *testing.T) {
	env := contactsEnv(&fakeContactsReader{})
	env.NewSpacesReader = func(config.Config) (SpacesReader, error) {
		return &fakeSpacesReader{spaces: map[string]any{
			"fam1": map[string]any{"type": "family"},
			"fam2": map[string]any{"type": "family"},
		}}, nil
	}
	env.Store = &fakeStore{load: &session.Session{UID: "u1"}} // no CurrentSpace -> defaults to family
	root := Root(env)
	root.AddCommand(Contact(env))
	root.SetArgs([]string{"contact", "list"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected 'multiple family spaces found' error")
	}
}
