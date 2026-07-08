package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sneat-co/contactus/backend/dbo4contactus"
	"github.com/sneat-co/sneat-cli/internal/firestoredb"
	"github.com/strongo/strongoapp/person"
)

// --- fakes ---

type fakeSpaces struct {
	spaces map[string]any
	err    error
}

func (f fakeSpaces) ListSpaces(context.Context, string) (map[string]any, error) {
	return f.spaces, f.err
}

type fakeContacts struct {
	bySpace map[string][]firestoredb.Contact
	calls   map[string]int
	err     error
}

func (f *fakeContacts) ListContacts(_ context.Context, spaceID string) ([]firestoredb.Contact, error) {
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[spaceID]++
	return f.bySpace[spaceID], f.err
}

func contact(id, name string, roles ...string) firestoredb.Contact {
	d := &dbo4contactus.ContactDbo{}
	d.Type = "person"
	d.Status = "active"
	d.Names = &person.NameFields{FirstName: name}
	d.Roles = roles
	return firestoredb.Contact{ID: id, Contact: d}
}

// runCmd executes a command and returns the message it produced (nil-safe).
func runCmd(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

// step applies a message and returns the model plus the produced command.
func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	tm, cmd := m.Update(msg)
	return tm.(Model), cmd
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// newLoadedSpaces builds a model already showing a loaded Spaces list.
func newLoadedSpaces(t *testing.T, spaces map[string]any, contacts *fakeContacts) Model {
	t.Helper()
	m := New(fakeSpaces{spaces: spaces}, contacts, "uid")
	loadCmd := m.Init()
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = step(t, m, runCmd(loadCmd)) // spacesLoadedMsg
	return m
}

// --- helper tests ---

func TestRoleHelpers(t *testing.T) {
	roles := []string{"member", "parent", "cook"}
	if !hasMemberRole(roles) {
		t.Error("hasMemberRole should be true")
	}
	if hasMemberRole([]string{"child"}) {
		t.Error("hasMemberRole should be false")
	}
	got := withoutMemberRole(roles)
	if len(got) != 2 || got[0] != "parent" || got[1] != "cook" {
		t.Errorf("withoutMemberRole = %v", got)
	}
}

func TestContactItemsFrom_MembersOnly(t *testing.T) {
	cs := []firestoredb.Contact{
		contact("c1", "Alice", "member", "parent"),
		contact("c2", "Bob", "child"),
		contact("c3", "Cara", "member"),
	}
	members := contactItemsFrom(cs, true)
	if len(members) != 2 {
		t.Fatalf("members count = %d, want 2", len(members))
	}
	// Alice keeps "parent" but not "member"; roles are stripped of member.
	alice := members[0].(contactItem)
	if hasMemberRole(alice.roles) {
		t.Errorf("member role should be stripped, got %v", alice.roles)
	}
	if len(alice.roles) != 1 || alice.roles[0] != "parent" {
		t.Errorf("alice roles = %v, want [parent]", alice.roles)
	}

	all := contactItemsFrom(cs, false)
	if len(all) != 3 {
		t.Fatalf("all count = %d, want 3", len(all))
	}
	// In the full list, member role is preserved.
	if !hasMemberRole(all[0].(contactItem).roles) {
		t.Errorf("full list should keep member role, got %v", all[0].(contactItem).roles)
	}
}

func TestSpaceItemsFrom_SortedAndMapped(t *testing.T) {
	spaces := map[string]any{
		"z1": map[string]any{"title": "Zeta", "type": "family", "status": "active"},
		"a1": map[string]any{"title": "Alpha", "type": "private", "status": "active"},
	}
	items := spaceItemsFrom(spaces)
	if len(items) != 2 || items[0].(spaceItem).id != "a1" {
		t.Fatalf("expected sorted by id, got %v", items)
	}
	if items[0].(spaceItem).title != "Alpha" {
		t.Errorf("title = %q", items[0].(spaceItem).title)
	}
}

// --- navigation tests ---

func twoSpaces() map[string]any {
	return map[string]any{
		"fam":  map[string]any{"title": "Family", "type": "family", "status": "active", "roles": []any{"member"}},
		"priv": map[string]any{"title": "Private", "type": "private", "status": "active"},
	}
}

func TestNavigation_SpacesToSpaceToMembersToCard(t *testing.T) {
	fc := &fakeContacts{bySpace: map[string][]firestoredb.Contact{
		"fam": {contact("c1", "Alice", "member", "parent"), contact("c2", "Bob", "child")},
	}}
	m := newLoadedSpaces(t, twoSpaces(), fc)

	sp, ok := m.top().(*spacesScreen)
	if !ok {
		t.Fatalf("top is %T, want *spacesScreen", m.top())
	}
	if len(sp.list.Items()) != 2 {
		t.Fatalf("spaces list has %d items", len(sp.list.Items()))
	}

	// Enter the first space ("fam" sorts before "priv").
	m, cmd := step(t, m, key("enter"))
	m, _ = step(t, m, runCmd(cmd)) // pushMsg -> pushes spaceScreen, returns loadContacts cmd
	spaceScr, ok := m.top().(*spaceScreen)
	if !ok {
		t.Fatalf("top is %T, want *spaceScreen", m.top())
	}
	if spaceScr.space.id != "fam" {
		t.Fatalf("entered space %q, want fam", spaceScr.space.id)
	}

	// Deliver the contacts load for the space.
	m, _ = step(t, m, contactsLoadedMsg{spaceID: "fam", contacts: fc.bySpace["fam"]})
	if got := m.top().(*spaceScreen).count; got != 2 {
		t.Fatalf("space contact count = %d, want 2", got)
	}

	// Select "Members" (first menu item) and open it.
	m, cmd = step(t, m, key("enter"))
	m, _ = step(t, m, runCmd(cmd)) // push contactsScreen(membersOnly); Init uses cache
	cs, ok := m.top().(*contactsScreen)
	if !ok {
		t.Fatalf("top is %T, want *contactsScreen", m.top())
	}
	if !cs.membersOnly {
		t.Error("expected membersOnly screen")
	}
	if len(cs.list.Items()) != 1 {
		t.Fatalf("members list has %d items, want 1", len(cs.list.Items()))
	}

	// Open the contact card.
	m, cmd = step(t, m, key("enter"))
	m, _ = step(t, m, runCmd(cmd))
	card, ok := m.top().(*contactCardScreen)
	if !ok {
		t.Fatalf("top is %T, want *contactCardScreen", m.top())
	}
	if card.contact.title != "Alice" {
		t.Errorf("card contact = %q, want Alice", card.contact.title)
	}
}

func TestNavigation_BackPopsAndRootQuits(t *testing.T) {
	fc := &fakeContacts{bySpace: map[string][]firestoredb.Contact{"fam": {contact("c1", "Alice", "member")}}}
	m := newLoadedSpaces(t, twoSpaces(), fc)

	// Descend into a space.
	m, cmd := step(t, m, key("enter"))
	m, _ = step(t, m, runCmd(cmd))
	if len(m.stack) != 2 {
		t.Fatalf("stack depth = %d, want 2", len(m.stack))
	}

	// esc pops back to spaces.
	m, cmd = step(t, m, key("esc"))
	m, _ = step(t, m, runCmd(cmd)) // popMsg
	if len(m.stack) != 1 {
		t.Fatalf("stack depth after esc = %d, want 1", len(m.stack))
	}
	if _, ok := m.top().(*spacesScreen); !ok {
		t.Fatalf("top is %T, want *spacesScreen", m.top())
	}

	// esc at the root quits.
	_, cmd = step(t, m, key("esc"))
	if _, ok := runCmd(cmd).(tea.QuitMsg); !ok {
		t.Fatal("esc at spaces screen should quit")
	}
}

func TestNavigation_ContactsCachePreventsRefetch(t *testing.T) {
	fc := &fakeContacts{bySpace: map[string][]firestoredb.Contact{
		"fam": {contact("c1", "Alice", "member"), contact("c2", "Bob", "child")},
	}}
	m := newLoadedSpaces(t, twoSpaces(), fc)

	// Enter space; its Init issues the one and only ListContacts call.
	m, cmd := step(t, m, key("enter"))
	loadCmd := runCmd(cmd).(pushMsg)
	m, initCmd := step(t, m, loadCmd)
	m, _ = step(t, m, runCmd(initCmd)) // contactsLoadedMsg populates the cache

	// Open Members, back, open Contacts — all served from cache.
	m, cmd = step(t, m, key("enter")) // Members
	m, _ = step(t, m, runCmd(cmd))
	m, cmd = step(t, m, key("esc")) // back to space
	m, _ = step(t, m, runCmd(cmd))
	// move selection to Contacts (second item) then open
	m, _ = step(t, m, key("down"))
	m, cmd = step(t, m, key("enter"))
	m, _ = step(t, m, runCmd(cmd))

	full, ok := m.top().(*contactsScreen)
	if !ok {
		t.Fatalf("top is %T, want *contactsScreen", m.top())
	}
	if full.membersOnly {
		t.Error("expected full Contacts screen")
	}
	if len(full.list.Items()) != 2 {
		t.Errorf("contacts list has %d items, want 2", len(full.list.Items()))
	}
	if fc.calls["fam"] != 1 {
		t.Errorf("ListContacts called %d times for fam, want 1 (cache)", fc.calls["fam"])
	}
}

func TestSpacesScreen_LoadError(t *testing.T) {
	m := New(fakeSpaces{err: errors.New("boom")}, &fakeContacts{}, "uid")
	loadCmd := m.Init()
	m, _ = step(t, m, runCmd(loadCmd)) // errMsg
	view := m.View()
	if view == "" || !contains(view, "boom") {
		t.Errorf("error view should mention the error, got %q", view)
	}
}

func TestViews_RenderAcrossScreens(t *testing.T) {
	fc := &fakeContacts{bySpace: map[string][]firestoredb.Contact{
		"fam": {contact("c1", "Alice", "member", "parent")},
	}}
	m := newLoadedSpaces(t, twoSpaces(), fc)

	// Spaces view lists a space and shows the footer help.
	if v := m.View(); !contains(v, "Family") || !contains(v, "quit") {
		t.Errorf("spaces view = %q", v)
	}

	// Space screen: header shows details.
	m, cmd := step(t, m, key("enter"))
	m, initCmd := step(t, m, runCmd(cmd))
	m, _ = step(t, m, runCmd(initCmd))
	if v := m.View(); !contains(v, "id:") || !contains(v, "Members") || !contains(v, "fam") {
		t.Errorf("space view = %q", v)
	}

	// Members list.
	m, cmd = step(t, m, key("enter"))
	m, _ = step(t, m, runCmd(cmd))
	if v := m.View(); !contains(v, "Alice") {
		t.Errorf("members view = %q", v)
	}

	// Contact card shows fields.
	m, cmd = step(t, m, key("enter"))
	m, _ = step(t, m, runCmd(cmd))
	v := m.View()
	if !contains(v, "Alice") || !contains(v, "roles") || !contains(v, "parent") {
		t.Errorf("card view = %q", v)
	}

	// Left arrow pops the card back to the members list.
	m, cmd = step(t, m, key("left"))
	m, _ = step(t, m, runCmd(cmd))
	if _, ok := m.top().(*contactsScreen); !ok {
		t.Fatalf("left should return to contacts, got %T", m.top())
	}
}

func TestItemAndScreenMetadata(t *testing.T) {
	if (spaceItem{id: "x", title: "X"}).FilterValue() != "X x" {
		t.Error("space FilterValue")
	}
	if (spaceItem{id: "y"}).name() != "y" {
		t.Error("space name falls back to id")
	}
	if (menuItem{label: "Members"}).FilterValue() != "Members" {
		t.Error("menu FilterValue")
	}
	if (contactItem{title: "Alice"}).FilterValue() != "Alice" {
		t.Error("contact FilterValue")
	}
	sp := spaceItem{id: "fam", title: "Family"}
	cases := map[string]string{
		newSpacesScreen().Title():                                  "Spaces",
		newSpaceScreen(sp).Title():                                 "Family",
		newContactsScreen(sp, true).Title():                        "Members",
		newContactsScreen(sp, false).Title():                       "Contacts",
		newContactCardScreen(sp, contactItem{title: "Al"}).Title(): "Al",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("Title = %q, want %q", got, want)
		}
	}
}

