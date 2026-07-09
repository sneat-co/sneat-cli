package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sneat-co/sneat-cli/internal/sneatauth"
)

// buildConvoCmd creates a root cobra command with the convo subtree wired.
func buildConvoCmd(t *testing.T) (*bytes.Buffer, func(args ...string) error) {
	t.Helper()
	env := testEnv(&fakeStore{}, sneatauth.Result{})
	root := Root(env)
	root.AddCommand(Convo(env))
	var buf bytes.Buffer
	root.SetOut(&buf)
	return &buf, func(args ...string) error {
		buf.Reset()
		root.SetArgs(args)
		return root.Execute()
	}
}

func TestConvoActions_JSON_ContainsExpectedActions(t *testing.T) {
	buf, exec := buildConvoCmd(t)
	if err := exec("convo", "actions", "--json"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "lists.add_items") {
		t.Errorf("output missing lists.add_items: %s", out)
	}
	if !strings.Contains(out, "contacts.create") {
		t.Errorf("output missing contacts.create: %s", out)
	}
	// Must be valid JSON
	var v any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
}

func TestConvoActions_Table_PrintsRows(t *testing.T) {
	buf, exec := buildConvoCmd(t)
	if err := exec("convo", "actions"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "lists.add_items") {
		t.Errorf("table missing lists.add_items: %s", out)
	}
}

func TestConvoSay_BuyMilk_JSON(t *testing.T) {
	buf, exec := buildConvoCmd(t)
	if err := exec("convo", "say", "buy milk", "--json"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	// Must contain the action ID and a summary mentioning the item
	if !strings.Contains(out, "lists.add_items") {
		t.Errorf("output missing lists.add_items: %s", out)
	}
	if !strings.Contains(strings.ToLower(out), "milk") {
		t.Errorf("output missing milk: %s", out)
	}
	var turns []map[string]any
	if err := json.Unmarshal([]byte(out), &turns); err != nil {
		t.Fatalf("not valid JSON array: %v\n%s", err, out)
	}
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
}

func TestConvoSay_DeleteContactBob_NoPending_WithoutYes(t *testing.T) {
	buf, exec := buildConvoCmd(t)
	// No contacts named Bob in sandbox — should get clarification (no match)
	if err := exec("convo", "say", "delete contact Bob"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	// Must NOT execute (no "Deleted" in output) but report pending or clarification
	if strings.Contains(out, "Deleted") {
		t.Errorf("should not have executed deletion: %s", out)
	}
}

func TestConvoSay_MultiTurn_AddThenListContacts(t *testing.T) {
	buf, exec := buildConvoCmd(t)
	// Two messages in one say invocation share sandbox state
	if err := exec("convo", "say", "add contact Jane Doe", "list my contacts", "--json"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Jane Doe") {
		t.Errorf("Jane Doe not found in output: %s", out)
	}
	var turns []map[string]any
	if err := json.Unmarshal([]byte(out), &turns); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	if len(turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(turns))
	}
}

func TestConvoSay_MeetSarah_Yes_Resolves(t *testing.T) {
	buf, exec := buildConvoCmd(t)
	if err := exec("convo", "say", "meet Sarah tomorrow at 3pm", "--yes", "--json"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	// With --yes, pending should be auto-resolved; response should contain scheduling info
	lout := strings.ToLower(out)
	if !strings.Contains(lout, "scheduled") && !strings.Contains(lout, "meet sarah") && !strings.Contains(lout, "sarah") {
		t.Errorf("expected scheduling confirmation in output: %s", out)
	}
}

func TestConvoReplay_BuyAndRemoveMilk(t *testing.T) {
	// Write a temp script file
	script := "buy milk\nremove milk from the shopping list\nyes\n"
	f, err := os.CreateTemp("", "convo-replay-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(script); err != nil {
		t.Fatal(err)
	}
	f.Close()

	buf, exec := buildConvoCmd(t)
	if err := exec("convo", "replay", f.Name(), "--json"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(strings.ToLower(out), "removed") {
		t.Errorf("expected 'Removed' in final output: %s", out)
	}
	var turns []map[string]any
	if err := json.Unmarshal([]byte(out), &turns); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
}
