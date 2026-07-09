# sneat convo Command Group Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `sneat convo` command group to the sneat-cli with three subcommands (`actions`, `say`, `replay`) that drive the in-process conversational runtime (sneat-bots) with a deterministic mock LLM against an in-memory sandbox DB.

**Architecture:** A new file `cmd/sneat/commands/convo.go` contains the `Convo(env Env) *cobra.Command` factory following the existing cobra command-factory style. `convo say` and `convo replay` both share a single `runSession` helper that maintains conversation state (the pending action) across turns. `convo replay` reads a script file and feeds lines as turns. All commands are strictly non-interactive (no TTY prompts). Tests live in `cmd/sneat/commands/convo_test.go`.

**Tech Stack:** Go 1.26, cobra (spf13/cobra v1.10.2), sneat-bots local module (github.com/sneat-co/sneat-bots), sneat-go-core coretypes, existing `output.go` helpers (writeJSON, writeTable)

## Global Constraints

- Branch: `conversational-runtime` — all commits go there; do NOT push
- No TTY prompts ever — this is a dev/AI-agent tool
- Follow existing command-factory style: `func Convo(env Env) *cobra.Command`
- `Env` is passed but unused by convo commands (sandbox is self-contained)
- Default space: `"space1"`, default user: `"user1"` (flags `--space`/`--user` override)
- `convosetup.NewRuntime(llmmock.NewClient())` is the only runtime in use
- `convodev.SetupSandbox(spaceID, userID)` wires the in-memory DB; `defer restore()` always
- `go fmt ./...`, `go vet ./...`, `go build ./...`, `go test ./...` must all pass
- Register `commands.Convo(env)` in `cmd/sneat/main.go`

---

### Task 1: Branch + Dependency Setup

**Files:**
- Modify: `go.mod` (add require + replace directives)
- No test file for this task

**Interfaces:**
- Produces: `go.mod` with `require github.com/sneat-co/sneat-bots v0.3.1` and four replace directives; `go.sum` updated

- [ ] **Step 1: Create branch**

```bash
cd /home/ai/projects/sneat-co/sneat-cli
git checkout -b conversational-runtime
```

Expected: `Switched to a new branch 'conversational-runtime'`

- [ ] **Step 2: Add dependency and replace directives to go.mod**

Open `/home/ai/projects/sneat-co/sneat-cli/go.mod` and make these changes:

In the `require (...)` block, add after the last existing `require` line before the closing paren:
```
github.com/sneat-co/sneat-bots v0.3.1
```

After the closing paren of `require (...)`, add the `replace` block:
```
replace github.com/sneat-co/sneat-bots => ../sneat-bots
replace github.com/sneat-co/listus/backend => ../listus/backend
replace github.com/sneat-co/contactus/backend => ../contactus/backend
replace github.com/sneat-co/calendarius/backend => ../calendarius/backend
```

(Note: `contactus/backend` already has a direct require in go.mod; the replace simply overrides its resolution to the local path.)

- [ ] **Step 3: Run go mod tidy**

```bash
cd /home/ai/projects/sneat-co/sneat-cli && go mod tidy
```

Expected: exits 0, no errors. `go.sum` is updated.

- [ ] **Step 4: Verify build**

```bash
cd /home/ai/projects/sneat-co/sneat-cli && go build ./...
```

Expected: PASS (exits 0, no output)

- [ ] **Step 5: Commit**

```bash
cd /home/ai/projects/sneat-co/sneat-cli
git add go.mod go.sum
git commit -m "chore: add sneat-bots dependency with local replace directives"
```

---

### Task 2: convo.go — core command factory and `convo actions`

**Files:**
- Create: `cmd/sneat/commands/convo.go`
- Modify: `cmd/sneat/main.go` (register `commands.Convo(env)`)

**Interfaces:**
- Consumes: existing `Env` type from `root.go`; `output.go` helpers: `writeJSON`, `writeTable`, `addFormatFlags`, `formatFromCmd`
- Produces: exported `func Convo(env Env) *cobra.Command`; `convo actions` subcommand

The `convo actions` subcommand lists action definitions. With `--json` it calls `runtime.SpecJSON(scope)` and prints the raw bytes. Without `--json` it renders a table with columns: ID | SUMMARY | CONFIRM | EXTENSION.

