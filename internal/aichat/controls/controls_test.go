package controls

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/strongo/aichat/ai/session"
	"github.com/strongo/aichat/tui"
	"github.com/strongo/aichat/tui/transcript"
)

func plain(s string) string { return ansi.Strip(s) }

func TestListBlock_View(t *testing.T) {
	items := []Item{
		{Title: "Dentist", Subtitle: "16:00", Ref: session.EntityRef{Type: "happening"}},
		{Title: "Standup", Subtitle: "10:00", Ref: session.EntityRef{Type: "happening"}},
	}
	b := NewListBlock("Today", items)
	if !b.Focusable() {
		t.Fatal("expected a non-empty list to be focusable")
	}
	view := plain(b.View(40, false))
	if !strings.Contains(view, "Today") || !strings.Contains(view, "Dentist") || !strings.Contains(view, "Standup") {
		t.Fatalf("view = %q", view)
	}
	if got := b.Current(); got == nil || got.Type != "happening" {
		t.Fatalf("Current() = %v, want the cursor's (first) item", got)
	}
}

func TestListBlock_EmptyIsNotFocusable(t *testing.T) {
	b := NewListBlock("Todos", nil)
	if b.Focusable() {
		t.Fatal("an empty list must not be focusable")
	}
	if b.Current() != nil {
		t.Fatal("Current() on an empty list must be nil")
	}
	view := plain(b.View(40, false))
	if !strings.Contains(view, "nothing") {
		t.Fatalf("view = %q, want an explicit empty message", view)
	}
}

func TestListBlock_CursorMovesWithArrowKeys(t *testing.T) {
	items := []Item{
		{Title: "A", Ref: session.EntityRef{Type: "happening", Keys: map[string]string{"id": "a"}}},
		{Title: "B", Ref: session.EntityRef{Type: "happening", Keys: map[string]string{"id": "b"}}},
	}
	b := NewListBlock("List", items)
	if b.Current().Keys["id"] != "a" {
		t.Fatalf("initial cursor should be the first item")
	}
	blk, cmd := b.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if cmd != nil {
		t.Fatal("ListBlock.Update must not return a command")
	}
	if blk.(*ListBlock).Current().Keys["id"] != "b" {
		t.Fatalf("cursor did not move down")
	}
}

// TestListBlock_PlusEmitsAddToSidebar covers brief §4/§18 scenario 8: "+"
// over the cursor's item pins it to the sidebar, the same convention as
// tui/grid's own "+" handling.
func TestListBlock_PlusEmitsAddToSidebar(t *testing.T) {
	items := []Item{
		{Title: "A", Ref: session.EntityRef{Type: "happening", Keys: map[string]string{"id": "a"}}},
		{Title: "B", Ref: session.EntityRef{Type: "happening", Keys: map[string]string{"id": "b"}}},
	}
	b := NewListBlock("List", items)
	blk, cmd := b.Update(tea.KeyPressMsg{Text: "+", Code: '+'})
	if cmd == nil {
		t.Fatal("expected a command for '+'")
	}
	if blk != b {
		t.Fatal("Update must return the same block for '+'")
	}
	msg := cmd()
	added, ok := msg.(tui.AddToSidebarMsg)
	if !ok {
		t.Fatalf("msg = %#v, want tui.AddToSidebarMsg", msg)
	}
	if added.Ref.Keys["id"] != "a" {
		t.Fatalf("pinned ref = %+v, want the cursor's item", added.Ref)
	}
}

func TestListBlock_PlusOnEmptyListIsNoOp(t *testing.T) {
	b := NewListBlock("Todos", nil)
	_, cmd := b.Update(tea.KeyPressMsg{Text: "+", Code: '+'})
	if cmd != nil {
		t.Fatal("expected no command for '+' on an empty list")
	}
}

