package commands

import (
	"errors"
	"testing"

	"github.com/sneat-co/sneat-cli/internal/config"
)

func TestQuery_ResolveSpaceIDError(t *testing.T) {
	env := actionsEnv(&fakeActionsAPI{}, t.TempDir())
	env.Store = &fakeStore{} // no session -> Store.Load fails
	root := Root(env)
	root.AddCommand(Query(env))
	root.SetArgs([]string{"query", "--json", `{"contacts":[]}`})
	if err := root.Execute(); err == nil {
		t.Fatal("expected resolveSpaceID error")
	}
}

func TestQuery_InvalidJSON(t *testing.T) {
	env := actionsEnv(&fakeActionsAPI{}, t.TempDir())
	root := Root(env)
	root.AddCommand(Query(env))
	root.SetArgs([]string{"query", "--space", "famID", "--json", "{not valid json"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected --json parse error")
	}
}

func TestQuery_ActionsAPIConstructorError(t *testing.T) {
	env := actionsEnv(&fakeActionsAPI{}, t.TempDir())
	apiErr := errors.New("ctor boom")
	env.NewActionsAPI = func(config.Config) (ActionsAPI, error) { return nil, apiErr }
	root := Root(env)
	root.AddCommand(Query(env))
	root.SetArgs([]string{"query", "--space", "famID", "--json", `{"contacts":[]}`})
	if err := root.Execute(); !errors.Is(err, apiErr) {
		t.Fatalf("Execute error = %v, want %v", err, apiErr)
	}
}

func TestQuery_APIQueryError(t *testing.T) {
	queryErr := errors.New("query boom")
	env := actionsEnv(&fakeActionsAPI{queryErr: queryErr}, t.TempDir())
	root := Root(env)
	root.AddCommand(Query(env))
	root.SetArgs([]string{"query", "--space", "famID", "--json", `{"contacts":[]}`})
	if err := root.Execute(); !errors.Is(err, queryErr) {
		t.Fatalf("Execute error = %v, want %v", err, queryErr)
	}
}
