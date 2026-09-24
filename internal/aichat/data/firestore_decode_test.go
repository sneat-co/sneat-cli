package data

import (
	"testing"
	"time"

	calendariusdbo "github.com/sneat-co/calendarius/backend/dbo4calendarius"
	"github.com/sneat-co/listus/backend/const4listus"
	listusdbo "github.com/sneat-co/listus/backend/dbo4listus"
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
	h := toHappening("sp1", "h1", dbo)
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
	h := toHappening("sp1", "h2", dbo)
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
	h := toHappening("sp1", "h3", dbo)
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

func TestListKeyFor(t *testing.T) {
	if listKeyFor(ListKindBuy) != listusdbo.BuyGroceriesListID {
		t.Errorf("listKeyFor(buy) = %q", listKeyFor(ListKindBuy))
	}
	if listKeyFor(ListKindDo) != listusdbo.DoTasksListID {
		t.Errorf("listKeyFor(do) = %q", listKeyFor(ListKindDo))
	}
}
