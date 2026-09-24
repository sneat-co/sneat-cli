package pipeline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sneat-co/calendarius/backend/dbo4calendarius"
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

// realisticSlot is a full calendarius slot (weekdays/timezone/locations),
// per the brief's testing guidance -- not a bare timing struct -- so B2's
// "preserve every non-time field" assertion actually exercises fields a
// naive update_slot request would drop.
func realisticSlot(startDate, startTime, endTime string) dbo4calendarius.HappeningSlot {
	return dbo4calendarius.HappeningSlot{
		HappeningSlotTiming: dbo4calendarius.HappeningSlotTiming{
			Timing: dbo4calendarius.Timing{
				Start:    dbo4calendarius.DateTime{Date: startDate, Time: startTime},
				End:      dbo4calendarius.DateTime{Date: startDate, Time: endTime},
				TimeZone: "America/New_York",
			},
			Repeats: dbo4calendarius.RepeatPeriodOnce,
		},
		Locations: []dbo4calendarius.Location{{Type: "physical", Title: "Downtown Dental"}},
	}
}

func TestSneatExecutor_RescheduleHappening_PreservesFullSlot(t *testing.T) {
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
		// B2: everything besides the time must survive untouched.
		if slot["timeZone"] != "America/New_York" {
			t.Errorf("slot.timeZone = %v, want preserved America/New_York", slot["timeZone"])
		}
		locations, _ := slot["locations"].([]any)
		if len(locations) != 1 {
			t.Errorf("slot.locations = %v, want the original location preserved", slot["locations"])
		}
		w.WriteHeader(http.StatusOK)
	})
	slot := realisticSlot("2026-09-24", "10:00", "10:30")
	happenings := &data.FakeHappenings{Items: []data.Happening{
		{ID: "h1", SpaceID: "sp1", Title: "Dentist", SlotID: "s1",
			Start: time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 24, 10, 30, 0, 0, time.UTC),
			Slot: &slot},
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

// TestSneatExecutor_RescheduleHappening_NoSlot_Errors: a happening with no
// preserved slot data cannot be safely rescheduled (there is nothing to
// round-trip) -- must error rather than silently building a bare slot that
// would drop fields on write.
func TestSneatExecutor_RescheduleHappening_NoSlot_Errors(t *testing.T) {
	happenings := &data.FakeHappenings{Items: []data.Happening{
		{ID: "h1", SpaceID: "sp1", Title: "Dentist", SlotID: "s1",
			Start: time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)}, // Slot left nil
	}}
	exec := SneatExecutor{Calendar: &sneatapi.Client{}, Happenings: happenings, Now: func() time.Time { return time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC) }}
	target := session.EntityRef{Type: "happening", Title: "Dentist", Keys: map[string]string{"spaceID": "sp1", "happeningID": "h1"}}
	_, err := exec.Execute(context.Background(), session.Action{
		Kind: "calendar.reschedule_happening", Target: &target, Args: map[string]string{"when": "tomorrow 16:00"},
	})
	if err == nil {
		t.Fatal("expected an error when the happening has no preserved slot")
	}
}

