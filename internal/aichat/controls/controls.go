// Package controls implements the sneat-chat MVP's structured presentations
// (brief §3) as strongo/aichat tui/transcript.Block values: HappeningCard,
// DayCalendar, WeekCalendar, HappeningsList, TodoList, BuyList and
// ContactCard render as ListBlock/CardBlock (below); ContactsGrid reuses
// strongo-tui's pkg/grid -- the SAME generic grid DataTug uses -- wrapped
// with tuigoff/pkg/gridblock for the transcript (brief §3).
//
// Models are semantic (a title, a subtitle, an EntityRef) rather than
// terminal drawing instructions: the CLI (this package) owns turning a
// pipeline.Output into one of these, and the rendering/focus/interaction
// that follows.
package controls

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/strongo/aichat/ai/session"
	"github.com/strongo/aichat/tui"
	"github.com/tuigoff/tuigoff/pkg/grid"
	"github.com/tuigoff/tuigoff/pkg/gridblock"
	"github.com/tuigoff/tuigoff/pkg/transcript"
)

// Item is one row of a ListBlock: a title, an optional one-line subtitle
// (e.g. a happening's time, a todo's list), and the entity it refers to.
//
// Header marks a non-actionable section-heading row (S9: WeekCalendar's
// "Monday"/"Tuesday"/... day sections) -- it carries no entity, the cursor
// skips over it, and View renders it without a bullet/reverse-video cursor
// treatment.
type Item struct {
	Title    string
	Subtitle string
	Ref      session.EntityRef
	Header   bool
}

// ItemActivatedMsg is emitted when Enter is pressed over a ListBlock's
// cursor item -- the ListBlock counterpart of pkg/grid's RowActivatedMsg.
// internal/chatapp's handler.OnMsg (a chatshell.MsgHandler) focuses the
// entity on it, the same way it already focuses a grid.RowActivatedMsg's
// row: a real, UI-driven way to set session.State.Focused (brief §4/§18
// scenario 5), rather than a test or product calling state.Focus directly.
type ItemActivatedMsg struct {
	Ref session.EntityRef
}

// ListBlock renders a chronological/enumerated list of Items -- the shared
// shape behind DayCalendar, WeekCalendar, HappeningsList, TodoList and
// BuyList (they differ only in Heading and how Items was built, not in how
// the block renders or navigates). It implements transcript.Block and
// transcript.EntityBlock (Current() reports the cursor's Ref, for "+ add to
// sidebar" and pronoun resolution).
type ListBlock struct {
	Heading string
	Items   []Item
	cursor  int
}

// NewListBlock builds a ListBlock. items may be empty (an empty list is
// still a valid, renderable block: "Nothing scheduled."). The cursor starts
// on the first non-Header item, so a block that opens with a section
// heading (WeekCalendar's "Monday") doesn't report that heading as
// Current().
func NewListBlock(heading string, items []Item) *ListBlock {
	b := &ListBlock{Heading: heading, Items: items}
	if len(items) > 0 && items[0].Header {
		if i, ok := nextSelectable(items, 0, 1); ok {
			b.cursor = i
		}
	}
	return b
}

// nextSelectable scans from start (inclusive) in the given direction
// (+1/-1) for the next non-Header index within bounds. ok is false when
// nothing selectable is found in that direction (including a list of all
// headers -- degenerate, but must not panic or loop forever), in which case
// the caller keeps its current position.
func nextSelectable(items []Item, start, dir int) (idx int, ok bool) {
	for i := start; i >= 0 && i < len(items); i += dir {
		if !items[i].Header {
			return i, true
		}
	}
	return 0, false
}

// Focusable reports whether the block has at least one non-Header item --
// an all-headers list (degenerate) is not a real destination for focus.
func (b *ListBlock) Focusable() bool {
	for _, it := range b.Items {
		if !it.Header {
			return true
		}
	}
	return false
}

