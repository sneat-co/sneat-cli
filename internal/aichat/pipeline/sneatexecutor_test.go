package pipeline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/strongo/aichat/ai/session"
	"golang.org/x/oauth2"

	"github.com/sneat-co/sneat-cli/internal/aichat/data"
	"github.com/sneat-co/sneat-cli/internal/sneatapi"
)

type staticTokenSource struct{}

func (staticTokenSource) Token() (*oauth2.Token, error) {
	return &oauth2.Token{AccessToken: "test-token"}, nil
}

// newTestSneatAPI starts an httptest server recording every request's method
// and path (and decoded JSON body, when present) and returns a real
// *sneatapi.Client pointed at it, so tests exercise the ACTUAL HTTP request
// shape SneatExecutor sends -- never a Firestore write, per brief §17.
func newTestSneatAPI(t *testing.T, respond func(w http.ResponseWriter, r *http.Request, body map[string]any)) (*sneatapi.Client, *[]string) {
	t.Helper()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		var body map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		if respond != nil {
			respond(w, r, body)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return sneatapi.New(srv.URL+"/v0/", staticTokenSource{}, srv.Client()), &calls
}

func TestSneatExecutor_RescheduleHappening_SendsUpdateSlot(t *testing.T) {
	api, calls := newTestSneatAPI(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if r.URL.Path != "/v0/happenings/update_slot" {
			t.Errorf("path = %s, want /v0/happenings/update_slot", r.URL.Path)
		}
		slot, _ := body["slot"].(map[string]any)
		if slot["id"] != "s1" {
			t.Errorf("slot.id = %v, want s1", slot["id"])
		}
		start, _ := slot["start"].(map[string]any)
		if start["date"] != "2026-09-25" || start["time"] != "16:00" {
			t.Errorf("slot.start = %v, want 2026-09-25 16:00", start)
		}
		w.WriteHeader(http.StatusOK)
	})
	happenings := &data.FakeHappenings{Items: []data.Happening{
		{ID: "h1", SpaceID: "sp1", Title: "Dentist", SlotID: "s1",
			Start: time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 24, 10, 30, 0, 0, time.UTC)},
	}}
	exec := SneatExecutor{Calendar: api, Happenings: happenings, Now: func() time.Time { return time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC) }}
	target := session.EntityRef{Type: "happening", Title: "Dentist", Keys: map[string]string{"spaceID": "sp1", "happeningID": "h1"}}
	undo, err := exec.Execute(context.Background(), session.Action{
		Kind: "calendar.reschedule_happening", Target: &target, Args: map[string]string{"when": "tomorrow 16:00"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("calls = %v", *calls)
	}
	if undo == nil || undo.Args["when"] != "2026-09-24 10:00" {
		t.Fatalf("undo = %+v, want the original start time", undo)
	}
}

func TestSneatExecutor_CancelHappening_SendsCancelAndUndoRevokes(t *testing.T) {
	api, calls := newTestSneatAPI(t, nil)
	exec := SneatExecutor{Calendar: api}
	target := session.EntityRef{Type: "happening", Title: "Standup", Keys: map[string]string{"spaceID": "sp1", "happeningID": "h1"}}
	undo, err := exec.Execute(context.Background(), session.Action{Kind: "calendar.cancel_happening", Target: &target})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if undo == nil || undo.Kind != calendarRevokeCancellationKind {
		t.Fatalf("undo = %+v", undo)
	}
	if _, err := exec.Execute(context.Background(), *undo); err != nil {
		t.Fatalf("Execute(undo): %v", err)
	}
	want := []string{"POST /v0/happenings/cancel_happening", "POST /v0/happenings/revoke_happening_cancellation"}
	if len(*calls) != 2 || (*calls)[0] != want[0] || (*calls)[1] != want[1] {
		t.Fatalf("calls = %v, want %v", *calls, want)
	}
}

func TestSneatExecutor_CompleteTodo_SendsSetIsDoneAndUndoReopens(t *testing.T) {
	api, calls := newTestSneatAPI(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if body["isDone"] != true {
			t.Errorf("isDone = %v, want true", body["isDone"])
		}
	})
	exec := SneatExecutor{Todo: api}
	target := session.EntityRef{Type: "todo", Title: "Buy milk", Keys: map[string]string{"spaceID": "sp1", "list": "do", "itemID": "t1"}}
	undo, err := exec.Execute(context.Background(), session.Action{Kind: "todo.complete_todo", Target: &target})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if undo == nil || undo.Kind != "todo.reopen_todo" {
		t.Fatalf("undo = %+v", undo)
	}
	if len(*calls) != 1 || (*calls)[0] != "POST /v0/listus/list_items_set_is_done" {
		t.Fatalf("calls = %v", *calls)
	}
}

func TestSneatExecutor_AddTodo_UsesCreatedIDForUndo(t *testing.T) {
	api, calls := newTestSneatAPI(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"new1","title":"Buy milk"}]}`))
	})
	exec := SneatExecutor{Todo: api}
	undo, err := exec.Execute(context.Background(), session.Action{
		Kind: "todo.add_todo", Args: map[string]string{"spaceID": "sp1", "title": "Buy milk"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if undo == nil || undo.Kind != "todo.delete_todo" || undo.Target.Keys["itemID"] != "new1" {
		t.Fatalf("undo = %+v", undo)
	}
	if len(*calls) != 1 || (*calls)[0] != "POST /v0/listus/list_items_create" {
		t.Fatalf("calls = %v", *calls)
	}
}

func TestSneatExecutor_DeleteTodo_NoUndo(t *testing.T) {
	api, calls := newTestSneatAPI(t, nil)
	exec := SneatExecutor{Todo: api}
	target := session.EntityRef{Type: "todo", Keys: map[string]string{"spaceID": "sp1", "list": "do", "itemID": "t1"}}
	undo, err := exec.Execute(context.Background(), session.Action{Kind: "todo.delete_todo", Target: &target})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if undo != nil {
		t.Fatalf("undo = %+v, want nil (delete is not undoable via this API)", undo)
	}
	if len(*calls) != 1 || (*calls)[0] != "DELETE /v0/listus/list_items_delete" {
		t.Fatalf("calls = %v", *calls)
	}
}

func TestSneatExecutor_UnknownKindErrors(t *testing.T) {
	exec := SneatExecutor{}
	_, err := exec.Execute(context.Background(), session.Action{Kind: "contacts.teleport"})
	if err == nil {
		t.Fatal("expected an error for an unknown action kind")
	}
}
