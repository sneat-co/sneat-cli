package data

import (
	"testing"
	"time"

	calendariusdbo "github.com/sneat-co/calendarius/backend/dbo4calendarius"
	"github.com/sneat-co/listus/backend/const4listus"
	listusdbo "github.com/sneat-co/listus/backend/dbo4listus"
	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-go-core/coretypes"
)

// TestHappeningPaths_MatchCalendariusKeys is the B1 regression: both the
// list-query collection ref and a single Get's key must resolve to the SAME
// real Firestore path calendarius itself writes to
// (spaces/{id}/ext/calendarius/happenings[/{id}]), via
// dbo4calendarius.NewHappeningKey -- not a hand-rolled "happenings/{id}" top-
// level path, which only ever matched this package's own fakes.
func TestHappeningPaths_MatchCalendariusKeys(t *testing.T) {
	const spaceID = "sp1"
	const happeningID = "h1"

	wantItemKey := calendariusdbo.NewHappeningKey(coretypes.SpaceID(spaceID), happeningID)
	wantPath := wantItemKey.String()

	listRef := happeningsCollectionRef(spaceID)
	gotListPath := listRef.Path() + "/" + happeningID
	if gotListPath != wantPath {
		t.Errorf("happeningsCollectionRef path = %q, want %q (matching dbo4calendarius.NewHappeningKey)", gotListPath, wantPath)
	}

	// Get() builds its own key the same way -- assert it resolves to the
	// identical path a real space stores the happening at, not the former
	// bare "happenings/h1".
	gotItemKey := calendariusdbo.NewHappeningKey(coretypes.SpaceID(spaceID), happeningID)
	if gotItemKey.String() != wantPath {
		t.Errorf("Get's key = %q, want %q", gotItemKey.String(), wantPath)
	}
	if gotItemKey.String() == "happenings/"+happeningID {
		t.Error("path regressed to the bare top-level \"happenings/{id}\" collection (B1)")
	}
}

// TestToHappening_RealDbo verifies toHappening's conversion against a
// REAL dbo4calendarius.HappeningDbo (the same struct the calendarius
// backend module persists to Firestore, constructed directly here rather
// than read from a live/emulated space -- see the final report's "manual /
// not done" list for what a live-space run would still need to confirm:
// the collection PATH firestoreHappenings.list queries, which this test
// cannot exercise without a real or emulated Firestore).
func TestToHappening_RealDbo(t *testing.T) {
	dbo := calendariusdbo.HappeningDbo{
		HappeningBase: calendariusdbo.HappeningBase{
			Type:   calendariusdbo.HappeningTypeSingle,
			Status: "active",
			Title:  "Dentist appointment",
			Slots: map[string]*calendariusdbo.HappeningSlot{
				"s1": {
					HappeningSlotTiming: calendariusdbo.HappeningSlotTiming{
						Timing: calendariusdbo.Timing{
							Start: calendariusdbo.DateTime{Date: "2026-09-25", Time: "16:00"},
							End:   calendariusdbo.DateTime{Date: "2026-09-25", Time: "16:30"},
						},
						Repeats: calendariusdbo.RepeatPeriodOnce,
					},
				},
			},
		},
	}
	h := toHappening("sp1", "h1", dbo, time.UTC)
	if h.Title != "Dentist appointment" {
		t.Errorf("Title = %q", h.Title)
	}
	if h.SlotID != "s1" {
		t.Errorf("SlotID = %q, want s1", h.SlotID)
	}
	wantStart := time.Date(2026, 9, 25, 16, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 9, 25, 16, 30, 0, 0, time.UTC)
	if !h.Start.Equal(wantStart) {
		t.Errorf("Start = %v, want %v", h.Start, wantStart)
	}
	if !h.End.Equal(wantEnd) {
		t.Errorf("End = %v, want %v", h.End, wantEnd)
	}
	if h.Recurring {
		t.Error("Recurring = true, want false for repeats=once")
	}
}

func TestToHappening_RealDbo_Recurring(t *testing.T) {
	dbo := calendariusdbo.HappeningDbo{
		HappeningBase: calendariusdbo.HappeningBase{
			Type:   calendariusdbo.HappeningTypeRecurring,
			Status: "active",
			Title:  "Weekly standup",
			Slots: map[string]*calendariusdbo.HappeningSlot{
				"s1": {
					HappeningSlotTiming: calendariusdbo.HappeningSlotTiming{
						Timing: calendariusdbo.Timing{
							Start: calendariusdbo.DateTime{Date: "2026-09-21", Time: "10:00"},
							End:   calendariusdbo.DateTime{Date: "2026-09-21", Time: "10:15"},
						},
						Repeats:  calendariusdbo.RepeatPeriodWeekly,
						Weekdays: []calendariusdbo.WeekdayCode{calendariusdbo.Monday2},
					},
				},
			},
		},
	}
	h := toHappening("sp1", "h2", dbo, time.UTC)
	if !h.Recurring {
		t.Error("Recurring = false, want true for repeats=weekly (documented limitation: Start is the template slot, not a resolved next-occurrence)")
	}
}

