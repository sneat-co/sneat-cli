package commands

import (
	"errors"
	"testing"

	"github.com/sneat-co/sneat-cli/internal/config"
)

func TestActionSchema_NewActionsAPIError_Propagates(t *testing.T) {
	env := actionsEnv(&fakeActionsAPI{}, t.TempDir())
	env.NewActionsAPI = func(config.Config) (ActionsAPI, error) { return nil, errors.New("no api") }
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "schema"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when NewActionsAPI fails")
	}
}

func TestActionSchema_APIError_Propagates(t *testing.T) {
	api := &fakeActionsAPI{schemaErr: errors.New("schema failed")}
	env := actionsEnv(api, t.TempDir())
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "schema"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error from Schema")
	}
}
