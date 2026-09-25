package commands

import (
	"errors"
	"testing"

	"github.com/sneat-co/sneat-cli/internal/config"
)

func TestContext_ResolveSpaceIDError(t *testing.T) {
	env := actionsEnv(&fakeActionsAPI{}, t.TempDir())
	env.Store = &fakeStore{} // no session -> Store.Load fails
	root := Root(env)
	root.AddCommand(Context(env))
	root.SetArgs([]string{"context"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected resolveSpaceID error")
	}
}

func TestContext_ActionsAPIConstructorError(t *testing.T) {
	env := actionsEnv(&fakeActionsAPI{}, t.TempDir())
	apiErr := errors.New("ctor boom")
	env.NewActionsAPI = func(config.Config) (ActionsAPI, error) { return nil, apiErr }
	root := Root(env)
	root.AddCommand(Context(env))
	root.SetArgs([]string{"context", "--space", "famID"})
	if err := root.Execute(); !errors.Is(err, apiErr) {
		t.Fatalf("Execute error = %v, want %v", err, apiErr)
	}
}

func TestContext_APIContextError(t *testing.T) {
	ctxErr := errors.New("context boom")
	env := actionsEnv(&fakeActionsAPI{contextErr: ctxErr}, t.TempDir())
	root := Root(env)
	root.AddCommand(Context(env))
	root.SetArgs([]string{"context", "--space", "famID"})
	if err := root.Execute(); !errors.Is(err, ctxErr) {
		t.Fatalf("Execute error = %v, want %v", err, ctxErr)
	}
}