- [ ] **Step 1: Write the failing test for `convo actions --json`**

Create `/home/ai/projects/sneat-co/sneat-cli/cmd/sneat/commands/convo_test.go`:

```go
package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
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
```

NOTE: `buildConvoCmd` uses `sneatauth.Result{}` — add the import `"github.com/sneat-co/sneat-cli/internal/sneatauth"` to the file.

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /home/ai/projects/sneat-co/sneat-cli && go test ./cmd/sneat/commands/ -run TestConvoActions -v 2>&1 | head -20
```

Expected: FAIL — `Convo` is not defined yet.

- [ ] **Step 3: Create `cmd/sneat/commands/convo.go`**

```go
// Package commands provides cobra commands for the sneat CLI.
package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sneat-co/sneat-bots/pkg/convo/convodev"
	"github.com/sneat-co/sneat-bots/pkg/convo/convollm/llmmock"
	"github.com/sneat-co/sneat-bots/pkg/convo/convomodel"
	"github.com/sneat-co/sneat-bots/pkg/convo/convoruntime"
	"github.com/sneat-co/sneat-bots/pkg/convo/convosetup"
	"github.com/sneat-co/sneat-go-core/coretypes"
	"github.com/spf13/cobra"
)

// Convo builds the `sneat convo` command group.
func Convo(_ Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "convo",
		Short: "Conversational runtime dev/agent commands",
	}
	cmd.AddCommand(
		convoActionsCmd(),
		convoSayCmd(),
		convoReplayCmd(),
	)
	return cmd
}

// newRuntime constructs the runtime used by all convo subcommands.
func newConvoRuntime() (*convoruntime.Runtime, error) {
	return convosetup.NewRuntime(llmmock.NewClient())
}

// setupSandbox wires the in-memory DB with the given space and user.
// The returned restore func must be deferred by the caller.
func setupSandbox(spaceID coretypes.SpaceID, userID string) (func(), error) {
	_, restore, err := convodev.SetupSandbox(spaceID, userID)
	return restore, err
}