// Current implements transcript.EntityBlock.
func (b *ListBlock) Current() *session.EntityRef {
	if b.cursor < 0 || b.cursor >= len(b.Items) || b.Items[b.cursor].Header {
		return nil
	}
	ref := b.Items[b.cursor].Ref
	return &ref
}

func (b *ListBlock) Update(msg tea.Msg) (transcript.Block, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return b, nil
	}
	switch key.String() {
	case "up", "k":
		if b.cursor > 0 {
			if i, ok := nextSelectable(b.Items, b.cursor-1, -1); ok {
				b.cursor = i
			}
		}
	case "down", "j":
		if b.cursor < len(b.Items)-1 {
			if i, ok := nextSelectable(b.Items, b.cursor+1, 1); ok {
				b.cursor = i
			}
		}
	case "+":
		// Same convention as pkg/grid's "+": pin the entity under the cursor
		// to the working-context sidebar (brief §4/§18 scenario 8).
		if ref := b.Current(); ref != nil {
			return b, func() tea.Msg { return tui.AddToSidebarMsg{Ref: *ref} }
		}
	case "enter":
		// Same convention as pkg/grid's Enter/RowActivatedMsg: focus the
		// entity under the cursor (brief §4/§18 scenario 5).
		if ref := b.Current(); ref != nil {
			return b, func() tea.Msg { return ItemActivatedMsg{Ref: *ref} }
		}
	}
	return b, nil
}

// View is content only (no lipgloss colour/padding/border of its own --
// shared-look cutover, strongo/aichat#chat-shared-look): the block renders
// inside a tui/theme.Card, which already supplies the card's own
// focus/fill treatment (REQ: theme-card-rendering); the ">"/"  " prefix
// below is this block's own plain-text cursor marker, not a styled one.
func (b *ListBlock) View(width int, focused bool) string {
	var sb strings.Builder
	sb.WriteString(b.Heading)
	if len(b.Items) == 0 {
		sb.WriteString("\n(nothing here)")
		return clampWidth(sb.String(), width)
	}
	for i, it := range b.Items {
		if it.Header {
			sb.WriteString("\n" + it.Title)
			continue
		}
		line := "\n  " + it.Title
		if it.Subtitle != "" {
			line += "  " + it.Subtitle
		}
		if focused && i == b.cursor {
			line = "\n> " + it.Title
			if it.Subtitle != "" {
				line += "  " + it.Subtitle
			}
		}
		sb.WriteString(line)
	}
	return clampWidth(sb.String(), width)
}

// clampWidth clamps each line independently (ansi.Truncate, a pure content
// operation -- no colour/padding/border) so a multi-line View is never
// widened past width on any one line.
func clampWidth(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "")
	}
	return strings.Join(lines, "\n")
}

// CardBlock renders one entity's detail -- HappeningCard and ContactCard.
// Fields is an ordered label/value list (kept generic so both control kinds
// share one renderer rather than two near-identical ones).
type CardBlock struct {
	Title  string
	Fields [][2]string // [label, value]
	Ref    session.EntityRef
}

func NewCardBlock(title string, ref session.EntityRef, fields ...[2]string) *CardBlock {
	return &CardBlock{Title: title, Fields: fields, Ref: ref}
}

func (b *CardBlock) Focusable() bool { return true }

func (b *CardBlock) Current() *session.EntityRef {
	ref := b.Ref
	return &ref
}

func (b *CardBlock) Update(msg tea.Msg) (transcript.Block, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return b, nil
	}
	if key.String() == "+" {
		// Same convention as pkg/grid's "+" and ListBlock's above: pin this
		// card's entity to the sidebar (brief §4/§18 scenario 8).
		ref := b.Ref
		return b, func() tea.Msg { return tui.AddToSidebarMsg{Ref: ref} }
	}
	return b, nil
}

