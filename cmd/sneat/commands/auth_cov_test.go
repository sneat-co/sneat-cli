package commands

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/sneat-co/sneat-cli/internal/sneatauth"
)

// failingAuthClient always fails password sign-in, to exercise authLogin's
// SignInWithPassword error branch.
type failingAuthClient struct{ err error }

func (f failingAuthClient) SignInWithPassword(context.Context, string, string) (sneatauth.Result, error) {
	return sneatauth.Result{}, f.err
}
func (f failingAuthClient) Refresh(context.Context, string) (sneatauth.Result, error) {
	return sneatauth.Result{}, f.err
}

// erroringDeviceFlow lets tests control both Run and Logout outcomes,
// unlike the auth_test.go fakeDeviceFlow which always succeeds Run.
type erroringDeviceFlow struct {
	res       sneatauth.Result
	runErr    error
	logoutErr error
}

func (f erroringDeviceFlow) Run(context.Context, io.Writer, io.Writer) (sneatauth.Result, error) {
	return f.res, f.runErr
}
func (f erroringDeviceFlow) Logout(context.Context) error { return f.logoutErr }

// --- authLogin gaps ---

func TestAuthLogin_InsecureStorageNotConfigured(t *testing.T) {
	env := testEnv(&fakeStore{}, sneatauth.Result{})
	env.NewInsecureStore = nil
	root := Root(env)
	root.AddCommand(Auth(env))
	root.SetArgs([]string{"auth", "--insecure-storage", "login"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "insecure session storage is not configured") {
		t.Fatalf("err = %v, want insecure-storage-not-configured", err)
	}
}

func TestAuthLogin_PasswordSignInError(t *testing.T) {
	env := testEnv(&fakeStore{}, sneatauth.Result{})
	wantErr := errors.New("bad credentials")
	env.NewAuthClient = func(config.Config) AuthClient { return failingAuthClient{err: wantErr} }
	root := Root(env)
	root.AddCommand(Auth(env))
	root.SetArgs([]string{"auth", "login", "--email", "a@b.c", "--password", "wrong"})
	err := root.Execute()
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestAuthLogin_DeviceFlowNotConfigured(t *testing.T) {
	env := testEnv(&fakeStore{}, sneatauth.Result{})
	env.NewDeviceFlow = nil
	root := Root(env)
	root.AddCommand(Auth(env))
	root.SetArgs([]string{"auth", "login"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "device login is not configured") {
		t.Fatalf("err = %v, want device-login-not-configured", err)
	}
}

func TestAuthLogin_NewDeviceFlowError(t *testing.T) {
	env := testEnv(&fakeStore{}, sneatauth.Result{})
	wantErr := errors.New("issuer unreachable")
	env.NewDeviceFlow = func(config.Config, string, SessionStore) (DeviceFlow, error) {
		return nil, wantErr
	}
	root := Root(env)
	root.AddCommand(Auth(env))
	root.SetArgs([]string{"auth", "login"})
	err := root.Execute()
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestAuthLogin_DeviceFlowRunError(t *testing.T) {
	env := testEnv(&fakeStore{}, sneatauth.Result{})
	wantErr := errors.New("device flow denied")
	env.NewDeviceFlow = func(config.Config, string, SessionStore) (DeviceFlow, error) {
		return erroringDeviceFlow{runErr: wantErr}, nil
	}
	root := Root(env)
	root.AddCommand(Auth(env))
	root.SetArgs([]string{"auth", "login"})
	err := root.Execute()
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

// --- saveAndPrint gap: store.Save error ---

func TestAuthLogin_SaveErrorPropagates(t *testing.T) {
	store := &failingStore{}
	env := testEnv(store, sneatauth.Result{
		IDToken: "idt", RefreshToken: "rft", UID: "u1", Email: "a@b.c", ExpiresIn: time.Hour,
	})
	root := Root(env)
	root.AddCommand(Auth(env))
	root.SetArgs([]string{"auth", "login", "--email", "a@b.c", "--password", "pw"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "keyring unavailable") {
		t.Fatalf("err = %v, want keyring unavailable", err)
	}
	if !store.called {
		t.Fatalf("failing store was never called")
	}
}

// --- authStatus gaps ---

func TestAuthStatus_InsecureStorageNotConfigured(t *testing.T) {
	env := testEnv(&fakeStore{}, sneatauth.Result{})
	env.NewInsecureStore = nil
	root := Root(env)
	root.AddCommand(Auth(env))
	root.SetArgs([]string{"auth", "--insecure-storage", "status"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "insecure session storage is not configured") {
		t.Fatalf("err = %v, want insecure-storage-not-configured", err)
	}
}

func TestAuthStatus_LoadErrorPropagates(t *testing.T) {
	store := &failingStore{}
	env := testEnv(store, sneatauth.Result{})
	root := Root(env)
	root.AddCommand(Auth(env))
	root.SetArgs([]string{"auth", "status"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "keyring unavailable") {
		t.Fatalf("err = %v, want keyring unavailable", err)
	}
}

// --- authLogout gaps ---

func TestAuthLogout_InsecureStorageNotConfigured(t *testing.T) {
	env := testEnv(&fakeStore{}, sneatauth.Result{})
	env.NewInsecureStore = nil
	root := Root(env)
	root.AddCommand(Auth(env))
	root.SetArgs([]string{"auth", "--insecure-storage", "logout"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "insecure session storage is not configured") {
		t.Fatalf("err = %v, want insecure-storage-not-configured", err)
	}
}

func TestAuthLogout_NoSessionIsNotAnError(t *testing.T) {
	store := &fakeStore{} // load == nil => ErrNoSession
	env := testEnv(store, sneatauth.Result{})
	root := Root(env)
	root.AddCommand(Auth(env))
	root.SetArgs([]string{"auth", "logout"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if store.cleared {
		t.Fatalf("Clear should not be called when there is no session")
	}
}

func TestAuthLogout_LoadErrorPropagates(t *testing.T) {
	store := &failingStore{}
	env := testEnv(store, sneatauth.Result{})
	root := Root(env)
	root.AddCommand(Auth(env))
	root.SetArgs([]string{"auth", "logout"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "keyring unavailable") {
		t.Fatalf("err = %v, want keyring unavailable", err)
	}
}

func TestAuthLogout_DeviceSessionRevokesRemotely(t *testing.T) {
	store := &fakeStore{load: &session.Session{UID: "u1", Issuer: "https://auth.sneat.co", ClientID: "cid"}}
	env := testEnv(store, sneatauth.Result{})
	env.NewDeviceFlow = func(config.Config, string, SessionStore) (DeviceFlow, error) {
		return erroringDeviceFlow{logoutErr: nil}, nil
	}
	root := Root(env)
	root.AddCommand(Auth(env))
	root.SetArgs([]string{"auth", "logout"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Local store.Clear must not be called directly by authLogout for a
	// device session; Client.Logout owns clearing local storage.
	if store.cleared {
		t.Fatalf("authLogout should not call store.Clear directly for a device session")
	}
}

func TestAuthLogout_NewDeviceFlowError(t *testing.T) {
	store := &fakeStore{load: &session.Session{UID: "u1", Issuer: "https://auth.sneat.co", ClientID: "cid"}}
	wantErr := errors.New("issuer unreachable")
	env := testEnv(store, sneatauth.Result{})
	env.NewDeviceFlow = func(config.Config, string, SessionStore) (DeviceFlow, error) {
		return nil, wantErr
	}
	root := Root(env)
	root.AddCommand(Auth(env))
	root.SetArgs([]string{"auth", "logout"})
	err := root.Execute()
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestAuthLogout_DeviceFlowLogoutError(t *testing.T) {
	store := &fakeStore{load: &session.Session{UID: "u1", Issuer: "https://auth.sneat.co", ClientID: "cid"}}
	wantErr := errors.New("revoke failed")
	env := testEnv(store, sneatauth.Result{})
	env.NewDeviceFlow = func(config.Config, string, SessionStore) (DeviceFlow, error) {
		return erroringDeviceFlow{logoutErr: wantErr}, nil
	}
	root := Root(env)
	root.AddCommand(Auth(env))
	root.SetArgs([]string{"auth", "logout"})
	err := root.Execute()
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

// The --auth-host flag is a persistent flag on Root. Running the auth group
// without Root (as an embedder could) must surface the flag lookup error
// rather than silently using a default issuer.
func TestAuthLogin_AuthHostFlagMissingWithoutRoot(t *testing.T) {
	var flowBuilt bool
	env := testEnv(&fakeStore{}, sneatauth.Result{})
	env.NewDeviceFlow = func(config.Config, string, SessionStore) (DeviceFlow, error) {
		flowBuilt = true
		return erroringDeviceFlow{}, nil
	}
	cmd := Auth(env)
	cmd.SetArgs([]string{"login"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "auth-host") {
		t.Fatalf("err = %v, want auth-host flag lookup error", err)
	}
	if flowBuilt {
		t.Fatalf("device flow must not be built when the issuer flag is unavailable")
	}
}