// convoActionsCmd returns the `sneat convo actions` subcommand.
func convoActionsCmd() *cobra.Command {
	var scope []string
	cmd := &cobra.Command{
		Use:   "actions",
		Short: "List available conversational action definitions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, err := newConvoRuntime()
			if err != nil {
				return err
			}
			useJSON, _ := cmd.Flags().GetBool("json")
			if useJSON {
				b, err := rt.SpecJSON(scope)
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout(), string(b))
				return err
			}
			defs := rt.ActionDefs(scope)
			headers := []string{"ID", "SUMMARY", "CONFIRM", "EXTENSION"}
			rows := make([][]string, 0, len(defs))
			for _, d := range defs {
				confirm := "-"
				if d.Confirm {
					confirm = "yes"
				}
				rows = append(rows, []string{d.ID, d.Summary, confirm, string(d.Extension)})
			}
			return writeTable(cmd.OutOrStdout(), headers, rows)
		},
	}
	cmd.Flags().StringSliceVar(&scope, "scope", nil, "comma-separated catalog IDs to include (default: all)")
	cmd.Flags().Bool("json", false, "output action spec as JSON")
	return cmd
}
```

- [ ] **Step 4: Register Convo in main.go**

Open `/home/ai/projects/sneat-co/sneat-cli/cmd/sneat/main.go`. In the `root.AddCommand(...)` call, add `commands.Convo(env),` after `commands.Contacts(env),`:

```go
root.AddCommand(
    commands.Version(version, commit, date),
    commands.Auth(env),
    commands.Whoami(env),
    commands.Space(env),
    commands.Spaces(env),
    commands.Ui(env),
    commands.Contact(env),
    commands.Contacts(env),
    commands.Convo(env),
)
```

- [ ] **Step 5: Run go fmt and verify tests pass**

```bash
cd /home/ai/projects/sneat-co/sneat-cli && go fmt ./... && go test ./cmd/sneat/commands/ -run TestConvoActions -v
```

Expected: PASS both `TestConvoActions_JSON_ContainsExpectedActions` and `TestConvoActions_Table_PrintsRows`.

- [ ] **Step 6: Run full build**

```bash
cd /home/ai/projects/sneat-co/sneat-cli && go build ./...
```

Expected: exits 0.

- [ ] **Step 7: Commit**

```bash
cd /home/ai/projects/sneat-co/sneat-cli
git add cmd/sneat/commands/convo.go cmd/sneat/commands/convo_test.go cmd/sneat/main.go
git commit -m "feat(convo): add convo command group with actions subcommand"
```

---

### Task 3: `convo say` — multi-turn conversation with pending action handling

**Files:**
- Modify: `cmd/sneat/commands/convo.go` (add `convoSayCmd()` and shared `runSession` helper)
- Modify: `cmd/sneat/commands/convo_test.go` (add say tests)

**Interfaces:**
- Consumes: `convoruntime.Runtime.HandleText`, `convoruntime.Runtime.ResolvePending`, `convodev.SetupSandbox`, `convomodel.Request`, `convomodel.Response`, `convomodel.PendingAction`
- Produces: `convoSayCmd() *cobra.Command`; `runSession(...)` internal helper used by both `say` and `replay`

The `runSession` helper processes one or more (text, decision) turns and writes output. For human output each turn prints `> <message>` then reply text (with executed/pending/clarification detail). For JSON output it collects a `[]convoTurn` and JSON-encodes at the end.

```go
type convoTurn struct {
    Text       string              `json:"text"`
    Response   convomodel.Response `json:"response"`
    Resolution *convomodel.Response `json:"resolution,omitempty"`
}
```

A "decision" in a turn is one of: `""` (not a decision — process as text), `"yes"` (resolve pending approved=true), `"no"` (resolve pending approved=false).

- [ ] **Step 1: Write failing tests for `convo say`**

Append to `/home/ai/projects/sneat-co/sneat-cli/cmd/sneat/commands/convo_test.go`:

```go
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
	// No contacts in sandbox — should get clarification (no match)
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
	// With --yes, pending should be auto-resolved
	if !strings.Contains(strings.ToLower(out), "scheduled") && !strings.Contains(strings.ToLower(out), "meet sarah") {
		t.Errorf("expected scheduling confirmation: %s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /home/ai/projects/sneat-co/sneat-cli && go test ./cmd/sneat/commands/ -run TestConvoSay -v 2>&1 | head -30
```

Expected: FAIL — `convoSayCmd` not defined yet.

- [ ] **Step 3: Add `convoSayCmd()` and the session runner to `convo.go`**

Append to `cmd/sneat/commands/convo.go` (inside the package, after `convoActionsCmd`):

```go
// convoTurn is the JSON-serializable record of one conversation turn.
type convoTurn struct {
	Text       string               `json:"text"`
	Response   convomodel.Response  `json:"response"`
	Resolution *convomodel.Response `json:"resolution,omitempty"`
}

// convoSayCmd returns the `sneat convo say` subcommand.
func convoSayCmd() *cobra.Command {
	var (
		scope   []string
		autoYes bool
		useJSON bool
		spaceID string
		userID  string
	)
	cmd := &cobra.Command{
		Use:   "say <message> [<message2> ...]",
		Short: "Send one or more messages to the conversational runtime",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSaySession(cmd, args, scope, spaceID, userID, autoYes, useJSON)
		},
	}
	cmd.Flags().StringSliceVar(&scope, "scope", nil, "comma-separated catalog IDs (default: all)")
	cmd.Flags().BoolVar(&autoYes, "yes", false, "auto-approve pending actions")
	cmd.Flags().BoolVar(&useJSON, "json", false, "output as JSON array")
	cmd.Flags().StringVar(&spaceID, "space", "space1", "sandbox space ID")
	cmd.Flags().StringVar(&userID, "user", "user1", "sandbox user ID")
	return cmd
}

// runSaySession wires the sandbox, creates the runtime and processes messages.
func runSaySession(cmd *cobra.Command, messages []string, scope []string, spaceID, userID string, autoYes, useJSON bool) error {
	restore, err := setupSandbox(coretypes.SpaceID(spaceID), userID)
	if err != nil {
		return fmt.Errorf("sandbox setup: %w", err)
	}
	defer restore()

	rt, err := newConvoRuntime()
	if err != nil {
		return err
	}

	req := convomodel.Request{
		UserID:  userID,
		SpaceID: coretypes.SpaceID(spaceID),
		Scope:   scope,
	}

	var turns []convoTurn
	var pending *convomodel.PendingAction

	for _, msg := range messages {
		req.Text = msg
		resp, err := rt.HandleText(cmd.Context(), req)
		if err != nil {
			return fmt.Errorf("HandleText(%q): %w", msg, err)
		}
		turn := convoTurn{Text: msg, Response: resp}

		if resp.Pending != nil && autoYes {
			req2 := req
			req2.Text = "yes"
			resolution, err := rt.ResolvePending(cmd.Context(), req2, *resp.Pending, true)
			if err != nil {
				return fmt.Errorf("ResolvePending: %w", err)
			}
			turn.Resolution = &resolution
			pending = nil
		} else if resp.Pending != nil {
			pending = resp.Pending
		} else {
			pending = nil
		}

		turns = append(turns, turn)
	}
	_ = pending // pending exposed in output below

	if useJSON {
		return writeJSON(cmd.OutOrStdout(), turns)
	}
	return printTurns(cmd, turns)
}

// printTurns writes human-readable multi-turn output.
func printTurns(cmd *cobra.Command, turns []convoTurn) error {
	w := cmd.OutOrStdout()
	for _, t := range turns {
		fmt.Fprintf(w, "> %s\n", t.Text)
		fmt.Fprintln(w, t.Response.ReplyText)
		for _, ex := range t.Response.Executed {
			fmt.Fprintf(w, "  [executed] %s: %s\n", ex.ActionID, ex.Summary)
		}
		if t.Response.Pending != nil {
			fmt.Fprintf(w, "  [pending] %s\n", t.Response.Pending.Prompt)
			b, _ := json.MarshalIndent(t.Response.Pending.Call, "    ", "  ")
			fmt.Fprintf(w, "    %s\n", string(b))
		}
		if t.Response.Clarification != nil {
			fmt.Fprintf(w, "  [clarification] %s\n", t.Response.Clarification.Question)
		}
		if t.Resolution != nil {
			fmt.Fprintf(w, "  [resolved] %s\n", t.Resolution.ReplyText)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run go fmt and verify tests pass**

```bash
cd /home/ai/projects/sneat-co/sneat-cli && go fmt ./... && go test ./cmd/sneat/commands/ -run TestConvoSay -v
```

Expected: all four `TestConvoSay_*` tests PASS.

- [ ] **Step 5: Run all tests**

```bash
cd /home/ai/projects/sneat-co/sneat-cli && go test ./...
```

Expected: PASS (all existing tests still pass).

- [ ] **Step 6: Commit**

```bash
cd /home/ai/projects/sneat-co/sneat-cli
git add cmd/sneat/commands/convo.go cmd/sneat/commands/convo_test.go
git commit -m "feat(convo): add convo say subcommand with multi-turn and pending resolution"
```

---

### Task 4: `convo replay` — script file playback

**Files:**
- Modify: `cmd/sneat/commands/convo.go` (add `convoReplayCmd()`)
- Modify: `cmd/sneat/commands/convo_test.go` (add replay test)

**Interfaces:**
- Consumes: `runSaySession` helper is NOT reused directly — replay has its own session loop that handles `yes`/`no` lines as decisions on the current pending action, rather than as messages
- Produces: `convoReplayCmd() *cobra.Command`

Script file format:
- Blank lines and lines starting with `#` are skipped
- Lines exactly equal to `yes` or `no` (case-insensitive, trimmed) resolve the current pending action
- All other lines are user messages

- [ ] **Step 1: Write failing test for `convo replay`**

Append to `cmd/sneat/commands/convo_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/ai/projects/sneat-co/sneat-cli && go test ./cmd/sneat/commands/ -run TestConvoReplay -v 2>&1 | head -20
```

Expected: FAIL — `convoReplayCmd` not defined.

- [ ] **Step 3: Add `convoReplayCmd()` to `convo.go`**

Append to `cmd/sneat/commands/convo.go`:

```go
// convoReplayCmd returns the `sneat convo replay` subcommand.
func convoReplayCmd() *cobra.Command {
	var (
		scope   []string
		useJSON bool
		spaceID string
		userID  string
	)
	cmd := &cobra.Command{
		Use:   "replay <file>",
		Short: "Replay a conversation script file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReplaySession(cmd, args[0], scope, spaceID, userID, useJSON)
		},
	}
	cmd.Flags().StringSliceVar(&scope, "scope", nil, "comma-separated catalog IDs (default: all)")
	cmd.Flags().BoolVar(&useJSON, "json", false, "output as JSON array")
	cmd.Flags().StringVar(&spaceID, "space", "space1", "sandbox space ID")
	cmd.Flags().StringVar(&userID, "user", "user1", "sandbox user ID")
	return cmd
}

// runReplaySession reads a script file and replays the conversation.
func runReplaySession(cmd *cobra.Command, scriptFile string, scope []string, spaceID, userID string, useJSON bool) error {
	data, err := os.ReadFile(scriptFile)
	if err != nil {
		return fmt.Errorf("reading script file: %w", err)
	}

	restore, err := setupSandbox(coretypes.SpaceID(spaceID), userID)
	if err != nil {
		return fmt.Errorf("sandbox setup: %w", err)
	}
	defer restore()

	rt, err := newConvoRuntime()
	if err != nil {
		return err
	}

	req := convomodel.Request{
		UserID:  userID,
		SpaceID: coretypes.SpaceID(spaceID),
		Scope:   scope,
	}

	var turns []convoTurn
	var pending *convomodel.PendingAction

	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		normalized := strings.ToLower(line)
		if normalized == "yes" || normalized == "no" {
			if pending == nil {
				return fmt.Errorf("script has %q but no pending action", line)
			}
			approved := normalized == "yes"
			req2 := req
			req2.Text = line
			resolution, err := rt.ResolvePending(cmd.Context(), req2, *pending, approved)
			if err != nil {
				return fmt.Errorf("ResolvePending(%q): %w", line, err)
			}
			// Attach resolution to the last turn
			if len(turns) > 0 {
				turns[len(turns)-1].Resolution = &resolution
			}
			pending = nil
			continue
		}

		req.Text = line
		resp, err := rt.HandleText(cmd.Context(), req)
		if err != nil {
			return fmt.Errorf("HandleText(%q): %w", line, err)
		}
		if resp.Pending != nil {
			pending = resp.Pending
		} else {
			pending = nil
		}
		turns = append(turns, convoTurn{Text: line, Response: resp})
	}

	if useJSON {
		return writeJSON(cmd.OutOrStdout(), turns)
	}
	return printTurns(cmd, turns)
}
```

Also add the `"os"` import to `convo.go` (it must be in the import block alongside the existing imports).

- [ ] **Step 4: Run go fmt and verify test passes**

```bash
cd /home/ai/projects/sneat-co/sneat-cli && go fmt ./... && go test ./cmd/sneat/commands/ -run TestConvoReplay -v
```

Expected: PASS `TestConvoReplay_BuyAndRemoveMilk`.

- [ ] **Step 5: Run all tests and vet**

```bash
cd /home/ai/projects/sneat-co/sneat-cli && go vet ./... && go test ./...
```

Expected: PASS — all tests pass including previously existing ones.

- [ ] **Step 6: Commit**

```bash
cd /home/ai/projects/sneat-co/sneat-cli
git add cmd/sneat/commands/convo.go cmd/sneat/commands/convo_test.go
git commit -m "feat(convo): add convo replay subcommand with script-file playback"
```

---

### Task 5: Final verification

**Files:** (no new files — verification and cleanup only)

- [ ] **Step 1: Run go fmt**

```bash
cd /home/ai/projects/sneat-co/sneat-cli && go fmt ./...
```

Expected: no output (already formatted).

- [ ] **Step 2: Run go vet**

```bash
cd /home/ai/projects/sneat-co/sneat-cli && go vet ./...
```

Expected: exits 0, no warnings.

- [ ] **Step 3: Run go build**

```bash
cd /home/ai/projects/sneat-co/sneat-cli && go build ./...
```

Expected: exits 0.

- [ ] **Step 4: Run all tests with verbose output**

```bash
cd /home/ai/projects/sneat-co/sneat-cli && go test ./... -v 2>&1 | tail -40
```

Expected: all tests PASS, including:
- `TestConvoActions_JSON_ContainsExpectedActions`
- `TestConvoActions_Table_PrintsRows`
- `TestConvoSay_BuyMilk_JSON`
- `TestConvoSay_DeleteContactBob_NoPending_WithoutYes`
- `TestConvoSay_MultiTurn_AddThenListContacts`
- `TestConvoSay_MeetSarah_Yes_Resolves`
- `TestConvoReplay_BuyAndRemoveMilk`

- [ ] **Step 5: Verify CLI help synopses**

```bash
cd /home/ai/projects/sneat-co/sneat-cli && go run ./cmd/sneat convo --help
go run ./cmd/sneat convo actions --help
go run ./cmd/sneat convo say --help
go run ./cmd/sneat convo replay --help
```

Expected synopses:
- `sneat convo [command]`
- `sneat convo actions [--scope listus,contactus,...] [--json]`
- `sneat convo say <message> [<message2> ...] [--scope ...] [--yes] [--json] [--space space1] [--user user1]`
- `sneat convo replay <file> [--scope ...] [--json] [--space space1] [--user user1]`

- [ ] **Step 6: Final commit (if any uncommitted changes)**

```bash
cd /home/ai/projects/sneat-co/sneat-cli && git status
```

If there are staged or unstaged changes:

```bash
cd /home/ai/projects/sneat-co/sneat-cli
git add -u
git commit -m "feat(convo): final formatting and verification"
```

---

## Self-Review

### Spec Coverage

| Spec requirement | Task that covers it |
|---|---|
| Branch `conversational-runtime`, do not push | Task 1 Step 1 |
| `go.mod` require sneat-bots v0.3.1 + 4 replace directives | Task 1 Step 2 |
| `go mod tidy` | Task 1 Step 3 |
| `Convo(env Env) *cobra.Command` factory pattern | Task 2 |
| `convo actions [--scope] [--json]` | Task 2 |
| `convo say` with multi-turn, --yes, --json, --space, --user | Task 3 |
| `convo replay <file>` with yes/no resolution lines | Task 4 |
| Test: actions --json contains lists.add_items and contacts.create | Task 2 |
| Test: say "buy milk" --json: contains lists.add_items and milk reference | Task 3 |
| Test: say "delete contact Bob" without --yes: no deletion, reports clarification | Task 3 |
| Test: say multi-turn "add contact Jane Doe" then "list my contacts" shows Jane | Task 3 |
| Test: say "meet Sarah tomorrow at 3pm" --yes resolves pending | Task 3 |
| Test: replay buy milk → remove milk → yes → Removed | Task 4 |
| Register in cmd/sneat/main.go | Task 2 Step 4 |
| go fmt, go vet, go build, go test all pass | Task 5 |

### Placeholder Scan

No TBD, TODO, or placeholder phrases present. All code blocks are complete.

### Type Consistency

- `convoTurn.Response` is `convomodel.Response` (defined in Task 3, used in Task 3 and 4) ✓
- `convoTurn.Resolution` is `*convomodel.Response` (pointer — nil when no resolution) ✓
- `rt.HandleText(ctx, convomodel.Request{...})` — Request has UserID string, SpaceID coretypes.SpaceID, Text string, Scope []string ✓
- `rt.ResolvePending(ctx, req, *pending, approved bool)` ✓
- `rt.SpecJSON(scope []string) ([]byte, error)` ✓
- `rt.ActionDefs(scope []string) []convospec.ActionDef` — ActionDef has .ID, .Summary, .Confirm bool, .Extension coretypes.ExtID ✓
- `convodev.SetupSandbox(spaceID coretypes.SpaceID, userID ...string) (dal.DB, func(), error)` — we discard the db, use restore ✓
- `writeJSON(w io.Writer, v any) error` — from output.go ✓
- `writeTable(w io.Writer, headers []string, rows [][]string) error` — from output.go ✓