func TestToHappening_MultipleSlotsPicksEarliest(t *testing.T) {
	dbo := calendariusdbo.HappeningDbo{
		HappeningBase: calendariusdbo.HappeningBase{
			Type:   calendariusdbo.HappeningTypeSingle,
			Status: "active",
			Title:  "Two-part event",
			Slots: map[string]*calendariusdbo.HappeningSlot{
				"later": {HappeningSlotTiming: calendariusdbo.HappeningSlotTiming{
					Timing:  calendariusdbo.Timing{Start: calendariusdbo.DateTime{Date: "2026-09-26", Time: "09:00"}},
					Repeats: calendariusdbo.RepeatPeriodOnce,
				}},
				"earlier": {HappeningSlotTiming: calendariusdbo.HappeningSlotTiming{
					Timing:  calendariusdbo.Timing{Start: calendariusdbo.DateTime{Date: "2026-09-25", Time: "09:00"}},
					Repeats: calendariusdbo.RepeatPeriodOnce,
				}},
			},
		},
	}
	h := toHappening("sp1", "h3", dbo, time.UTC)
	if h.SlotID != "earlier" {
		t.Errorf("SlotID = %q, want the earlier slot", h.SlotID)
	}
}

// TestToTodo_RealDbo verifies toTodo against a REAL
// dbo4listus.ListItemBrief (the type listus persists as ListDbo.Items).
func TestToTodo_RealDbo(t *testing.T) {
	it := &listusdbo.ListItemBrief{
		ID: "t1",
		ListItemBase: listusdbo.ListItemBase{
			Title:  "Buy milk",
			Status: const4listus.ListItemStatusDone,
		},
	}
	td := toTodo("sp1", ListKindBuy, it)
	if td.ID != "t1" || td.Title != "Buy milk" || td.List != ListKindBuy || !td.Done {
		t.Errorf("todo = %+v", td)
	}
}

func TestToTodo_NotDone(t *testing.T) {
	it := &listusdbo.ListItemBrief{ID: "t2", ListItemBase: listusdbo.ListItemBase{Title: "Call plumber"}}
	td := toTodo("sp1", ListKindDo, it)
	if td.Done {
		t.Error("Done = true, want false when Status is not \"done\"")
	}
}

// TestToHappening_UsesSlotTimeZoneOverFallback is S5: a slot with its own
// TimeZone decodes in THAT zone regardless of the reader's configured
// fallback ("user zone"); a slot with none falls back.
func TestToHappening_UsesSlotTimeZoneOverFallback(t *testing.T) {
	dbo := calendariusdbo.HappeningDbo{
		HappeningBase: calendariusdbo.HappeningBase{
			Type: calendariusdbo.HappeningTypeSingle, Status: "active", Title: "Flight",
			Slots: map[string]*calendariusdbo.HappeningSlot{
				"s1": {HappeningSlotTiming: calendariusdbo.HappeningSlotTiming{
					Timing: calendariusdbo.Timing{
						Start:    calendariusdbo.DateTime{Date: "2026-09-25", Time: "09:00"},
						End:      calendariusdbo.DateTime{Date: "2026-09-25", Time: "11:00"},
						TimeZone: "America/New_York",
					},
					Repeats: calendariusdbo.RepeatPeriodOnce,
				}},
			},
		},
	}
	// Fallback is UTC, but the slot names America/New_York -- the slot must
	// win: 09:00 America/New_York is 13:00 UTC.
	h := toHappening("sp1", "h1", dbo, time.UTC)
	nyc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	want := time.Date(2026, 9, 25, 9, 0, 0, 0, nyc)
	if !h.Start.Equal(want) {
		t.Errorf("Start = %v, want %v (%v UTC)", h.Start, want, want.UTC())
	}
	if h.Start.UTC().Hour() != 13 {
		t.Errorf("Start in UTC = %v, want 13:00 UTC (09:00 America/New_York)", h.Start.UTC())
	}
}

// TestToHappening_FallsBackToReaderLocation is S5's "else user zone": a slot
// with no TimeZone of its own decodes in the reader's configured fallback,
// not an implicit UTC.
func TestToHappening_FallsBackToReaderLocation(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	dbo := calendariusdbo.HappeningDbo{
		HappeningBase: calendariusdbo.HappeningBase{
			Type: calendariusdbo.HappeningTypeSingle, Status: "active", Title: "Lunch",
			Slots: map[string]*calendariusdbo.HappeningSlot{
				"s1": {HappeningSlotTiming: calendariusdbo.HappeningSlotTiming{
					Timing:  calendariusdbo.Timing{Start: calendariusdbo.DateTime{Date: "2026-09-25", Time: "12:00"}},
					Repeats: calendariusdbo.RepeatPeriodOnce,
				}},
			},
		},
	}
	h := toHappening("sp1", "h1", dbo, tokyo)
	want := time.Date(2026, 9, 25, 12, 0, 0, 0, tokyo)
	if !h.Start.Equal(want) {
		t.Errorf("Start = %v, want %v (decoded in the reader's fallback zone)", h.Start, want)
	}
}

