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
