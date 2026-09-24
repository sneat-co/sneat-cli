package data

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dal-go/dalgo/dal"
	"github.com/dal-go/record"
	calendariusdbo "github.com/sneat-co/calendarius/backend/dbo4calendarius"
	listusdbo "github.com/sneat-co/listus/backend/dbo4listus"
)

// newHappeningRecord builds a query-result row shaped like list()'s own
// SelectIntoRecord factory does, so fake queryReaderFn implementations do
// not need to know that factory's internals.
func newHappeningRecord(id string, dbo calendariusdbo.HappeningDbo) record.Record {
	key := record.NewIncompleteKey("happenings", reflect.String, nil)
	key.ID = id
	rec := record.NewRecordWithData(key, &dbo)
	return rec
}

func happeningsSession(t *testing.T, runner *fakeRunner) *fakeSession {
	t.Helper()
	return &fakeSession{runner: runner}
}

// TestFirestoreHappenings_Window drives Window end to end through the
// sessionProvider seam: no real Firestore project needed.
func TestFirestoreHappenings_Window(t *testing.T) {
	dbo := calendariusdbo.HappeningDbo{
		HappeningBase: calendariusdbo.HappeningBase{
			Title: "Dentist",
			Slots: map[string]*calendariusdbo.HappeningSlot{
				"s1": {HappeningSlotTiming: calendariusdbo.HappeningSlotTiming{
					Timing: calendariusdbo.Timing{Start: calendariusdbo.DateTime{Date: "2026-09-25", Time: "16:00"}},
				}},
			},
		},
	}
	tx := &fakeReadTransaction{queryReaderFn: func(ctx context.Context, query dal.Query) (dal.RecordsReader, error) {
		return dal.NewRecordsReader([]record.Record{newHappeningRecord("h1", dbo)}), nil
	}}
	r := &firestoreHappenings{session: happeningsSession(t, &fakeRunner{tx: tx}), loc: time.UTC}

	from := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	got, err := r.Window(context.Background(), "sp1", from, to)
	if err != nil {
		t.Fatalf("Window() = %v, want nil", err)
	}
	if len(got) != 1 || got[0].ID != "h1" || got[0].Title != "Dentist" {
		t.Fatalf("got = %+v", got)
	}
}

