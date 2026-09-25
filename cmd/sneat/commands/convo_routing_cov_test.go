package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/sneat-co/sneat-bots/platform/convo/convoruntime"
	"github.com/sneat-co/sneat-cli/internal/sneatauth"
	"github.com/spf13/cobra"
)

// buildConvoCmdWithOut is buildConvoCmd's twin for tests that need to swap in
// a non-buffer io.Writer (e.g. one that fails every Write) instead of the
// captured *bytes.Buffer.
func buildConvoCmdWithOut(out interface{ Write([]byte) (int, error) }) *cobra.Command {
	env := testEnv(&fakeStore{}, sneatauth.Result{})
	root := Root(env)
	root.AddCommand(Convo(env))
	root.SetOut(out)
	return root
}

// alwaysFailWriter errors on every Write, so the very first output statement
// in whatever it backs fails — used to exercise writeTable's and errWriter's
// error-propagation branches without a real closed pipe.
type alwaysFailWriter struct{}

func (alwaysFailWriter) Write([]byte) (int, error) {
	return 0, errors.New("write: broken pipe")
}

func withFailingConvoRegistryFactory(t *testing.T, err error) {
	t.Helper()
	prev := convoRegistryFactory
	convoRegistryFactory = func() (*convoruntime.Registry, error) { return nil, err }
	t.Cleanup(func() { convoRegistryFactory = prev })
}

func TestConvoCatalogs_RegistryFactoryError_Propagates(t *testing.T) {
	withFailingConvoRegistryFactory(t, errors.New("boom: registry composition"))
	buf, exec := buildConvoCmd(t)
	err := exec("convo", "catalogs")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "boom: registry composition") {
		t.Errorf("unexpected error: %v", err)
	}
	_ = buf
}

func TestConvoRoute_RegistryFactoryError_Propagates(t *testing.T) {
	withFailingConvoRegistryFactory(t, errors.New("boom: registry composition"))
	_, exec := buildConvoCmd(t)
	err := exec("convo", "route", "hello")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "boom: registry composition") {
		t.Errorf("unexpected error: %v", err)
	}
}

// writeTable's error branch (a closed pipe / failing stdout) needs a writer
// that fails; the registry itself must compose successfully so execution
// reaches the table render.
func TestConvoCatalogs_WriteTableError_Propagates(t *testing.T) {
	root := buildConvoCmdWithOut(alwaysFailWriter{})
	root.SetArgs([]string{"convo", "catalogs"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected a write error to propagate")
	}
	if !strings.Contains(err.Error(), "write: broken pipe") {
		t.Errorf("expected the underlying write error, got: %v", err)
	}
}

// A message claimed by two OR MORE catalogs' declared triggers must report
// the FAILS-OPEN multi-match branch, not the single-match "narrows" branch.
// "buy" is a real Listus trigger and "kg" is a real Trackus trigger (see the
// EffectiveScope doc comment's own example), so this exercises the default
// branch of convoRouteCmd's switch with the real registered catalog set.
func TestConvoRoute_MultipleCatalogsClaim_FailsOpen(t *testing.T) {
	buf, exec := buildConvoCmd(t)
	if err := exec("convo", "route", "buy 2 kg of protein powder"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "FAILS OPEN") {
		t.Errorf("multiple claiming catalogs must fail open:\n%s", out)
	}
	if !strings.Contains(out, "catalogs claim this") {
		t.Errorf("expected the multi-catalog wording:\n%s", out)
	}
	if !strings.Contains(out, "listus") || !strings.Contains(out, "trackus") {
		t.Errorf("expected both claiming catalogs named:\n%s", out)
	}
}

// reportTriggerHealth is exercised directly against a hand-built registry so
// both the trigger-conflict and the shadowed-trigger branches are reachable —
// neither situation exists among the real registered catalogs today (verified
// separately), so producing them for real would mean shipping a broken
// registration rather than testing the reporting code.
func TestReportTriggerHealth_ConflictsAndShadows(t *testing.T) {
	registry, err := convoruntime.NewRegistry(
		convoruntime.Catalog{ID: "cat-a", Triggers: []string{"hello"}},
		convoruntime.Catalog{ID: "cat-b", Triggers: []string{"hello"}},
		convoruntime.Catalog{ID: "cat-c", Triggers: []string{"hi there"}},
		convoruntime.Catalog{ID: "cat-d", Triggers: []string{"hi"}},
	)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	cmd := &cobra.Command{}
	var buf strings.Builder
	cmd.SetOut(&buf)

	if err := reportTriggerHealth(cmd, registry, nil); err != nil {
		t.Fatalf("reportTriggerHealth: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "trigger conflict") {
		t.Errorf("expected a trigger conflict to be reported:\n%s", out)
	}
	if !strings.Contains(out, `"hello"`) {
		t.Errorf("expected the conflicting trigger word named:\n%s", out)
	}
	if !strings.Contains(out, "shadowed trigger") {
		t.Errorf("expected a shadowed trigger to be reported:\n%s", out)
	}
	if !strings.Contains(out, `"hi there"`) {
		t.Errorf("expected the shadowed (longer) trigger named:\n%s", out)
	}
}

// errWriter must not keep writing after its first error: once e.err is set,
// printf/println become no-ops rather than issuing a second failing write.
// Driving `convo route` with an always-failing writer against a multi-match
// message forces at least 4 sequential out.printf/println calls, so the
// first sets e.err and the rest exercise the early-return branch.
func TestConvoRoute_ErrWriter_LatchesFirstErrorAndStopsWriting(t *testing.T) {
	fw := &countingFailWriter{}
	root := buildConvoCmdWithOut(fw)
	root.SetArgs([]string{"convo", "route", "buy 2 kg of protein powder"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected the first write error to propagate")
	}
	// Only the FIRST write must have gone through the failing io.Writer;
	// every write after e.err is set must be a no-op, i.e. Write is never
	// invoked from the second logical printf/println call onward. Since our
	// writer always fails and always increments writes on invocation, and
	// errWriter is supposed to stop calling Write once err is set, the
	// invocation count must be exactly 1.
	if fw.writes != 1 {
		t.Errorf("errWriter must stop calling Write after the first failure, got %d Write calls", fw.writes)
	}
}

// countingFailWriter fails on every Write it is actually asked to perform,
// and counts how many times Write was invoked.
type countingFailWriter struct{ writes int }

func (w *countingFailWriter) Write(p []byte) (int, error) {
	w.writes++
	return 0, errors.New("write: broken pipe")
}