// TestSneatExecutor_RescheduleHappening_Recurring_UsesAdjustSlot covers the
// B2 ruling's recurring branch: moving "this Friday's Yoga" (weekly) sends
// adjust_slot (a per-date deviation) with the FULL slot -- weekdays included
// -- not update_slot, which would rewrite the template every future
// occurrence inherits.
func TestSneatExecutor_RescheduleHappening_Recurring_UsesAdjustSlot(t *testing.T) {
	api, calls := newTestSneatAPI(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if r.URL.Path != "/v0/happenings/adjust_slot" {
			t.Errorf("path = %s, want /v0/happenings/adjust_slot", r.URL.Path)
		}
		if body["date"] != "2026-09-25" {
			t.Errorf("date = %v, want 2026-09-25 (the adjusted occurrence, not the template date)", body["date"])
		}
		slot, _ := body["slot"].(map[string]any)
		start, _ := slot["start"].(map[string]any)
		if start["time"] != "16:00" {
			t.Errorf("slot.start.time = %v, want 16:00", start["time"])
		}
		weekdays, _ := slot["weekdays"].([]any)
		if len(weekdays) != 1 || weekdays[0] != "fr" {
			t.Errorf("slot.weekdays = %v, want [\"fr\"] preserved from the template", slot["weekdays"])
		}
		w.WriteHeader(http.StatusOK)
	})
	slot := dbo4calendarius.HappeningSlot{
		HappeningSlotTiming: dbo4calendarius.HappeningSlotTiming{
			Timing:   dbo4calendarius.Timing{Start: dbo4calendarius.DateTime{Date: "2026-09-18", Time: "09:00"}, End: dbo4calendarius.DateTime{Date: "2026-09-18", Time: "09:30"}},
			Repeats:  dbo4calendarius.RepeatPeriodWeekly,
			Weekdays: []dbo4calendarius.WeekdayCode{dbo4calendarius.Friday2},
		},
	}
	happenings := &data.FakeHappenings{Items: []data.Happening{
		{ID: "h2", SpaceID: "sp1", Title: "Yoga", SlotID: "s1", Recurring: true,
			Start: time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 18, 9, 30, 0, 0, time.UTC),
			Slot: &slot},
	}}
	// "now" is Friday 2026-09-25 09:00 so "tomorrow"/plain-time resolution
	// below picks this Friday, not the template's stale 09-18 date.
	exec := SneatExecutor{Calendar: api, Happenings: happenings, Now: func() time.Time { return time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC) }}
	target := session.EntityRef{Type: "happening", Title: "Yoga", Keys: map[string]string{"spaceID": "sp1", "happeningID": "h2"}}
	_, err := exec.Execute(context.Background(), session.Action{
		Kind: "calendar.reschedule_happening", Target: &target, Args: map[string]string{"when": "16:00"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(*calls) != 1 || (*calls)[0] != "POST /v0/happenings/adjust_slot" {
		t.Fatalf("calls = %v", *calls)
	}
}

func TestSneatExecutor_CancelHappening_SendsCancelAndUndoRevokes(t *testing.T) {
	api, calls := newTestSneatAPI(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		// A non-recurring happening's cancel/revoke must NOT carry a
		// date/slotID -- that would cancel/revoke one occurrence of a series
		// this happening isn't.
		if _, has := body["date"]; has {
			t.Errorf("body = %v, single happening must not send a date", body)
		}
	})
	happenings := &data.FakeHappenings{Items: []data.Happening{
		{ID: "h1", SpaceID: "sp1", Title: "Standup"}, // Recurring: false
	}}
	exec := SneatExecutor{Calendar: api, Happenings: happenings}
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

// TestSneatExecutor_CancelHappening_Recurring_CancelsOnlyOneOccurrence is
// S6's cancel ruling: a recurring happening's cancel sends the occurrence's
// date+slotID, not a bare happening-wide cancel, and undo revokes that SAME
// occurrence.
func TestSneatExecutor_CancelHappening_Recurring_CancelsOnlyOneOccurrence(t *testing.T) {
	var lastCancelDate, lastRevokeDate string
	api, calls := newTestSneatAPI(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		date, _ := body["date"].(string)
		slotID, _ := body["slotID"].(string)
		if slotID != "s1" {
			t.Errorf("slotID = %q, want s1", slotID)
		}
		switch r.URL.Path {
		case "/v0/happenings/cancel_happening":
			lastCancelDate = date
		case "/v0/happenings/revoke_happening_cancellation":
			lastRevokeDate = date
		}
	})
	slot := dbo4calendarius.HappeningSlot{
		HappeningSlotTiming: dbo4calendarius.HappeningSlotTiming{
			Timing:   dbo4calendarius.Timing{Start: dbo4calendarius.DateTime{Date: "2026-09-18", Time: "09:00"}},
			Repeats:  dbo4calendarius.RepeatPeriodWeekly,
			Weekdays: []dbo4calendarius.WeekdayCode{dbo4calendarius.Friday2},
		},
	}
	happenings := &data.FakeHappenings{Items: []data.Happening{
		{ID: "h2", SpaceID: "sp1", Title: "Yoga", SlotID: "s1", Recurring: true,
			Start: time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC), Slot: &slot},
	}}
	// "now" is Friday 2026-09-25 -- recurringAnchor resolves the current
	// occurrence to that date, not the template's stale 09-18.
	exec := SneatExecutor{Calendar: api, Happenings: happenings, Now: func() time.Time { return time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC) }}
	target := session.EntityRef{Type: "happening", Title: "Yoga", Keys: map[string]string{"spaceID": "sp1", "happeningID": "h2"}}
	undo, err := exec.Execute(context.Background(), session.Action{Kind: "calendar.cancel_happening", Target: &target})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if lastCancelDate != "2026-09-25" {
		t.Errorf("cancel date = %q, want 2026-09-25 (this occurrence)", lastCancelDate)
	}
	if _, err := exec.Execute(context.Background(), *undo); err != nil {
		t.Fatalf("Execute(undo): %v", err)
	}
	if lastRevokeDate != "2026-09-25" {
		t.Errorf("revoke date = %q, want the SAME occurrence date the cancel used", lastRevokeDate)
	}
	if len(*calls) != 2 {
		t.Fatalf("calls = %v", *calls)
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
