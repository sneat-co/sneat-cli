# Sneat CLI Phase 1: Auth Foundation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn this repo into a cobra-based `sneat` CLI that can sign in headlessly (email+password) against Firebase `sneat-eur3-1` or its Auth emulator, persist the session, and report the current user as JSON.

**Architecture:** Thin `cmd/sneat/main.go` wires real dependencies into cobra commands built by `cmd/sneat/commands`. Commands take injected collaborators (session store, auth-client factory, `getenv`, `now`) for testability. Firebase Auth REST lives in `internal/sneatauth`; config resolution in `internal/config`; the 0600 token file in `internal/session`.

**Tech Stack:** Go 1.26.4, `github.com/spf13/cobra`, stdlib `net/http`/`encoding/json`. No viper. Browser sign-in, Firestore/DALgo reads, and contact writes are later phases.

## Global Constraints

- Module path becomes `github.com/sneat-co/sneat-cli` (was `github.com/sneat-co/sneat-tui`).
- Default project `sneat-eur3-1`; default web API key `AIzaSyCeQu1WC182yD0VHrRm4nHUxVf27fY-MLQ`.
- Config precedence is **flag > env > default** everywhere.
- All command data output is JSON (2-space indent) on stdout; errors on stderr; non-zero exit on failure.
- Session file is `os.UserConfigDir()/sneat/session.json`, dir `0700`, file `0600`.
- Every command constructor takes injected dependencies — no globals, no `init()` registration.
- Tests accompany every task; keep statement coverage ≥ 80% (repo norm).

---

### Task 1: Rename Go module to `sneat-cli`

**Files:**
- Modify: `go.mod:1`
- Modify: `main.go:5` (import path)
- Modify: `README.md:1`

**Interfaces:**
- Consumes: nothing.
- Produces: module path `github.com/sneat-co/sneat-cli` for all later tasks' imports.

- [ ] **Step 1: Change the module line**

In `go.mod` line 1: `module github.com/sneat-co/sneat-tui` → `module github.com/sneat-co/sneat-cli`.

- [ ] **Step 2: Fix the internal import in main.go**

In `main.go`, change `"github.com/sneat-co/sneat-tui/sneatui"` → `"github.com/sneat-co/sneat-cli/sneatui"`.

- [ ] **Step 3: Update README title**

In `README.md` line 1: `# sneat-tui` → `# sneat-cli`.

- [ ] **Step 4: Verify build and tests**

Run: `go build ./... && go test ./...`
Expected: builds; existing tests PASS.

- [ ] **Step 5: Commit**

```bash
git add go.mod main.go README.md
git commit -m "refactor: rename module sneat-tui -> sneat-cli"
```

---

### Task 2: Remove the TUI, add the cobra skeleton + JSON output

Replaces the tview app with a cobra root command and a `version` subcommand, and establishes the JSON output helper. Deletes the `sneatui` package and drops `tcell`/`tview`.

**Files:**
- Delete: `main.go`, `main_test.go`, `sneatui/` (whole directory)
- Create: `cmd/sneat/main.go`
- Create: `cmd/sneat/commands/root.go`
- Create: `cmd/sneat/commands/output.go`
- Create: `cmd/sneat/commands/output_test.go`
- Create: `cmd/sneat/commands/version.go`
- Create: `cmd/sneat/commands/version_test.go`
- Modify: `go.mod`, `go.sum` (via `go mod tidy`)

**Interfaces:**
- Produces:
  - `commands.Root(env Env) *cobra.Command` — root command; `Env` is extended in Task 6, so define it minimally now: `type Env struct{ Getenv func(string) string; Now func() time.Time }`.
  - `commands.writeJSON(w io.Writer, v any) error` — indented JSON + trailing newline.
  - `commands.Version(ver, commit, date string) *cobra.Command`.

- [ ] **Step 1: Delete the TUI code**

```bash
git rm -r sneatui main.go main_test.go
```

- [ ] **Step 2: Write the failing output test**

Create `cmd/sneat/commands/output_test.go`:

```go
package commands

import (
	"bytes"
	"testing"
)

func TestWriteJSON_IndentsAndNewline(t *testing.T) {
	var buf bytes.Buffer
	if err := writeJSON(&buf, map[string]string{"a": "b"}); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	want := "{\n  \"a\": \"b\"\n}\n"
	if buf.String() != want {
		t.Fatalf("got %q, want %q", buf.String(), want)
	}
}
```

- [ ] **Step 3: Run it, expect failure**

