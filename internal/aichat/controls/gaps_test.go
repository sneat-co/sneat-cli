package controls

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/strongo/aichat/ai/session"
)

// TestNewBuyList_SameShapeAsTodoList covers NewBuyList, which had no direct
// test: it must render the same "[x]"/"[ ]" marker shape NewTodoList does.
func TestNewBuyList_SameShapeAsTodoList(t *testing.T) {
	rows := []TodoRow{{Title: "Milk", Done: false}, {Title: "Bread", Done: true}}
	b := NewBuyList("Buy", rows)
	if len(b.Items) != 2 {
		t.Fatalf("Items = %+v", b.Items)
	}
	if b.Items[0].Title != "[ ] Milk" || b.Items[1].Title != "[x] Bread" {
		t.Fatalf("Items = %+v", b.Items)
	}
}

// TestNextSelectable_AllHeaders covers the degenerate "nothing selectable"
// branch (a list of only Header items).
func TestNextSelectable_AllHeaders(t *testing.T) {
	items := []Item{{Header: true}, {Header: true}}
	if _, ok := nextSelectable(items, 0, 1); ok {
		t.Fatal("nextSelectable over all-Header items = ok, want !ok")
	}
	if _, ok := nextSelectable(items, 1, -1); ok {
		t.Fatal("nextSelectable(dir=-1) over all-Header items = ok, want !ok")
	}
}

// TestListBlock_Update_UpDownAtBounds covers Update's up/down when the
// cursor is already at the first/last item (no nextSelectable move
// possible) and a non-arrow, non-mapped key (no-op passthrough).
func TestListBlock_Update_UpDownAtBounds(t *testing.T) {
	b := NewListBlock("H", []Item{{Title: "a"}, {Title: "b"}})
	b.cursor = 0
	if _, cmd := b.Update(tea.KeyPressMsg{Code: 'k'}); cmd != nil {
		t.Fatal("up at first item should be a no-op")
	}
	if b.cursor != 0 {
		t.Fatalf("cursor = %d, want unchanged 0", b.cursor)
	}

	b.cursor = 1
	if _, cmd := b.Update(tea.KeyPressMsg{Code: 'j'}); cmd != nil {
		t.Fatal("down at last item should be a no-op")
	}
	if b.cursor != 1 {
		t.Fatalf("cursor = %d, want unchanged 1", b.cursor)
	}

	// A message that isn't a KeyPressMsg at all is a no-op passthrough.
	nb, cmd := b.Update(struct{ tea.Msg }{})
	if nb != b || cmd != nil {
		t.Fatal("non-key message should be a pure no-op")
	}

	// A key that maps to nothing falls through the switch untouched.
	if _, cmd := b.Update(tea.KeyPressMsg{Code: 'x'}); cmd != nil {
		t.Fatal("an unmapped key should be a no-op")
	}
}

// TestListBlock_Update_UpMovesToNextSelectable covers Update's up-key
// success branch: moving the cursor past a Header item to the previous
// selectable one.
func TestListBlock_Update_UpMovesToNextSelectable(t *testing.T) {
	b := NewListBlock("H", []Item{{Title: "a"}, {Title: "hdr", Header: true}, {Title: "c"}})
	b.cursor = 2
	if _, cmd := b.Update(tea.KeyPressMsg{Code: 'k'}); cmd != nil {
		t.Fatal("ListBlock.Update must not return a command")
	}
	if b.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (skipping the Header at index 1)", b.cursor)
	}
}

// TestListBlock_View_RendersHeaderRows covers View's Header-row branch
// (bold section heading, no cursor/bullet treatment).
func TestListBlock_View_RendersHeaderRows(t *testing.T) {
	b := &ListBlock{Heading: "Week", Items: []Item{{Title: "Monday", Header: true}, {Title: "Standup"}}}
	got := plain(b.View(80, false))
	if !strings.Contains(got, "Monday") || !strings.Contains(got, "Standup") {
		t.Fatalf("View() = %q, want both the header and the item rendered", got)
	}
}

// TestCardBlock_Update_NonKeyAndOtherKeys covers Update's non-KeyPressMsg
// no-op and its non-"+" key fallthrough, neither exercised by the package's
// existing TestCardBlock_PlusEmitsAddToSidebar.
func TestCardBlock_Update_NonKeyAndOtherKeys(t *testing.T) {
	b := NewCardBlock("Title", session.EntityRef{}, [2]string{"k", "v"})

	if _, cmd := b.Update(struct{ tea.Msg }{}); cmd != nil {
		t.Fatal("a non-KeyPressMsg should be a no-op")
	}
	if _, cmd := b.Update(tea.KeyPressMsg{Code: 'x'}); cmd != nil {
		t.Fatal("a key other than \"+\" should be a no-op")
	}
}

// TestNewHappeningCard_Recurring covers the Repeats field appended for a
// recurring happening.
func TestNewHappeningCard_Recurring(t *testing.T) {
	c := NewHappeningCard(HappeningRow{Title: "Standup", Recurring: true})
	found := false
	for _, f := range c.Fields {
		if f[0] == "Repeats" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Fields = %+v, want a Repeats field for a recurring happening", c.Fields)
	}
}

