package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/sneat-co/sneat-ai-backend/actionspec"
	"github.com/sneat-co/sneat-ai-backend/dbo4sneatai"
	"github.com/sneat-co/sneat-ai-backend/dto4sneatai"
	"github.com/sneat-co/sneat-cli/internal/actionstore"
	"github.com/sneat-co/sneat-cli/internal/config"
)

func TestActionAdd_InvalidJSON_Errors(t *testing.T) {
	env := actionsEnv(&fakeActionsAPI{}, t.TempDir())
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "add", "act_abc12345", "--json", `{not json`})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--json:") {
		t.Fatalf("err = %v, want a --json: wrapped error", err)
	}
}

func TestActionAdd_NewActionStoreError_Propagates(t *testing.T) {
	env := actionsEnvWithBrokenStore(t, &fakeActionsAPI{})
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "add", "act_abc12345", "--json", `{"kind":"buy"}`})
	if err := root.Execute(); err == nil {
		t.Fatal("expected an error when the action store cannot be opened")
	}
}

func TestActionAdd_LoadGenericError_Propagates(t *testing.T) {
	dir := t.TempDir()
	writeCorruptDraft(t, dir, "act_abc12345")
	env := actionsEnv(&fakeActionsAPI{}, dir)
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "add", "act_abc12345", "--json", `{"kind":"buy"}`})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, actionstore.ErrNotFound) {
		t.Fatalf("expected a generic load error, not ErrNotFound: %v", err)
	}
}

func TestActionAdd_NewActionsAPIError_Propagates(t *testing.T) {
	dir := t.TempDir()
	store := actionstoreFor(t, dir)
	_ = store.Save(actionstore.Draft{ActionID: "act_abc12345", SpaceID: "famID"})
	env := actionsEnv(&fakeActionsAPI{}, dir)
	env.NewActionsAPI = func(config.Config) (ActionsAPI, error) { return nil, errors.New("no api") }
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "add", "act_abc12345", "--json", `{"kind":"buy"}`})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when NewActionsAPI fails")
	}
}

func TestActionAdd_ServerHeld_PatchError_Propagates(t *testing.T) {
	dir := t.TempDir()
	store := actionstoreFor(t, dir)
	_ = store.Save(actionstore.Draft{ActionID: "act_srv000001", SpaceID: "famID", ServerHeld: true})
	api := &fakeActionsAPI{patchErr: errors.New("patch failed")}
	env := actionsEnv(api, dir)
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "add", "act_srv000001", "--json", `{"kind":"buy"}`})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error from ActionPatch")
	}
}

func TestActionAdd_ClientHeld_ValidateError_Propagates(t *testing.T) {
	dir := t.TempDir()
	store := actionstoreFor(t, dir)
	_ = store.Save(actionstore.Draft{ActionID: "act_abc12345", SpaceID: "famID"})
	api := &fakeActionsAPI{validateErr: errors.New("validate failed")}
	env := actionsEnv(api, dir)
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "add", "act_abc12345", "--json", `{"kind":"buy"}`})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error from ActionValidate")
	}
}

// TestActionAdd_ClientHeld_CommittedCarriesHappeningIDAndSetsFlags covers
// the previously-committed-draft branch (CommittedHappeningID is forwarded)
// together with the --utterance/--language draft-update branches.
func TestActionAdd_ClientHeld_CommittedCarriesHappeningIDAndSetsFlags(t *testing.T) {
	dir := t.TempDir()
	store := actionstoreFor(t, dir)
	base := actionspec.Semantic{Kind: actionspec.KindBuy}
	if err := store.Save(actionstore.Draft{
		ActionID: "act_abc12345", SpaceID: "famID", Semantic: base,
		Committed: &dbo4sneatai.CommitResult{HappeningID: "hap1"},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	api := &fakeActionsAPI{validateResp: dto4sneatai.ActionResponse{
		Semantic: base, Validation: canCommitValidation(),
	}}
	env := actionsEnv(api, dir)
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{
		"action", "add", "act_abc12345", "--json", `{"kind":"buy"}`,
		"--utterance", "buy it", "--language", "en",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if api.validateReq == nil || api.validateReq.CommittedHappeningID != "hap1" {
		t.Fatalf("validate req = %+v, want CommittedHappeningID hap1", api.validateReq)
	}
	got, err := store.Load("act_abc12345")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Utterance != "buy it" || got.Language != "en" {
		t.Fatalf("draft not updated with utterance/language: %+v", got)
	}
}

func TestActionAdd_ClientHeld_SaveError_Propagates(t *testing.T) {
	dir := t.TempDir()
	store := actionstoreFor(t, dir)
	base := actionspec.Semantic{Kind: actionspec.KindBuy}
	if err := store.Save(actionstore.Draft{ActionID: "act_abc12345", SpaceID: "famID", Semantic: base}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	api := &fakeActionsAPI{validateResp: dto4sneatai.ActionResponse{
		Semantic: base, Validation: canCommitValidation(),
	}}
	env := actionsEnv(api, dir)
	makeDraftFileReadOnly(t, dir, "act_abc12345")
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "add", "act_abc12345", "--json", `{"kind":"buy"}`})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when re-saving the draft fails")
	}
}
