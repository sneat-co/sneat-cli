package main

import (
	"errors"
	"testing"
)

type fakeApplication struct{ run func() error }

func (f fakeApplication) Run() error { return f.run() }

func TestNewApplication_NotNil(t *testing.T) {
	app := newApplication()
	if app == nil {
		t.Fatalf("newApplication() returned nil")
	}
}

func TestMain_RunSuccess_NoExit(t *testing.T) {
	// Save and restore hooks
	oldGet := getApplication
	oldExit := exit
	defer func() {
		getApplication = oldGet
		exit = oldExit
	}()

	exitCalled := false
	exit = func(code int) { exitCalled = true }

	getApplication = func() application {
		return fakeApplication{run: func() error { return nil }}
	}

	main()
	if exitCalled {
		t.Fatalf("exit was called on success path, want not called")
	}
}

func TestMain_RunError_ExitCalled(t *testing.T) {
	oldGet := getApplication
	oldExit := exit
	defer func() {
		getApplication = oldGet
		exit = oldExit
	}()

	exitCalled := false
	exitCode := 0
	exit = func(code int) { exitCalled = true; exitCode = code }

	getApplication = func() application {
		return fakeApplication{run: func() error { return errors.New("boom") }}
	}

	main()
	if !exitCalled {
		t.Fatalf("exit was not called on error path")
	}
	if exitCode != 1 {
		t.Fatalf("exit called with code %d, want 1", exitCode)
	}
}