Run: `go test ./cmd/sneat/commands/ -run TestWriteJSON -v`
Expected: FAIL (package/`writeJSON` undefined).

- [ ] **Step 4: Implement output.go**

Create `cmd/sneat/commands/output.go`:

```go
package commands

import (
	"encoding/json"
	"io"
)

// writeJSON encodes v as indented JSON (2 spaces) followed by a newline.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
```

- [ ] **Step 5: Implement root.go**

Create `cmd/sneat/commands/root.go`:

```go
package commands

import (
	"time"

	"github.com/spf13/cobra"
)

// Env holds injected process dependencies so commands stay unit-testable.
// Extended in later tasks (session store, auth-client factory, browser opener).
type Env struct {
	Getenv func(string) string
	Now    func() time.Time
}

// Root builds the top-level `sneat` command.
func Root(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "sneat",
		Short:         "Sneat.app command-line interface",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	// Persistent config flags (resolved in Task 3's configFromCmd helper).
	cmd.PersistentFlags().String("project", "", "Firebase project id (default sneat-eur3-1)")
	cmd.PersistentFlags().String("api-key", "", "Firebase web API key")
	cmd.PersistentFlags().String("api-base-url", "", "sneat-go API base URL")
	cmd.PersistentFlags().String("auth-emulator", "", "Firebase Auth emulator host, e.g. localhost:9099")
	cmd.PersistentFlags().String("firestore-emulator", "", "Firestore emulator host, e.g. localhost:8080")
	cmd.PersistentFlags().Bool("emulator", false, "use local Auth+Firestore emulators on default ports")
	return cmd
}
```

- [ ] **Step 6: Write the failing version test**

Create `cmd/sneat/commands/version_test.go`:

```go
package commands

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestVersionCommand_PrintsJSON(t *testing.T) {
	cmd := Version("1.2.3", "abc123", "2026-07-08")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output not JSON: %v (%q)", err, buf.String())
	}
	if got["version"] != "1.2.3" {
		t.Fatalf("version = %q, want 1.2.3", got["version"])
	}
}
```

- [ ] **Step 7: Run it, expect failure**

Run: `go test ./cmd/sneat/commands/ -run TestVersion -v`
Expected: FAIL (`Version` undefined).

- [ ] **Step 8: Implement version.go**

Create `cmd/sneat/commands/version.go`:

```go
package commands

import "github.com/spf13/cobra"

// Version prints build metadata as JSON. Values are injected via -ldflags.
func Version(ver, commit, date string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build version, commit hash, and build date",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeJSON(cmd.OutOrStdout(), map[string]string{
				"version": ver, "commit": commit, "date": date,
			})
		},
	}
}
```

- [ ] **Step 9: Implement main.go**

Create `cmd/sneat/main.go`:

```go
package main

import (
	"os"
	"time"

	"github.com/sneat-co/sneat-cli/cmd/sneat/commands"
)

// Build metadata, overridable via -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	env := commands.Env{Getenv: os.Getenv, Now: time.Now}
	root := commands.Root(env)
	root.AddCommand(commands.Version(version, commit, date))
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
```

- [ ] **Step 10: Tidy dependencies and verify**

Run: `go mod tidy && go build ./... && go test ./... && go run ./cmd/sneat version`
Expected: `tcell`/`tview` removed from `go.mod`; build + tests PASS; `version` prints JSON.

- [ ] **Step 11: Commit**

```bash
git add -A
git commit -m "feat: replace TUI with cobra CLI skeleton and version command"
```

---

### Task 3: internal/config resolution

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `cmd/sneat/commands/config.go` (the `configFromCmd` helper)

**Interfaces:**
- Produces:
  - `config.Config{ Project, APIKey, AuthDomain, APIBaseURL, AuthEmulatorHost, FirestoreEmulatorHost string }`
  - `config.Resolve(o config.Overrides, getenv func(string) string) config.Config`
  - `config.Overrides{ Project, APIKey, AuthDomain, APIBaseURL, AuthEmulatorHost, FirestoreEmulatorHost string; Emulator bool }`
  - `commands.configFromCmd(cmd *cobra.Command, getenv func(string) string) config.Config`

- [ ] **Step 1: Write the failing test**

Create `internal/config/config_test.go`:

```go
package config

import "testing"

func TestResolve_DefaultsWhenEmpty(t *testing.T) {
	c := Resolve(Overrides{}, func(string) string { return "" })
	if c.Project != DefaultProject {
		t.Fatalf("project = %q, want %q", c.Project, DefaultProject)
	}
	if c.APIBaseURL != DefaultAPIBaseURL {
		t.Fatalf("apiBaseURL = %q", c.APIBaseURL)
	}
	if c.AuthEmulatorHost != "" {
		t.Fatalf("authEmulatorHost = %q, want empty", c.AuthEmulatorHost)
	}
}

func TestResolve_FlagBeatsEnvBeatsDefault(t *testing.T) {
	env := map[string]string{"SNEAT_FIREBASE_PROJECT": "from-env"}
	getenv := func(k string) string { return env[k] }

	c := Resolve(Overrides{}, getenv)
	if c.Project != "from-env" {
		t.Fatalf("env not applied: %q", c.Project)
	}
	c = Resolve(Overrides{Project: "from-flag"}, getenv)
	if c.Project != "from-flag" {
		t.Fatalf("flag did not beat env: %q", c.Project)
	}
}

func TestResolve_EmulatorConvenienceSetsHosts(t *testing.T) {
	c := Resolve(Overrides{Emulator: true}, func(string) string { return "" })
	if c.AuthEmulatorHost != DefaultAuthEmulatorHost {
		t.Fatalf("authEmulatorHost = %q", c.AuthEmulatorHost)
	}
	if c.FirestoreEmulatorHost != DefaultFirestoreEmulatorHost {
		t.Fatalf("firestoreEmulatorHost = %q", c.FirestoreEmulatorHost)
	}
}
```

- [ ] **Step 2: Run it, expect failure**

Run: `go test ./internal/config/ -v`
Expected: FAIL (package undefined).

- [ ] **Step 3: Implement config.go**

Create `internal/config/config.go`:

```go
package config

const (
	DefaultProject               = "sneat-eur3-1"
	DefaultAPIKey                = "AIzaSyCeQu1WC182yD0VHrRm4nHUxVf27fY-MLQ"
	DefaultAuthDomain            = "sneat-eur3-1.firebaseapp.com"
	DefaultAPIBaseURL            = "https://api.sneat.cloud/v0/"
	DefaultAuthEmulatorHost      = "localhost:9099"
	DefaultFirestoreEmulatorHost = "localhost:8080"
)

type Config struct {
	Project               string
	APIKey                string
	AuthDomain            string
	APIBaseURL            string
	AuthEmulatorHost      string
	FirestoreEmulatorHost string
}

// Overrides are the flag-supplied values (empty string / false = not set).
type Overrides struct {
	Project               string
	APIKey                string
	AuthDomain            string
	APIBaseURL            string
	AuthEmulatorHost      string
	FirestoreEmulatorHost string
	Emulator              bool
}

func pick(flag, env, def string) string {
	if flag != "" {
		return flag
	}
	if env != "" {
		return env
	}
	return def
}

// Resolve applies precedence flag > env > default for each field.
func Resolve(o Overrides, getenv func(string) string) Config {
	c := Config{
		Project:               pick(o.Project, getenv("SNEAT_FIREBASE_PROJECT"), DefaultProject),
		APIKey:                pick(o.APIKey, getenv("FIREBASE_API_KEY"), DefaultAPIKey),
		AuthDomain:            pick(o.AuthDomain, getenv("FIREBASE_AUTH_DOMAIN"), DefaultAuthDomain),
		APIBaseURL:            pick(o.APIBaseURL, getenv("SNEAT_API_BASE_URL"), DefaultAPIBaseURL),
		AuthEmulatorHost:      pick(o.AuthEmulatorHost, getenv("FIREBASE_AUTH_EMULATOR_HOST"), ""),
		FirestoreEmulatorHost: pick(o.FirestoreEmulatorHost, getenv("FIRESTORE_EMULATOR_HOST"), ""),
	}
	if o.Emulator {
		if c.AuthEmulatorHost == "" {
			c.AuthEmulatorHost = DefaultAuthEmulatorHost
		}
		if c.FirestoreEmulatorHost == "" {
			c.FirestoreEmulatorHost = DefaultFirestoreEmulatorHost
		}
	}
	return c
}
```

- [ ] **Step 4: Run it, expect pass**

Run: `go test ./internal/config/ -v`
Expected: PASS.

- [ ] **Step 5: Implement configFromCmd helper**

Create `cmd/sneat/commands/config.go`:

```go
package commands

import (
	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/spf13/cobra"
)

// configFromCmd reads the root persistent flags and resolves config.
func configFromCmd(cmd *cobra.Command, getenv func(string) string) config.Config {
	f := cmd.Flags()
	s := func(name string) string { v, _ := f.GetString(name); return v }
	emulator, _ := f.GetBool("emulator")
	return config.Resolve(config.Overrides{
		Project:               s("project"),
		APIKey:                s("api-key"),
		APIBaseURL:            s("api-base-url"),
		AuthEmulatorHost:      s("auth-emulator"),
		FirestoreEmulatorHost: s("firestore-emulator"),
		Emulator:              emulator,
	}, getenv)
}
```

- [ ] **Step 6: Verify and commit**

Run: `go build ./... && go test ./...`
Expected: PASS.

```bash
git add -A
git commit -m "feat: config resolution with flag>env>default precedence"
```

---

### Task 4: internal/session token store

**Files:**
- Create: `internal/session/session.go`
- Create: `internal/session/session_test.go`

**Interfaces:**
- Produces:
  - `session.Session{ Project, UID, Email, IDToken, RefreshToken string; ExpiresAt time.Time }` (JSON tags: `project,uid,email,idToken,refreshToken,expiresAt`)
  - `session.NewStore(path string) *session.Store`
  - `session.DefaultPath(userConfigDir func() (string, error)) (string, error)` → `<dir>/sneat/session.json`
  - `(*Store).Save(Session) error`, `(*Store).Load() (Session, error)`, `(*Store).Clear() error`
  - `session.ErrNoSession`

- [ ] **Step 1: Write the failing test**

Create `internal/session/session_test.go`:

```go
package session

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_SaveLoadClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sneat", "session.json")
	s := NewStore(path)

	if _, err := s.Load(); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Load on empty = %v, want ErrNoSession", err)
	}

	want := Session{
		Project: "sneat-eur3-1", UID: "u1", Email: "a@b.c",
		IDToken: "idtok", RefreshToken: "reftok",
		ExpiresAt: time.Now().Add(time.Hour).Round(time.Second),
	}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.UID != want.UID || got.IDToken != want.IDToken || !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Fatalf("roundtrip mismatch: %+v vs %+v", got, want)
	}
	if err := s.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, err := s.Load(); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Load after Clear = %v, want ErrNoSession", err)
	}
}

func TestStore_FilePermIs0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	s := NewStore(path)
	if err := s.Save(Session{UID: "x"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := statFile(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("perm = %o, want 600", perm)
	}
}

func TestDefaultPath(t *testing.T) {
	p, err := DefaultPath(func() (string, error) { return "/cfg", nil })
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if p != "/cfg/sneat/session.json" {
		t.Fatalf("path = %q", p)
	}
}
```

- [ ] **Step 2: Run it, expect failure**

Run: `go test ./internal/session/ -v`
Expected: FAIL (undefined).

- [ ] **Step 3: Implement session.go**

Create `internal/session/session.go`:

```go
package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// ErrNoSession is returned by Load when no session file exists.
var ErrNoSession = errors.New("no session; run 'sneat auth login'")

// Session is the persisted authentication state.
type Session struct {
	Project      string    `json:"project"`
	UID          string    `json:"uid"`
	Email        string    `json:"email"`
	IDToken      string    `json:"idToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

// Store reads/writes a Session as a 0600 JSON file.
type Store struct{ path string }

func NewStore(path string) *Store { return &Store{path: path} }

// DefaultPath returns <userConfigDir>/sneat/session.json.
func DefaultPath(userConfigDir func() (string, error)) (string, error) {
	dir, err := userConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sneat", "session.json"), nil
}

func (s *Store) Save(sess Session) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

func (s *Store) Load() (Session, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Session{}, ErrNoSession
	}
	if err != nil {
		return Session{}, err
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return Session{}, err
	}
	return sess, nil
}

