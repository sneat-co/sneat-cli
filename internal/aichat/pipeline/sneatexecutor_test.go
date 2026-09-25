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
	"github.com/sneat-co/calendarius/backend/dto4calendarius"
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

// TestSplitListItemTitle covers the deterministic multi-item split: comma,
// semicolon and newline separated, surrounding whitespace, the single-item
// pass-through (no separator at all) -- and, per the coordinator's PR #56
// review round 2, that "and" is NEVER treated as a separator, since a
// title can legitimately contain it ("fish and chips", "salt and pepper",
// "Q and A"). Splitting multiple items is now entirely the model's job
// (sneatActionInstruction tells it to comma-join slots.title); this
// function only splits what it was told to comma-join.
func TestSplitListItemTitle(t *testing.T) {
	cases := []struct {
		title string
		want  []string
	}{
		{"milk", []string{"milk"}},
		{"milk, bread", []string{"milk", "bread"}},
		{"milk, bread, eggs", []string{"milk", "bread", "eggs"}},
		{"  milk , bread  ", []string{"milk", "bread"}},
		{"milk;bread", []string{"milk", "bread"}},
		{"milk\nbread", []string{"milk", "bread"}},
		{"", nil},
		// "and" is never a separator -- these must all stay ONE item.
		{"milk and bread", []string{"milk and bread"}},
		{"fish and chips", []string{"fish and chips"}},
		{"salt and pepper", []string{"salt and pepper"}},
		{"Q and A", []string{"Q and A"}},
	}
	for _, c := range cases {
		got := splitListItemTitle(c.title)
		if len(got) != len(c.want) {
			t.Errorf("splitListItemTitle(%q) = %v, want %v", c.title, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("splitListItemTitle(%q) = %v, want %v", c.title, got, c.want)
				break
			}
		}
	}
}

