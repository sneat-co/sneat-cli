package sneatapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sneat-co/contactus/backend/dto4contactus"
	"golang.org/x/oauth2"
)

type fakeTS struct{}

func (fakeTS) Token() (*oauth2.Token, error) { return &oauth2.Token{AccessToken: "idt"}, nil }

func TestCreateContact_PostsWithAuth(t *testing.T) {
	var gotAuth, gotPath, gotMethod, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath, gotMethod = r.Header.Get("Authorization"), r.URL.Path, r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{"contact":{"id":"c1"}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, fakeTS{}, srv.Client())
	out, err := c.CreateContact(context.Background(), dto4contactus.CreateContactRequest{Type: "person", Status: "active"})
	if err != nil {
		t.Fatalf("CreateContact: %v", err)
	}
	if gotAuth != "Bearer idt" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if gotMethod != http.MethodPost || !strings.HasSuffix(gotPath, "contactus/create_contact") {
		t.Fatalf("%s %s", gotMethod, gotPath)
	}
	if !strings.Contains(gotBody, `"type":"person"`) {
		t.Fatalf("body = %s", gotBody)
	}
	if out["contact"] == nil {
		t.Fatalf("out = %+v", out)
	}
}

func TestDeleteContact(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, fakeTS{}, srv.Client())
	if err := c.DeleteContact(context.Background(), dto4contactus.ContactRequest{ContactID: "c1"}); err != nil {
		t.Fatalf("DeleteContact: %v", err)
	}
	if gotMethod != http.MethodDelete || !strings.HasSuffix(gotPath, "contactus/delete_contact") {
		t.Fatalf("%s %s", gotMethod, gotPath)
	}
}

func TestDo_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	c := New(srv.URL, fakeTS{}, srv.Client())
	err := c.DeleteContact(context.Background(), dto4contactus.ContactRequest{ContactID: "c1"})
	if err == nil || !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "bad request") {
		t.Fatalf("err = %v", err)
	}
}