func (s *Store) Clear() error {
	err := os.Remove(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// statFile is a test seam over os.Stat.
var statFile = os.Stat
```

- [ ] **Step 4: Run it, expect pass**

Run: `go test ./internal/session/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/session
git commit -m "feat: session token store (0600 JSON)"
```

---

### Task 5: internal/sneatauth Firebase Auth REST client

**Files:**
- Create: `internal/sneatauth/sneatauth.go`
- Create: `internal/sneatauth/sneatauth_test.go`

**Interfaces:**
- Produces:
  - `sneatauth.New(o sneatauth.Options) *sneatauth.Client` where `Options{ APIKey, AuthEmulatorHost string; HTTPClient *http.Client }`
  - `sneatauth.Result{ IDToken, RefreshToken, UID, Email string; ExpiresIn time.Duration }`
  - `(*Client).SignInWithPassword(ctx context.Context, email, password string) (Result, error)`
  - `(*Client).Refresh(ctx context.Context, refreshToken string) (Result, error)`
- Consumes: nothing (base URLs derived from `Options`).

Notes: prod bases are `https://identitytoolkit.googleapis.com/v1` and `https://securetoken.googleapis.com/v1`; when `AuthEmulatorHost` is set they become `http://<host>/identitytoolkit.googleapis.com/v1` and `http://<host>/securetoken.googleapis.com/v1`. The tests override the base fields directly (exported for tests via `newWithBases`).

- [ ] **Step 1: Write the failing test**

Create `internal/sneatauth/sneatauth_test.go`:

```go
package sneatauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSignInWithPassword_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "accounts:signInWithPassword") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "test-key" {
			t.Errorf("missing api key")
		}
		_, _ = w.Write([]byte(`{"idToken":"idt","refreshToken":"rft","localId":"u1","email":"a@b.c","expiresIn":"3600"}`))
	}))
	defer srv.Close()

	c := newWithBases(srv.URL, srv.URL, "test-key", srv.Client())
	res, err := c.SignInWithPassword(context.Background(), "a@b.c", "pw")
	if err != nil {
		t.Fatalf("SignInWithPassword: %v", err)
	}
	if res.IDToken != "idt" || res.UID != "u1" || res.Email != "a@b.c" {
		t.Fatalf("bad result: %+v", res)
	}
	if res.ExpiresIn.Seconds() != 3600 {
		t.Fatalf("expiresIn = %v", res.ExpiresIn)
	}
}

func TestSignInWithPassword_ErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"INVALID_PASSWORD"}}`))
	}))
	defer srv.Close()

	c := newWithBases(srv.URL, srv.URL, "k", srv.Client())
	if _, err := c.SignInWithPassword(context.Background(), "a@b.c", "bad"); err == nil ||
		!strings.Contains(err.Error(), "INVALID_PASSWORD") {
		t.Fatalf("err = %v, want INVALID_PASSWORD", err)
	}
}

func TestRefresh_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id_token":"idt2","refresh_token":"rft2","user_id":"u1","expires_in":"3600"}`))
	}))
	defer srv.Close()

	c := newWithBases(srv.URL, srv.URL, "k", srv.Client())
	res, err := c.Refresh(context.Background(), "rft")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if res.IDToken != "idt2" || res.RefreshToken != "rft2" || res.UID != "u1" {
		t.Fatalf("bad result: %+v", res)
	}
}

func TestNew_EmulatorBases(t *testing.T) {
	c := New(Options{APIKey: "k", AuthEmulatorHost: "localhost:9099"})
	if !strings.HasPrefix(c.identityBase, "http://localhost:9099/identitytoolkit.googleapis.com") {
		t.Fatalf("identityBase = %q", c.identityBase)
	}
	if !strings.HasPrefix(c.secureTokenBase, "http://localhost:9099/securetoken.googleapis.com") {
		t.Fatalf("secureTokenBase = %q", c.secureTokenBase)
	}
}
```

- [ ] **Step 2: Run it, expect failure**

Run: `go test ./internal/sneatauth/ -v`
Expected: FAIL (undefined).

- [ ] **Step 3: Implement sneatauth.go**

Create `internal/sneatauth/sneatauth.go`:

```go
package sneatauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	http            *http.Client
	apiKey          string
	identityBase    string
	secureTokenBase string
}

type Options struct {
	APIKey           string
	AuthEmulatorHost string
	HTTPClient       *http.Client
}

func New(o Options) *Client {
	identity := "https://identitytoolkit.googleapis.com/v1"
	secure := "https://securetoken.googleapis.com/v1"
	if o.AuthEmulatorHost != "" {
		identity = "http://" + o.AuthEmulatorHost + "/identitytoolkit.googleapis.com/v1"
		secure = "http://" + o.AuthEmulatorHost + "/securetoken.googleapis.com/v1"
	}
	return newWithBases(identity, secure, o.APIKey, o.HTTPClient)
}

func newWithBases(identityBase, secureTokenBase, apiKey string, hc *http.Client) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{http: hc, apiKey: apiKey, identityBase: identityBase, secureTokenBase: secureTokenBase}
}

// Result is a normalized Firebase auth response.
type Result struct {
	IDToken      string
	RefreshToken string
	UID          string
	Email        string
	ExpiresIn    time.Duration
}

