package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sneat-co/sneat-cli/internal/actionstore"
	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/sneat-co/sneat-cli/internal/sneatauth"
)

// TestActionsDir_DefaultsToUserConfigDir covers the branch of actionsDir
// taken when SNEAT_CONFIG_DIR is unset: it must fall back to
// actionstore.DefaultDir(os.UserConfigDir). $HOME is pinned to a temp dir so
// the result is deterministic and no real user config directory is touched.
func TestActionsDir_DefaultsToUserConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	env := testEnv(&fakeStore{}, sneatauth.Result{})
	env.Getenv = func(string) string { return "" }

	got, err := actionsDir(env)
	if err != nil {
		t.Fatalf("actionsDir: %v", err)
	}
	want, err := actionstore.DefaultDir(os.UserConfigDir)
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if got != want {
		t.Fatalf("actionsDir = %q, want %q", got, want)
	}
}

// TestActionsDir_UserConfigDirError_Propagates covers actionsDir's error
// path: with $HOME unset, os.UserConfigDir fails on darwin and the error
// must propagate rather than being swallowed.
func TestActionsDir_UserConfigDirError_Propagates(t *testing.T) {
	origUserConfigDir := osUserConfigDir
	osUserConfigDir = func() (string, error) { return "", errors.New("no user config dir") }
	t.Cleanup(func() { osUserConfigDir = origUserConfigDir })
	env := testEnv(&fakeStore{}, sneatauth.Result{})
	env.Getenv = func(string) string { return "" }

	if _, err := actionsDir(env); err == nil {
		t.Fatal("expected an error when $HOME is undefined")
	}
}

// TestNewActionStore_PropagatesActionsDirError covers newActionStore's own
// error branch, reached when actionsDir fails.
func TestNewActionStore_PropagatesActionsDirError(t *testing.T) {
	origUserConfigDir := osUserConfigDir
	osUserConfigDir = func() (string, error) { return "", errors.New("no user config dir") }
	t.Cleanup(func() { osUserConfigDir = origUserConfigDir })
	env := testEnv(&fakeStore{}, sneatauth.Result{})
	env.Getenv = func(string) string { return "" }

	if _, err := newActionStore(env); err == nil {
		t.Fatal("expected an error when actionsDir fails")
	}
}

// TestNewActionStore_Success sanity-checks the happy path used by every
// action_*.go command: a store rooted at SNEAT_CONFIG_DIR/sneat/actions.
func TestNewActionStore_Success(t *testing.T) {
	dir := t.TempDir()
	env := testEnv(&fakeStore{load: &session.Session{UID: "u1"}}, sneatauth.Result{})
	env.Getenv = func(k string) string {
		if k == "SNEAT_CONFIG_DIR" {
			return dir
		}
		return ""
	}
	store, err := newActionStore(env)
	if err != nil {
		t.Fatalf("newActionStore: %v", err)
	}
	if err := store.Save(actionstore.Draft{ActionID: "act_abc12345", SpaceID: "famID"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sneat", "actions", "act_abc12345.json")); err != nil {
		t.Fatalf("draft not written where expected: %v", err)
	}
}

// TestNewActionID_RandReadError_Propagates covers newActionID's practically
// unreachable crypto/rand failure path via the randRead seam.
func TestNewActionID_RandReadError_Propagates(t *testing.T) {
	orig := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("rand: boom") }
	t.Cleanup(func() { randRead = orig })

	if _, err := newActionID(); err == nil {
		t.Fatal("expected an error when randRead fails")
	}
}

// TestNewActionID_Shape checks the generated id's shape (prefix + 8 chars).
func TestNewActionID_Shape(t *testing.T) {
	id, err := newActionID()
	if err != nil {
		t.Fatalf("newActionID: %v", err)
	}
	if !strings.HasPrefix(id, "act_") || len(id) != len("act_")+8 {
		t.Fatalf("newActionID() = %q, want act_ + 8 chars", id)
	}
}

// --- shared test helpers for the action_*.go coverage tests below ---

// actionsEnvWithBrokenStore builds an Env whose newActionStore(env) call
// always fails: no SNEAT_CONFIG_DIR override and the osUserConfigDir seam
// forced to fail, so actionstore.DefaultDir errors (hermetic: os.UserConfigDir
// itself still succeeds on Linux with an empty $HOME when XDG_CONFIG_HOME is set). Used to exercise every
// action_*.go command's `if err != nil { return err }` right after
// newActionStore(env).
func actionsEnvWithBrokenStore(t *testing.T, api *fakeActionsAPI) Env {
	t.Helper()
	origUserConfigDir := osUserConfigDir
	osUserConfigDir = func() (string, error) { return "", errors.New("no user config dir") }
	t.Cleanup(func() { osUserConfigDir = origUserConfigDir })
	env := actionsEnv(api, "")
	env.Getenv = func(string) string { return "" }
	return env
}

// draftPath mirrors actionsDir's SNEAT_CONFIG_DIR resolution to locate a
// draft's on-disk file for tests that need to reach past the Store API
// (corrupting a file, or chmod'ing it/its directory to force an I/O error).
func draftPath(configDir, actionID string) string {
	return filepath.Join(configDir, "sneat", "actions", actionID+".json")
}

func draftsDir(configDir string) string {
	return filepath.Join(configDir, "sneat", "actions")
}

// writeCorruptDraft plants unparsable JSON at actionID's draft path so
// store.Load(actionID) returns a generic (non-ErrNotFound) error.
func writeCorruptDraft(t *testing.T, configDir, actionID string) {
	t.Helper()
	dir := draftsDir(configDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(draftPath(configDir, actionID), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// makeDraftFileReadOnly chmods an existing draft file read-only so a
// subsequent store.Save (which overwrites it) fails, and restores write
// permission during cleanup so t.TempDir() can remove it.
func makeDraftFileReadOnly(t *testing.T, configDir, actionID string) {
	t.Helper()
	path := draftPath(configDir, actionID)
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
}

// makeDraftsDirReadOnly chmods the drafts directory read+execute-only, so a
// store.Save creating a brand-new draft file, or a store.Delete removing an
// existing one, fails with a permission error. Restored during cleanup so
// t.TempDir() can remove it.
func makeDraftsDirReadOnly(t *testing.T, configDir string) {
	t.Helper()
	dir := draftsDir(configDir)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
}

// makeDraftsDirUnreadable chmods the drafts directory so os.ReadDir on it
// fails, forcing store.List() to return a generic error. Restored during
// cleanup so t.TempDir() can remove it.
func makeDraftsDirUnreadable(t *testing.T, configDir string) {
	t.Helper()
	dir := draftsDir(configDir)
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
}

// failWriter is an io.Writer that always fails, used to force writeJSON
// errors on command output.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("write: boom") }
