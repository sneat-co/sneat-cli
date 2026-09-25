package commands

import (
	"errors"
	"testing"

	"github.com/sneat-co/sneat-cli/internal/actionstore"
	"github.com/sneat-co/sneat-cli/internal/config"
)

func TestActionCancel_NewActionStoreError_Propagates(t *testing.T) {
	env := actionsEnvWithBrokenStore(t, &fakeActionsAPI{})
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "cancel", "act_abc12345"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected an error when the action store cannot be opened")
	}
}

func TestActionCancel_UnknownID_Errors(t *testing.T) {
	env := actionsEnv(&fakeActionsAPI{}, t.TempDir())
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "cancel", "act_missing1"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected an error for an unknown action id")
	}
}

func TestActionCancel_LoadGenericError_Propagates(t *testing.T) {
	dir := t.TempDir()
	writeCorruptDraft(t, dir, "act_abc12345")
	env := actionsEnv(&fakeActionsAPI{}, dir)
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "cancel", "act_abc12345"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, actionstore.ErrNotFound) {
		t.Fatalf("expected a generic load error, not ErrNotFound: %v", err)
	}
}

func TestActionCancel_ServerHeld_NewActionsAPIError_Propagates(t *testing.T) {
	dir := t.TempDir()
	store := actionstoreFor(t, dir)
	_ = store.Save(actionstore.Draft{ActionID: "act_srv000001", SpaceID: "famID", ServerHeld: true})
	env := actionsEnv(&fakeActionsAPI{}, dir)
	env.NewActionsAPI = func(config.Config) (ActionsAPI, error) { return nil, errors.New("no api") }
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "cancel", "act_srv000001"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when NewActionsAPI fails")
	}
}

func TestActionCancel_ServerHeld_APIError_Propagates(t *testing.T) {
	dir := t.TempDir()
	store := actionstoreFor(t, dir)
	_ = store.Save(actionstore.Draft{ActionID: "act_srv000001", SpaceID: "famID", ServerHeld: true})
	api := &fakeActionsAPI{cancelErr: errors.New("cancel failed")}
	env := actionsEnv(api, dir)
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "cancel", "act_srv000001"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error from ActionCancel")
	}
}

func TestActionCancel_ServerHeld_DeleteError_Propagates(t *testing.T) {
	dir := t.TempDir()
	store := actionstoreFor(t, dir)
	_ = store.Save(actionstore.Draft{ActionID: "act_srv000001", SpaceID: "famID", ServerHeld: true})
	api := &fakeActionsAPI{}
	env := actionsEnv(api, dir)
	makeDraftsDirReadOnly(t, dir)
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "cancel", "act_srv000001"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when deleting the server-held index entry fails")
	}
}

func TestActionCancel_ClientHeld_DeleteError_Propagates(t *testing.T) {
	dir := t.TempDir()
	store := actionstoreFor(t, dir)
	_ = store.Save(actionstore.Draft{ActionID: "act_abc12345", SpaceID: "famID"})
	env := actionsEnv(&fakeActionsAPI{}, dir)
	makeDraftsDirReadOnly(t, dir)
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "cancel", "act_abc12345"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when deleting the client-held draft fails")
	}
}
