package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/sneat-co/sneat-cli/internal/sneatauth"
)

type fakeSpacesReader struct {
	spaces map[string]any
	err    error
	gotUID string
}

func (f *fakeSpacesReader) ListSpaces(_ context.Context, uid string) (map[string]any, error) {
	f.gotUID = uid
	return f.spaces, f.err
}

func TestSpacesList_PrintsSpacesForCurrentUser(t *testing.T) {
	reader := &fakeSpacesReader{spaces: map[string]any{
		"ao58m": map[string]any{"type": "private"},
		"vaoyj": map[string]any{"type": "family"},
	}}
	env := testEnv(&fakeStore{load: &session.Session{UID: "u1"}}, sneatauth.Result{})
	env.NewSpacesReader = func(config.Config) (SpacesReader, error) { return reader, nil }

	root := Root(env)
	root.AddCommand(Space(env))
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"space", "list", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if reader.gotUID != "u1" {
		t.Fatalf("reader got uid %q, want u1", reader.gotUID)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output not JSON: %v (%q)", err, buf.String())
	}
	if len(got) != 2 || got["ao58m"] == nil {
		t.Fatalf("spaces = %+v", got)
	}
}

func TestSpaces_AliasListsSpaces(t *testing.T) {
	reader := &fakeSpacesReader{spaces: map[string]any{"ao58m": map[string]any{"type": "private"}}}
	env := testEnv(&fakeStore{load: &session.Session{UID: "u1"}}, sneatauth.Result{})
	env.NewSpacesReader = func(config.Config) (SpacesReader, error) { return reader, nil }

	root := Root(env)
	root.AddCommand(Spaces(env)) // bare `spaces` == `space list`
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"spaces", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output not JSON: %v (%q)", err, buf.String())
	}
	if got["ao58m"] == nil {
		t.Fatalf("spaces = %+v", got)
	}
}

func TestSpacesList_NoSession(t *testing.T) {
	env := testEnv(&fakeStore{}, sneatauth.Result{})
	env.NewSpacesReader = func(config.Config) (SpacesReader, error) {
		return &fakeSpacesReader{}, nil
	}
	root := Root(env)
	root.AddCommand(Space(env))
	root.SetArgs([]string{"space", "list"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected error when not signed in")
	}
}

func TestSpacesList_ReaderError(t *testing.T) {
	env := testEnv(&fakeStore{load: &session.Session{UID: "u1"}}, sneatauth.Result{})
	env.NewSpacesReader = func(config.Config) (SpacesReader, error) {
		return &fakeSpacesReader{err: errors.New("boom")}, nil
	}
	root := Root(env)
	root.AddCommand(Space(env))
	root.SetArgs([]string{"space", "list"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected reader error")
	}
}
