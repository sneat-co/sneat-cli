package sneatapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sneat-co/listus/backend/dto4listus"
)

// TestListusEndpoints drives every listus.go method against a stub server
// and asserts each call's method + path, the same contract
// sneatapi_test.go's TestCreateContact_PostsWithAuth asserts for contactus.
func TestListusEndpoints(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := New(srv.URL, fakeTS{}, srv.Client())
	ctx := context.Background()

	t.Run("CreateListItems", func(t *testing.T) {
		gotMethod, gotPath = "", ""
		if _, err := c.CreateListItems(ctx, dto4listus.CreateListItemsRequest{}); err != nil {
			t.Fatalf("CreateListItems: %v", err)
		}
		if gotMethod != http.MethodPost {
			t.Fatalf("method = %q, want POST", gotMethod)
		}
		if !strings.HasSuffix(gotPath, "listus/list_items_create") {
			t.Fatalf("path = %q", gotPath)
		}
	})

	t.Run("SetListItemsIsDone", func(t *testing.T) {
		gotMethod, gotPath = "", ""
		if err := c.SetListItemsIsDone(ctx, dto4listus.ListItemsSetIsDoneRequest{IsDone: true}); err != nil {
			t.Fatalf("SetListItemsIsDone: %v", err)
		}
		if gotMethod != http.MethodPost {
			t.Fatalf("method = %q, want POST", gotMethod)
		}
		if !strings.HasSuffix(gotPath, "listus/list_items_set_is_done") {
			t.Fatalf("path = %q", gotPath)
		}
	})

	t.Run("DeleteListItems", func(t *testing.T) {
		gotMethod, gotPath = "", ""
		if err := c.DeleteListItems(ctx, dto4listus.ListItemIDsRequest{ItemIDs: []string{"i1"}}); err != nil {
			t.Fatalf("DeleteListItems: %v", err)
		}
		if gotMethod != http.MethodDelete {
			t.Fatalf("method = %q, want DELETE", gotMethod)
		}
		if !strings.HasSuffix(gotPath, "listus/list_items_delete") {
			t.Fatalf("path = %q", gotPath)
		}
	})
}

func TestListusPath_StripsV0Prefix(t *testing.T) {
	if got := listusPath("/v0/listus/list_items_create"); got != "listus/list_items_create" {
		t.Fatalf("listusPath = %q", got)
	}
}