func TestContactTitleFallbacks(t *testing.T) {
	d := &dbo4contactus.ContactDbo{}
	d.Title = "Acme Ltd"
	if contactTitle(d) != "Acme Ltd" {
		t.Errorf("title from Title = %q", contactTitle(d))
	}
	if contactTitle(&dbo4contactus.ContactDbo{}) != "(unnamed)" {
		t.Error("unnamed fallback")
	}
}

func TestContactItemsFrom_SkipsNil(t *testing.T) {
	cs := []firestoredb.Contact{{ID: "x", Contact: nil}, contact("c1", "Al", "member")}
	if got := contactItemsFrom(cs, false); len(got) != 1 {
		t.Errorf("nil contact not skipped: %d items", len(got))
	}
}

func TestSpaceScreen_ReentryUsesCache(t *testing.T) {
	fc := &fakeContacts{bySpace: map[string][]firestoredb.Contact{"fam": {contact("c1", "Al", "member")}}}
	m := newLoadedSpaces(t, twoSpaces(), fc)
	// First entry loads contacts.
	m, cmd := step(t, m, key("enter"))
	m, initCmd := step(t, m, runCmd(cmd))
	m, _ = step(t, m, runCmd(initCmd)) // contactsLoadedMsg -> cache
	// Back to spaces, then re-enter: Init hits the cache branch (count set, no reload).
	m, cmd = step(t, m, key("esc"))
	m, _ = step(t, m, runCmd(cmd))
	m, cmd = step(t, m, key("enter"))
	m, _ = step(t, m, runCmd(cmd))
	if got := m.top().(*spaceScreen).count; got != 1 {
		t.Errorf("re-entry count = %d, want 1", got)
	}
	if fc.calls["fam"] != 1 {
		t.Errorf("ListContacts called %d times, want 1", fc.calls["fam"])
	}
}

