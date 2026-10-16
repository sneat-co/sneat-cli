package deviceflow

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/sneat-co/sneat-cli/internal/sneatauth"
)

// recordingServer is a local stand-in for the Sneat OAuth server that records
// every request path and the last revoked token.
type recordingServer struct {
	*httptest.Server
	mu      sync.Mutex
	paths   []string
	revoked []string
}

func newRecordingServer(t *testing.T) *recordingServer {
	t.Helper()
	s := &recordingServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.paths = append(s.paths, r.URL.Path)
		if r.URL.Path == "/oauth/revoke" {
			s.revoked = append(s.revoked, r.FormValue("token"))
		}
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/oauth/device/code":
			_, _ = io.WriteString(w, `{"device_code":"device","user_code":"ABCD-EFGH","verification_uri":"https://verify.example/device","expires_in":300,"interval":1}`)
		case "/oauth/token":
			_, _ = io.WriteString(w, `{"access_token":"custom-token","token_type":"urn:ietf:params:oauth:token-type:firebase-custom-token","expires_in":300}`)
		case "/oauth/userinfo":
			_, _ = io.WriteString(w, `{"sub":"user-1","aud":"sneat-cli","scope":"openid profile sneat:spaces:read sneat:spaces:write"}`)
		case "/oauth/revoke":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *recordingServer) snapshot() (paths, revoked []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...), append([]string(nil), s.revoked...)
}

// closedLoopbackURL is a loopback URL on which nothing listens: requests to it
// fail like a host that is not serving yet.
func closedLoopbackURL(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.NotFoundHandler())
	rawURL := server.URL
	server.Close()
	return rawURL
}

func newSessionExchange() exchangeFunc {
	return func(context.Context, string) (sneatauth.Result, error) {
		return sneatauth.Result{IDToken: "new-id", RefreshToken: "new-refresh", UID: "user-1", ExpiresIn: time.Hour}, nil
	}
}

func previousIssuerSession(expiresAt time.Time) *memoryStore {
	return &memoryStore{has: true, value: session.Session{
		Project: "sneat-eur3-1", UID: "user-1", IDToken: "old-id", RefreshToken: "old-refresh",
		ExpiresAt: expiresAt, Issuer: PreviousIssuer, ClientID: ClientID, Scopes: Scopes, TokenType: "Bearer",
		CurrentSpace: "family",
	}}
}

func TestResolveIssuer(t *testing.T) {
	env := map[string]string{IssuerEnv: " http://127.0.0.1:9099 "}
	getenv := func(key string) string { return env[key] }
	if got := ResolveIssuer(" http://localhost:8080 ", getenv); got != "http://localhost:8080" {
		t.Fatalf("flag override = %q", got)
	}
	if got := ResolveIssuer("", getenv); got != "http://127.0.0.1:9099" {
		t.Fatalf("env override = %q", got)
	}
	if got := ResolveIssuer("", func(string) string { return "" }); got != "https://oauth.sneat.co" {
		t.Fatalf("default = %q", got)
	}
}

func TestNew_DefaultIssuerKnowsItsPreviousHost(t *testing.T) {
	flow, err := New(Options{Exchange: newSessionExchange(), Store: &memoryStore{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := flow.client.PreviousIssuers(); len(got) != 1 || got[0] != "https://auth.sneat.co" {
		t.Fatalf("PreviousIssuers() = %v", got)
	}
	if got := flow.options.FallbackIssuers; len(got) != 1 || got[0] != PreviousIssuer {
		t.Fatalf("FallbackIssuers = %v", got)
	}
}

func TestNew_OverrideNeverFallsBackToProduction(t *testing.T) {
	server := newRecordingServer(t)
	flow, err := New(Options{Issuer: server.URL, Exchange: newSessionExchange(), Store: &memoryStore{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(flow.options.FallbackIssuers) != 0 || len(flow.client.PreviousIssuers()) != 0 {
		t.Fatalf("override configured migration: fallbacks=%v previous=%v", flow.options.FallbackIssuers, flow.client.PreviousIssuers())
	}
}

func TestRun_ReplacesSessionFromPreviousIssuerOnce(t *testing.T) {
	server := newRecordingServer(t)
	store := previousIssuerSession(time.Now().Add(time.Hour))
	flow, err := New(Options{Issuer: server.URL, PreviousIssuers: []string{PreviousIssuer}, HTTPClient: server.Client(), Exchange: newSessionExchange(), Store: store, Project: "sneat-eur3-1"})
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	result, err := flow.Run(context.Background(), io.Discard, &stderr)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stderr.String() != IssuerMovedNotice+"\n" {
		t.Fatalf("stderr = %q, want exactly the moved notice", stderr.String())
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %v", result.Warnings)
	}
	if store.value.Issuer != server.URL || store.value.IDToken != "new-id" || store.value.CurrentSpace != "family" {
		t.Fatalf("stored session = %+v", store.value)
	}
	if _, revoked := server.snapshot(); len(revoked) != 1 || revoked[0] != "old-id" {
		t.Fatalf("revoked = %v, want the previous session revoked at the current issuer", revoked)
	}

	stderr.Reset()
	if _, err := flow.Run(context.Background(), io.Discard, &stderr); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("second login repeated the notice: %q", stderr.String())
	}
}

func TestRun_FallsBackWhileIssuerIsNotServing(t *testing.T) {
	fallback := newRecordingServer(t)
	issuer := closedLoopbackURL(t)
	store := &memoryStore{}
	flow, err := New(Options{Issuer: issuer, FallbackIssuers: []string{fallback.URL}, HTTPClient: fallback.Client(), Exchange: newSessionExchange(), Store: store, Project: "sneat-eur3-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := flow.Run(context.Background(), io.Discard, io.Discard); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if store.value.Issuer != issuer {
		t.Fatalf("session issuer = %q, want the new issuer %q", store.value.Issuer, issuer)
	}
	if paths, _ := fallback.snapshot(); strings.Join(paths, ",") != "/oauth/device/code,/oauth/token,/oauth/userinfo" {
		t.Fatalf("fallback paths = %v", paths)
	}
}

func TestLogout_RevokesPreviousIssuerSessionAtCurrentIssuer(t *testing.T) {
	for _, tc := range []struct {
		name        string
		expiresAt   time.Time
		wantRevoked string
	}{
		{name: "current session", expiresAt: time.Now().Add(time.Hour), wantRevoked: "old-id"},
		{name: "expired session", expiresAt: time.Now().Add(-time.Hour), wantRevoked: "refreshed-id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := newRecordingServer(t)
			store := previousIssuerSession(tc.expiresAt)
			flow, err := New(Options{
				Issuer: server.URL, PreviousIssuers: []string{PreviousIssuer}, HTTPClient: server.Client(),
				Exchange: newSessionExchange(),
				Refresh: refreshFunc(func(context.Context, string) (sneatauth.Result, error) {
					return sneatauth.Result{IDToken: "refreshed-id", ExpiresIn: time.Hour}, nil
				}),
				Store: store, Project: "sneat-eur3-1",
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := flow.Logout(context.Background()); err != nil {
				t.Fatalf("Logout: %v", err)
			}
			if store.has {
				t.Fatal("local session retained after logout")
			}
			if _, revoked := server.snapshot(); len(revoked) != 1 || revoked[0] != tc.wantRevoked {
				t.Fatalf("revoked = %v, want %q", revoked, tc.wantRevoked)
			}
		})
	}
}