func (c *Client) SignInWithPassword(ctx context.Context, email, password string) (Result, error) {
	body, _ := json.Marshal(map[string]any{"email": email, "password": password, "returnSecureToken": true})
	u := c.identityBase + "/accounts:signInWithPassword?key=" + url.QueryEscape(c.apiKey)
	var out struct {
		IDToken      string `json:"idToken"`
		RefreshToken string `json:"refreshToken"`
		LocalID      string `json:"localId"`
		Email        string `json:"email"`
		ExpiresIn    string `json:"expiresIn"`
	}
	if err := c.doJSON(ctx, u, "application/json", bytes.NewReader(body), &out); err != nil {
		return Result{}, err
	}
	return Result{
		IDToken: out.IDToken, RefreshToken: out.RefreshToken, UID: out.LocalID,
		Email: out.Email, ExpiresIn: parseSeconds(out.ExpiresIn),
	}, nil
}

func (c *Client) Refresh(ctx context.Context, refreshToken string) (Result, error) {
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}}
	u := c.secureTokenBase + "/token?key=" + url.QueryEscape(c.apiKey)
	var out struct {
		IDToken      string `json:"id_token"`
		RefreshToken string `json:"refresh_token"`
		UserID       string `json:"user_id"`
		ExpiresIn    string `json:"expires_in"`
	}
	if err := c.doJSON(ctx, u, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()), &out); err != nil {
		return Result{}, err
	}
	return Result{
		IDToken: out.IDToken, RefreshToken: out.RefreshToken, UID: out.UserID,
		ExpiresIn: parseSeconds(out.ExpiresIn),
	}, nil
}

func (c *Client) doJSON(ctx context.Context, url, contentType string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		if e.Error.Message != "" {
			return fmt.Errorf("firebase auth: %s", e.Error.Message)
		}
		return fmt.Errorf("firebase auth: http %d", resp.StatusCode)
	}
	return json.Unmarshal(data, out)
}

func parseSeconds(s string) time.Duration {
	n, _ := strconv.Atoi(s)
	return time.Duration(n) * time.Second
}
```

- [ ] **Step 4: Run it, expect pass**

Run: `go test ./internal/sneatauth/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sneatauth
git commit -m "feat: Firebase Auth REST client (signInWithPassword, refresh)"
```

---

### Task 6: auth login/logout + whoami commands

Wires config + auth client + session store into `sneat auth login|logout` and `sneat whoami`.

**Files:**
- Modify: `cmd/sneat/commands/root.go` (extend `Env`)
- Create: `cmd/sneat/commands/auth.go`
- Create: `cmd/sneat/commands/auth_test.go`
- Create: `cmd/sneat/commands/whoami.go`
- Create: `cmd/sneat/commands/whoami_test.go`
- Modify: `cmd/sneat/main.go` (wire real deps + register commands)

**Interfaces:**
- Consumes: `configFromCmd` (Task 3), `session.Session`/`session.Store` (Task 4), `sneatauth.Result` (Task 5), `writeJSON` (Task 2).
- Produces:
  - Extended `Env{ Getenv func(string) string; Now func() time.Time; Store SessionStore; NewAuthClient func(config.Config) AuthClient }`
  - `type SessionStore interface { Save(session.Session) error; Load() (session.Session, error); Clear() error }`
  - `type AuthClient interface { SignInWithPassword(ctx context.Context, email, password string) (sneatauth.Result, error); Refresh(ctx context.Context, refreshToken string) (sneatauth.Result, error) }`
  - `commands.Auth(env Env) *cobra.Command`, `commands.Whoami(env Env) *cobra.Command`

- [ ] **Step 1: Extend Env and declare seams in root.go**

Replace `cmd/sneat/commands/root.go` `Env` and add interfaces:

```go
package commands

import (
	"context"
	"time"

	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/sneat-co/sneat-cli/internal/sneatauth"
	"github.com/spf13/cobra"
)

// SessionStore persists the authenticated session.
type SessionStore interface {
	Save(session.Session) error
	Load() (session.Session, error)
	Clear() error
}

// AuthClient performs Firebase auth REST calls.
type AuthClient interface {
	SignInWithPassword(ctx context.Context, email, password string) (sneatauth.Result, error)
	Refresh(ctx context.Context, refreshToken string) (sneatauth.Result, error)
}

// Env holds injected process dependencies so commands stay unit-testable.
type Env struct {
	Getenv        func(string) string
	Now           func() time.Time
	Store         SessionStore
	NewAuthClient func(cfg config.Config) AuthClient
}

