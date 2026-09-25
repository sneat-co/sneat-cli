package commands

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dal-go/dalgo/dal"
	"github.com/sneat-co/sneat-bots/platform/convo/convomodel"
	"github.com/sneat-co/sneat-bots/platform/convo/convospec"
	"github.com/sneat-co/sneat-go-core/coretypes"
	"github.com/spf13/cobra"
)

// fakeConvoRuntime is a controllable convoRuntimeAPI used to exercise
// error-propagation paths in runSaySession/runReplaySession/convoActionsCmd
// that are impractical to provoke from the real deterministic mock LLM (a
// malformed model reply, a stale pending action, a JSON marshal failure).
type fakeConvoRuntime struct {
	specJSONErr    error
	actionDefsOut  []convospec.ActionDef
	handleTextResp convomodel.Response
	handleTextErr  error
	resolvePending struct {
		resp convomodel.Response
		err  error
	}
}

func (f *fakeConvoRuntime) SpecJSON([]string) ([]byte, error) {
	if f.specJSONErr != nil {
		return nil, f.specJSONErr
	}
	return []byte("[]"), nil
}

func (f *fakeConvoRuntime) ActionDefs([]string) []convospec.ActionDef {
	return f.actionDefsOut
}

func (f *fakeConvoRuntime) HandleText(context.Context, convomodel.Request) (convomodel.Response, error) {
	return f.handleTextResp, f.handleTextErr
}

func (f *fakeConvoRuntime) ResolvePending(context.Context, convomodel.Request, convomodel.PendingAction, bool) (convomodel.Response, error) {
	return f.resolvePending.resp, f.resolvePending.err
}

func withFakeConvoRuntime(t *testing.T, fake *fakeConvoRuntime) {
	t.Helper()
	prev := convoRuntimeFactory
	convoRuntimeFactory = func() (convoRuntimeAPI, error) { return fake, nil }
	t.Cleanup(func() { convoRuntimeFactory = prev })
}

func withFailingConvoRuntimeFactory(t *testing.T, err error) {
	t.Helper()
	prev := convoRuntimeFactory
	convoRuntimeFactory = func() (convoRuntimeAPI, error) { return nil, err }
	t.Cleanup(func() { convoRuntimeFactory = prev })
}

// --- setupSandbox ---

func TestSetupSandbox_ResolveSandboxDBError_Propagates(t *testing.T) {
	t.Setenv(envSneatStorage, "bogus-driver")
	ctx, err := setupSandbox(coretypes.SpaceID("space1"), "user1")
	if err == nil {
		t.Fatal("expected an error")
	}
	if ctx != nil {
		t.Errorf("expected a nil context on error, got %v", ctx)
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("expected resolveSandboxDB's error to propagate, got: %v", err)
	}
}

func TestSetupSandbox_SandboxSetupFnError_Propagates(t *testing.T) {
	t.Setenv(envSneatStorage, "memory")
	prev := sandboxSetupFn
	wantErr := errors.New("boom: seed failed")
	sandboxSetupFn = func(_ dal.DB, _ coretypes.SpaceID, _ ...string) (context.Context, dal.DB, error) {
		return nil, nil, wantErr
	}
	t.Cleanup(func() { sandboxSetupFn = prev })

	ctx, err := setupSandbox(coretypes.SpaceID("space1"), "user1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected the injected sandbox setup error, got: %v", err)
	}
	if ctx != nil {
		t.Errorf("expected a nil context on error, got %v", ctx)
	}
}

func TestRunSaySession_SandboxSetupError_Wrapped(t *testing.T) {
	t.Setenv(envSneatStorage, "bogus-driver")
	_, exec := buildConvoCmd(t)
	err := exec("convo", "say", "hi")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "sandbox setup") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- convoActionsCmd ---

func TestConvoActionsCmd_RuntimeFactoryError_Propagates(t *testing.T) {
	withFailingConvoRuntimeFactory(t, errors.New("boom: runtime composition"))
	_, exec := buildConvoCmd(t)
	err := exec("convo", "actions")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "boom: runtime composition") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestConvoActionsCmd_SpecJSONError_Propagates(t *testing.T) {
	withFakeConvoRuntime(t, &fakeConvoRuntime{specJSONErr: errors.New("boom: marshal failed")})
	_, exec := buildConvoCmd(t)
	err := exec("convo", "actions", "--json")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "boom: marshal failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- runSaySession ---

func TestRunSaySession_RuntimeFactoryError_Propagates(t *testing.T) {
	withFailingConvoRuntimeFactory(t, errors.New("boom: runtime composition"))
	_, exec := buildConvoCmd(t)
	err := exec("convo", "say", "hi")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "boom: runtime composition") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunSaySession_HandleTextError_Wrapped(t *testing.T) {
	withFakeConvoRuntime(t, &fakeConvoRuntime{handleTextErr: errors.New("boom: bad model reply")})
	_, exec := buildConvoCmd(t)
	err := exec("convo", "say", "hi")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), `HandleText("hi")`) {
		t.Errorf("expected the message to be named in the wrapped error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "boom: bad model reply") {
		t.Errorf("expected the underlying error preserved, got: %v", err)
	}
}

