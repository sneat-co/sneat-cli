package sneatauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSignInWithPassword_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "accounts:signInWithPassword") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "test-key" {
			t.Errorf("missing api key")
		}
		_, _ = w.Write([]byte(`{"idToken":"idt","refreshToken":"rft","localId":"u1","email":"a@b.c","expiresIn":"3600"}`))
	}))
	defer srv.Close()

	c := newWithBases(srv.URL, srv.URL, "test-key", srv.Client())
	res, err := c.SignInWithPassword(context.Background(), "a@b.c", "pw")
	if err != nil {
		t.Fatalf("SignInWithPassword: %v", err)
	}
	if res.IDToken != "idt" || res.UID != "u1" || res.Email != "a@b.c" {
		t.Fatalf("bad result: %+v", res)
	}
	if res.ExpiresIn.Seconds() != 3600 {
		t.Fatalf("expiresIn = %v", res.ExpiresIn)
	}
}

func TestSignInWithPassword_ErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"INVALID_PASSWORD"}}`))
	}))
	defer srv.Close()

	c := newWithBases(srv.URL, srv.URL, "k", srv.Client())
	if _, err := c.SignInWithPassword(context.Background(), "a@b.c", "bad"); err == nil ||
		!strings.Contains(err.Error(), "INVALID_PASSWORD") {
		t.Fatalf("err = %v, want INVALID_PASSWORD", err)
	}
}

func TestRefresh_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id_token":"idt2","refresh_token":"rft2","user_id":"u1","expires_in":"3600"}`))
	}))
	defer srv.Close()

	c := newWithBases(srv.URL, srv.URL, "k", srv.Client())
	res, err := c.Refresh(context.Background(), "rft")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if res.IDToken != "idt2" || res.RefreshToken != "rft2" || res.UID != "u1" {
		t.Fatalf("bad result: %+v", res)
	}
}

func TestSignInWithCustomToken_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "accounts:signInWithCustomToken") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"idToken":"idt3","refreshToken":"rft3","localId":"u1","email":"a@b.c","expiresIn":"3600"}`))
	}))
	defer srv.Close()

	c := newWithBases(srv.URL, srv.URL, "k", srv.Client())
	res, err := c.SignInWithCustomToken(context.Background(), "custom-token")
	if err != nil {
		t.Fatalf("SignInWithCustomToken: %v", err)
	}
	if res.IDToken != "idt3" || res.RefreshToken != "rft3" || res.UID != "u1" {
		t.Fatalf("bad result: %+v", res)
	}
}

func TestSignInWithCustomToken_ErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"INVALID_CUSTOM_TOKEN"}}`))
	}))
	defer srv.Close()

	c := newWithBases(srv.URL, srv.URL, "k", srv.Client())
	if _, err := c.SignInWithCustomToken(context.Background(), "bad"); err == nil ||
		!strings.Contains(err.Error(), "INVALID_CUSTOM_TOKEN") {
		t.Fatalf("err = %v, want INVALID_CUSTOM_TOKEN", err)
	}
}

func TestRefresh_ErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"INVALID_REFRESH_TOKEN"}}`))
	}))
	defer srv.Close()

	c := newWithBases(srv.URL, srv.URL, "k", srv.Client())
	if _, err := c.Refresh(context.Background(), "bad"); err == nil ||
		!strings.Contains(err.Error(), "INVALID_REFRESH_TOKEN") {
		t.Fatalf("err = %v, want INVALID_REFRESH_TOKEN", err)
	}
}

func TestDoJSON_InvalidRequestURL(t *testing.T) {
	// A control character in the endpoint makes http.NewRequestWithContext
	// fail before any network I/O happens.
	c := newWithBases("http://\x7f", "http://\x7f", "k", http.DefaultClient)
	if _, err := c.SignInWithPassword(context.Background(), "a@b.c", "pw"); err == nil {
		t.Fatalf("expected request construction error")
	}
}

func TestDoJSON_DoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	url := srv.URL
	srv.Close() // closed: connection refused on any request

	c := newWithBases(url, url, "k", http.DefaultClient)
	if _, err := c.SignInWithPassword(context.Background(), "a@b.c", "pw"); err == nil {
		t.Fatalf("expected transport error")
	}
}

func TestDoJSON_InvalidJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer srv.Close()

	c := newWithBases(srv.URL, srv.URL, "k", srv.Client())
	if _, err := c.SignInWithPassword(context.Background(), "a@b.c", "pw"); err == nil {
		t.Fatalf("expected unmarshal error")
	}
}

func TestDoJSON_ErrorStatusWithoutMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newWithBases(srv.URL, srv.URL, "k", srv.Client())
	if _, err := c.SignInWithPassword(context.Background(), "a@b.c", "pw"); err == nil ||
		!strings.Contains(err.Error(), "http 500") {
		t.Fatalf("err = %v, want http 500", err)
	}
}

func TestNew_EmulatorBases(t *testing.T) {
	c := New(Options{APIKey: "k", AuthEmulatorHost: "localhost:9099"})
	if !strings.HasPrefix(c.identityBase, "http://localhost:9099/identitytoolkit.googleapis.com") {
		t.Fatalf("identityBase = %q", c.identityBase)
	}
	if !strings.HasPrefix(c.secureTokenBase, "http://localhost:9099/securetoken.googleapis.com") {
		t.Fatalf("secureTokenBase = %q", c.secureTokenBase)
	}
}
