package commands

import (
	"errors"
	"testing"

	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/sneat-co/sneat-cli/internal/sneatauth"
)

// chatEnv builds an Env wired for the chat session with a capturing RunChat.
// A nil store session makes fakeStore.Load fail with session.ErrNoSession.
func chatEnv(isTTY bool, store SessionStore, gotUID *string, calls *int) Env {
	env := testEnv(store, sneatauth.Result{})
	env.NewSpacesReader = func(config.Config) (SpacesReader, error) { return &fakeSpacesReader{}, nil }
	env.IsTerminal = func() bool { return isTTY }
	env.RunChat = func(_ SpacesReader, uid string) error {
		*calls++
		*gotUID = uid
		return nil
	}
	return env
}

func TestChatCommand_LaunchesChat(t *testing.T) {
	var calls int
	var gotUID string
	env := chatEnv(true, &fakeStore{load: &session.Session{UID: "u1"}}, &gotUID, &calls)
	root := Root(env)
	root.AddCommand(Chat(env))
	root.SetArgs([]string{"chat"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if calls != 1 || gotUID != "u1" {
		t.Fatalf("RunChat calls=%d uid=%q, want 1/u1", calls, gotUID)
	}
}

func TestChat_NonTerminalErrors(t *testing.T) {
	var calls int
	var gotUID string
	env := chatEnv(false, &fakeStore{load: &session.Session{UID: "u1"}}, &gotUID, &calls)
	root := Root(env)
	root.AddCommand(Chat(env))
	root.SetArgs([]string{"chat"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when not a terminal")
	}
	if calls != 0 {
		t.Fatal("RunChat must not be called without a terminal")
	}
}

func TestChat_SessionStoreErrorAborts(t *testing.T) {
	var calls int
	var gotUID string
	env := chatEnv(true, &fakeStore{}, &gotUID, &calls) // no session => Load fails
	root := Root(env)
	root.AddCommand(Chat(env))
	root.SetArgs([]string{"chat"})
	err := root.Execute()
	if !errors.Is(err, session.ErrNoSession) {
		t.Fatalf("Execute error = %v, want %v", err, session.ErrNoSession)
	}
	if calls != 0 {
		t.Fatal("RunChat must not be called when the session store fails")
	}
}

func TestChat_SpacesReaderErrorAborts(t *testing.T) {
	var calls int
	var gotUID string
	env := chatEnv(true, &fakeStore{load: &session.Session{UID: "u1"}}, &gotUID, &calls)
	readerErr := errors.New("no reader")
	env.NewSpacesReader = func(config.Config) (SpacesReader, error) { return nil, readerErr }
	root := Root(env)
	root.AddCommand(Chat(env))
	root.SetArgs([]string{"chat"})
	if err := root.Execute(); !errors.Is(err, readerErr) {
		t.Fatalf("Execute error = %v, want %v", err, readerErr)
	}
	if calls != 0 {
		t.Fatal("RunChat must not be called when the spaces reader fails")
	}
}
