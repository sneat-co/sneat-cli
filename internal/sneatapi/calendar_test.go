package sneatapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sneat-co/calendarius/backend/dto4calendarius"
)

// TestCalendarEndpoints drives every calendar.go method against a stub
// server and asserts each POSTs its own calendarius path, the same
// method+path contract sneatapi_test.go's TestCreateContact_PostsWithAuth
// already asserts for the contactus endpoints.
func TestCalendarEndpoints(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := New(srv.URL, fakeTS{}, srv.Client())
	ctx := context.Background()

	cases := []struct {
		name       string
		call       func() error
		wantSuffix string
	}{
		{"UpdateSlot", func() error {
			return c.UpdateSlot(ctx, dto4calendarius.HappeningSlotRequest{})
		}, "happenings/update_slot"},
		{"AdjustSlot", func() error {
			return c.AdjustSlot(ctx, dto4calendarius.HappeningSlotDateRequest{})
		}, "happenings/adjust_slot"},
		{"CancelAdjustment", func() error {
			return c.CancelAdjustment(ctx, dto4calendarius.HappeningDateSlotIDRequest{})
		}, "happenings/cancel_adjustment"},
		{"CancelHappening", func() error {
			return c.CancelHappening(ctx, dto4calendarius.CancelHappeningRequest{})
		}, "happenings/cancel_happening"},
		{"RevokeHappeningCancellation", func() error {
			return c.RevokeHappeningCancellation(ctx, dto4calendarius.CancelHappeningRequest{})
		}, "happenings/revoke_happening_cancellation"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotMethod, gotPath = "", ""
			if err := tc.call(); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if gotMethod != http.MethodPost {
				t.Fatalf("%s: method = %q, want POST", tc.name, gotMethod)
			}
			if !strings.HasSuffix(gotPath, tc.wantSuffix) {
				t.Fatalf("%s: path = %q, want suffix %q", tc.name, gotPath, tc.wantSuffix)
			}
		})
	}
}

func TestCalendariusPath_StripsV0Prefix(t *testing.T) {
	if got := calendariusPath("/v0/happenings/update_slot"); got != "happenings/update_slot" {
		t.Fatalf("calendariusPath = %q", got)
	}
}