// TestFirestoreHappenings_FindByTitle covers the title-search path.
func TestFirestoreHappenings_FindByTitle(t *testing.T) {
	dbo := calendariusdbo.HappeningDbo{HappeningBase: calendariusdbo.HappeningBase{Title: "Dentist appointment"}}
	tx := &fakeReadTransaction{queryReaderFn: func(ctx context.Context, query dal.Query) (dal.RecordsReader, error) {
		return dal.NewRecordsReader([]record.Record{newHappeningRecord("h1", dbo)}), nil
	}}
	r := &firestoreHappenings{session: happeningsSession(t, &fakeRunner{tx: tx}), loc: time.UTC}

	got, err := r.FindByTitle(context.Background(), "sp1", "dentist")
	if err != nil {
		t.Fatalf("FindByTitle() = %v, want nil", err)
	}
	if len(got) != 1 || got[0].ID != "h1" {
		t.Fatalf("got = %+v", got)
	}

	got, err = r.FindByTitle(context.Background(), "sp1", "haircut")
	if err != nil {
		t.Fatalf("FindByTitle(no match) = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Fatalf("got = %+v, want no matches", got)
	}
}

// TestFirestoreHappenings_ListError propagates list()'s errors from both
// Window and FindByTitle.
func TestFirestoreHappenings_ListError(t *testing.T) {
	wantErr := errors.New("query failed")
	r := &firestoreHappenings{session: happeningsSession(t, &fakeRunner{runErr: wantErr}), loc: time.UTC}

	if _, err := r.Window(context.Background(), "sp1", time.Time{}, time.Time{}); !errors.Is(err, wantErr) {
		t.Fatalf("Window() = %v, want %v", err, wantErr)
	}
	if _, err := r.FindByTitle(context.Background(), "sp1", "x"); !errors.Is(err, wantErr) {
		t.Fatalf("FindByTitle() = %v, want %v", err, wantErr)
	}
}

// TestFirestoreHappenings_ListSessionError covers list()'s Session.DB error
// branch (a failed open, before any query runs).
func TestFirestoreHappenings_ListSessionError(t *testing.T) {
	wantErr := errors.New("open failed")
	r := &firestoreHappenings{session: &fakeSession{openErr: wantErr}, loc: time.UTC}
	if _, _, err := r.list(context.Background(), "sp1"); !errors.Is(err, wantErr) {
		t.Fatalf("list() = %v, want %v", err, wantErr)
	}
}

// TestFirestoreHappenings_Get covers the found and error paths.
func TestFirestoreHappenings_Get(t *testing.T) {
	dbo := calendariusdbo.HappeningDbo{HappeningBase: calendariusdbo.HappeningBase{Title: "Dentist"}}
	tx := &fakeReadTransaction{getFn: func(ctx context.Context, rec record.Record) error {
		d, ok := rec.Data().(*calendariusdbo.HappeningDbo)
		if !ok {
			t.Fatalf("Data() = %T", rec.Data())
		}
		*d = dbo
		rec.SetError(nil)
		return nil
	}}
	r := &firestoreHappenings{session: happeningsSession(t, &fakeRunner{tx: tx}), loc: time.UTC}

	got, err := r.Get(context.Background(), "sp1", "h1")
	if err != nil {
		t.Fatalf("Get() = %v, want nil", err)
	}
	if got.Title != "Dentist" {
		t.Fatalf("got = %+v", got)
	}

	wantErr := errors.New("open failed")
	r2 := &firestoreHappenings{session: &fakeSession{openErr: wantErr}, loc: time.UTC}
	if _, err := r2.Get(context.Background(), "sp1", "h1"); !errors.Is(err, wantErr) {
		t.Fatalf("Get() session error = %v, want %v", err, wantErr)
	}
}

// TestFirestoreHappenings_Close forwards to the session.
func TestFirestoreHappenings_Close(t *testing.T) {
	s := &fakeSession{}
	r := &firestoreHappenings{session: s, loc: time.UTC}
	if err := r.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
	if s.closeCall != 1 {
		t.Fatalf("closeCall = %d, want 1", s.closeCall)
	}
}

// -- firestoreTodos --------------------------------------------------------

// TestFirestoreTodos_List covers getList + List's not-done-first sort.
func TestFirestoreTodos_List(t *testing.T) {
	l := listusdbo.ListDbo{Items: []*listusdbo.ListItemBrief{
		{ID: "i1"},
		{ID: "i2"},
	}}
	tx := &fakeReadTransaction{getFn: func(ctx context.Context, rec record.Record) error {
		d, ok := rec.Data().(*listusdbo.ListDbo)
		if !ok {
			t.Fatalf("Data() = %T", rec.Data())
		}
		*d = l
		rec.SetError(nil)
		return nil
	}}
	r := &firestoreTodos{session: happeningsSession(t, &fakeRunner{tx: tx})}

	got, err := r.List(context.Background(), "sp1", ListKindDo)
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("got = %+v", got)
	}
}

// TestFirestoreTodos_List_SessionError covers getList's open-error branch.
func TestFirestoreTodos_List_SessionError(t *testing.T) {
	wantErr := errors.New("open failed")
	r := &firestoreTodos{session: &fakeSession{openErr: wantErr}}
	if _, err := r.List(context.Background(), "sp1", ListKindDo); !errors.Is(err, wantErr) {
		t.Fatalf("List() = %v, want %v", err, wantErr)
	}
}