// View is content only (shared-look cutover: no lipgloss colour/padding/
// border of its own -- the wrapping tui/theme.Card already supplies the
// card's own bold-header/focus-fill treatment, REQ: theme-card-rendering).
func (b *CardBlock) View(width int, _ bool) string {
	var sb strings.Builder
	sb.WriteString(b.Title)
	for _, f := range b.Fields {
		fmt.Fprintf(&sb, "\n%s: %s", f[0], f[1])
	}
	return clampWidth(sb.String(), width)
}

// NewContactsGrid builds the ContactsGrid presentation over strongo-tui's
// pkg/grid -- reused verbatim, per brief §3, rather than a Sneat-specific
// competing grid -- and wraps it as a transcript block. Row.Values is
// positional (name, then nothing else for the MVP's contact list; a real
// product would add columns as contactus grows queryable fields).
func NewContactsGrid(title string, contacts []Contact) *gridblock.Block {
	columns := []grid.Column{{Name: "Name"}}
	rows := make([]grid.Row, 0, len(contacts))
	for _, c := range contacts {
		ref := c.Ref
		rows = append(rows, grid.Row{Key: c.Ref.Keys["contactID"], Values: []any{c.Name}, Ref: &ref})
	}
	return gridblock.Wrap(grid.New(columns, rows, grid.WithTitle(title)))
}

// Contact is the sliver of a contact ContactsGrid needs -- decoupled from
// internal/aichat/data.Contact so this package stays a leaf (no import
// cycle risk as chatapp/data both grow).
type Contact struct {
	Name string
	Ref  session.EntityRef
}

// HappeningRow is the sliver of a happening a calendar presentation needs
// (S9): the entity ref for resolution/actions/"+", plus the Start/End/
// Recurring a bare session.EntityRef cannot carry. Decoupled from
// internal/aichat/data.Happening for the same leaf-package reason as
// Contact above.
type HappeningRow struct {
	Ref       session.EntityRef
	Title     string
	Start     time.Time
	End       time.Time
	Recurring bool
}

// TodoRow is the sliver of a todo/buy-list item a list presentation needs
// (S9: "TodoList/BuyList with done state").
type TodoRow struct {
	Ref   session.EntityRef
	Title string
	Done  bool
}

// sortedHappenings returns happenings sorted by Start (zero Start, e.g. a
// recurring happening's unresolved occurrence, sorts last rather than
// first -- see HappeningRow's doc comment on Recurring). The input is not
// mutated.
func sortedHappenings(happenings []HappeningRow) []HappeningRow {
	out := make([]HappeningRow, len(happenings))
	copy(out, happenings)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Start.IsZero() != out[j].Start.IsZero() {
			return out[j].Start.IsZero()
		}
		return out[i].Start.Before(out[j].Start)
	})
	return out
}

// timeRangeLabel renders a happening's Subtitle for a same-day presentation
// (DayCalendar, a WeekCalendar's day section): "10:00-10:30", or just
// "10:00" with no End, or "(recurring)" when Start couldn't be resolved to
// a real occurrence at all (see HappeningsReader's documented limitation).
func timeRangeLabel(h HappeningRow) string {
	if h.Start.IsZero() {
		if h.Recurring {
			return "(recurring)"
		}
		return ""
	}
	label := h.Start.Format("15:04")
	if !h.End.IsZero() && h.End.After(h.Start) {
		label += "-" + h.End.Format("15:04")
	}
	if h.Recurring {
		label += " (recurring)"
	}
	return label
}

// dateTimeLabel renders a happening's Subtitle for a cross-day chronological
// presentation (HappeningsList/upcoming): "Sep 26 10:00".
func dateTimeLabel(h HappeningRow) string {
	if h.Start.IsZero() {
		if h.Recurring {
			return "(recurring)"
		}
		return ""
	}
	label := h.Start.Format("Jan 2 15:04")
	if h.Recurring {
		label += " (recurring)"
	}
	return label
}

