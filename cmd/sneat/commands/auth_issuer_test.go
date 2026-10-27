package commands

import (
	"testing"

	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/deviceflow"
	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/sneat-co/sneat-cli/internal/sneatauth"
)

// loginIssuer runs `sneat auth login` with args and returns the issuer the
// command handed to the device flow.
func loginIssuer(t *testing.T, getenv func(string) string, args ...string) string {
	t.Helper()
	var issuer string
	env := testEnv(&fakeStore{}, sneatauth.Result{})
	env.Getenv = getenv
	env.NewDeviceFlow = func(_ config.Config, got string, _ SessionStore) (DeviceFlow, error) {
		issuer = got
		return fakeDeviceFlow{}, nil
	}
	root := Root(env)
	root.AddCommand(Auth(env))
	root.SetArgs(append([]string{"auth", "login"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return issuer
}

func TestAuthLogin_IssuerResolution(t *testing.T) {
	noEnv := func(string) string { return "" }
	withEnv := func(key string) string {
		if key == deviceflow.IssuerEnv {
			return "http://127.0.0.1:9099"
		}
		return ""
	}
	if got := loginIssuer(t, noEnv); got != "https://oauth.sneat.co" {
		t.Fatalf("default issuer = %q", got)
	}
	if got := loginIssuer(t, withEnv); got != "http://127.0.0.1:9099" {
		t.Fatalf("env issuer = %q", got)
	}
	if got := loginIssuer(t, withEnv, "--auth-host", "http://localhost:8080"); got != "http://localhost:8080" {
		t.Fatalf("flag issuer = %q", got)
	}
}

func TestAuthLogout_PreviousIssuerSessionUsesCurrentIssuer(t *testing.T) {
	for _, tc := range []struct{ stored, want string }{
		{stored: deviceflow.PreviousIssuer, want: deviceflow.DefaultIssuer},
		{stored: "http://127.0.0.1:9099", want: "http://127.0.0.1:9099"},
	} {
		store := &fakeStore{load: &session.Session{UID: "u1", Issuer: tc.stored, ClientID: "sneat-cli"}}
		env := testEnv(store, sneatauth.Result{})
		var issuer string
		env.NewDeviceFlow = func(_ config.Config, got string, _ SessionStore) (DeviceFlow, error) {
			issuer = got
			return erroringDeviceFlow{}, nil
		}
		root := Root(env)
		root.AddCommand(Auth(env))
		root.SetArgs([]string{"auth", "logout"})
		if err := root.Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if issuer != tc.want {
			t.Fatalf("logout of a %s session used issuer %q, want %q", tc.stored, issuer, tc.want)
		}
	}
}
