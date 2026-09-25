package commands

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sneat-co/sneat-ai-backend/actionspec"
	"github.com/sneat-co/sneat-ai-backend/dbo4sneatai"
	"github.com/sneat-co/sneat-ai-backend/semlint"
	"github.com/sneat-co/sneat-cli/internal/actionstore"
)

func TestActionList_NewActionStoreError_Propagates(t *testing.T) {
	env := actionsEnvWithBrokenStore(t, &fakeActionsAPI{})
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "list"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected an error when the action store cannot be opened")
	}
}

func TestActionList_StoreListError_Propagates(t *testing.T) {
	dir := t.TempDir()
	store := actionstoreFor(t, dir)
	// Create the drafts directory first so there is something to chmod.
	_ = store.Save(actionstore.Draft{ActionID: "act_abc12345", SpaceID: "famID"})
	env := actionsEnv(&fakeActionsAPI{}, dir)
	makeDraftsDirUnreadable(t, dir)
	root := Root(env)
	root.AddCommand(Action(env))
	root.SetArgs([]string{"action", "list"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when the drafts directory cannot be read")
	}
}

// TestActionListRow_AllStatusBranches drives `action list` over drafts that
// hit every actionListRow status branch: cancelled, committed, server-held,
// validated (CanCommit, not yet committed/serverHeld), and the plain-draft
// default.
func TestActionListRow_AllStatusBranches(t *testing.T) {
	dir := t.TempDir()
	store := actionstoreFor(t, dir)
	now := time.Unix(1000, 0)
	drafts := []actionstore.Draft{
		{ActionID: "act_cancel001", SpaceID: "famID", Semantic: actionspec.Semantic{Kind: actionspec.KindBuy}, Cancelled: true, UpdatedAt: now},
		{ActionID: "act_commit001", SpaceID: "famID", Semantic: actionspec.Semantic{Kind: actionspec.KindBuy}, Committed: &dbo4sneatai.CommitResult{HappeningID: "hap1"}, UpdatedAt: now},
		{ActionID: "act_server001", SpaceID: "famID", ServerHeld: true, UpdatedAt: now},
		{ActionID: "act_valid0001", SpaceID: "famID", Semantic: actionspec.Semantic{Kind: actionspec.KindBuy}, Validation: semlint.Validation{CanCommit: true}, UpdatedAt: now},
		{ActionID: "act_draft0001", SpaceID: "famID", Semantic: actionspec.Semantic{Kind: actionspec.KindBuy}, UpdatedAt: now},
	}
	for _, d := range drafts {
		if err := store.Save(d); err != nil {
			t.Fatalf("Save(%s): %v", d.ActionID, err)
		}
	}
	env := actionsEnv(&fakeActionsAPI{}, dir)
	root := Root(env)
	root.AddCommand(Action(env))
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"action", "list", "--table"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"cancelled", "committed", "server-held", "validated", "draft"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table output missing status %q:\n%s", want, out)
		}
	}

	// Also confirm the JSON path still round-trips all 5 drafts (sanity,
	// not a duplicate of the table-format assertions above).
	var buf2 bytes.Buffer
	root2 := Root(env)
	root2.AddCommand(Action(env))
	root2.SetOut(&buf2)
	root2.SetArgs([]string{"action", "list", "--json"})
	if err := root2.Execute(); err != nil {
		t.Fatalf("Execute (json): %v", err)
	}
	var got []actionstore.Draft
	if err := json.Unmarshal(buf2.Bytes(), &got); err != nil {
		t.Fatalf("output not JSON: %v (%s)", err, buf2.String())
	}
	if len(got) != 5 {
		t.Fatalf("got %d drafts, want 5", len(got))
	}
}