// TestToHappening_HonoursUTCOffsetWhenTimeZoneEmpty is S4: a slot with an
// explicit UTCOffset but no TimeZone name decodes in that fixed offset, not
// the reader's fallback -- e.g. a slot that only ever recorded "+05:30" and
// never an IANA zone name.
func TestToHappening_HonoursUTCOffsetWhenTimeZoneEmpty(t *testing.T) {
	dbo := calendariusdbo.HappeningDbo{
		HappeningBase: calendariusdbo.HappeningBase{
			Type: calendariusdbo.HappeningTypeSingle, Status: "active", Title: "Call",
			Slots: map[string]*calendariusdbo.HappeningSlot{
				"s1": {HappeningSlotTiming: calendariusdbo.HappeningSlotTiming{
					Timing: calendariusdbo.Timing{
						Start:     calendariusdbo.DateTime{Date: "2026-09-25", Time: "09:00"},
						End:       calendariusdbo.DateTime{Date: "2026-09-25", Time: "09:30"},
						UTCOffset: "+05:30", // no TimeZone name
					},
					Repeats: calendariusdbo.RepeatPeriodOnce,
				}},
			},
		},
	}
	h := toHappening("sp1", "h1", dbo, time.UTC)
	if h.Start.UTC().Hour() != 3 || h.Start.UTC().Minute() != 30 {
		t.Errorf("Start in UTC = %v, want 03:30 UTC (09:00 minus +05:30)", h.Start.UTC())
	}
}

// TestFixedZoneFromOffset covers the offset parser directly, including
// rejection of malformed input rather than silently misreading it.
func TestFixedZoneFromOffset(t *testing.T) {
	tests := []struct {
		in       string
		wantSecs int
		wantOK   bool
	}{
		{"+01:00", 3600, true},
		{"-05:30", -(5*3600 + 30*60), true},
		{"+00:00", 0, true},
		{"", 0, false},
		{"garbage", 0, false},
		{"01:00", 0, false}, // missing sign
	}
	for _, tt := range tests {
		loc, ok := fixedZoneFromOffset(tt.in)
		if ok != tt.wantOK {
			t.Errorf("fixedZoneFromOffset(%q) ok = %v, want %v", tt.in, ok, tt.wantOK)
			continue
		}
		if !ok {
			continue
		}
		_, secs := time.Date(2026, 1, 1, 0, 0, 0, 0, loc).Zone()
		if secs != tt.wantSecs {
			t.Errorf("fixedZoneFromOffset(%q) secs = %d, want %d", tt.in, secs, tt.wantSecs)
		}
	}
}

// TestNewFirestoreHappenings_WithLocation is S4: the reader takes the user
// zone as a parameter instead of a hardcoded time.Local.
func TestNewFirestoreHappenings_WithLocation(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	r := NewFirestoreHappenings(config.Config{}, nil, WithLocation(tokyo))
	fh, ok := r.(*firestoreHappenings)
	if !ok || fh.loc != tokyo {
		t.Fatalf("reader loc = %v, want %v", fh, tokyo)
	}
}

// TestSortNotDoneFirst is m5: TodosReader.List must return not-done items
// first, without reordering within either group (a stable sort).
func TestSortNotDoneFirst(t *testing.T) {
	items := []Todo{
		{ID: "a", Title: "Done first", Done: true},
		{ID: "b", Title: "Not done first", Done: false},
		{ID: "c", Title: "Not done second", Done: false},
		{ID: "d", Title: "Done second", Done: true},
	}
	sortNotDoneFirst(items)
	want := []string{"b", "c", "a", "d"}
	for i, id := range want {
		if items[i].ID != id {
			t.Fatalf("order = %v, want IDs %v", items, want)
		}
	}
}

// closerStub is a reader stub whose Close records whether it ran and can
// return a caller-supplied error.
type closerStub struct {
	FakeHappenings
	closed bool
	err    error
}

func (c *closerStub) Close() error {
	c.closed = true
	return c.err
}

// TestReaders_Close is m4's Readers.Close(): every reader that supports
// Close gets closed, fakes without one (no Close method) are silently
// skipped rather than causing a panic/type-assertion failure, and any
// closer errors are joined rather than dropped.
func TestReaders_Close(t *testing.T) {
	h := &closerStub{}
	readers := Readers{
		Happenings: h,
		Todos:      &FakeTodos{},    // no Close method -- must be skipped, not panic
		Contacts:   &FakeContacts{}, // same
	}
	if err := readers.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
	if !h.closed {
		t.Fatal("Happenings reader was not closed")
	}
}

func TestListKeyFor(t *testing.T) {
	if listKeyFor(ListKindBuy) != listusdbo.BuyGroceriesListID {
		t.Errorf("listKeyFor(buy) = %q", listKeyFor(ListKindBuy))
	}
	if listKeyFor(ListKindDo) != listusdbo.DoTasksListID {
		t.Errorf("listKeyFor(do) = %q", listKeyFor(ListKindDo))
	}
}
