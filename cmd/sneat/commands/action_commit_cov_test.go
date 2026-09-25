package commands

import (
	"errors"
	"testing"

	"github.com/sneat-co/sneat-ai-backend/actionspec"
	"github.com/sneat-co/sneat-ai-backend/dto4sneatai"
	"github.com/sneat-co/sneat-cli/internal/actionstore"
	"github.com/sneat-co/sneat-cli/internal/config"
)

func TestActionCommit_NewActionStoreError_Propagates(t *testing.T) {
	env := actionsEnvWithBrokenStore(t, &fakeActionsAPI{})
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "commit", "act_abc12345"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected an error when the action store cannot be opened")
	}
}

func TestActionCommit_UnknownID_Errors(t *testing.T) {
	env := actionsEnv(&fakeActionsAPI{}, t.TempDir())
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "commit", "act_missing1"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected an error for an unknown action id")
	}
}

func TestActionCommit_LoadGenericError_Propagates(t *testing.T) {
	dir := t.TempDir()
	writeCorruptDraft(t, dir, "act_abc12345")
	env := actionsEnv(&fakeActionsAPI{}, dir)
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "commit", "act_abc12345"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, actionstore.ErrNotFound) {
		t.Fatalf("expected a generic load error, not ErrNotFound: %v", err)
	}
}

func TestActionCommit_NewActionsAPIError_Propagates(t *testing.T) {
	dir := t.TempDir()
	store := actionstoreFor(t, dir)
	_ = store.Save(actionstore.Draft{ActionID: "act_abc12345", SpaceID: "famID"})
	env := actionsEnv(&fakeActionsAPI{}, dir)
	env.NewActionsAPI = func(config.Config) (ActionsAPI, error) { return nil, errors.New("no api") }
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "commit", "act_abc12345"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when NewActionsAPI fails")
	}
}

// TestActionCommit_Refused_WriteJSONFails covers the inner error branch of
// the refusal handler: writeJSON failing while reporting the refusal.
func TestActionCommit_Refused_WriteJSONFails(t *testing.T) {
	dir := t.TempDir()
	store := actionstoreFor(t, dir)
	_ = store.Save(actionstore.Draft{ActionID: "act_abc12345", SpaceID: "famID"})
	refusal := errors.New("sneat api POST sneatai/action_commit: http 400: action cannot be committed: validation requires input")
	api := &fakeActionsAPI{commitErr: refusal}
	env := actionsEnv(api, dir)
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetOut(failWriter{})
	root.SetArgs([]string{"action", "commit", "act_abc12345"})
	err := root.Execute()
	var exitErr *ExitCodeError
	if errors.As(err, &exitErr) {
		t.Fatalf("expected the writeJSON failure to surface instead of ExitCodeError, got %v", err)
	}
	if err == nil || err.Error() != "write: boom" {
		t.Fatalf("err = %v, want the writeJSON failure", err)
	}
}

func TestActionCommit_ClientHeld_SaveError_Propagates(t *testing.T) {
	dir := t.TempDir()
	store := actionstoreFor(t, dir)
	dirty := actionspec.Semantic{Kind: actionspec.KindBuy}
	if err := store.Save(actionstore.Draft{ActionID: "act_abc12345", SpaceID: "famID", Semantic: dirty}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	api := &fakeActionsAPI{commitResp: dto4sneatai.CommitActionResponse{ActionID: "act_abc12345"}}
	env := actionsEnv(api, dir)
	makeDraftFileReadOnly(t, dir, "act_abc12345")
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "commit", "act_abc12345"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when storing the commit result fails")
	}
}
