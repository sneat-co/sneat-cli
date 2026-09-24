package sneatapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sneat-co/contactus/backend/dto4contactus"
	"golang.org/x/oauth2"
)

// TestCreateContact_Error covers CreateContact's error-propagation branch.
func TestCreateContact_Error(t *testing.T) {
	wantErr := errors.New("token unavailable")
	c := New("https://api.example.com", errTokenSource{err: wantErr}, http.DefaultClient)
	out, err := c.CreateContact(context.Background(), dto4contactus.CreateContactRequest{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("CreateContact() err = %v, want %v", err, wantErr)
	}
	if out != nil {
		t.Fatalf("CreateContact() out = %+v, want nil on error", out)
	}
}

// TestNew_DefaultsAndNormalizesBaseURL covers New's nil-http.Client default
// and its baseURL-without-trailing-slash normalization branch.
func TestNew_DefaultsAndNormalizesBaseURL(t *testing.T) {
	c := New("https://api.example.com", fakeTS{}, nil)
	if c.http != http.DefaultClient {
		t.Fatalf("http = %v, want http.DefaultClient when hc is nil", c.http)
	}
	if c.baseURL != "https://api.example.com/" {
		t.Fatalf("baseURL = %q, want a trailing slash appended", c.baseURL)
	}

	// Already-terminated baseURL is left alone (no double slash).
	c2 := New("https://api.example.com/", fakeTS{}, nil)
	if c2.baseURL != "https://api.example.com/" {
		t.Fatalf("baseURL = %q, want unchanged", c2.baseURL)
	}
}

type unmarshalableBody struct {
	Ch chan int
}

// TestDo_MarshalError covers do's json.Marshal error branch.
func TestDo_MarshalError(t *testing.T) {
	c := New("https://api.example.com", fakeTS{}, http.DefaultClient)
	err := c.do(context.Background(), http.MethodPost, "x", unmarshalableBody{Ch: make(chan int)}, nil)
	if err == nil {
		t.Fatal("do() = nil, want a json.Marshal error")
	}
}

// TestDo_NewRequestError covers do's http.NewRequestWithContext error
// branch: an invalid method (containing a space) is rejected by net/http.
func TestDo_NewRequestError(t *testing.T) {
	c := New("https://api.example.com", fakeTS{}, http.DefaultClient)
	err := c.do(context.Background(), "IN VALID", "x", nil, nil)
	if err == nil {
		t.Fatal("do() = nil, want an invalid-method error")
	}
}

type errTokenSource struct{ err error }

func (e errTokenSource) Token() (*oauth2.Token, error) { return nil, e.err }

// TestDo_TokenError covers do's TokenSource.Token() error branch.
func TestDo_TokenError(t *testing.T) {
	wantErr := errors.New("token unavailable")
	c := New("https://api.example.com", errTokenSource{err: wantErr}, http.DefaultClient)
	err := c.do(context.Background(), http.MethodGet, "x", nil, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("do() = %v, want %v", err, wantErr)
	}
}

// TestDo_TransportError covers do's http.Client.Do error branch: a server
// that is closed before the request lands refuses the connection.
func TestDo_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // now nothing is listening on url

	c := New(url, fakeTS{}, http.DefaultClient)
	err := c.do(context.Background(), http.MethodGet, "x", nil, nil)
	if err == nil {
		t.Fatal("do() = nil, want a connection error against a closed server")
	}
}
