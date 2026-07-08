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

func TestNew_EmulatorBases(t *testing.T) {
	c := New(Options{APIKey: "k", AuthEmulatorHost: "localhost:9099"})
	if !strings.HasPrefix(c.identityBase, "http://localhost:9099/identitytoolkit.googleapis.com") {
		t.Fatalf("identityBase = %q", c.identityBase)
	}
	if !strings.HasPrefix(c.secureTokenBase, "http://localhost:9099/securetoken.googleapis.com") {
		t.Fatalf("secureTokenBase = %q", c.secureTokenBase)
	}
}