func TestCardBlock_PlusEmitsAddToSidebar(t *testing.T) {
	ref := session.EntityRef{Type: "contact", Keys: map[string]string{"contactID": "c1"}}
	b := NewCardBlock("Alice", ref)
	_, cmd := b.Update(tea.KeyPressMsg{Text: "+", Code: '+'})
	if cmd == nil {
		t.Fatal("expected a command for '+'")
	}
	msg := cmd()
	added, ok := msg.(tui.AddToSidebarMsg)
	if !ok {
		t.Fatalf("msg = %#v, want tui.AddToSidebarMsg", msg)
	}
	if added.Ref.Keys["contactID"] != "c1" {
		t.Fatalf("pinned ref = %+v", added.Ref)
	}
}

func TestCardBlock_View(t *testing.T) {
	ref := session.EntityRef{Type: "contact", Keys: map[string]string{"contactID": "c1"}}
	b := NewCardBlock("Alice", ref, [2]string{"Email", "alice@example.com"})
	if !b.Focusable() {
		t.Fatal("a card must be focusable")
	}
	if b.Current() == nil || b.Current().Keys["contactID"] != "c1" {
		t.Fatal("Current() must report the card's own entity")
	}
	view := plain(b.View(40, true))
	if !strings.Contains(view, "Alice") || !strings.Contains(view, "alice@example.com") {
		t.Fatalf("view = %q", view)
	}
}

// TestNewDayCalendar_TimeSortedWithStartEnd covers S9: DayCalendar is
// time-sorted with real start-end times shown, not just a title.
func TestNewDayCalendar_TimeSortedWithStartEnd(t *testing.T) {
	later := HappeningRow{Title: "Standup", Ref: session.EntityRef{Type: "happening", Keys: map[string]string{"id": "later"}},
		Start: time.Date(2026, 9, 25, 14, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 25, 14, 15, 0, 0, time.UTC)}
	earlier := HappeningRow{Title: "Dentist", Ref: session.EntityRef{Type: "happening", Keys: map[string]string{"id": "earlier"}},
		Start: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 25, 10, 30, 0, 0, time.UTC)}
	b := NewDayCalendar("Today", []HappeningRow{later, earlier})
	if len(b.Items) != 2 || b.Items[0].Title != "Dentist" || b.Items[1].Title != "Standup" {
		t.Fatalf("Items = %+v, want time-sorted (Dentist 10:00 before Standup 14:00)", b.Items)
	}
	if b.Items[0].Subtitle != "10:00-10:30" {
		t.Errorf("Subtitle = %q, want the start-end range", b.Items[0].Subtitle)
	}
}

// TestNewHappeningsList_ChronologicalDateTime covers S9: HappeningsList
// shows a chronological date+time label, not just a bare time.
func TestNewHappeningsList_ChronologicalDateTime(t *testing.T) {
	h := HappeningRow{Title: "Flight", Start: time.Date(2026, 9, 26, 7, 30, 0, 0, time.UTC)}
	b := NewHappeningsList("Upcoming", []HappeningRow{h})
	if b.Items[0].Subtitle != "Sep 26 07:30" {
		t.Errorf("Subtitle = %q, want a date+time label", b.Items[0].Subtitle)
	}
}

// TestNewWeekCalendar_GroupsByDayAndSkipsHeadersOnNavigation covers S9:
// Mon..Sun sections, each time-sorted, with the cursor skipping non-
// actionable section-heading/placeholder rows.
func TestNewWeekCalendar_GroupsByDayAndSkipsHeadersOnNavigation(t *testing.T) {
	monday := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	standup := HappeningRow{Title: "Standup", Ref: session.EntityRef{Type: "happening", Keys: map[string]string{"id": "standup"}},
		Start: time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)}
	dentist := HappeningRow{Title: "Dentist", Ref: session.EntityRef{Type: "happening", Keys: map[string]string{"id": "dentist"}},
		Start: time.Date(2026, 9, 24, 16, 0, 0, 0, time.UTC)} // Thursday
	b := NewWeekCalendar("This week", monday, []HappeningRow{dentist, standup})

	// The cursor must start on the FIRST real item (Standup, Monday), not
	// the "Monday, Sep 21" heading at index 0.
	cur := b.Current()
	if cur == nil || cur.Keys["id"] != "standup" {
		t.Fatalf("initial Current() = %v, want Standup (the first non-header item)", cur)
	}

	// Moving down must skip Tuesday/Wednesday's "(nothing scheduled)"
	// placeholders and Thursday's heading, landing directly on Dentist.
	blk := b
	for i := 0; i < 20 && blk.Current().Keys["id"] != "dentist"; i++ {
		var b2 transcript.Block
		b2, _ = blk.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		blk = b2.(*ListBlock)
	}
	if blk.Current() == nil || blk.Current().Keys["id"] != "dentist" {
		t.Fatalf("cursor never reached Dentist by pressing down; Current() = %v", blk.Current())
	}
}

