package chatapp

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/strongo/aichat/ai/aiconfig"
	"golang.org/x/oauth2"

	"github.com/sneat-co/sneat-cli/internal/aichat/data"
	"github.com/sneat-co/sneat-cli/internal/config"
)

type fakeTokenSource struct {
	tok *oauth2.Token
	err error
}

func (f fakeTokenSource) Token() (*oauth2.Token, error) { return f.tok, f.err }

// fakeCloser is an io.Closer double recording calls and returning a
// caller-supplied error.
type fakeCloser struct {
	err    error
	closed int
}

func (f *fakeCloser) Close() error {
	f.closed++
	return f.err
}

// runTestDeps builds a minimal, valid Deps for exercising Run's wiring.
func runTestDeps() Deps {
	return Deps{
		Spaces:      spaceIDFakeSpaces{},
		UID:         "u1",
		Version:     "test",
		Cfg:         config.Config{Project: "p1", APIBaseURL: "https://api.example.com"},
		TokenSource: fakeTokenSource{tok: &oauth2.Token{AccessToken: "t"}},
	}
}

// TestRun_ReadersClosedOnBuildAIConfigError covers Run's early-return path:
// buildAIConfig failing must still close the readers' Firestore session
// (the defer runs on every return path) and Run must return the wrapped
// aiconfig error, not swallow it.
func TestRun_ReadersClosedOnBuildAIConfigError(t *testing.T) {
	closer := &fakeCloser{}
	origNewReaders, origBuild, origRun := newReaders, buildAIConfig, runProgram
	t.Cleanup(func() { newReaders, buildAIConfig, runProgram = origNewReaders, origBuild, origRun })

	newReaders = func(cfg config.Config, ts oauth2.TokenSource, loc *time.Location) (data.Readers, io.Closer) {
		return data.Readers{Happenings: &data.FakeHappenings{}, Todos: &data.FakeTodos{}, Contacts: &data.FakeContacts{}}, closer
	}
	wantErr := errors.New("aiconfig build failed")
	buildAIConfig = func(cfg aiconfig.Config, deps aiconfig.Deps) (aiconfig.Providers, error) {
		return aiconfig.Providers{}, wantErr
	}
	runProgram = func(model tea.Model) error {
		t.Fatal("runProgram must not be reached when buildAIConfig fails")
		return nil
	}

	err := Run(runTestDeps())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() = %v, want it to wrap %v", err, wantErr)
	}
	if closer.closed != 1 {
		t.Fatalf("closer.closed = %d, want 1 (the deferred close must still run)", closer.closed)
	}
}

// TestRun_Success covers Run's happy path: buildAIConfig succeeds,
// runProgram (the terminal driver) is reached with a real tea.Model, and
// its result is what Run returns; the readers' session is still closed
// afterwards.
func TestRun_Success(t *testing.T) {
	closer := &fakeCloser{}
	origNewReaders, origBuild, origRun := newReaders, buildAIConfig, runProgram
	t.Cleanup(func() { newReaders, buildAIConfig, runProgram = origNewReaders, origBuild, origRun })

	newReaders = func(cfg config.Config, ts oauth2.TokenSource, loc *time.Location) (data.Readers, io.Closer) {
		return data.Readers{Happenings: &data.FakeHappenings{}, Todos: &data.FakeTodos{}, Contacts: &data.FakeContacts{}}, closer
	}
	buildAIConfig = func(cfg aiconfig.Config, deps aiconfig.Deps) (aiconfig.Providers, error) {
		return aiconfig.Providers{}, nil
	}
	var gotModel tea.Model
	runProgram = func(model tea.Model) error {
		gotModel = model
		return nil
	}

	if err := Run(runTestDeps()); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if gotModel == nil {
		t.Fatal("runProgram was not called with a tea.Model")
	}
	if closer.closed != 1 {
		t.Fatalf("closer.closed = %d, want 1", closer.closed)
	}
}