// TestSneatExecutor_AddToBuy_MultiItem_SplitsAndUndoesAll covers the
// comma-separated multi-item contract sneatActionInstruction now asks the
// model to follow (e.g. "milk, bread" for the founder's originally
// reported "buy milk and bread"): the executor splits it into TWO created
// items in one CreateListItems call, and the returned undo deletes both
// (comma-joined itemID), not just the first.
func TestSneatExecutor_AddToBuy_MultiItem_SplitsAndUndoesAll(t *testing.T) {
	var sentItems []map[string]any
	api, calls := newTestSneatAPI(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if r.URL.Path == "/v0/listus/list_items_create" {
			items, _ := body["items"].([]any)
			for _, it := range items {
				if m, ok := it.(map[string]any); ok {
					sentItems = append(sentItems, m)
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[{"id":"i1","title":"milk"},{"id":"i2","title":"bread"}]}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	exec := SneatExecutor{Todo: api}
	undo, err := exec.Execute(context.Background(), "sp1", session.Action{
		Kind: "todo.add_to_buy", Args: map[string]string{"title": "milk, bread"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(sentItems) != 2 {
		t.Fatalf("sent %d items in one CreateListItems call, want 2: %+v", len(sentItems), sentItems)
	}
	if undo == nil || undo.Kind != "todo.delete_todo" || undo.Target.Keys["itemID"] != "i1,i2" {
		t.Fatalf("undo = %+v, want delete_todo itemID=\"i1,i2\"", undo)
	}

	// Running the undo must delete BOTH items in one call.
	var deletedIDs []string
	api2, _ := newTestSneatAPI(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if ids, ok := body["itemIDs"].([]any); ok {
			for _, id := range ids {
				deletedIDs = append(deletedIDs, id.(string))
			}
		}
	})
	exec2 := SneatExecutor{Todo: api2}
	if _, err := exec2.Execute(context.Background(), "sp1", *undo); err != nil {
		t.Fatalf("Execute(undo): %v", err)
	}
	if len(deletedIDs) != 2 || deletedIDs[0] != "i1" || deletedIDs[1] != "i2" {
		t.Fatalf("deletedIDs = %v, want [i1 i2]", deletedIDs)
	}
	_ = calls
}

func TestSneatExecutor_AddListItem_EmptyTitleAfterSplit_Errors(t *testing.T) {
	e, _ := errAPIServer(t)
	if _, err := e.addListItem(context.Background(), "sp1", session.Action{Args: map[string]string{"title": "   , , "}}, data.ListKindDo); err == nil {
		t.Error("addListItem with only-separator title should error (no real items)")
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

// TestSneatExecutor_RecurringMoveToOtherDay_Refused is the BLOCKER ruling
// from fix round r3b's review: moving Monday's yoga to Wednesday via one
// adjust_slot (Date=Monday, Slot.Start=Wednesday) LOOKS like it should work,
// but calendarius's real read paths never honour a cross-day Slot.Start (see
// crossDayMoveRefusal's doc comment) -- so the executor must refuse the
// request outright, calling the API zero times, rather than silently
// sending a mutation that does nothing a user can observe. This replaces
// the prior TestSneatExecutor_RecurringMoveToOtherDay_DateIsOriginalOccurrence,
// which asserted the now-known-broken adjust_slot(Date=original,
// Slot.Start=new day) request actually got sent.
func TestSneatExecutor_RecurringMoveToOtherDay_Refused(t *testing.T) {
	yoga, _ := monFriYoga(t)
	now := mondayNoon(t)
	api, calls := newTestSneatAPI(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {})
	hs := &data.FakeHappenings{Items: []data.Happening{yoga}}
	exec := SneatExecutor{Calendar: api, Happenings: hs, Now: func() time.Time { return now }}
	target := session.EntityRef{Type: "happening", Title: "Yoga", Keys: map[string]string{"spaceID": "sp1", "happeningID": "yoga"}}
	_, err := exec.Execute(context.Background(), "sp1", session.Action{
		Kind: "calendar.reschedule_happening", Target: &target, Args: map[string]string{"when": "Wednesday 16:00"},
	})
	if err == nil {
		t.Fatal("Execute: err = nil, want a refusal for a cross-day move of a recurring occurrence")
	}
	if !strings.Contains(err.Error(), crossDayMoveRefusal) {
		t.Errorf("err = %q, want it to contain the fixed refusal text %q", err.Error(), crossDayMoveRefusal)
	}
	if len(*calls) != 0 {
		t.Errorf("calls = %v, want NO API call for a refused move", *calls)
	}
}

// TestSneatExecutor_RecurringSameDayRetime_StillWorks proves the BLOCKER
// ruling's refusal is scoped to a DIFFERENT calendar day only -- retiming
// Monday's yoga to a later time still THE SAME Monday must keep working
// exactly as before, via one adjust_slot call.
func TestSneatExecutor_RecurringSameDayRetime_StillWorks(t *testing.T) {
	yoga, _ := monFriYoga(t)
	now := mondayNoon(t)
	var gotDate, gotStartDate string
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
		Kind: "calendar.reschedule_happening", Target: &target, Args: map[string]string{"when": "20:00"},
	}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0] != "POST /v0/happenings/adjust_slot" {
		t.Fatalf("calls = %v", *calls)
	}
	if gotDate != "2026-09-21" || gotStartDate != "2026-09-21" {
		t.Errorf("adjust_slot date = %q, slot.start.date = %q, want both 2026-09-21 (same-day retime)", gotDate, gotStartDate)
	}
}

// TestSneatExecutor_RecurringReschedule_UndoUsesCancelAdjustment is B2's
// undo half: undo calls cancel_adjustment (not another adjust_slot) with
// the SAME date+slotID the move used. Uses a SAME-DAY retime ("20:00" on
// the Monday occurrence the test's "now" anchors to) -- a cross-day move is
// refused outright since the BLOCKER fix above, so it can no longer stand
// in here for "any recurring reschedule".
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
		Kind: "calendar.reschedule_happening", Target: &target, Args: map[string]string{"when": "20:00"},
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

// r3Pipeline wires a full Pipeline (Resolver + real SneatExecutor over an
// httptest sneatapi server) over hs, for the M1 end-to-end reference tests
// below (fix round r3b review; adapted from the coordinator's probe:
// zz_probe3_test.go). log records every HTTP call's path+JSON body.
func r3Pipeline(t *testing.T, hs *data.FakeHappenings, now func() time.Time, log *[]string) (Pipeline, *session.State) {
	t.Helper()
	api, _ := newTestSneatAPI(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		b, _ := json.Marshal(body)
		*log = append(*log, r.URL.Path+" "+string(b))
	})
	readers := data.Readers{Happenings: hs}
	p := Pipeline{Resolver: Resolver{Readers: readers, Now: now}, Readers: readers,
		Executor: SneatExecutor{Calendar: api, Happenings: hs, Now: now}, Now: now}
	return p, &session.State{}
}

// TestPipeline_RescheduleFridayByReference_MovesFridayNotMonday is M1's
// core end-to-end scenario: "move Friday's yoga to 16:00" from a Monday
// "now" must retime FRIDAY's occurrence, not whichever occurrence
// recurringAnchor would otherwise guess (the next one from "now", i.e.
// Monday's own, today's). The reference's own "Friday's yoga" carries the
// resolved date (Resolver, Keys["date"]) through to rescheduleSummary's
// confirmation and the executor's adjust_slot call alike.
func TestPipeline_RescheduleFridayByReference_MovesFridayNotMonday(t *testing.T) {
	yoga, _ := monFriYoga(t)
	now := mondayNoon(t)
	var log []string
	p, st := r3Pipeline(t, &data.FakeHappenings{Items: []data.Happening{yoga}}, func() time.Time { return now }, &log)
	out, err := p.HandleAction(context.Background(),
		Action{Kind: "calendar.reschedule_happening", Reference: "Friday's yoga", Slots: map[string]string{"when": "16:00"}}, st, "sp1")
	if err != nil {
		t.Fatalf("HandleAction: %v", err)
	}
	if !strings.Contains(out.Text, "Sep 25") {
		t.Fatalf("confirmation text = %q, want it to name Friday Sep 25 (not Monday Sep 21)", out.Text)
	}
	if _, err := p.confirmPending(context.Background(), "sp1", st); err != nil {
		t.Fatalf("confirmPending: %v", err)
	}
	if len(log) != 1 || !strings.HasPrefix(log[0], "/v0/happenings/adjust_slot ") {
		t.Fatalf("log = %v, want exactly one adjust_slot call", log)
	}
	if !strings.Contains(log[0], `"date":"2026-09-25"`) {
		t.Errorf("log[0] = %q, want adjust_slot's date = 2026-09-25 (Friday, the REFERENCED occurrence), not Monday", log[0])
	}
}

// TestPipeline_CancelFridayByReference_CancelsFridayNotMonday is M1's
// cancel counterpart: "cancel Friday's yoga" must cancel FRIDAY's
// occurrence via the reference's own resolved date, not recurringAnchor's
// "next from now" default.
func TestPipeline_CancelFridayByReference_CancelsFridayNotMonday(t *testing.T) {
	yoga, _ := monFriYoga(t)
	now := mondayNoon(t)
	var log []string
	p, st := r3Pipeline(t, &data.FakeHappenings{Items: []data.Happening{yoga}}, func() time.Time { return now }, &log)
	out, err := p.HandleAction(context.Background(),
		Action{Kind: "calendar.cancel_happening", Reference: "Friday's yoga"}, st, "sp1")
	if err != nil {
		t.Fatalf("HandleAction: %v", err)
	}
	if !strings.Contains(out.Text, "Sep 25") {
		t.Fatalf("confirmation text = %q, want it to name Friday Sep 25", out.Text)
	}
	if _, err := p.confirmPending(context.Background(), "sp1", st); err != nil {
		t.Fatalf("confirmPending: %v", err)
	}
	if len(log) != 1 || !strings.HasPrefix(log[0], "/v0/happenings/cancel_happening ") {
		t.Fatalf("log = %v, want exactly one cancel_happening call", log)
	}
	if !strings.Contains(log[0], `"date":"2026-09-25"`) {
		t.Errorf("log[0] = %q, want cancel_happening's date = 2026-09-25 (Friday, the REFERENCED occurrence)", log[0])
	}
}

// TestHappeningRowsInWindow_RecurringOccurrences_CarryDistinctDateKeys is
// M1's week-view half: picking Monday's row vs Friday's row for the SAME
// recurring happening must resolve to DIFFERENT occurrences -- a prior
// version built one shared session.EntityRef (no date) for every occurrence
// a recurring happening contributed to the window, so every row picked the
// same, occurrence-less reference.
func TestHappeningRowsInWindow_RecurringOccurrences_CarryDistinctDateKeys(t *testing.T) {
	yoga, loc := monFriYoga(t)
	from := time.Date(2026, 9, 21, 0, 0, 0, 0, loc) // Monday
	to := from.AddDate(0, 0, 7)
	refs, rows := happeningRowsInWindow([]data.Happening{yoga}, from, to, false)
	if len(refs) != 2 || len(rows) != 2 {
		t.Fatalf("refs/rows = %d/%d, want 2 (Monday + Friday)", len(refs), len(rows))
	}
	if refs[0].Keys["date"] != "2026-09-21" {
		t.Errorf("refs[0].Keys[date] = %q, want 2026-09-21 (Monday)", refs[0].Keys["date"])
	}
	if refs[1].Keys["date"] != "2026-09-25" {
		t.Errorf("refs[1].Keys[date] = %q, want 2026-09-25 (Friday)", refs[1].Keys["date"])
	}
	if refs[0].Keys["date"] == refs[1].Keys["date"] {
		t.Fatal("refs[0] and refs[1] carry the SAME date -- picking either row would resolve identically")
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

// TestSneatExecutor_AddHappening_OneOff_SendsValidPayloadAndUndoDeletes
// covers calendar.add_happening's one-off shape end to end: the REAL wire
// body decodes into the REAL dto4calendarius.CreateHappeningRequest and
// passes its own NormalizeTags+Validate (founder's "payload correctness"
// testing requirement -- a payload the backend would 400 must fail here
// too), the server's returned ID becomes the undo target, and the undo
// actually calls delete_happening for that ID.
func TestSneatExecutor_AddHappening_OneOff_SendsValidPayloadAndUndoDeletes(t *testing.T) {
	var createBody map[string]any
	api, calls := newTestSneatAPI(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		switch r.URL.Path {
		case "/v0/happenings/create_happening":
			createBody = body
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("re-marshal: %v", err)
			}
			var req dto4calendarius.CreateHappeningRequest
			if err := json.Unmarshal(raw, &req); err != nil {
				t.Fatalf("decode into real CreateHappeningRequest: %v", err)
			}
			req.NormalizeTags()
			if err := req.Validate(); err != nil {
				t.Fatalf("real CreateHappeningRequest.Validate(): %v (body: %s)", err, raw)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"h9"}`))
		case "/v0/happenings/delete_happening":
			// nothing to assert beyond the call landing -- see calls below.
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	exec := SneatExecutor{Calendar: api}
	action := session.Action{
		Kind: "calendar.add_happening",
		Args: map[string]string{"spaceID": "sp1", "title": "Dentist", "date": "2026-09-26", "start": "10:30", "end": "11:30"},
	}
	undo, err := exec.Execute(context.Background(), "sp1", action)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	happening, _ := createBody["happening"].(map[string]any)
	if happening["title"] != "Dentist" {
		t.Errorf("title = %v, want Dentist", happening["title"])
	}
	if happening["type"] != "single" {
		t.Errorf("type = %v, want single (one-off)", happening["type"])
	}
	if undo == nil || undo.Kind != calendarDeleteHappeningKind || undo.Target.Keys["happeningID"] != "h9" {
		t.Fatalf("undo = %+v, want a delete_happening undo targeting the server-assigned ID h9", undo)
	}
	if _, err := exec.Execute(context.Background(), "sp1", *undo); err != nil {
		t.Fatalf("Execute(undo): %v", err)
	}
	want := []string{"POST /v0/happenings/create_happening", "DELETE /v0/happenings/delete_happening"}
	if len(*calls) != 2 || (*calls)[0] != want[0] || (*calls)[1] != want[1] {
		t.Fatalf("calls = %v, want %v", *calls, want)
	}
}

// TestSneatExecutor_AddHappening_Recurring_SendsValidPayload covers the
// weekly-recurring shape's own wire body against the same real DTO
// Validate() as the one-off case.
func TestSneatExecutor_AddHappening_Recurring_SendsValidPayload(t *testing.T) {
	var createBody map[string]any
	api, _ := newTestSneatAPI(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		createBody = body
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("re-marshal: %v", err)
		}
		var req dto4calendarius.CreateHappeningRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		req.NormalizeTags()
		if err := req.Validate(); err != nil {
			t.Fatalf("Validate(): %v (body: %s)", err, raw)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"h10"}`))
	})
	exec := SneatExecutor{Calendar: api}
	action := session.Action{
		Kind: "calendar.add_happening",
		Args: map[string]string{"spaceID": "sp1", "title": "Yoga", "weekdays": "tu,th", "start": "07:00", "end": "08:00"},
	}
	if _, err := exec.Execute(context.Background(), "sp1", action); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	happening, _ := createBody["happening"].(map[string]any)
	if happening["type"] != "recurring" {
		t.Errorf("type = %v, want recurring", happening["type"])
	}
	slots, _ := happening["slots"].(map[string]any)
	slot, _ := slots["s1"].(map[string]any)
	weekdays, _ := slot["weekdays"].([]any)
	if len(weekdays) != 2 || weekdays[0] != "tu" || weekdays[1] != "th" {
		t.Errorf("weekdays = %v, want [tu th]", slot["weekdays"])
	}
}

// TestSneatExecutor_AddHappening_NoCalendarAPI_Errors covers the defensive
// nil-Calendar guard.
func TestSneatExecutor_AddHappening_NoCalendarAPI_Errors(t *testing.T) {
	exec := SneatExecutor{}
	_, err := exec.Execute(context.Background(), "sp1", session.Action{
		Kind: "calendar.add_happening",
		Args: map[string]string{"title": "X", "date": "2026-09-26", "start": "10:00", "end": "11:00"},
	})
	if err == nil {
		t.Fatal("expected an error with no Calendar API configured")
	}
}

// TestSneatExecutor_AddHappening_BadArgs_Errors covers buildHappeningBrief's
// error path surfacing through Execute without ever calling the API.
func TestSneatExecutor_AddHappening_BadArgs_Errors(t *testing.T) {
	api, calls := newTestSneatAPI(t, nil)
	exec := SneatExecutor{Calendar: api}
	_, err := exec.Execute(context.Background(), "sp1", session.Action{
		Kind: "calendar.add_happening", Args: map[string]string{"title": ""},
	})
	if err == nil {
		t.Fatal("expected an error for a missing title")
	}
	if len(*calls) != 0 {
		t.Fatalf("calls = %v, want no HTTP call for invalid Args", *calls)
	}
}

// TestSneatExecutor_RenameHappening_SendsUpdateAndUndoRenamesBack covers B
// (rename-only) end to end: the REAL wire body decodes into the REAL
// dto4calendarius.UpdateHappeningRequest and passes its own Validate, and
// the undo re-runs the SAME action kind with the original title.
func TestSneatExecutor_RenameHappening_SendsUpdateAndUndoRenamesBack(t *testing.T) {
	var titles []string
	api, calls := newTestSneatAPI(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("re-marshal: %v", err)
		}
		var req dto4calendarius.UpdateHappeningRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Fatalf("decode into real UpdateHappeningRequest: %v", err)
		}
		if err := req.Validate(); err != nil {
			t.Fatalf("real UpdateHappeningRequest.Validate(): %v (body: %s)", err, raw)
		}
		if req.Title != nil {
			titles = append(titles, *req.Title)
		}
		w.WriteHeader(http.StatusOK)
	})
	exec := SneatExecutor{Calendar: api}
	target := session.EntityRef{Type: "happening", Title: "Team sync", Keys: map[string]string{"spaceID": "sp1", "happeningID": "h1"}}
	undo, err := exec.Execute(context.Background(), "sp1", session.Action{
		Kind: "calendar.update_happening", Target: &target, Args: map[string]string{"title": "Weekly planning"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if undo == nil || undo.Kind != "calendar.update_happening" || undo.Args["title"] != "Team sync" {
		t.Fatalf("undo = %+v, want a rename-back-to-original-title undo", undo)
	}
	if _, err := exec.Execute(context.Background(), "sp1", *undo); err != nil {
		t.Fatalf("Execute(undo): %v", err)
	}
	if len(titles) != 2 || titles[0] != "Weekly planning" || titles[1] != "Team sync" {
		t.Fatalf("titles sent = %v, want [Weekly planning, Team sync]", titles)
	}
	want := []string{"POST /v0/happenings/update_happening_texts", "POST /v0/happenings/update_happening_texts"}
	if len(*calls) != 2 || (*calls)[0] != want[0] || (*calls)[1] != want[1] {
		t.Fatalf("calls = %v, want %v", *calls, want)
	}
}

// TestSneatExecutor_RenameHappening_MissingTitleOrTarget_Errors covers both
// defensive guards without ever calling the API.
func TestSneatExecutor_RenameHappening_MissingTitleOrTarget_Errors(t *testing.T) {
	api, calls := newTestSneatAPI(t, nil)
	exec := SneatExecutor{Calendar: api}
	target := session.EntityRef{Type: "happening", Title: "Team sync", Keys: map[string]string{"spaceID": "sp1", "happeningID": "h1"}}
	if _, err := exec.Execute(context.Background(), "sp1", session.Action{Kind: "calendar.update_happening", Target: &target, Args: map[string]string{"title": "  "}}); err == nil {
		t.Fatal("expected an error for a blank title")
	}
	if _, err := exec.Execute(context.Background(), "sp1", session.Action{Kind: "calendar.update_happening", Args: map[string]string{"title": "X"}}); err == nil {
		t.Fatal("expected an error for a missing target")
	}
	if len(*calls) != 0 {
		t.Fatalf("calls = %v, want no HTTP call for either invalid case", *calls)
	}
}

// TestSneatExecutor_DeleteHappening_SendsDelete covers calendarDeleteHappeningKind
// (addHappening's undo-only kind), including the real
// dto4calendarius.HappeningRequest.Validate() over the wire body.
func TestSneatExecutor_DeleteHappening_SendsDelete(t *testing.T) {
	api, calls := newTestSneatAPI(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("re-marshal: %v", err)
		}
		var req dto4calendarius.HappeningRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Fatalf("decode into real HappeningRequest: %v", err)
		}
		if err := req.Validate(); err != nil {
			t.Fatalf("real HappeningRequest.Validate(): %v (body: %s)", err, raw)
		}
		w.WriteHeader(http.StatusOK)
	})
	exec := SneatExecutor{Calendar: api}
	target := session.EntityRef{Type: "happening", Title: "Dentist", Keys: map[string]string{"spaceID": "sp1", "happeningID": "h9"}}
	_, err := exec.Execute(context.Background(), "sp1", session.Action{Kind: calendarDeleteHappeningKind, Target: &target})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(*calls) != 1 || (*calls)[0] != "DELETE /v0/happenings/delete_happening" {
		t.Fatalf("calls = %v", *calls)
	}
}

