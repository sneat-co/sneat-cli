package deviceflow

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sneat-co/sneat-cli/internal/sneatauth"
	"github.com/strongo/deviceauth"
)

type exchangeFunc func(context.Context, string) (sneatauth.Result, error)

func (f exchangeFunc) SignInWithCustomToken(ctx context.Context, token string) (sneatauth.Result, error) {
	return f(ctx, token)
}

type memoryStore struct{ credential deviceauth.Credential }

func (s *memoryStore) Save(value deviceauth.Credential) error { s.credential = value; return nil }
func (s *memoryStore) Load() (deviceauth.Credential, error) {
	if s.credential.AccessToken == "" {
		return deviceauth.Credential{}, deviceauth.ErrCredentialNotFound
	}
	return s.credential, nil
}
func (s *memoryStore) Delete() error { s.credential = deviceauth.Credential{}; return nil }

func TestRun_ExchangesAndValidatesAudience(t *testing.T) {
	var gotFirebaseToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/oauth/device/code":
			if got := r.FormValue("client_id"); got != ClientID {
				t.Errorf("client_id = %q", got)
			}
			if got := r.FormValue("scope"); got != strings.Join(Scopes, " ") {
				t.Errorf("scope = %q", got)
			}
			_, _ = io.WriteString(w, `{"device_code":"device","user_code":"ABCD-EFGH","verification_uri":"https://verify.example/device","expires_in":300,"interval":1}`)
		case "/oauth/token":
			_, _ = io.WriteString(w, `{"access_token":"custom-token","token_type":"urn:ietf:params:oauth:token-type:firebase-custom-token","expires_in":300}`)
		case "/oauth/userinfo":
			if got := r.Header.Get("Authorization"); got != "Bearer firebase-id" {
				t.Errorf("Authorization = %q", got)
			}
			_, _ = io.WriteString(w, `{"sub":"user-1","aud":"sneat-cli","scope":"openid profile sneat:spaces:read sneat:spaces:write"}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	defer server.Close()

	flow, err := New(Options{
		Issuer:      server.URL,
		HTTPClient:  server.Client(),
		OpenBrowser: func(string) error { return nil },
		Exchange: exchangeFunc(func(_ context.Context, token string) (sneatauth.Result, error) {
			gotFirebaseToken = token
			return sneatauth.Result{IDToken: "firebase-id", RefreshToken: "firebase-refresh", UID: "user-1", ExpiresIn: time.Hour}, nil
		}),
		Store: &memoryStore{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var output, errorOutput bytes.Buffer
	result, err := flow.Run(context.Background(), &output, &errorOutput)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotFirebaseToken != "custom-token" || result.RefreshToken != "firebase-refresh" {
		t.Fatalf("unexpected exchange result: token=%q result=%+v", gotFirebaseToken, result)
	}
	if strings.Contains(output.String(), "custom-token") || strings.Contains(errorOutput.String(), "custom-token") {
		t.Fatalf("custom token leaked to output")
	}
}

func TestRun_BrowserFailureIsManualFallback(t *testing.T) {
	server := newDeviceServer(t, ClientID)
	defer server.Close()
	flow, err := New(Options{
		Issuer:      server.URL,
		HTTPClient:  server.Client(),
		OpenBrowser: func(string) error { return errors.New("no browser") },
		Exchange: exchangeFunc(func(context.Context, string) (sneatauth.Result, error) {
			return sneatauth.Result{IDToken: "firebase-id", UID: "user-1"}, nil
		}),
		Store: &memoryStore{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var output, errorOutput bytes.Buffer
	if _, err := flow.Run(context.Background(), &output, &errorOutput); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(output.String(), "ABCD-EFGH") || !strings.Contains(errorOutput.String(), "Continue with the URL above") {
		t.Fatalf("manual fallback was not printed: out=%q err=%q", output.String(), errorOutput.String())
	}
}

func TestRun_RejectsWrongAudience(t *testing.T) {
	server := newDeviceServer(t, "datatug-cli")
	defer server.Close()
	flow, err := New(Options{Issuer: server.URL, HTTPClient: server.Client(), Exchange: exchangeFunc(func(context.Context, string) (sneatauth.Result, error) {
		return sneatauth.Result{IDToken: "firebase-id", UID: "user-1"}, nil
	}), Store: &memoryStore{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = flow.Run(context.Background(), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "audience") {
		t.Fatalf("Run error = %v, want audience rejection", err)
	}
}

func newDeviceServer(t *testing.T, audience string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/oauth/device/code":
			_, _ = io.WriteString(w, `{"device_code":"device","user_code":"ABCD-EFGH","verification_uri":"https://verify.example/device","expires_in":300,"interval":1}`)
		case "/oauth/token":
			_, _ = io.WriteString(w, `{"access_token":"custom-token","token_type":"urn:ietf:params:oauth:token-type:firebase-custom-token","expires_in":300}`)
		case "/oauth/userinfo":
			_, _ = io.WriteString(w, `{"sub":"user-1","aud":"`+audience+`","scope":"openid profile sneat:spaces:read sneat:spaces:write"}`)
		case "/oauth/revoke":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
}
