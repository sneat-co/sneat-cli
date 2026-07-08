package commands

import (
	"testing"

	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/sneat-co/sneat-cli/internal/sneatauth"
)

// uiEnv builds an Env wired for the interactive UI with a capturing RunTUI.
func uiEnv(isTTY bool, gotUID *string, called *bool) Env {
	env := testEnv(&fakeStore{load: &session.Session{UID: "u1"}}, sneatauth.Result{})
	env.NewSpacesReader = func(config.Config) (SpacesReader, error) { return &fakeSpacesReader{}, nil }
	env.NewContactsReader = func(config.Config) (ContactsReader, error) { return &fakeContactsReader{}, nil }
	env.IsTerminal = func() bool { return isTTY }
	env.RunTUI = func(_ SpacesReader, _ ContactsReader, _ ContactDeleter, uid string) error {
		*called = true
		*gotUID = uid
		return nil
	}
	return env
}

func TestUiCommand_LaunchesTUI(t *testing.T) {
	var called bool
	var gotUID string
	env := uiEnv(true, &gotUID, &called)
	root := Root(env)
	root.AddCommand(Ui(env))
	root.SetArgs([]string{"ui"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !called || gotUID != "u1" {
		t.Fatalf("RunTUI called=%v uid=%q, want true/u1", called, gotUID)
	}
}

func TestSpacesUIFlag_LaunchesTUI(t *testing.T) {
	var called bool
	var gotUID string
	env := uiEnv(true, &gotUID, &called)
	root := Root(env)
	root.AddCommand(Spaces(env))
	root.SetArgs([]string{"spaces", "--ui"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !called || gotUID != "u1" {
		t.Fatalf("RunTUI called=%v uid=%q, want true/u1", called, gotUID)
	}
}

func TestSpaceListUIFlag_LaunchesTUI(t *testing.T) {
	var called bool
	var gotUID string
	env := uiEnv(true, &gotUID, &called)
	root := Root(env)
	root.AddCommand(Space(env))
	root.SetArgs([]string{"space", "list", "--ui"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !called {
		t.Fatal("space list --ui should launch the TUI")
	}
}

func TestUi_NonTerminalErrors(t *testing.T) {
	var called bool
	var gotUID string
	env := uiEnv(false, &gotUID, &called)
	root := Root(env)
	root.AddCommand(Ui(env))
	root.SetArgs([]string{"ui"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when not a terminal")
	}
	if called {
		t.Fatal("RunTUI must not be called without a terminal")
	}
}