func TestQuitKeyAndResize(t *testing.T) {
	m := newLoadedSpaces(t, twoSpaces(), &fakeContacts{})
	// 'q' quits from the root.
	if _, cmd := step(t, m, key("q")); func() bool { _, ok := runCmd(cmd).(tea.QuitMsg); return !ok }() {
		t.Error("q should quit at spaces screen")
	}
	// ctrl+c quits globally.
	if _, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyCtrlC}); func() bool { _, ok := runCmd(cmd).(tea.QuitMsg); return !ok }() {
		t.Error("ctrl+c should quit")
	}
	// Resize does not crash and keeps us on the spaces screen.
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if _, ok := m.top().(*spacesScreen); !ok {
		t.Fatalf("after resize top is %T", m.top())
	}
}

func TestContactsScreen_LoadErrorView(t *testing.T) {
	fc := &fakeContacts{err: errors.New("net down")}
	m := newLoadedSpaces(t, twoSpaces(), fc)
	m, cmd := step(t, m, key("enter"))    // open space
	m, initCmd := step(t, m, runCmd(cmd)) // push space screen
	m, _ = step(t, m, runCmd(initCmd))    // errMsg from loadContacts
	if v := m.View(); !contains(v, "net down") {
		t.Errorf("space error view = %q", v)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

var _ list.Item = spaceItem{}