func TestRunSaySession_ResolvePendingError_Wrapped(t *testing.T) {
	fake := &fakeConvoRuntime{
		handleTextResp: convomodel.Response{
			ReplyText: "confirm?",
			Pending: &convomodel.PendingAction{
				ID:     "p1",
				Call:   convomodel.ActionCall{ActionID: "contacts.delete"},
				Prompt: "Delete contact?",
			},
		},
	}
	fake.resolvePending.err = errors.New("boom: stale pending action")
	withFakeConvoRuntime(t, fake)

	_, exec := buildConvoCmd(t)
	err := exec("convo", "say", "delete contact Bob", "--yes")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "ResolvePending:") {
		t.Errorf("expected the ResolvePending wrapper text, got: %v", err)
	}
	if !strings.Contains(err.Error(), "boom: stale pending action") {
		t.Errorf("expected the underlying error preserved, got: %v", err)
	}
}

// --- printTurns: each write-error branch, isolated with a writer that fails
// on exactly the Nth Write call (every fmt.Fprintf/Fprintln issues exactly
// one Write). Turns are hand-built rather than produced by a real
// conversation so every field (Executed/Pending/Clarification/Resolution) is
// populated in one pass without needing five different real scenarios.

// failAtWriter succeeds on writes [1, failAt) and fails from failAt onward.
type failAtWriter struct {
	calls  int
	failAt int
}

func (w *failAtWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls >= w.failAt {
		return 0, errors.New("write: broken pipe")
	}
	return len(p), nil
}

func fullConvoTurn() convoTurn {
	return convoTurn{
		Text: "hi",
		Response: convomodel.Response{
			ReplyText: "ok",
			Executed:  []convomodel.ActionResult{{ActionID: "x.y", Summary: "did it"}},
			Pending: &convomodel.PendingAction{
				ID:     "p1",
				Call:   convomodel.ActionCall{ActionID: "x.z", Args: map[string]any{"a": 1}},
				Prompt: "confirm?",
			},
			Clarification: &convomodel.Clarification{Question: "which one?"},
		},
		Resolution: &convomodel.Response{ReplyText: "resolved"},
	}
}

func TestPrintTurns_EachWriteFailureBranch(t *testing.T) {
	// Call order per turn (see printTurns): 1: "> text", 2: replyText,
	// 3: executed line, 4: pending prompt, 5: pending call JSON,
	// 6: clarification, 7: resolution.
	for callIndex := 1; callIndex <= 7; callIndex++ {
		w := &failAtWriter{failAt: callIndex}
		cmd := &cobra.Command{}
		cmd.SetOut(w)
		err := printTurns(cmd, []convoTurn{fullConvoTurn()})
		if err == nil {
			t.Fatalf("call #%d: expected the write error to propagate", callIndex)
		}
		if w.calls != callIndex {
			t.Errorf("call #%d: expected printTurns to stop at write #%d, stopped at #%d", callIndex, callIndex, w.calls)
		}
	}
}

// --- runReplaySession ---

func TestRunReplaySession_ReadFileError_Wrapped(t *testing.T) {
	_, exec := buildConvoCmd(t)
	err := exec("convo", "replay", filepath.Join(t.TempDir(), "does-not-exist.txt"))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "reading script file") {
		t.Errorf("unexpected error: %v", err)
	}
}

func writeReplayScript(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "script.txt")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestRunReplaySession_SandboxSetupError_Wrapped(t *testing.T) {
	path := writeReplayScript(t, "hello\n")
	t.Setenv(envSneatStorage, "bogus-driver")
	_, exec := buildConvoCmd(t)
	err := exec("convo", "replay", path)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "sandbox setup") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunReplaySession_RuntimeFactoryError_Propagates(t *testing.T) {
	path := writeReplayScript(t, "hello\n")
	withFailingConvoRuntimeFactory(t, errors.New("boom: runtime composition"))
	_, exec := buildConvoCmd(t)
	err := exec("convo", "replay", path)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "boom: runtime composition") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunReplaySession_YesWithNoPending_Errors(t *testing.T) {
	path := writeReplayScript(t, "yes\n")
	_, exec := buildConvoCmd(t)
	err := exec("convo", "replay", path)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), `script has "yes" but no pending action`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunReplaySession_ResolvePendingError_Wrapped(t *testing.T) {
	fake := &fakeConvoRuntime{
		handleTextResp: convomodel.Response{
			ReplyText: "confirm?",
			Pending: &convomodel.PendingAction{
				ID:     "p1",
				Call:   convomodel.ActionCall{ActionID: "contacts.delete"},
				Prompt: "Delete contact?",
			},
		},
	}
	fake.resolvePending.err = errors.New("boom: stale pending action")
	withFakeConvoRuntime(t, fake)

	path := writeReplayScript(t, "delete contact Bob\nyes\n")
	_, exec := buildConvoCmd(t)
	err := exec("convo", "replay", path)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), `ResolvePending("yes")`) {
		t.Errorf("expected the resolved line to be named in the wrapped error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "boom: stale pending action") {
		t.Errorf("expected the underlying error preserved, got: %v", err)
	}
}

func TestRunReplaySession_HandleTextError_Wrapped(t *testing.T) {
	withFakeConvoRuntime(t, &fakeConvoRuntime{handleTextErr: errors.New("boom: bad model reply")})
	path := writeReplayScript(t, "hello\n")
	_, exec := buildConvoCmd(t)
	err := exec("convo", "replay", path)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), `HandleText("hello")`) {
		t.Errorf("expected the line to be named in the wrapped error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "boom: bad model reply") {
		t.Errorf("expected the underlying error preserved, got: %v", err)
	}
}