// TestSneatExecutor_DeleteHappening_NoTargetOrAPI_Errors covers both
// defensive guards.
func TestSneatExecutor_DeleteHappening_NoTargetOrAPI_Errors(t *testing.T) {
	api, calls := newTestSneatAPI(t, nil)
	exec := SneatExecutor{Calendar: api}
	if _, err := exec.Execute(context.Background(), "sp1", session.Action{Kind: calendarDeleteHappeningKind}); err == nil {
		t.Fatal("expected an error for a missing target")
	}
	if len(*calls) != 0 {
		t.Fatalf("calls = %v, want no HTTP call", *calls)
	}
	noAPI := SneatExecutor{}
	target := session.EntityRef{Keys: map[string]string{"happeningID": "h1"}}
	if _, err := noAPI.Execute(context.Background(), "sp1", session.Action{Kind: calendarDeleteHappeningKind, Target: &target}); err == nil {
		t.Fatal("expected an error with no Calendar API configured")
	}
}

// TestSneatExecutor_AddHappening_APIError_Propagates covers CreateHappening
// returning an error (e.g. a 500 or 400 from the real backend) -- addHappening
// must surface it rather than fabricate an undo for a happening that was
// never actually created.
func TestSneatExecutor_AddHappening_APIError_Propagates(t *testing.T) {
	api, _ := newTestSneatAPI(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	exec := SneatExecutor{Calendar: api}
	_, err := exec.Execute(context.Background(), "sp1", session.Action{
		Kind: "calendar.add_happening",
		Args: map[string]string{"title": "X", "date": "2026-09-26", "start": "10:00", "end": "11:00"},
	})
	if err == nil {
		t.Fatal("expected the backend error to propagate")
	}
}

// TestSneatExecutor_RenameHappening_APIError_Propagates covers
// UpdateHappeningTexts returning an error -- renameHappening must surface it
// rather than fabricate an undo for a rename that never actually happened.
func TestSneatExecutor_RenameHappening_APIError_Propagates(t *testing.T) {
	api, _ := newTestSneatAPI(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	exec := SneatExecutor{Calendar: api}
	target := session.EntityRef{Type: "happening", Title: "Team sync", Keys: map[string]string{"spaceID": "sp1", "happeningID": "h1"}}
	_, err := exec.Execute(context.Background(), "sp1", session.Action{
		Kind: "calendar.update_happening", Target: &target, Args: map[string]string{"title": "Weekly planning"},
	})
	if err == nil {
		t.Fatal("expected the backend error to propagate")
	}
}
