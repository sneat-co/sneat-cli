package commands

import (
	"errors"
	"testing"

	"github.com/sneat-co/sneat-ai-backend/actionspec"
	"github.com/sneat-co/sneat-ai-backend/dbo4sneatai"
	"github.com/sneat-co/sneat-ai-backend/dto4sneatai"
	"github.com/sneat-co/sneat-cli/internal/actionstore"
	"github.com/sneat-co/sneat-cli/internal/config"
)

func TestActionValidate_NewActionStoreError_Propagates(t *testing.T) {
	env := actionsEnvWithBrokenStore(t, &fakeActionsAPI{})
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "validate", "act_abc12345"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected an error when the action store cannot be opened")
	}
}

func TestActionValidate_UnknownID_Errors(t *testing.T) {
	env := actionsEnv(&fakeActionsAPI{}, t.TempDir())
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "validate", "act_missing1"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected an error for an unknown action id")
	}
}

func TestActionValidate_LoadGenericError_Propagates(t *testing.T) {
	dir := t.TempDir()
	writeCorruptDraft(t, dir, "act_abc12345")
	env := actionsEnv(&fakeActionsAPI{}, dir)
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "validate", "act_abc12345"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, actionstore.ErrNotFound) {
		t.Fatalf("expected a generic load error, not ErrNotFound: %v", err)
	}
}

func TestActionValidate_NewActionsAPIError_Propagates(t *testing.T) {
	dir := t.TempDir()
	store := actionstoreFor(t, dir)
	_ = store.Save(actionstore.Draft{ActionID: "act_abc12345", SpaceID: "famID"})
	env := actionsEnv(&fakeActionsAPI{}, dir)
	env.NewActionsAPI = func(config.Config) (ActionsAPI, error) { return nil, errors.New("no api") }
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "validate", "act_abc12345"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when NewActionsAPI fails")
	}
}

// TestActionValidate_ClientHeld_CommittedCarriesHappeningID covers the
// draft.Committed != nil branch that forwards CommittedHappeningID.
func TestActionValidate_ClientHeld_CommittedCarriesHappeningID(t *testing.T) {
	dir := t.TempDir()
	store := actionstoreFor(t, dir)
	if err := store.Save(actionstore.Draft{
		ActionID: "act_abc12345", SpaceID: "famID",
		Semantic:  actionspec.Semantic{Kind: actionspec.KindBuy},
		Committed: &dbo4sneatai.CommitResult{HappeningID: "hap1"},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	api := &fakeActionsAPI{validateResp: dto4sneatai.ActionResponse{
		Semantic: actionspec.Semantic{Kind: actionspec.KindBuy}, Validation: canCommitValidation(),
	}}
	env := actionsEnv(api, dir)
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "validate", "act_abc12345"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if api.validateReq == nil || api.validateReq.CommittedHappeningID != "hap1" {
		t.Fatalf("validate req = %+v, want CommittedHappeningID hap1", api.validateReq)
	}
}

func TestActionValidate_ClientHeld_ValidateError_Propagates(t *testing.T) {
	dir := t.TempDir()
	store := actionstoreFor(t, dir)
	_ = store.Save(actionstore.Draft{ActionID: "act_abc12345", SpaceID: "famID", Semantic: actionspec.Semantic{Kind: actionspec.KindBuy}})
	api := &fakeActionsAPI{validateErr: errors.New("validate failed")}
	env := actionsEnv(api, dir)
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "validate", "act_abc12345"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error from ActionValidate")
	}
}

func TestActionValidate_ClientHeld_SaveError_Propagates(t *testing.T) {
	dir := t.TempDir()
	store := actionstoreFor(t, dir)
	_ = store.Save(actionstore.Draft{ActionID: "act_abc12345", SpaceID: "famID", Semantic: actionspec.Semantic{Kind: actionspec.KindBuy}})
	api := &fakeActionsAPI{validateResp: dto4sneatai.ActionResponse{
		Semantic: actionspec.Semantic{Kind: actionspec.KindBuy}, Validation: canCommitValidation(),
	}}
	env := actionsEnv(api, dir)
	makeDraftFileReadOnly(t, dir, "act_abc12345")
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "validate", "act_abc12345"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when re-saving the draft fails")
	}
}