// NewHappeningCard renders one happening's detail (S9: HappeningCard) --
// used both for a single resolved happening and for a reschedule/cancel
// confirmation's "what am I about to change" context.
func NewHappeningCard(h HappeningRow) *CardBlock {
	fields := [][2]string{{"When", dateTimeLabel(h)}}
	if !h.End.IsZero() && h.End.After(h.Start) {
		fields[0][1] = h.Start.Format("Jan 2 15:04") + "-" + h.End.Format("15:04")
	}
	if h.Recurring {
		fields = append(fields, [2]string{"Repeats", "yes (this MVP shows the template time, not the resolved next occurrence)"})
	}
	return NewCardBlock(h.Title, h.Ref, fields...)
}

// NewDayCalendar renders happenings time-sorted with start-end times (S9):
// "10:00-10:30  Dentist appointment".
func NewDayCalendar(heading string, happenings []HappeningRow) *ListBlock {
	sorted := sortedHappenings(happenings)
	items := make([]Item, 0, len(sorted))
	for _, h := range sorted {
		items = append(items, Item{Title: h.Title, Subtitle: timeRangeLabel(h), Ref: h.Ref})
	}
	return NewListBlock(heading, items)
}

// NewHappeningsList renders happenings chronologically with date+time (S9):
// "Sep 26 10:00  Dentist appointment".
func NewHappeningsList(heading string, happenings []HappeningRow) *ListBlock {
	sorted := sortedHappenings(happenings)
	items := make([]Item, 0, len(sorted))
	for _, h := range sorted {
		items = append(items, Item{Title: h.Title, Subtitle: dateTimeLabel(h), Ref: h.Ref})
	}
	return NewListBlock(heading, items)
}

// NewWeekCalendar groups happenings into Mon..Sun sections, each time-sorted
// with times (S9). weekStart is the Monday the week begins on (pipeline.go's
// showWeek already computes this the same way). Section headings and an
// empty day's placeholder row are both non-focusable Header items -- the
// cursor skips them (see Item.Header/nextSelectable).
func NewWeekCalendar(heading string, weekStart time.Time, happenings []HappeningRow) *ListBlock {
	byDay := make(map[string][]HappeningRow)
	for _, h := range happenings {
		if h.Start.IsZero() {
			continue // nothing to file under a day section without a resolved date
		}
		// File and label the occurrence in the requested calendar's zone.
		// A slot's own zone may put its local date on a different weekday.
		h.Start = h.Start.In(weekStart.Location())
		if !h.End.IsZero() {
			h.End = h.End.In(weekStart.Location())
		}
		key := h.Start.Format("2006-01-02")
		byDay[key] = append(byDay[key], h)
	}
	var items []Item
	for i := 0; i < 7; i++ {
		day := weekStart.AddDate(0, 0, i)
		items = append(items, Item{Title: day.Format("Monday, Jan 2"), Header: true})
		dayItems := sortedHappenings(byDay[day.Format("2006-01-02")])
		if len(dayItems) == 0 {
			items = append(items, Item{Title: "  (nothing scheduled)", Header: true})
			continue
		}
		for _, h := range dayItems {
			items = append(items, Item{Title: h.Title, Subtitle: timeRangeLabel(h), Ref: h.Ref})
		}
	}
	return NewListBlock(heading, items)
}

// NewTodoList renders todo-list items with their done state (S9): a "[x]"/
// "[ ]" marker. Does not itself sort -- TodosReader.List already orders
// not-done first (m5); this only shows the state that ordering implies.
func NewTodoList(heading string, todos []TodoRow) *ListBlock {
	items := make([]Item, 0, len(todos))
	for _, t := range todos {
		mark := "[ ]"
		if t.Done {
			mark = "[x]"
		}
		items = append(items, Item{Title: mark + " " + t.Title, Ref: t.Ref})
	}
	return NewListBlock(heading, items)
}

// NewBuyList renders a to-buy/shopping list with done state (S9). Same
// shape as NewTodoList today; kept as its own named constructor (rather
// than callers reusing NewTodoList directly) so a to-buy-specific field
// (quantity, say) can be added later without an ambiguous shared name.
func NewBuyList(heading string, items []TodoRow) *ListBlock {
	return NewTodoList(heading, items)
}