// Root builds the top-level `sneat` command.
func Root(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "sneat",
		Short:         "Sneat.app command-line interface",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.PersistentFlags().String("project", "", "Firebase project id (default sneat-eur3-1)")
	cmd.PersistentFlags().String("api-key", "", "Firebase web API key")
	cmd.PersistentFlags().String("api-base-url", "", "sneat-go API base URL")
	cmd.PersistentFlags().String("auth-emulator", "", "Firebase Auth emulator host, e.g. localhost:9099")
	cmd.PersistentFlags().String("firestore-emulator", "", "Firestore emulator host, e.g. localhost:8080")
	cmd.PersistentFlags().Bool("emulator", false, "use local Auth+Firestore emulators on default ports")
	return cmd
}
```

- [ ] **Step 2: Write the failing auth + whoami tests**

Create `cmd/sneat/commands/auth_test.go`:

```go
package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/sneat-co/sneat-cli/internal/sneatauth"
)

type fakeStore struct {
	saved   *session.Session
	cleared bool
	load    *session.Session
}

func (f *fakeStore) Save(s session.Session) error { f.saved = &s; return nil }
func (f *fakeStore) Load() (session.Session, error) {
	if f.load == nil {
		return session.Session{}, session.ErrNoSession
	}
	return *f.load, nil
}
func (f *fakeStore) Clear() error { f.cleared = true; return nil }

type fakeAuth struct{ res sneatauth.Result }

func (f fakeAuth) SignInWithPassword(_ context.Context, _, _ string) (sneatauth.Result, error) {
	return f.res, nil
}
func (f fakeAuth) Refresh(_ context.Context, _ string) (sneatauth.Result, error) {
	return f.res, nil
}

func testEnv(store SessionStore, res sneatauth.Result) Env {
	return Env{
		Getenv: func(string) string { return "" },
		Now:    func() time.Time { return time.Unix(1000, 0) },
		Store:  store,
		NewAuthClient: func(config.Config) AuthClient { return fakeAuth{res: res} },
	}
}

func TestAuthLogin_SavesSessionAndPrintsUser(t *testing.T) {
	store := &fakeStore{}
	env := testEnv(store, sneatauth.Result{
		IDToken: "idt", RefreshToken: "rft", UID: "u1", Email: "a@b.c", ExpiresIn: time.Hour,
	})
	root := Root(env)
	root.AddCommand(Auth(env))
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"auth", "login", "--email", "a@b.c", "--password", "pw"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if store.saved == nil || store.saved.UID != "u1" || store.saved.IDToken != "idt" {
		t.Fatalf("session not saved: %+v", store.saved)
	}
	if !store.saved.ExpiresAt.Equal(time.Unix(1000, 0).Add(time.Hour)) {
		t.Fatalf("expiresAt = %v", store.saved.ExpiresAt)
	}
	var got map[string]string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output not JSON: %v (%q)", err, buf.String())
	}
	if got["email"] != "a@b.c" {
		t.Fatalf("email = %q", got["email"])
	}
}

func TestAuthLogout_ClearsSession(t *testing.T) {
	store := &fakeStore{}
	env := testEnv(store, sneatauth.Result{})
	root := Root(env)
	root.AddCommand(Auth(env))
	root.SetArgs([]string{"auth", "logout"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !store.cleared {
		t.Fatalf("store.Clear not called")
	}
}
```

Create `cmd/sneat/commands/whoami_test.go`:

```go
package commands

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/sneat-co/sneat-cli/internal/sneatauth"
)

func TestWhoami_PrintsSession(t *testing.T) {
	store := &fakeStore{load: &session.Session{UID: "u1", Email: "a@b.c", Project: "sneat-eur3-1"}}
	env := testEnv(store, sneatauth.Result{})
	root := Root(env)
	root.AddCommand(Whoami(env))
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"whoami"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if got["uid"] != "u1" || got["project"] != "sneat-eur3-1" {
		t.Fatalf("got %+v", got)
	}
}

func TestWhoami_NoSession(t *testing.T) {
	env := testEnv(&fakeStore{}, sneatauth.Result{})
	root := Root(env)
	root.AddCommand(Whoami(env))
	root.SetArgs([]string{"whoami"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected error when not signed in")
	}
}
```

- [ ] **Step 3: Run tests, expect failure**

Run: `go test ./cmd/sneat/commands/ -run 'Auth|Whoami' -v`
Expected: FAIL (`Auth`/`Whoami` undefined).

- [ ] **Step 4: Implement auth.go**

Create `cmd/sneat/commands/auth.go`:

```go
package commands

import (
	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/spf13/cobra"
)

// Auth builds the `sneat auth` command group.
func Auth(env Env) *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Sign in and out of Sneat.app"}
	cmd.AddCommand(authLogin(env), authLogout(env))
	return cmd
}

func authLogin(env Env) *cobra.Command {
	var email, password string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in (email+password; browser flow added later)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := configFromCmd(cmd, env.Getenv)
			ac := env.NewAuthClient(cfg)
			res, err := ac.SignInWithPassword(cmd.Context(), email, password)
			if err != nil {
				return err
			}
			sess := session.Session{
				Project: cfg.Project, UID: res.UID, Email: res.Email,
				IDToken: res.IDToken, RefreshToken: res.RefreshToken,
				ExpiresAt: env.Now().Add(res.ExpiresIn),
			}
			if err := env.Store.Save(sess); err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), map[string]string{
				"uid": res.UID, "email": res.Email, "project": cfg.Project,
			})
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "account email")
	cmd.Flags().StringVar(&password, "password", "", "account password")
	_ = cmd.MarkFlagRequired("email")
	_ = cmd.MarkFlagRequired("password")
	return cmd
}

