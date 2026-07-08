package session

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var errBoom = errors.New("boom")

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

func TestStore_LoadInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := NewStore(path).Load(); err == nil {
		t.Fatalf("expected error on malformed JSON")
	}
}

func TestStore_ClearAbsentIsNoError(t *testing.T) {
	if err := NewStore(filepath.Join(t.TempDir(), "missing.json")).Clear(); err != nil {
		t.Fatalf("Clear on absent = %v, want nil", err)
	}
}

func TestDefaultPath_Error(t *testing.T) {
	if _, err := DefaultPath(func() (string, error) { return "", errBoom }); err == nil {
		t.Fatalf("expected propagated error")
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