// TestNewWeekCalendar_SkipsUnresolvedStart covers the zero-Start skip branch
// -- such a happening must not panic or be filed under a bogus day section.
func TestNewWeekCalendar_SkipsUnresolvedStart(t *testing.T) {
	weekStart := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC) // a Monday
	b := NewWeekCalendar("Week", weekStart, []HappeningRow{{Title: "Unresolved recurring", Recurring: true}})
	for _, it := range b.Items {
		if it.Title == "Unresolved recurring" {
			t.Fatal("a zero-Start happening must not be filed under any day section")
		}
	}
}

// TestListBlock_Update_Enter covers Enter's ItemActivatedMsg emission and
// its no-op when Current() is nil (an empty list).
func TestListBlock_Update_Enter(t *testing.T) {
	ref := session.EntityRef{Type: "happening", Keys: map[string]string{"id": "h1"}}
	b := NewListBlock("H", []Item{{Title: "a", Ref: ref}})
	_, cmd := b.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter over a real item should emit ItemActivatedMsg")
	}
	msg := cmd()
	activated, ok := msg.(ItemActivatedMsg)
	if !ok || activated.Ref.Type != ref.Type || activated.Ref.Keys["id"] != ref.Keys["id"] {
		t.Fatalf("msg = %+v, want ItemActivatedMsg{Ref: %+v}", msg, ref)
	}

	empty := NewListBlock("H", nil)
	if _, cmd := empty.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("enter over an empty list should be a no-op")
	}
}

// TestClampWidth covers both the width<=0 passthrough and the positive-width
// clamp branch.
func TestClampWidth(t *testing.T) {
	if got := clampWidth("hello", 0); got != "hello" {
		t.Fatalf("clampWidth(width=0) = %q, want unchanged", got)
	}
	if got := clampWidth("hello", -1); got != "hello" {
		t.Fatalf("clampWidth(width=-1) = %q, want unchanged", got)
	}
	got := clampWidth("hello world this is long", 5)
	if !strings.Contains(got, "hello") {
		t.Fatalf("clampWidth(width=5) = %q, want it to still start with the content", got)
	}
}

// TestListBlock_View_FocusedCursorLine covers View's focused-cursor-with-
// subtitle rendering branch, not exercised by the package's existing
// TestListBlock_View (unfocused).
func TestListBlock_View_FocusedCursorLine(t *testing.T) {
	b := NewListBlock("H", []Item{{Title: "a", Subtitle: "sub"}})
	got := b.View(80, true)
	if !strings.Contains(got, "a") || !strings.Contains(got, "sub") {
		t.Fatalf("View(focused) = %q, want title and subtitle rendered", got)
	}
}

// TestTimeRangeLabel covers every branch: zero+recurring, zero+non-recurring,
// start-only, start+end, and recurring-with-times.
func TestTimeRangeLabel(t *testing.T) {
	start := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)

	if got := timeRangeLabel(HappeningRow{Recurring: true}); got != "(recurring)" {
		t.Errorf("zero+recurring = %q", got)
	}
	if got := timeRangeLabel(HappeningRow{}); got != "" {
		t.Errorf("zero+non-recurring = %q, want empty", got)
	}
	if got := timeRangeLabel(HappeningRow{Start: start}); got != "10:00" {
		t.Errorf("start-only = %q, want 10:00", got)
	}
	if got := timeRangeLabel(HappeningRow{Start: start, End: end}); got != "10:00-10:30" {
		t.Errorf("start+end = %q, want 10:00-10:30", got)
	}
	if got := timeRangeLabel(HappeningRow{Start: start, End: end, Recurring: true}); got != "10:00-10:30 (recurring)" {
		t.Errorf("start+end+recurring = %q", got)
	}
	// End not after Start (bad/zero-duration data) must not append a range.
	if got := timeRangeLabel(HappeningRow{Start: start, End: start}); got != "10:00" {
		t.Errorf("end==start = %q, want just 10:00", got)
	}
}

// TestDateTimeLabel covers zero+recurring, zero+non-recurring, and the
// recurring-with-a-resolved-start branch (dateTimeLabel's own tests
// elsewhere only cover the plain resolved-start case).
func TestDateTimeLabel(t *testing.T) {
	if got := dateTimeLabel(HappeningRow{Recurring: true}); got != "(recurring)" {
		t.Errorf("zero+recurring = %q", got)
	}
	if got := dateTimeLabel(HappeningRow{}); got != "" {
		t.Errorf("zero+non-recurring = %q, want empty", got)
	}
	start := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	if got := dateTimeLabel(HappeningRow{Start: start, Recurring: true}); got != "Sep 25 10:00 (recurring)" {
		t.Errorf("resolved+recurring = %q", got)
	}
}

// TestSortedHappenings_ZeroStartSortsLast covers the zero-Start-sorts-last
// branch and confirms the input slice is not mutated.
func TestSortedHappenings_ZeroStartSortsLast(t *testing.T) {
	start := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	in := []HappeningRow{{Title: "zero"}, {Title: "timed", Start: start}}
	out := sortedHappenings(in)
	if out[0].Title != "timed" || out[1].Title != "zero" {
		t.Fatalf("out = %+v, want timed before zero-Start", out)
	}
	if in[0].Title != "zero" {
		t.Fatal("sortedHappenings must not mutate its input")
	}
}
