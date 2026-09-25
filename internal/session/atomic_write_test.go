package session

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// withSeam swaps a package-level test seam for the duration of the test and
// restores it afterward.
func withSeam[T any](t *testing.T, seam *T, replacement T) {
	t.Helper()
	prev := *seam
	*seam = replacement
	t.Cleanup(func() { *seam = prev })
}

func TestAtomicWriteFile_MkdirAllError(t *testing.T) {
	withSeam(t, &mkdirAllFn, func(string, os.FileMode) error { return errors.New("mkdir boom") })
	if err := atomicWriteFile(filepath.Join(t.TempDir(), "s.json"), []byte("{}"), 0o600); err == nil {
		t.Fatal("expected mkdir error")
	}
}

func TestAtomicWriteFile_CreateTempError(t *testing.T) {
	withSeam(t, &createTempFn, func(string, string) (*os.File, error) { return nil, errors.New("create boom") })
	if err := atomicWriteFile(filepath.Join(t.TempDir(), "s.json"), []byte("{}"), 0o600); err == nil {
		t.Fatal("expected create-temp error")
	}
}

func TestAtomicWriteFile_ChmodFileError(t *testing.T) {
	withSeam(t, &chmodFileFn, func(*os.File, os.FileMode) error { return errors.New("chmod boom") })
	if err := atomicWriteFile(filepath.Join(t.TempDir(), "s.json"), []byte("{}"), 0o600); err == nil {
		t.Fatal("expected chmod error")
	}
}

func TestAtomicWriteFile_WriteError(t *testing.T) {
	withSeam(t, &writeFileFn, func(*os.File, []byte) (int, error) { return 0, errors.New("write boom") })
	if err := atomicWriteFile(filepath.Join(t.TempDir(), "s.json"), []byte("{}"), 0o600); err == nil {
		t.Fatal("expected write error")
	}
}

func TestAtomicWriteFile_SyncFileError(t *testing.T) {
	withSeam(t, &syncFileFn, func(*os.File) error { return errors.New("sync boom") })
	if err := atomicWriteFile(filepath.Join(t.TempDir(), "s.json"), []byte("{}"), 0o600); err == nil {
		t.Fatal("expected sync error")
	}
}

func TestAtomicWriteFile_CloseFileError(t *testing.T) {
	withSeam(t, &closeFileFn, func(*os.File) error { return errors.New("close boom") })
	if err := atomicWriteFile(filepath.Join(t.TempDir(), "s.json"), []byte("{}"), 0o600); err == nil {
		t.Fatal("expected close error")
	}
}

func TestAtomicWriteFile_RenameError(t *testing.T) {
	withSeam(t, &renameFileFn, func(string, string) error { return errors.New("rename boom") })
	if err := atomicWriteFile(filepath.Join(t.TempDir(), "s.json"), []byte("{}"), 0o600); err == nil {
		t.Fatal("expected rename error")
	}
}

func TestAtomicWriteFile_ChmodPathError(t *testing.T) {
	withSeam(t, &chmodPathFn, func(string, os.FileMode) error { return errors.New("chmod path boom") })
	if err := atomicWriteFile(filepath.Join(t.TempDir(), "s.json"), []byte("{}"), 0o600); err == nil {
		t.Fatal("expected chmod-path error")
	}
}

func TestAtomicWriteFile_OpenDirError(t *testing.T) {
	withSeam(t, &openDirFn, func(string) (*os.File, error) { return nil, errors.New("open dir boom") })
	if err := atomicWriteFile(filepath.Join(t.TempDir(), "s.json"), []byte("{}"), 0o600); err == nil {
		t.Fatal("expected open-dir error")
	}
}

func TestAtomicWriteFile_SyncDirError(t *testing.T) {
	withSeam(t, &syncDirFn, func(*os.File) error { return errors.New("sync dir boom") })
	if err := atomicWriteFile(filepath.Join(t.TempDir(), "s.json"), []byte("{}"), 0o600); err == nil {
		t.Fatal("expected sync-dir error")
	}
}

func TestAtomicWriteFile_CloseDirError(t *testing.T) {
	withSeam(t, &closeDirFn, func(*os.File) error { return errors.New("close dir boom") })
	if err := atomicWriteFile(filepath.Join(t.TempDir(), "s.json"), []byte("{}"), 0o600); err == nil {
		t.Fatal("expected close-dir error")
	}
}

func TestAtomicWriteFile_Success(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "s.json")
	if err := atomicWriteFile(path, []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != `{"a":1}` {
		t.Fatalf("data = %q", data)
	}
}
