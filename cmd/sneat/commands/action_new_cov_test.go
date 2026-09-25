package commands

import (
	"errors"
	"testing"

	"github.com/sneat-co/sneat-ai-backend/actionspec"
	"github.com/sneat-co/sneat-ai-backend/dto4sneatai"
	"github.com/sneat-co/sneat-cli/internal/actionstore"
	"github.com/sneat-co/sneat-cli/internal/config"
)

// TestActionNew_ResolveSpaceIDError_Propagates covers actionNewCmd's very
// first error branch: no --space given and env.Store.Load() failing (empty
// session store, no current space to fall back to).
func TestActionNew_ResolveSpaceIDError_Propagates(t *testing.T) {
	dir := t.TempDir()
	env := actionsEnv(&fakeActionsAPI{}, dir)
	env.Store = &fakeStore{} // Load() -> session.ErrNoSession
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "new", "--json", semanticJSON(actionspec.KindBuy)})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when resolveSpaceID fails")
	}
}

func TestActionNew_InvalidJSON_Errors(t *testing.T) {
	env := actionsEnv(&fakeActionsAPI{}, t.TempDir())
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "new", "--json", `{not json`, "--space", "famID"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error for invalid --json")
	}
}

func TestActionNew_NewActionsAPIError_Propagates(t *testing.T) {
	env := actionsEnv(&fakeActionsAPI{}, t.TempDir())
	env.NewActionsAPI = func(config.Config) (ActionsAPI, error) { return nil, errors.New("no api") }
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "new", "--json", semanticJSON(actionspec.KindBuy), "--space", "famID"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when NewActionsAPI fails")
	}
}

func TestActionNew_Server_ActionCreateError_Propagates(t *testing.T) {
	api := &fakeActionsAPI{createErr: errors.New("create failed")}
	env := actionsEnv(api, t.TempDir())
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "new", "--json", semanticJSON(actionspec.KindBuy), "--space", "famID", "--server"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error from ActionCreate")
	}
}

func TestActionNew_Server_NewActionStoreError_Propagates(t *testing.T) {
	api := &fakeActionsAPI{createResp: dto4sneatai.ActionResponse{ActionID: "act_srvabc12"}}
	env := actionsEnvWithBrokenStore(t, api)
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "new", "--json", semanticJSON(actionspec.KindBuy), "--space", "famID", "--server"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when the action store cannot be opened")
	}
}

func TestActionNew_Server_SaveError_Propagates(t *testing.T) {
	dir := t.TempDir()
	// Pre-create the drafts directory (via an unrelated draft) before
	// making it read-only, so newActionStore/Save's MkdirAll is a no-op and
	// the WriteFile for the brand-new "act_srvabc12" file fails instead.
	store := actionstoreFor(t, dir)
	_ = store.Save(actionstore.Draft{ActionID: "act_other0001", SpaceID: "famID"})
	api := &fakeActionsAPI{createResp: dto4sneatai.ActionResponse{ActionID: "act_srvabc12"}}
	env := actionsEnv(api, dir)
	makeDraftsDirReadOnly(t, dir)
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "new", "--json", semanticJSON(actionspec.KindBuy), "--space", "famID", "--server"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when saving the server-held index entry fails")
	}
}

func TestActionNew_ClientHeld_NewActionIDError_Propagates(t *testing.T) {
	orig := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("rand: boom") }
	t.Cleanup(func() { randRead = orig })

	env := actionsEnv(&fakeActionsAPI{}, t.TempDir())
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "new", "--json", semanticJSON(actionspec.KindBuy), "--space", "famID"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when newActionID fails")
	}
}

func TestActionNew_ClientHeld_ValidateError_Propagates(t *testing.T) {
	api := &fakeActionsAPI{validateErr: errors.New("validate failed")}
	env := actionsEnv(api, t.TempDir())
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "new", "--json", semanticJSON(actionspec.KindBuy), "--space", "famID"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error from ActionValidate")
	}
}

func TestActionNew_ClientHeld_NewActionStoreError_Propagates(t *testing.T) {
	api := &fakeActionsAPI{validateResp: dto4sneatai.ActionResponse{
		Semantic: actionspec.Semantic{Kind: actionspec.KindBuy},
	}}
	env := actionsEnvWithBrokenStore(t, api)
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "new", "--json", semanticJSON(actionspec.KindBuy), "--space", "famID"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when the action store cannot be opened")
	}
}

func TestActionNew_ClientHeld_SaveError_Propagates(t *testing.T) {
	dir := t.TempDir()
	store := actionstoreFor(t, dir)
	_ = store.Save(actionstore.Draft{ActionID: "act_other0001", SpaceID: "famID"})
	api := &fakeActionsAPI{validateResp: dto4sneatai.ActionResponse{
		Semantic: actionspec.Semantic{Kind: actionspec.KindBuy},
	}}
	env := actionsEnv(api, dir)
	makeDraftsDirReadOnly(t, dir)
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "new", "--json", semanticJSON(actionspec.KindBuy), "--space", "famID"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when saving the new draft fails")
	}
}
