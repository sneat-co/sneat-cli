package pipeline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	undo, err := exec.Execute(context.Background(), "sp1", session.Action{
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
	_, err := exec.Execute(context.Background(), "sp1", session.Action{
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
	_, err := exec.Execute(context.Background(), "sp1", session.Action{
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
	undo, err := exec.Execute(context.Background(), "sp1", session.Action{Kind: "calendar.cancel_happening", Target: &target})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if undo == nil || undo.Kind != calendarRevokeCancellationKind {
		t.Fatalf("undo = %+v", undo)
	}
	if _, err := exec.Execute(context.Background(), "sp1", *undo); err != nil {
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
	undo, err := exec.Execute(context.Background(), "sp1", session.Action{Kind: "calendar.cancel_happening", Target: &target})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if lastCancelDate != "2026-09-25" {
		t.Errorf("cancel date = %q, want 2026-09-25 (this occurrence)", lastCancelDate)
	}
	if _, err := exec.Execute(context.Background(), "sp1", *undo); err != nil {
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
	undo, err := exec.Execute(context.Background(), "sp1", session.Action{Kind: "todo.complete_todo", Target: &target})
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
	undo, err := exec.Execute(context.Background(), "sp1", session.Action{
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
	undo, err := exec.Execute(context.Background(), "sp1", session.Action{Kind: "todo.delete_todo", Target: &target})
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
	_, err := exec.Execute(context.Background(), "sp1", session.Action{Kind: "contacts.teleport"})
	if err == nil {
		t.Fatal("expected an error for an unknown action kind")
	}
}

// monFriYoga is a weekly Mon/Fri recurring happening in Europe/London, used
// by the fix-round-3 B1/B2/m4/m5 scenarios below (adapted from the
// coordinator's probe: /private/tmp/.../sneat-cli-r2/.../zz_probe_test.go).
func monFriYoga(t *testing.T) (data.Happening, *time.Location) {
	t.Helper()
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	slot := dbo4calendarius.HappeningSlot{HappeningSlotTiming: dbo4calendarius.HappeningSlotTiming{
		Timing: dbo4calendarius.Timing{
			Start:    dbo4calendarius.DateTime{Date: "2026-01-02", Time: "18:00"},
			End:      dbo4calendarius.DateTime{Date: "2026-01-02", Time: "19:00"},
			TimeZone: "Europe/London",
		},
		Repeats:  dbo4calendarius.RepeatPeriodWeekly,
		Weekdays: []dbo4calendarius.WeekdayCode{dbo4calendarius.Monday2, dbo4calendarius.Friday2},
	}}
	return data.Happening{ID: "yoga", SpaceID: "sp1", Title: "Yoga", SlotID: "s1", Recurring: true, Slot: &slot,
		Start: time.Date(2026, 1, 2, 18, 0, 0, 0, loc), End: time.Date(2026, 1, 2, 19, 0, 0, 0, loc)}, loc
}

// mondayNoon is 2026-09-21 10:00 Europe/London (a Monday); Friday of that
// week is 2026-09-25.
func mondayNoon(t *testing.T) time.Time {
	_, loc := monFriYoga(t)
	return time.Date(2026, 9, 21, 10, 0, 0, 0, loc)
}

// TestSneatExecutor_CancelHappening_HonoursExplicitWhen is B1: cancelling
// "Friday's" yoga on a Monday must cancel FRIDAY's occurrence, not
// recurringAnchor's default (today/next, which on a Monday would be
// today), and the confirmation (via cancelSummary in pipeline.go) must
// show that same resolved date.
func TestSneatExecutor_CancelHappening_HonoursExplicitWhen(t *testing.T) {
	yoga, _ := monFriYoga(t)
	now := mondayNoon(t)
	api, calls := newTestSneatAPI(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if d, _ := body["date"].(string); d != "2026-09-25" {
			t.Errorf("date = %v, want 2026-09-25 (Friday, as the user asked) not today (Monday)", body["date"])
		}
	})
	hs := &data.FakeHappenings{Items: []data.Happening{yoga}}
	exec := SneatExecutor{Calendar: api, Happenings: hs, Now: func() time.Time { return now }}
	target := session.EntityRef{Type: "happening", Title: "Yoga", Keys: map[string]string{"spaceID": "sp1", "happeningID": "yoga"}}
	_, err := exec.Execute(context.Background(), "sp1", session.Action{
		Kind: "calendar.cancel_happening", Target: &target, Args: map[string]string{"when": "Friday's"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("calls = %v", *calls)
	}
}

// TestCancelSummary_ShowsResolvedOccurrenceBeforeAsking is B1's confirmation
// half: the summary text must name the resolved Friday date, computed
// before the user answers yes/no.
func TestCancelSummary_ShowsResolvedOccurrenceBeforeAsking(t *testing.T) {
	yoga, _ := monFriYoga(t)
	now := mondayNoon(t)
	readers := data.Readers{Happenings: &data.FakeHappenings{Items: []data.Happening{yoga}}}
	p := Pipeline{Readers: readers, Now: func() time.Time { return now }}
	target := session.EntityRef{Type: "happening", Title: "Yoga", Keys: map[string]string{"spaceID": "sp1", "happeningID": "yoga"}}
	summary, _, ok := p.cancelSummary(context.Background(), "sp1", target, "Friday's")
	if !ok {
		t.Fatal("cancelSummary: ok = false")
	}
	if !strings.Contains(summary, "Sep 25") || !strings.Contains(summary, "this occurrence only") {
		t.Fatalf("summary = %q, want it to name the resolved Friday Sep 25 occurrence", summary)
	}
}

// TestSneatExecutor_RecurringMoveToOtherDay_DateIsOriginalOccurrence is B2:
// moving Monday's yoga to Wednesday sends adjust_slot with Date = the
// ORIGINAL occurrence (Monday, today), not the new Wednesday date -- the
// slot payload itself carries the new date/time.
func TestSneatExecutor_RecurringMoveToOtherDay_DateIsOriginalOccurrence(t *testing.T) {
	yoga, _ := monFriYoga(t)
	now := mondayNoon(t)
	var gotDate string
	var gotStartDate string
	api, calls := newTestSneatAPI(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		gotDate, _ = body["date"].(string)
		slot, _ := body["slot"].(map[string]any)
		start, _ := slot["start"].(map[string]any)
		gotStartDate, _ = start["date"].(string)
	})
	hs := &data.FakeHappenings{Items: []data.Happening{yoga}}
	exec := SneatExecutor{Calendar: api, Happenings: hs, Now: func() time.Time { return now }}
	target := session.EntityRef{Type: "happening", Title: "Yoga", Keys: map[string]string{"spaceID": "sp1", "happeningID": "yoga"}}
	if _, err := exec.Execute(context.Background(), "sp1", session.Action{
		Kind: "calendar.reschedule_happening", Target: &target, Args: map[string]string{"when": "Wednesday 16:00"},
	}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0] != "POST /v0/happenings/adjust_slot" {
		t.Fatalf("calls = %v", *calls)
	}
	if gotDate != "2026-09-21" {
		t.Errorf("adjust_slot date = %q, want 2026-09-21 (Monday, the ORIGINAL occurrence being moved)", gotDate)
	}
	if gotStartDate != "2026-09-23" {
		t.Errorf("slot.start.date = %q, want 2026-09-23 (Wednesday, the NEW date)", gotStartDate)
	}
}

// TestSneatExecutor_RecurringReschedule_UndoUsesCancelAdjustment is B2's
// undo half: undo calls cancel_adjustment (not another adjust_slot) with
// the SAME date+slotID the move used.
func TestSneatExecutor_RecurringReschedule_UndoUsesCancelAdjustment(t *testing.T) {
	yoga, _ := monFriYoga(t)
	now := mondayNoon(t)
	var paths []string
	var dates []string
	api, _ := newTestSneatAPI(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		paths = append(paths, r.URL.Path)
		if d, ok := body["date"].(string); ok {
			dates = append(dates, d)
		}
	})
	hs := &data.FakeHappenings{Items: []data.Happening{yoga}}
	exec := SneatExecutor{Calendar: api, Happenings: hs, Now: func() time.Time { return now }}
	target := session.EntityRef{Type: "happening", Title: "Yoga", Keys: map[string]string{"spaceID": "sp1", "happeningID": "yoga"}}
	undo, err := exec.Execute(context.Background(), "sp1", session.Action{
		Kind: "calendar.reschedule_happening", Target: &target, Args: map[string]string{"when": "Friday 16:00"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if undo == nil || undo.Kind != calendarCancelAdjustmentKind {
		t.Fatalf("undo = %+v, want kind %q", undo, calendarCancelAdjustmentKind)
	}
	if _, err := exec.Execute(context.Background(), "sp1", *undo); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[1] != "/v0/happenings/cancel_adjustment" {
		t.Fatalf("paths = %v, want the second call to be cancel_adjustment", paths)
	}
	if len(dates) != 2 || dates[0] != dates[1] {
		t.Errorf("dates = %v, want undo to cancel the adjustment for the SAME date the move used", dates)
	}
}

// TestSneatExecutor_UTCOffsetRecomputedOnReschedule is m4: a slot recorded
// with a UTCOffset (not just a TimeZone name) must get a FRESH offset for
// the new time, not the stale one from the original instant.
func TestSneatExecutor_UTCOffsetRecomputedOnReschedule(t *testing.T) {
	slot := dbo4calendarius.HappeningSlot{
		HappeningSlotTiming: dbo4calendarius.HappeningSlotTiming{
			Timing: dbo4calendarius.Timing{
				// January in London: GMT, +00:00. Moving to July would be BST
				// (+01:00) if this were a real IANA zone, but a bare UTCOffset
				// slot (no TimeZone name) doesn't know that -- it just needs its
				// OWN new-instant offset recomputed, not left stale.
				Start:     dbo4calendarius.DateTime{Date: "2026-01-02", Time: "10:00"},
				End:       dbo4calendarius.DateTime{Date: "2026-01-02", Time: "11:00"},
				UTCOffset: "+00:00", EndUTCOffset: "+00:00",
			},
			Repeats: dbo4calendarius.RepeatPeriodOnce,
		},
	}
	var gotOffset, gotEndOffset string
	api, _ := newTestSneatAPI(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		slotBody, _ := body["slot"].(map[string]any)
		gotOffset, _ = slotBody["utcOffset"].(string)
		gotEndOffset, _ = slotBody["endUTCOffset"].(string)
	})
	fixedZone := time.FixedZone("+00:00", 0)
	hs := &data.FakeHappenings{Items: []data.Happening{
		{ID: "h1", SpaceID: "sp1", Title: "Call", SlotID: "s1",
			Start: time.Date(2026, 1, 2, 10, 0, 0, 0, fixedZone), End: time.Date(2026, 1, 2, 11, 0, 0, 0, fixedZone),
			Slot: &slot},
	}}
	exec := SneatExecutor{Calendar: api, Happenings: hs, Now: func() time.Time { return time.Date(2026, 1, 2, 9, 0, 0, 0, fixedZone) }}
	target := session.EntityRef{Type: "happening", Title: "Call", Keys: map[string]string{"spaceID": "sp1", "happeningID": "h1"}}
	_, err := exec.Execute(context.Background(), "sp1", session.Action{
		Kind: "calendar.reschedule_happening", Target: &target, Args: map[string]string{"when": "14:00"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotOffset != "+00:00" {
		t.Errorf("utcOffset = %q, want +00:00 (recomputed for the new instant in the same fixed zone)", gotOffset)
	}
	if gotEndOffset != "+00:00" {
		t.Errorf("endUTCOffset = %q, want +00:00", gotEndOffset)
	}
}

// TestRecurringAnchor_SkipsPassedOccurrence is m5: an occurrence already
// passed today is skipped in favour of the NEXT matching day, not returned
// as if it were still upcoming.
func TestRecurringAnchor_SkipsPassedOccurrence(t *testing.T) {
	yoga, loc := monFriYoga(t)
	// 20:00 Monday -- today's 18:00 class already ended.
	now := time.Date(2026, 9, 21, 20, 0, 0, 0, loc)
	got := recurringAnchor(now, yoga)
	want := time.Date(2026, 9, 25, 18, 0, 0, 0, loc) // Friday, not today
	if !got.Equal(want) {
		t.Errorf("recurringAnchor = %v, want %v (Friday, today's class already passed)", got, want)
	}
}

// TestRecurringAnchor_TodayNotYetPassed_ReturnsToday is the companion case:
// before today's occurrence time, recurringAnchor still returns today.
func TestRecurringAnchor_TodayNotYetPassed_ReturnsToday(t *testing.T) {
	yoga, loc := monFriYoga(t)
	now := time.Date(2026, 9, 21, 9, 0, 0, 0, loc) // Monday, before 18:00
	got := recurringAnchor(now, yoga)
	want := time.Date(2026, 9, 21, 18, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("recurringAnchor = %v, want %v (today, not yet passed)", got, want)
	}
}

// TestSneatExecutor_Execute_RejectsCrossSpaceTarget is B3's executor-level
// defense in depth: a resolved Target whose OWN spaceID disagrees with the
// spaceID Execute was called with is refused, never dispatched.
func TestSneatExecutor_Execute_RejectsCrossSpaceTarget(t *testing.T) {
	api, calls := newTestSneatAPI(t, nil)
	exec := SneatExecutor{Calendar: api, Happenings: &data.FakeHappenings{}}
	target := session.EntityRef{Type: "happening", Title: "Old", Keys: map[string]string{"spaceID": "spOLD", "happeningID": "h1"}}
	_, err := exec.Execute(context.Background(), "spNEW", session.Action{Kind: "calendar.cancel_happening", Target: &target})
	if err == nil {
		t.Fatal("expected an error for a cross-space target")
	}
	if len(*calls) != 0 {
		t.Fatalf("calls = %v, want no HTTP call for a rejected cross-space target", *calls)
	}
}

// TestSneatExecutor_AddListItem_IgnoresArgsSpaceID is B3: Args["spaceID"]
// (model-controlled) is never read -- only the spaceID Execute was called
// with is used to build the request.
func TestSneatExecutor_AddListItem_IgnoresArgsSpaceID(t *testing.T) {
	var gotSpaceID string
	api, _ := newTestSneatAPI(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		spaceReq, _ := body["spaceID"].(string)
		gotSpaceID = spaceReq
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"new1","title":"Buy milk"}]}`))
	})
	exec := SneatExecutor{Todo: api}
	_, err := exec.Execute(context.Background(), "sp1", session.Action{
		Kind: "todo.add_todo", Args: map[string]string{"spaceID": "evil", "title": "Buy milk"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotSpaceID != "sp1" {
		t.Errorf("request spaceID = %q, want sp1 (Execute's own param, not Args[\"spaceID\"]=evil)", gotSpaceID)
	}
}
