package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/sneat-co/sneat-cli/internal/browserauth"
	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/sneat-co/sneat-cli/internal/sneatauth"
)

type fakeDeviceFlow struct {
	res       sneatauth.Result
	logoutErr error
}

func (f fakeDeviceFlow) Run(context.Context, io.Writer, io.Writer) (sneatauth.Result, error) {
	return f.res, nil
}
func (f fakeDeviceFlow) Logout(context.Context) error { return f.logoutErr }

type fakeBrowserFlow struct{ res browserauth.Result }

func (f fakeBrowserFlow) Run(context.Context) (browserauth.Result, error) { return f.res, nil }

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

type failingStore struct{ called bool }

func (f *failingStore) Save(session.Session) error {
	f.called = true
	return errors.New("keyring unavailable")
}
func (f *failingStore) Load() (session.Session, error) {
	f.called = true
	return session.Session{}, errors.New("keyring unavailable")
}
func (f *failingStore) Clear() error { f.called = true; return errors.New("keyring unavailable") }

type fakeAuth struct{ res sneatauth.Result }

func (f fakeAuth) SignInWithPassword(_ context.Context, _, _ string) (sneatauth.Result, error) {
	return f.res, nil
}
func (f fakeAuth) Refresh(_ context.Context, _ string) (sneatauth.Result, error) {
	return f.res, nil
}

func testEnv(store SessionStore, res sneatauth.Result) Env {
	return Env{
		Getenv:        func(string) string { return "" },
		Now:           func() time.Time { return time.Unix(1000, 0) },
		Store:         store,
		NewAuthClient: func(config.Config) AuthClient { return fakeAuth{res: res} },
		NewBrowserFlow: func(config.Config) BrowserFlow {
			return fakeBrowserFlow{res: browserauth.Result{
				IDToken: "bidt", RefreshToken: "brft", UID: "bu1", Email: "b@b.c", ExpiresIn: time.Hour,
			}}
		},
		NewDeviceFlow: func(config.Config, string, SessionStore) (DeviceFlow, error) {
			return fakeDeviceFlow{res: sneatauth.Result{
				IDToken: "bidt", RefreshToken: "brft", UID: "bu1", Email: "b@b.c", ExpiresIn: time.Hour,
			}}, nil
		},
	}
}

func TestAuthLogin_InsecureStorageDoesNotTouchUnavailableKeyring(t *testing.T) {
	secure := &failingStore{}
	insecure := &fakeStore{}
	var selected SessionStore
	env := testEnv(secure, sneatauth.Result{})
	env.NewInsecureStore = func() SessionStore { return insecure }
	env.NewDeviceFlow = func(_ config.Config, _ string, store SessionStore) (DeviceFlow, error) {
		selected = store
		return fakeDeviceFlow{res: sneatauth.Result{UID: "u1", Email: "u@example.test", IDToken: "id", RefreshToken: "refresh", ExpiresIn: time.Hour}}, nil
	}
	root := Root(env)
	root.AddCommand(Auth(env))
	root.SetArgs([]string{"auth", "--insecure-storage", "login"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if selected != insecure || secure.called {
		t.Fatalf("selected=%T secureCalled=%v", selected, secure.called)
	}
}

func TestAuthLogin_PrintsReplacementWarningWithoutSecret(t *testing.T) {
	store := &fakeStore{}
	env := testEnv(store, sneatauth.Result{})
	env.NewDeviceFlow = func(config.Config, string, SessionStore) (DeviceFlow, error) {
		return fakeDeviceFlow{res: sneatauth.Result{
			IDToken: "never-print", RefreshToken: "also-never-print", UID: "u1", Email: "a@b.c", ExpiresIn: time.Hour,
			Warnings: []error{errors.New("transport included never-print")},
		}}, nil
	}
	root := Root(env)
	root.AddCommand(Auth(env))
	var stderr bytes.Buffer
	root.SetErr(&stderr)
	root.SetArgs([]string{"auth", "login"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "previous device login") || strings.Contains(stderr.String(), "never-print") {
		t.Fatalf("warning output=%q", stderr.String())
	}
}

func TestAuthLogin_DeviceFlowDoesNotSaveTwice(t *testing.T) {
	store := &fakeStore{}
	env := testEnv(store, sneatauth.Result{})
	root := Root(env)
	root.AddCommand(Auth(env))
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"auth", "login"}) // no --email => browser flow
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if store.saved != nil {
		t.Fatalf("device login was saved by the command after the shared flow: %+v", store.saved)
	}
	var got map[string]string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output not JSON: %v", err)
	}
	if got["email"] != "b@b.c" {
		t.Fatalf("email = %q", got["email"])
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
	store := &fakeStore{load: &session.Session{UID: "u1"}}
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

func TestAuthStatus_PrintsNoSecret(t *testing.T) {
	store := &fakeStore{load: &session.Session{UID: "u1", Email: "a@b.c", Project: "sneat-eur3-1", IDToken: "never-print", RefreshToken: "also-never-print", ExpiresAt: time.Unix(1000, 0)}}
	env := testEnv(store, sneatauth.Result{})
	root := Root(env)
	root.AddCommand(Auth(env))
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetArgs([]string{"auth", "status"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(output.String(), "never-print") || strings.Contains(output.String(), "also-never-print") {
		t.Fatalf("status exposed a credential: %q", output.String())
	}
}
