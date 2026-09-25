package actionstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sneat-co/sneat-ai-backend/actionspec"
)

func TestSaveLoad_RoundTrips(t *testing.T) {
	s := NewStore(t.TempDir())
	d := Draft{ActionID: "act_abc12345", SpaceID: "sp1", Semantic: actionspec.Semantic{Kind: actionspec.KindBuy}}
	if err := s.Save(d); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Load("act_abc12345")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ActionID != d.ActionID || got.SpaceID != d.SpaceID || got.Semantic.Kind != actionspec.KindBuy {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}

func TestLoad_NotFound(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, err := s.Load("act_missing1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestList_EmptyDirIsNotAnError(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "does-not-exist"))
	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got = %+v, want empty", got)
	}
}

func TestList_SortedByID(t *testing.T) {
	s := NewStore(t.TempDir())
	_ = s.Save(Draft{ActionID: "act_zzz00000"})
	_ = s.Save(Draft{ActionID: "act_aaa00000"})
	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 || got[0].ActionID != "act_aaa00000" || got[1].ActionID != "act_zzz00000" {
		t.Fatalf("got = %+v", got)
	}
}

func TestDelete_RemovesDraft(t *testing.T) {
	s := NewStore(t.TempDir())
	_ = s.Save(Draft{ActionID: "act_abc12345"})
	if err := s.Delete("act_abc12345"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Load("act_abc12345"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound after delete", err)
	}
}

func TestDelete_MissingIsNotAnError(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Delete("act_missing1"); err != nil {
		t.Fatalf("Delete missing: %v", err)
	}
}

func TestDefaultDir(t *testing.T) {
	dir, err := DefaultDir(func() (string, error) { return "/cfg", nil })
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if dir != filepath.Join("/cfg", "sneat", "actions") {
		t.Fatalf("dir = %q", dir)
	}
}

func TestDefaultDir_Error(t *testing.T) {
	if _, err := DefaultDir(func() (string, error) { return "", errors.New("boom") }); err == nil {
		t.Fatalf("expected propagated error")
	}
}

func TestSave_MkdirAllFails(t *testing.T) {
	// A regular file in the path's place makes MkdirAll fail because a
	// non-directory component already exists.
	base := t.TempDir()
	blocker := filepath.Join(base, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("setup WriteFile: %v", err)
	}
	s := NewStore(filepath.Join(blocker, "sub"))
	if err := s.Save(Draft{ActionID: "act_abc12345"}); err == nil {
		t.Fatalf("expected MkdirAll error")
	}
}

func TestSave_MarshalFails(t *testing.T) {
	s := NewStore(t.TempDir())
	orig := jsonMarshalIndent
	t.Cleanup(func() { jsonMarshalIndent = orig })
	wantErr := errors.New("marshal boom")
	jsonMarshalIndent = func(v any, prefix, indent string) ([]byte, error) {
		return nil, wantErr
	}
	err := s.Save(Draft{ActionID: "act_abc12345"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestLoad_ReadFileErrorNotIsNotExist(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	// Make the target path a directory: ReadFile then fails with an
	// "is a directory" error, which is not os.IsNotExist.
	if err := os.MkdirAll(s.path("act_abc12345"), 0o700); err != nil {
		t.Fatalf("setup MkdirAll: %v", err)
	}
	_, err := s.Load("act_abc12345")
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want non-NotFound error", err)
	}
}

func TestLoad_UnmarshalFails(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := os.WriteFile(s.path("act_abc12345"), []byte("not json"), 0o600); err != nil {
		t.Fatalf("setup WriteFile: %v", err)
	}
	_, err := s.Load("act_abc12345")
	var syntaxErr *json.SyntaxError
	if !errors.As(err, &syntaxErr) {
		t.Fatalf("err = %v, want *json.SyntaxError", err)
	}
}

func TestDelete_ErrorNotIsNotExist(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	// A non-empty directory in the target path's place makes os.Remove fail
	// with an error other than "not exist".
	target := s.path("act_abc12345")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatalf("setup MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "child"), []byte("x"), 0o600); err != nil {
		t.Fatalf("setup WriteFile: %v", err)
	}
	if err := s.Delete("act_abc12345"); err == nil {
		t.Fatalf("expected Delete error for non-empty directory")
	}
}

func TestList_ReadDirErrorNotIsNotExist(t *testing.T) {
	base := t.TempDir()
	// A regular file in the store dir's place makes ReadDir fail with an
	// error other than "not exist".
	notADir := filepath.Join(base, "not-a-dir")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatalf("setup WriteFile: %v", err)
	}
	s := NewStore(notADir)
	if _, err := s.List(); err == nil {
		t.Fatalf("expected ReadDir error")
	}
}

func TestList_SkipsNonJSONEntriesAndCorruptFiles(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	// A subdirectory and a non-.json file must be skipped via the first
	// continue; a corrupt .json file must be skipped via the second.
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o700); err != nil {
		t.Fatalf("setup MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("setup WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "act_corrupt1.json"), []byte("not json"), 0o600); err != nil {
		t.Fatalf("setup WriteFile: %v", err)
	}
	if err := s.Save(Draft{ActionID: "act_valid001"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ActionID != "act_valid001" {
		t.Fatalf("got = %+v, want only the valid draft", got)
	}
}