func TestNewWeekCalendar_UsesRequestedCalendarTimezone(t *testing.T) {
	dublin, err := time.LoadLocation("Europe/Dublin")
	if err != nil {
		t.Fatal(err)
	}
	monday := time.Date(2026, 9, 28, 0, 0, 0, 0, dublin)
	// Sunday night in New York is Monday morning in Dublin.
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	event := HappeningRow{Title: "Late call", Start: time.Date(2026, 9, 27, 23, 0, 0, 0, newYork), End: time.Date(2026, 9, 27, 23, 30, 0, 0, newYork)}
	view := NewWeekCalendar("Week", monday, []HappeningRow{event}).View(0, false)
	if !strings.Contains(view, "Monday, Sep 28\n  Late call  04:00-04:30") {
		t.Fatalf("event should appear under Monday in the calendar zone:\n%s", view)
	}
}

// TestNewTodoList_ShowsDoneState covers S9: TodoList/BuyList show done
// state via a marker, not just the title.
func TestNewTodoList_ShowsDoneState(t *testing.T) {
	todos := []TodoRow{
		{Title: "Buy milk", Done: false},
		{Title: "Call plumber", Done: true},
	}
	b := NewTodoList("Todos", todos)
	if b.Items[0].Title != "[ ] Buy milk" {
		t.Errorf("Items[0].Title = %q", b.Items[0].Title)
	}
	if b.Items[1].Title != "[x] Call plumber" {
		t.Errorf("Items[1].Title = %q", b.Items[1].Title)
	}
}

// TestNewHappeningCard_ShowsWhen covers S9's HappeningCard: a single
// happening's detail shows its date/time, not just its title.
func TestNewHappeningCard_ShowsWhen(t *testing.T) {
	h := HappeningRow{Title: "Dentist appointment", Ref: session.EntityRef{Type: "happening"},
		Start: time.Date(2026, 9, 26, 16, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 26, 16, 30, 0, 0, time.UTC)}
	b := NewHappeningCard(h)
	view := plain(b.View(60, false))
	if !strings.Contains(view, "Dentist appointment") || !strings.Contains(view, "16:00") || !strings.Contains(view, "16:30") {
		t.Fatalf("view = %q, want title + start-end", view)
	}
}

func TestNewContactsGrid_BuildsPositionalRows(t *testing.T) {
	contacts := []Contact{
		{Name: "Alice", Ref: session.EntityRef{Type: "contact", Keys: map[string]string{"contactID": "c1"}}},
		{Name: "Bob", Ref: session.EntityRef{Type: "contact", Keys: map[string]string{"contactID": "c2"}}},
	}
	g := NewContactsGrid("Contacts", contacts)
	if len(g.Rows()) != 2 {
		t.Fatalf("rows = %d, want 2", len(g.Rows()))
	}
	if g.Rows()[0].Values[0] != "Alice" || g.Rows()[1].Values[0] != "Bob" {
		t.Fatalf("rows = %+v", g.Rows())
	}
	if g.Rows()[0].Ref == nil {
		t.Fatalf("row Ref = %+v", g.Rows()[0].Ref)
	}
	ref, ok := g.Rows()[0].Ref.(*session.EntityRef)
	if !ok || ref.Keys["contactID"] != "c1" {
		t.Fatalf("row Ref = %+v", g.Rows()[0].Ref)
	}
}
