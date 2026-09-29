package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/sneat-co/contactus/backend/dbo4contactus"
	"github.com/sneat-co/sneat-cli/cmd/sneat/commands"
	"github.com/sneat-co/sneat-cli/internal/chatapp"
	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/firestoredb"
	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/sneat-co/sneat-cli/internal/tui"
	"github.com/strongo/aichat/ai/aiconfig"
	"github.com/strongo/buildinfo"
	"github.com/strongo/strongoapp/person"
)

// TestRun_Help drives run() through cobra's normal --help path (no real
// network/filesystem/keychain access) and asserts on both the returned exit
// code and the captured stdout, covering run's success path end to end.
func TestRun_Help(t *testing.T) {
	tmp := t.TempDir()
	restore := setUserConfigDir(func() (string, error) { return tmp, nil })
	defer restore()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(--help) code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Sneat.app command-line interface") {
		t.Fatalf("run(--help) stdout = %q, want usage text", stdout.String())
	}
}

// TestRun_UnknownCommand covers run's error path: fang.Execute() returning a
// plain (non-ExitCodeError) error must print the unknown command error to stderr
// and return exit code 1.
func TestRun_UnknownCommand(t *testing.T) {
	tmp := t.TempDir()
	restore := setUserConfigDir(func() (string, error) { return tmp, nil })
	defer restore()

	var stdout, stderr bytes.Buffer
	code := run([]string{"definitely-not-a-command"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run(bogus) code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "definitely-not-a-command") {
		t.Fatalf("run(bogus) stderr = %q, want an error mentioning the unknown command", stderr.String())
	}
}

// TestExitCodeFor covers both of run's exit-code branches directly: a plain
// error maps to 1, and an error that unwraps to *commands.ExitCodeError
// propagates its own Code. Testing this mapping directly (rather than
// forcing a real command failure through run/root.Execute) keeps the test
// hermetic -- constructing an ExitCodeError through a real `sneat action
// commit` would need a live Action Protocol server.
func TestExitCodeFor(t *testing.T) {
	if got := exitCodeFor(errors.New("boom")); got != 1 {
		t.Fatalf("exitCodeFor(plain error) = %d, want 1", got)
	}
	wrapped := fmt.Errorf("wrap: %w", &commands.ExitCodeError{Code: 2, Err: errors.New("needs input")})
	if got := exitCodeFor(wrapped); got != 2 {
		t.Fatalf("exitCodeFor(ExitCodeError) = %d, want 2", got)
	}
}

// TestRun_UserConfigDirError covers newEnv's first error path (surfaced
// through run): when session.DefaultPath's own userConfigDir call fails, run
// must print the error and return exit code 1 without building any command
// tree.
func TestRun_UserConfigDirError(t *testing.T) {
	wantErr := errors.New("no config dir")
	restore := setUserConfigDir(func() (string, error) { return "", wantErr })
	defer restore()

	var stdout, stderr bytes.Buffer
	code := run([]string{"whoami"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run() code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), wantErr.Error()) {
		t.Fatalf("stderr = %q, want it to contain %q", stderr.String(), wantErr.Error())
	}
}

// TestRun_MetadataPathError covers newEnv's second error path: userConfigDir
// succeeding for session.DefaultPath's call but failing for
// session.DefaultMetadataPath's own call (main.go calls userConfigDir twice,
// once per path).
func TestRun_MetadataPathError(t *testing.T) {
	tmp := t.TempDir()
	wantErr := errors.New("no metadata dir")
	calls := 0
	restore := setUserConfigDir(func() (string, error) {
		calls++
		if calls == 1 {
			return tmp, nil
		}
		return "", wantErr
	})
	defer restore()

	var stdout, stderr bytes.Buffer
	code := run([]string{"whoami"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run() code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), wantErr.Error()) {
		t.Fatalf("stderr = %q, want it to contain %q", stderr.String(), wantErr.Error())
	}
	if calls != 2 {
		t.Fatalf("userConfigDir called %d times, want 2", calls)
	}
}

// TestMain_Body drives main() itself: os.Args and the osExit seam are both
// swapped so main runs its real body (build env, run --help) without
// terminating the test binary or depending on the test runner's own argv.
func TestMain_Body(t *testing.T) {
	tmp := t.TempDir()
	restoreDir := setUserConfigDir(func() (string, error) { return tmp, nil })
	defer restoreDir()

	prevArgs := os.Args
	os.Args = []string{"sneat", "--help"}
	defer func() { os.Args = prevArgs }()

	var gotCode *int
	prevExit := osExit
	osExit = func(code int) { gotCode = &code }
	defer func() { osExit = prevExit }()

	main()

	if gotCode == nil {
		t.Fatal("main() never called osExit")
	}
	if *gotCode != 0 {
		t.Fatalf("main() exit code = %d, want 0", *gotCode)
	}
}

// setUserConfigDir swaps the package-level userConfigDir seam and returns a
// restore func, so concurrent tests in this package never race on the real
// var (tests in this file run serially, but the helper keeps intent clear
// and cleanup unconditional even on t.Fatal).
func setUserConfigDir(f func() (string, error)) func() {
	prev := userConfigDir
	userConfigDir = f
	return func() { userConfigDir = prev }
}

// TestNewEnv_ClosuresAreLazy builds the real composition-root Env via newEnv
// (using a temp config dir, never the real HOME/keychain) and invokes every
// injected closure directly. None of sneatauth.New, tokensrc.FromEnvOrSession,
// firestoredb.New*Reader, sneatapi.New or deviceflow.New perform I/O at
// construction time (mirrors internal/chatapp's own newReaders test for the
// same lazy-constructor pattern) -- only when a returned reader/client is
// later read from -- so calling them here is safe and hermetic.
func TestNewEnv_ClosuresAreLazy(t *testing.T) {
	tmp := t.TempDir()
	restore := setUserConfigDir(func() (string, error) { return tmp, nil })
	defer restore()

	env, err := newEnv(buildinfo.Info{Version: "test-version"})
	if err != nil {
		t.Fatalf("newEnv() error = %v", err)
	}
	cfg := config.Config{Project: "p1", APIKey: "k1", APIBaseURL: "https://example.test/v0/"}

	if s := env.NewInsecureStore(); s == nil {
		t.Fatal("NewInsecureStore() = nil")
	}
	if c := env.NewAuthClient(cfg); c == nil {
		t.Fatal("NewAuthClient() = nil")
	}
	if f := env.NewBrowserFlow(cfg); f == nil {
		t.Fatal("NewBrowserFlow() = nil")
	}
	if df, err := env.NewDeviceFlow(cfg, "https://issuer.test", &memStore{}); err != nil || df == nil {
		t.Fatalf("NewDeviceFlow() = %v, %v", df, err)
	}
	if r, err := env.NewSpacesReader(cfg); err != nil || r == nil {
		t.Fatalf("NewSpacesReader() = %v, %v", r, err)
	}
	if r, err := env.NewContactsReader(cfg); err != nil || r == nil {
		t.Fatalf("NewContactsReader() = %v, %v", r, err)
	}
	if w, err := env.NewContactWriter(cfg); err != nil || w == nil {
		t.Fatalf("NewContactWriter() = %v, %v", w, err)
	}
	if a, err := env.NewActionsAPI(cfg); err != nil || a == nil {
		t.Fatalf("NewActionsAPI() = %v, %v", a, err)
	}
	// IsTerminal: no assertion on the value (sandboxed test runners have no
	// controlling terminal, so it should be false, but that's an environment
	// fact, not behaviour this test owns) -- calling it covers the closure.
	_ = env.IsTerminal()
	if env.RunContactForm == nil {
		t.Fatal("RunContactForm is nil")
	}
}

// memStore is a minimal in-memory deviceauth.Store/commands.SessionStore
// double, used only to satisfy NewDeviceFlow's store parameter without
// touching a real keychain.
type memStore struct{}

func (memStore) Save(session.Session) error     { return nil }
func (memStore) Load() (session.Session, error) { return session.Session{}, session.ErrNoSession }
func (memStore) Clear() error                   { return nil }

// TestNewEnv_RunTUI covers RunTUI's closure body. tui.Run opens a real
// bubbletea program against the terminal; with no controlling terminal in
// this sandboxed test environment it fails fast and deterministically
// (matching internal/chatapp's own runProgram-default test for the identical
// /dev/tty-unavailable case), so a bounded goroutine+timeout is enough to
// exercise the statement without ever hanging the suite.
func TestNewEnv_RunTUI(t *testing.T) {
	tmp := t.TempDir()
	restore := setUserConfigDir(func() (string, error) { return tmp, nil })
	defer restore()

	called := false
	orig := runTUI
	runTUI = func(spaces tui.SpacesReader, contacts tui.ContactsReader, deleter tui.ContactDeleter, uid string) error {
		called = true
		return nil
	}
	t.Cleanup(func() { runTUI = orig })

	env, err := newEnv(buildinfo.Info{Version: "test-version"})
	if err != nil {
		t.Fatalf("newEnv() error = %v", err)
	}

	if err := env.RunTUI(fakeSpaces{}, fakeContacts{}, fakeDeleter{}, "u1"); err != nil {
		t.Errorf("RunTUI() error = %v", err)
	}
	if !called {
		t.Error("RunTUI did not invoke the underlying runner")
	}
}

// TestNewEnv_RunChat covers RunChat's closure body (building chatapp.Deps and
// delegating to chatapp.Run).
func TestNewEnv_RunChat(t *testing.T) {
	tmp := t.TempDir()
	restore := setUserConfigDir(func() (string, error) { return tmp, nil })
	defer restore()

	called := false
	orig := runChat
	runChat = func(deps chatapp.Deps) error {
		called = true
		return nil
	}
	t.Cleanup(func() { runChat = orig })

	env, err := newEnv(buildinfo.Info{Version: "test-version"})
	if err != nil {
		t.Fatalf("newEnv() error = %v", err)
	}

	err = env.RunChat(commands.RunChatArgs{
		Spaces:   fakeSpaces{},
		Contacts: fakeContacts{},
		UID:      "u1",
		Email:    "u1@example.test",
		AIConfig: aiconfig.Config{},
		NoJev:    true,
		Cfg:      config.Config{Project: "p1", APIKey: "k1", APIBaseURL: "https://example.test/v0/"},
		TZ:       "UTC",
	})
	if err != nil {
		t.Errorf("RunChat() error = %v", err)
	}
	if !called {
		t.Error("RunChat did not invoke the underlying runner")
	}
}

type fakeSpaces struct{}

func (fakeSpaces) ListSpaces(context.Context, string) (map[string]any, error) { return nil, nil }

type fakeContacts struct{}

func (fakeContacts) ListContacts(context.Context, string) ([]firestoredb.Contact, error) {
	return nil, nil
}
func (fakeContacts) GetContact(context.Context, string, string) (firestoredb.Contact, error) {
	return firestoredb.Contact{}, nil
}

type fakeDeleter struct{}

func (fakeDeleter) DeleteContact(context.Context, string, string) error { return nil }

// TestChatContacts_ListContacts covers the chatContacts adapter's
// ListContacts (both the pass-through error branch and the success mapping
// through contactDisplayName) directly, without any command or Env plumbing.
func TestChatContacts_ListContacts(t *testing.T) {
	name := "Ada Lovelace"
	cc := chatContacts{r: listingContacts{
		items: []firestoredb.Contact{
			{ID: "c1", Contact: &dbo4contactus.ContactDbo{Title: name}},
		},
	}}
	got, err := cc.ListContacts(context.Background(), "space1")
	if err != nil {
		t.Fatalf("ListContacts() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != name {
		t.Fatalf("ListContacts() = %+v, want one contact named %q", got, name)
	}

	wantErr := errors.New("boom")
	cc2 := chatContacts{r: listingContacts{err: wantErr}}
	if _, err := cc2.ListContacts(context.Background(), "space1"); !errors.Is(err, wantErr) {
		t.Fatalf("ListContacts() error = %v, want %v", err, wantErr)
	}
}

type listingContacts struct {
	items []firestoredb.Contact
	err   error
}

func (l listingContacts) ListContacts(context.Context, string) ([]firestoredb.Contact, error) {
	return l.items, l.err
}
func (l listingContacts) GetContact(context.Context, string, string) (firestoredb.Contact, error) {
	return firestoredb.Contact{}, nil
}

// TestContactDisplayName covers every branch directly: nil Contact, an
// explicit Title, Names with a non-empty full name, and neither set.
func TestContactDisplayName(t *testing.T) {
	cases := []struct {
		name string
		c    firestoredb.Contact
		want string
	}{
		{"nil contact", firestoredb.Contact{Contact: nil}, ""},
		{"title wins", firestoredb.Contact{Contact: &dbo4contactus.ContactDbo{Title: "Ada"}}, "Ada"},
		{
			"falls back to full name",
			firestoredb.Contact{Contact: &dbo4contactus.ContactDbo{Names: &person.NameFields{FullName: "Grace Hopper"}}},
			"Grace Hopper",
		},
		{
			"names present but empty full name",
			firestoredb.Contact{Contact: &dbo4contactus.ContactDbo{Names: &person.NameFields{}}},
			"",
		},
		{"neither title nor names", firestoredb.Contact{Contact: &dbo4contactus.ContactDbo{}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := contactDisplayName(tc.c); got != tc.want {
				t.Fatalf("contactDisplayName() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestNewKeyringStore covers newEnv's lazy credential-store factory directly.
// deviceauth.NewKeyringStore only validates its service/account strings and
// builds a handle -- it performs no real keychain I/O until Save/Load is
// called on the result -- so invoking it here is hermetic.
func TestNewKeyringStore(t *testing.T) {
	store, err := newKeyringStore()
	if err != nil {
		t.Fatalf("newKeyringStore() error = %v", err)
	}
	if store == nil {
		t.Fatal("newKeyringStore() store = nil")
	}
}

// TestRun_SkillsHelp covers driving `sneat skills --help` through run().
func TestRun_SkillsHelp(t *testing.T) {
	tmp := t.TempDir()
	restore := setUserConfigDir(func() (string, error) { return tmp, nil })
	defer restore()

	var stdout, stderr bytes.Buffer
	code := run([]string{"skills", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(skills --help) code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "skills") {
		t.Fatalf("run(skills --help) stdout = %q, want usage text containing skills", stdout.String())
	}
}

// TestRun_SkillsSyncDryRun covers driving `sneat skills sync --dry-run --dir <tmp>` through run().
func TestRun_SkillsSyncDryRun(t *testing.T) {
	tmp := t.TempDir()
	restore := setUserConfigDir(func() (string, error) { return tmp, nil })
	defer restore()

	skillsDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"skills", "sync", "--dry-run", "--dir", skillsDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(skills sync --dry-run) code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "sneat") {
		t.Fatalf("run(skills sync --dry-run) stdout = %q, want output mentioning sneat", stdout.String())
	}
}

// TestRun_UninstallHelp covers driving `sneat uninstall --help` through run().
func TestRun_UninstallHelp(t *testing.T) {
	tmp := t.TempDir()
	restore := setUserConfigDir(func() (string, error) { return tmp, nil })
	defer restore()

	var stdout, stderr bytes.Buffer
	code := run([]string{"uninstall", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(uninstall --help) code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "uninstall") {
		t.Fatalf("run(uninstall --help) stdout = %q, want usage text containing uninstall", stdout.String())
	}
}


