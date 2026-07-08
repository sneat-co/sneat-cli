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
		Getenv:        func(string) string { return "" },
		Now:           func() time.Time { return time.Unix(1000, 0) },
		Store:         store,
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