// TestFirestoreTodos_FindByTitle covers the both-lists search, a
// not-found list being skipped rather than surfaced, and a real read
// error being surfaced (wrapped) rather than swallowed.
func TestFirestoreTodos_FindByTitle(t *testing.T) {
	t.Run("matches across lists, not-found list skipped", func(t *testing.T) {
		calls := 0
		tx := &fakeReadTransaction{getFn: func(ctx context.Context, rec record.Record) error {
			calls++
			d := rec.Data().(*listusdbo.ListDbo)
			if calls == 1 {
				*d = listusdbo.ListDbo{Items: []*listusdbo.ListItemBrief{{ID: "i1", ListItemBase: listusdbo.ListItemBase{Title: "Buy milk"}}}}
				rec.SetError(nil)
				return nil
			}
			// The second list (buy) does not exist yet in this space: a real
			// backend's tx.Get returns a not-found error directly (not a
			// found-but-empty record), which FindByTitle must skip rather than
			// surface.
			return record.ErrRecordNotFound
		}}
		r := &firestoreTodos{session: happeningsSession(t, &fakeRunner{tx: tx})}

		got, err := r.FindByTitle(context.Background(), "sp1", "milk")
		if err != nil {
			t.Fatalf("FindByTitle() = %v, want nil", err)
		}
		if len(got) != 1 || got[0].Title != "Buy milk" {
			t.Fatalf("got = %+v", got)
		}
	})

	t.Run("real read error is wrapped and surfaced", func(t *testing.T) {
		wantErr := errors.New("boom")
		r := &firestoreTodos{session: &fakeSession{openErr: wantErr}}
		_, err := r.FindByTitle(context.Background(), "sp1", "milk")
		if err == nil || !errors.Is(err, wantErr) {
			t.Fatalf("FindByTitle() = %v, want a wrapped %v", err, wantErr)
		}
	})
}

// TestFirestoreTodos_Close forwards to the session.
func TestFirestoreTodos_Close(t *testing.T) {
	s := &fakeSession{}
	r := &firestoreTodos{session: s}
	if err := r.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
	if s.closeCall != 1 {
		t.Fatalf("closeCall = %d, want 1", s.closeCall)
	}
}

// TestListKeyFor_UnknownDefaultsToDo covers listKeyFor's default branch,
// which the package's own TestListKeyFor (firestore_decode_test.go) does
// not exercise.
func TestListKeyFor_UnknownDefaultsToDo(t *testing.T) {
	if listKeyFor("bogus") != listusdbo.DoTasksListID {
		t.Fatal("listKeyFor(unknown) should default to the do list")
	}
}

func TestNewHappeningQueryRecord(t *testing.T) {
	rec := newHappeningQueryRecord()
	if rec == nil {
		t.Fatal("newHappeningQueryRecord() = nil")
	}
	if _, ok := rec.Data().(*calendariusdbo.HappeningDbo); !ok {
		t.Fatalf("Data() = %T, want *calendariusdbo.HappeningDbo", rec.Data())
	}
}

// TestFirestoreHappenings_List_SkipsRecordsWithWrongDataType covers list's
// defensive dbo==nil skip: a query-result record whose Data() is not a
// *calendariusdbo.HappeningDbo (should not happen from a real backend, but
// list must not panic or include a zero-value row) is dropped.
func TestFirestoreHappenings_List_SkipsRecordsWithWrongDataType(t *testing.T) {
	good := newHappeningRecord("h1", calendariusdbo.HappeningDbo{HappeningBase: calendariusdbo.HappeningBase{Title: "Good"}})
	wrongType := record.NewRecordWithData(record.NewKeyWithID("happenings", "h2"), &listusdbo.ListDbo{})
	tx := &fakeReadTransaction{queryReaderFn: func(ctx context.Context, query dal.Query) (dal.RecordsReader, error) {
		return dal.NewRecordsReader([]record.Record{good, wrongType}), nil
	}}
	r := &firestoreHappenings{session: happeningsSession(t, &fakeRunner{tx: tx}), loc: time.UTC}

	dbos, ids, err := r.list(context.Background(), "sp1")
	if err != nil {
		t.Fatalf("list() = %v, want nil", err)
	}
	if len(dbos) != 1 || len(ids) != 1 || ids[0] != "h1" {
		t.Fatalf("list() = %+v/%+v, want only the well-typed h1 row", dbos, ids)
	}
}

