// Package controls implements the sneat-chat MVP's structured presentations
// (brief §3) as strongo/aichat tui/transcript.Block values: HappeningCard,
// DayCalendar, WeekCalendar, HappeningsList, TodoList, BuyList and
// ContactCard render as ListBlock/CardBlock (below); ContactsGrid reuses
// tui/grid directly -- the SAME generic grid DataTug uses -- rather than a
// competing Sneat-specific grid (brief §3).
//
// Models are semantic (a title, a subtitle, an EntityRef) rather than
// terminal drawing instructions: the CLI (this package) owns turning a
// pipeline.Output into one of these, and the rendering/focus/interaction
// that follows.
package controls

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/strongo/aichat/ai/session"
	"github.com/strongo/aichat/tui/grid"
	"github.com/strongo/aichat/tui/transcript"
)

// Item is one row of a ListBlock: a title, an optional one-line subtitle
// (e.g. a happening's time, a todo's list), and the entity it refers to.
type Item struct {
	Title    string
	Subtitle string
	Ref      session.EntityRef
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
// still a valid, renderable block: "Nothing scheduled.").
func NewListBlock(heading string, items []Item) *ListBlock {
	return &ListBlock{Heading: heading, Items: items}
}

func (b *ListBlock) Focusable() bool { return len(b.Items) > 0 }

// Current implements transcript.EntityBlock.
func (b *ListBlock) Current() *session.EntityRef {
	if b.cursor < 0 || b.cursor >= len(b.Items) {
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
			b.cursor--
		}
	case "down", "j":
		if b.cursor < len(b.Items)-1 {
			b.cursor++
		}
	}
	return b, nil
}

func (b *ListBlock) View(width int, focused bool) string {
	var sb strings.Builder
	heading := b.Heading
	if focused {
		heading = lipgloss.NewStyle().Bold(true).Render(heading)
	}
	sb.WriteString(heading)
	if len(b.Items) == 0 {
		sb.WriteString("\n(nothing here)")
		return clampWidth(sb.String(), width)
	}
	for i, it := range b.Items {
		line := "\n  " + it.Title
		if it.Subtitle != "" {
			line += "  " + lipgloss.NewStyle().Faint(true).Render(it.Subtitle)
		}
		if focused && i == b.cursor {
			line = "\n> " + it.Title
			if it.Subtitle != "" {
				line += "  " + it.Subtitle
			}
			line = lipgloss.NewStyle().Reverse(true).Render(line)
		}
		sb.WriteString(line)
	}
	return clampWidth(sb.String(), width)
}

func clampWidth(s string, width int) string {
	if width <= 0 {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
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

func (b *CardBlock) Update(msg tea.Msg) (transcript.Block, tea.Cmd) { return b, nil }

func (b *CardBlock) View(width int, focused bool) string {
	title := b.Title
	if focused {
		title = lipgloss.NewStyle().Bold(true).Reverse(true).Render(title)
	} else {
		title = lipgloss.NewStyle().Bold(true).Render(title)
	}
	var sb strings.Builder
	sb.WriteString(title)
	for _, f := range b.Fields {
		fmt.Fprintf(&sb, "\n%s: %s", f[0], f[1])
	}
	return clampWidth(sb.String(), width)
}

// NewContactsGrid builds the ContactsGrid presentation over tui/grid --
// reused verbatim, per brief §3, rather than a Sneat-specific competing
// grid. Row.Values is positional (name, then nothing else for the MVP's
// contact list; a real product would add columns as contactus grows
// queryable fields).
func NewContactsGrid(title string, contacts []Contact) *grid.Model {
	columns := []grid.Column{{Name: "Name"}}
	rows := make([]grid.Row, 0, len(contacts))
	for _, c := range contacts {
		ref := c.Ref
		rows = append(rows, grid.Row{Key: c.Ref.Keys["contactID"], Values: []any{c.Name}, Ref: &ref})
	}
	return grid.New(columns, rows, grid.WithTitle(title))
}

// Contact is the sliver of a contact ContactsGrid needs -- decoupled from
// internal/aichat/data.Contact so this package stays a leaf (no import
// cycle risk as chatapp/data both grow).
type Contact struct {
	Name string
	Ref  session.EntityRef
}