func authLogout(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Clear the stored session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return env.Store.Clear()
		},
	}
}
```

- [ ] **Step 5: Implement whoami.go**

Create `cmd/sneat/commands/whoami.go`:

```go
package commands

import "github.com/spf13/cobra"

// Whoami prints the currently signed-in user as JSON.
func Whoami(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Print the currently signed-in user",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sess, err := env.Store.Load()
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), map[string]string{
				"uid": sess.UID, "email": sess.Email, "project": sess.Project,
			})
		},
	}
}
```

- [ ] **Step 6: Run tests, expect pass**

Run: `go test ./cmd/sneat/commands/ -v`
Expected: PASS.

- [ ] **Step 7: Wire real dependencies in main.go**

Replace `cmd/sneat/main.go`:

```go
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/sneat-co/sneat-cli/cmd/sneat/commands"
	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/sneat-co/sneat-cli/internal/sneatauth"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	path, err := session.DefaultPath(os.UserConfigDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sneat:", err)
		os.Exit(1)
	}
	env := commands.Env{
		Getenv: os.Getenv,
		Now:    time.Now,
		Store:  session.NewStore(path),
		NewAuthClient: func(cfg config.Config) commands.AuthClient {
			return sneatauth.New(sneatauth.Options{APIKey: cfg.APIKey, AuthEmulatorHost: cfg.AuthEmulatorHost})
		},
	}
	root := commands.Root(env)
	root.AddCommand(
		commands.Version(version, commit, date),
		commands.Auth(env),
		commands.Whoami(env),
	)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "sneat:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 8: Full build, vet, test**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS.

- [ ] **Step 9: Manual smoke against the Auth emulator (optional but recommended)**

```bash
# In another terminal: firebase emulators:start --only auth --project sneat-eur3-1
# Create a test user in the emulator UI, then:
go run ./cmd/sneat --emulator auth login --email test@example.com --password secret123
go run ./cmd/sneat whoami
```
Expected: login prints `{uid,email,project}`; whoami prints the same user.

- [ ] **Step 10: Commit**

```bash
git add -A
git commit -m "feat: sneat auth login/logout and whoami commands"
```

---

## Self-Review

**Spec coverage (Phase 1 scope):** CLI skeleton + JSON output (Task 2) ✓; config precedence §4 (Task 3) ✓; session store §6.3 (Task 4) ✓; email/password sign-in §6.2 + refresh §6.3 (Task 5) ✓; `auth login`/`logout`/`whoami` §1 (Task 6) ✓; module rename + TUI removal §3 (Tasks 1–2) ✓. Out of Phase-1 scope by design: browser sign-in §6.1, Firestore/DALgo reads §5, contact writes §7 — these are later plans.

**Placeholder scan:** none — every step has concrete code/commands.

**Type consistency:** `Env`, `SessionStore`, `AuthClient`, `sneatauth.Result`, `session.Session`, `config.Config`, `writeJSON`, `configFromCmd` names match across Tasks 2–6. `Env` is intentionally defined minimally in Task 2 and extended in Task 6 (Task 6 Step 1 replaces the file wholesale).

**Note:** `Refresh` on `AuthClient` is unused in Phase 1 (it's exercised by `internal/tokensrc` in Phase 2) but is declared now so the interface is stable; `sneatauth` already implements it and is tested in Task 5.