// TestParseSlotDateTime covers its fallback-to-UTC, invalid-timezone,
// invalid-UTC-offset and unparseable-date/time branches directly.
func TestParseSlotDateTime(t *testing.T) {
	dt := calendariusdbo.DateTime{Date: "2026-09-25", Time: "16:00"}

	if _, ok := parseSlotDateTime(dt, "", "", nil); !ok {
		t.Error("nil fallback should fall back to UTC and still parse")
	}
	if _, ok := parseSlotDateTime(dt, "Not/A/Zone", "", time.UTC); !ok {
		t.Error("an invalid IANA zone name should be ignored, not fail parsing")
	}
	if _, ok := parseSlotDateTime(dt, "", "bogus", time.UTC); !ok {
		t.Error("an invalid UTC offset should be ignored, not fail parsing")
	}
	if _, ok := parseSlotDateTime(calendariusdbo.DateTime{Date: "not-a-date", Time: "16:00"}, "", "", time.UTC); ok {
		t.Error("an unparseable date/time should return ok=false")
	}
}

// TestFixedZoneFromOffset covers its length/sign/colon validation and its
// non-numeric-digits branch, in addition to firestore_decode_test.go's own
// TestFixedZoneFromOffset (valid input).
func TestFixedZoneFromOffset_Invalid(t *testing.T) {
	for _, s := range []string{"", "+1:00", "01:00", "+01_00", "+ab:00", "+01:cd"} {
		if _, ok := fixedZoneFromOffset(s); ok {
			t.Errorf("fixedZoneFromOffset(%q) = ok, want !ok", s)
		}
	}
}

// TestFirstSlotWindow_SkipsNilAndUnparseableSlots covers the nil-slot and
// unparseable-start-time skip branches.
func TestFirstSlotWindow_SkipsNilAndUnparseableSlots(t *testing.T) {
	h := calendariusdbo.HappeningDbo{HappeningBase: calendariusdbo.HappeningBase{Slots: map[string]*calendariusdbo.HappeningSlot{
		"nil-slot": nil,
		"bad-time": {HappeningSlotTiming: calendariusdbo.HappeningSlotTiming{
			Timing: calendariusdbo.Timing{Start: calendariusdbo.DateTime{Date: "not-a-date", Time: "16:00"}},
		}},
		"good": {HappeningSlotTiming: calendariusdbo.HappeningSlotTiming{
			Timing: calendariusdbo.Timing{Start: calendariusdbo.DateTime{Date: "2026-09-25", Time: "16:00"}},
		}},
	}}}
	start, _, slotID, _, _ := firstSlotWindow(h, time.UTC)
	if slotID != "good" {
		t.Fatalf("slotID = %q, want the only parseable slot (good)", slotID)
	}
	if start.IsZero() {
		t.Fatal("start is zero, want the good slot's parsed time")
	}
}

// TestFirestoreHappenings_Get_TransactionError covers Get's transaction
// error branch (session opens fine, the read itself fails) -- distinct
// from TestFirestoreHappenings_Get's session-open-error case.
func TestFirestoreHappenings_Get_TransactionError(t *testing.T) {
	wantErr := errors.New("read failed")
	r := &firestoreHappenings{session: happeningsSession(t, &fakeRunner{runErr: wantErr}), loc: time.UTC}
	if _, err := r.Get(context.Background(), "sp1", "h1"); !errors.Is(err, wantErr) {
		t.Fatalf("Get() = %v, want %v", err, wantErr)
	}
}

// TestFirestoreTodos_GetList_TransactionError covers getList's transaction
// error branch (session opens fine, the read itself fails).
func TestFirestoreTodos_GetList_TransactionError(t *testing.T) {
	wantErr := errors.New("read failed")
	r := &firestoreTodos{session: happeningsSession(t, &fakeRunner{runErr: wantErr})}
	if _, err := r.getList(context.Background(), "sp1", listusdbo.DoTasksListID); !errors.Is(err, wantErr) {
		t.Fatalf("getList() = %v, want %v", err, wantErr)
	}
}

func TestToTodo_NilItem(t *testing.T) {
	got := toTodo("sp1", ListKindDo, nil)
	if got != (Todo{}) {
		t.Fatalf("toTodo(nil) = %+v, want zero value", got)
	}
}
