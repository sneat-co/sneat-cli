package chatapp

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestNewDebugLogger_UserCacheDirError covers the os.UserCacheDir failure
// branch: falls back to a discarded logger rather than erroring or writing
// to the real filesystem.
func TestNewDebugLogger_UserCacheDirError(t *testing.T) {
	origDir, origMkdir, origOpen := userCacheDir, mkdirAll, openLogFile
	t.Cleanup(func() { userCacheDir, mkdirAll, openLogFile = origDir, origMkdir, origOpen })

	wantErr := errors.New("no cache dir")
	userCacheDir = func() (string, error) { return "", wantErr }
	mkdirAll = func(string, os.FileMode) error {
		t.Fatal("mkdirAll should not be reached when UserCacheDir fails")
		return nil
	}
	openLogFile = func(string, int, os.FileMode) (*os.File, error) {
		t.Fatal("openLogFile should not be reached when UserCacheDir fails")
		return nil, nil
	}

	logger := newDebugLogger()
	if logger == nil {
		t.Fatal("newDebugLogger() = nil, want a discarded-but-non-nil logger")
	}
	// Should not panic when used, and should not have created anything.
	logger.Debug("probe")
}

// TestNewDebugLogger_MkdirAllError covers the os.MkdirAll failure branch.
func TestNewDebugLogger_MkdirAllError(t *testing.T) {
	origDir, origMkdir, origOpen := userCacheDir, mkdirAll, openLogFile
	t.Cleanup(func() { userCacheDir, mkdirAll, openLogFile = origDir, origMkdir, origOpen })

	userCacheDir = func() (string, error) { return "/tmp/does-not-matter", nil }
	wantErr := errors.New("cannot mkdir")
	mkdirAll = func(path string, perm os.FileMode) error { return wantErr }
	openLogFile = func(string, int, os.FileMode) (*os.File, error) {
		t.Fatal("openLogFile should not be reached when MkdirAll fails")
		return nil, nil
	}

	logger := newDebugLogger()
	if logger == nil {
		t.Fatal("newDebugLogger() = nil, want a discarded-but-non-nil logger")
	}
	logger.Debug("probe")
}

// TestNewDebugLogger_OpenFileError covers the os.OpenFile failure branch.
func TestNewDebugLogger_OpenFileError(t *testing.T) {
	origDir, origMkdir, origOpen := userCacheDir, mkdirAll, openLogFile
	t.Cleanup(func() { userCacheDir, mkdirAll, openLogFile = origDir, origMkdir, origOpen })

	userCacheDir = func() (string, error) { return "/tmp/does-not-matter", nil }
	mkdirAll = func(string, os.FileMode) error { return nil }
	wantErr := errors.New("cannot open")
	openLogFile = func(string, int, os.FileMode) (*os.File, error) { return nil, wantErr }

	logger := newDebugLogger()
	if logger == nil {
		t.Fatal("newDebugLogger() = nil, want a discarded-but-non-nil logger")
	}
	logger.Debug("probe")
}

// TestNewDebugLogger_Success covers the happy path: a real file-backed
// logger is produced, and writes actually reach the file, using a t.TempDir
// as the seamed "cache dir" rather than the real user cache directory.
func TestNewDebugLogger_Success(t *testing.T) {
	origDir, origMkdir, origOpen := userCacheDir, mkdirAll, openLogFile
	t.Cleanup(func() { userCacheDir, mkdirAll, openLogFile = origDir, origMkdir, origOpen })

	base := t.TempDir()
	userCacheDir = func() (string, error) { return base, nil }
	mkdirAll = os.MkdirAll
	openLogFile = os.OpenFile

	logger := newDebugLogger()
	if logger == nil {
		t.Fatal("newDebugLogger() = nil")
	}
	logger.Debug("probe message")

	logPath := filepath.Join(base, "sneat", "chat-debug.log")
	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected the log file to have been created at %s: %v", logPath, err)
	}
	if len(contents) == 0 {
		t.Fatal("expected the debug message to have been written to the log file")
	}
}
