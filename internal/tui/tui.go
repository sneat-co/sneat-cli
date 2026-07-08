// Package tui implements the interactive terminal UI for browsing spaces,
// their members and contacts, and a contact card. It is launched by
// `sneat ui` or `sneat spaces --ui`.
package tui

import (
	"context"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sneat-co/contactus/backend/dbo4contactus"
	"github.com/sneat-co/sneat-cli/internal/firestoredb"
)

// SpacesReader lists the signed-in user's spaces.
type SpacesReader interface {
	ListSpaces(ctx context.Context, uid string) (map[string]any, error)
}

// ContactsReader lists a space's contacts.
type ContactsReader interface {
	ListContacts(ctx context.Context, spaceID string) ([]firestoredb.Contact, error)
}

// screen is one view in the navigation stack.
type screen interface {
	Init(m *Model) tea.Cmd
	Update(m *Model, msg tea.Msg) (screen, tea.Cmd)
	View(m *Model) string
	Title() string
}

// Model is the root bubbletea model. It owns a navigation stack of screens and
// the shared data readers, and caches each space's contacts so switching
// between the Members and Contacts views does not refetch.
type Model struct {
	spaces   SpacesReader
	contacts ContactsReader
	uid      string
	width    int
	height   int
	stack    []screen
	cache    map[string][]firestoredb.Contact
}

// New builds the root model starting on the Spaces screen.
func New(spaces SpacesReader, contacts ContactsReader, uid string) Model {
	return Model{
		spaces:   spaces,
		contacts: contacts,
		uid:      uid,
		stack:    []screen{newSpacesScreen()},
		cache:    map[string][]firestoredb.Contact{},
	}
}

// Init kicks off the first screen's data load.
func (m Model) Init() tea.Cmd {
	if len(m.stack) == 0 {
		return nil
	}
	return m.stack[0].Init(&m)
}

// navigation messages let a screen push a child or pop itself without owning the stack.
type pushMsg struct{ s screen }
type popMsg struct{}

func push(s screen) tea.Cmd { return func() tea.Msg { return pushMsg{s} } }
func pop() tea.Cmd          { return func() tea.Msg { return popMsg{} } }

// data-load messages.
type spacesLoadedMsg struct{ spaces map[string]any }
type contactsLoadedMsg struct {
	spaceID  string
	contacts []firestoredb.Contact
}
type errMsg struct{ err error }

func loadSpaces(r SpacesReader, uid string) tea.Cmd {
	return func() tea.Msg {
		sp, err := r.ListSpaces(context.Background(), uid)
		if err != nil {
			return errMsg{err}
		}
		return spacesLoadedMsg{sp}
	}
}

func loadContacts(r ContactsReader, spaceID string) tea.Cmd {
	return func() tea.Msg {
		cs, err := r.ListContacts(context.Background(), spaceID)
		if err != nil {
			return errMsg{err}
		}
		return contactsLoadedMsg{spaceID: spaceID, contacts: cs}
	}
}

// Update handles global concerns (resize, hard quit, push/pop) and delegates
// everything else to the top screen.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pushMsg:
		m.stack = append(m.stack, msg.s)
		return m, m.stack[len(m.stack)-1].Init(&m)
	case popMsg:
		if len(m.stack) > 1 {
			m.stack = m.stack[:len(m.stack)-1]
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	if len(m.stack) == 0 {
		return m, nil
	}
	top := m.stack[len(m.stack)-1]
	ns, cmd := top.Update(&m, msg)
	m.stack[len(m.stack)-1] = ns
	return m, cmd
}

// View renders the top screen.
func (m Model) View() string {
	if len(m.stack) == 0 {
		return ""
	}
	return m.stack[len(m.stack)-1].View(&m)
}

// top returns the current screen (nil when the stack is empty). Test helper.
func (m Model) top() screen {
	if len(m.stack) == 0 {
		return nil
	}
	return m.stack[len(m.stack)-1]
}

// --- styles ---

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	labelStyle  = lipgloss.NewStyle().Faint(true)
	headerStyle = lipgloss.NewStyle().Padding(0, 1)
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	footerStyle = lipgloss.NewStyle().Faint(true).Padding(0, 1)
)

const footerHelp = "↑/↓ move · enter open · esc/← back · / filter · q quit"

// listHeight returns the height available for a list below a header.
func (m Model) listHeight(headerLines int) int {
	h := m.height - headerLines - 2 // header + footer
	if h < 3 {
		h = 3
	}
	return h
}

// --- roles helpers ---

const memberRole = "member"

// hasMemberRole reports whether roles include the member role.
func hasMemberRole(roles []string) bool {
	for _, r := range roles {
		if r == memberRole {
			return true
		}
	}
	return false
}

// withoutMemberRole returns roles with the member role removed.
func withoutMemberRole(roles []string) []string {
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		if r != memberRole {
			out = append(out, r)
		}
	}
	return out
}

// --- value coercion (spaces briefs are map[string]any) ---

func str(v any) string {
	s, _ := v.(string)
	return s
}

func strList(v any) []string {
	raw, _ := v.([]any)
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// contactTitle returns a display name for a contact, guarding a nil Names.
func contactTitle(d *dbo4contactus.ContactDbo) string {
	if d.Title != "" {
		return d.Title
	}
	if d.Names != nil {
		if n := d.Names.GetFullName(); n != "" {
			return n
		}
	}
	return "(unnamed)"
}

func joinRoles(roles []string) string {
	if len(roles) == 0 {
		return "—"
	}
	return strings.Join(roles, ", ")
}