// TestRun_CloserErrorSurfacedWhenProgramSucceeds covers the defer's own
// error-wrapping branch: runProgram succeeding (nil error) must not hide a
// failed Firestore-session close.
func TestRun_CloserErrorSurfacedWhenProgramSucceeds(t *testing.T) {
	wantErr := errors.New("close failed")
	closer := &fakeCloser{err: wantErr}
	origNewReaders, origBuild, origRun := newReaders, buildAIConfig, runProgram
	t.Cleanup(func() { newReaders, buildAIConfig, runProgram = origNewReaders, origBuild, origRun })

	newReaders = func(cfg config.Config, ts oauth2.TokenSource, loc *time.Location) (data.Readers, io.Closer) {
		return data.Readers{Happenings: &data.FakeHappenings{}, Todos: &data.FakeTodos{}, Contacts: &data.FakeContacts{}}, closer
	}
	buildAIConfig = func(cfg aiconfig.Config, deps aiconfig.Deps) (aiconfig.Providers, error) {
		return aiconfig.Providers{}, nil
	}
	runProgram = func(model tea.Model) error { return nil }

	err := Run(runTestDeps())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() = %v, want it to wrap %v", err, wantErr)
	}
}

// TestRun_CloserErrorDoesNotOverrideProgramError covers the defer's other
// branch: a failed close must NOT override an already-non-nil error from
// runProgram (or buildAIConfig) -- the `err == nil` guard.
func TestRun_CloserErrorDoesNotOverrideProgramError(t *testing.T) {
	closer := &fakeCloser{err: errors.New("close failed")}
	origNewReaders, origBuild, origRun := newReaders, buildAIConfig, runProgram
	t.Cleanup(func() { newReaders, buildAIConfig, runProgram = origNewReaders, origBuild, origRun })

	newReaders = func(cfg config.Config, ts oauth2.TokenSource, loc *time.Location) (data.Readers, io.Closer) {
		return data.Readers{Happenings: &data.FakeHappenings{}, Todos: &data.FakeTodos{}, Contacts: &data.FakeContacts{}}, closer
	}
	buildAIConfig = func(cfg aiconfig.Config, deps aiconfig.Deps) (aiconfig.Providers, error) {
		return aiconfig.Providers{}, nil
	}
	wantErr := errors.New("program failed")
	runProgram = func(model tea.Model) error { return wantErr }

	err := Run(runTestDeps())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() = %v, want the program's own error (%v), not the close failure", err, wantErr)
	}
}

// TestRun_DebugEnablesLogger covers the deps.Debug branch reaching
// newDebugLogger (it must not panic or otherwise disrupt Run's wiring; the
// logger's own behaviour is covered by newDebugLogger's own tests).
func TestRun_DebugEnablesLogger(t *testing.T) {
	closer := &fakeCloser{}
	origNewReaders, origBuild, origRun := newReaders, buildAIConfig, runProgram
	t.Cleanup(func() { newReaders, buildAIConfig, runProgram = origNewReaders, origBuild, origRun })

	newReaders = func(cfg config.Config, ts oauth2.TokenSource, loc *time.Location) (data.Readers, io.Closer) {
		return data.Readers{Happenings: &data.FakeHappenings{}, Todos: &data.FakeTodos{}, Contacts: &data.FakeContacts{}}, closer
	}
	buildAIConfig = func(cfg aiconfig.Config, deps aiconfig.Deps) (aiconfig.Providers, error) {
		return aiconfig.Providers{}, nil
	}
	runProgram = func(model tea.Model) error { return nil }

	deps := runTestDeps()
	deps.Debug = true
	if err := Run(deps); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
}

// TestCloudTokenFunc covers both branches: a successful Token() call
// returns the access token, and a failing one propagates the error.
func TestCloudTokenFunc(t *testing.T) {
	fn := cloudTokenFunc(fakeTokenSource{tok: &oauth2.Token{AccessToken: "abc"}})
	got, err := fn(context.Background())
	if err != nil || got != "abc" {
		t.Fatalf("got=%q err=%v, want abc/nil", got, err)
	}

	wantErr := errors.New("token unavailable")
	fn2 := cloudTokenFunc(fakeTokenSource{err: wantErr})
	if _, err := fn2(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}
