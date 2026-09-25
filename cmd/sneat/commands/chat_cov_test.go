package commands

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/strongo/aichat/ai/aiconfig"
)

// withUserConfigDir overrides the osUserConfigDir seam for the duration of
// the test and restores the real os.UserConfigDir on cleanup.
func withUserConfigDir(t *testing.T, dir string, err error) {
	t.Helper()
	orig := osUserConfigDir
	osUserConfigDir = func() (string, error) { return dir, err }
	t.Cleanup(func() { osUserConfigDir = orig })
}

func TestAiConfigPath_UserConfigDirError(t *testing.T) {
	withUserConfigDir(t, "", errors.New("no config dir"))
	if got := aiConfigPath(); got != "" {
		t.Fatalf("aiConfigPath() = %q, want empty on UserConfigDir error", got)
	}
}

func TestAiChatConfigFromCmd_ByokModelAndAPIKeyEnv(t *testing.T) {
	env := chatEnv(true, &fakeStore{}, new(string), new(int))
	cmd := Chat(env)
	if err := cmd.Flags().Parse([]string{"--byok-model", "gpt-x", "--byok-api-key-env", "MY_KEY"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	cfg := aiChatConfigFromCmd(cmd, aiconfig.Config{})
	if cfg.BYOK.Model != "gpt-x" {
		t.Fatalf("BYOK.Model = %q, want gpt-x", cfg.BYOK.Model)
	}
	if cfg.BYOK.APIKeyEnv != "MY_KEY" {
		t.Fatalf("BYOK.APIKeyEnv = %q, want MY_KEY", cfg.BYOK.APIKeyEnv)
	}
}

func TestTzFromCmd_FlagWins(t *testing.T) {
	env := chatEnv(true, &fakeStore{}, new(string), new(int))
	cmd := Chat(env)
	if err := cmd.Flags().Parse([]string{"--tz", "Europe/London"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := tzFromCmd(cmd, func(string) string { return "" }); got != "Europe/London" {
		t.Fatalf("tzFromCmd = %q, want Europe/London", got)
	}
}

func TestTzFromCmd_EnvVarWins(t *testing.T) {
	env := chatEnv(true, &fakeStore{}, new(string), new(int))
	cmd := Chat(env)
	if err := cmd.Flags().Parse(nil); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	getenv := func(k string) string {
		if k == "SNEAT_TZ" {
			return "Asia/Tokyo"
		}
		return ""
	}
	if got := tzFromCmd(cmd, getenv); got != "Asia/Tokyo" {
		t.Fatalf("tzFromCmd = %q, want Asia/Tokyo", got)
	}
}

func TestRunChat_ContactsReaderError(t *testing.T) {
	var calls int
	var gotUID string
	env := chatEnv(true, &fakeStore{load: &session.Session{UID: "u1"}}, &gotUID, &calls)
	readerErr := errors.New("no contacts reader")
	env.NewContactsReader = func(config.Config) (ContactsReader, error) { return nil, readerErr }
	root := Root(env)
	root.AddCommand(Chat(env))
	root.SetArgs([]string{"chat"})
	if err := root.Execute(); !errors.Is(err, readerErr) {
		t.Fatalf("Execute error = %v, want %v", err, readerErr)
	}
	if calls != 0 {
		t.Fatal("RunChat must not be called when the contacts reader fails")
	}
}

func TestRunChat_AIConfigLoadError(t *testing.T) {
	tmp := t.TempDir()
	withUserConfigDir(t, tmp, nil)
	// Make <tmp>/sneat/ai.yaml a DIRECTORY, so aiconfig.Load's os.ReadFile
	// fails with a non-not-exist error (a real, hermetic way to exercise
	// that error branch without weakening the file it reads).
	if err := os.MkdirAll(filepath.Join(tmp, "sneat", "ai.yaml"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	var calls int
	var gotUID string
	env := chatEnv(true, &fakeStore{load: &session.Session{UID: "u1"}}, &gotUID, &calls)
	root := Root(env)
	root.AddCommand(Chat(env))
	root.SetArgs([]string{"chat"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected aiconfig.Load error")
	}
	if calls != 0 {
		t.Fatal("RunChat must not be called when aiconfig.Load fails")
	}
}
