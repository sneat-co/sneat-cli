package tui

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/sneat-co/contactus/backend/dbo4contactus"
	"github.com/sneat-co/sneat-cli/internal/firestoredb"
	"github.com/strongo/strongo-tui/pkg/nav"
	"github.com/strongo/strongo-tui/pkg/nav/navtest"
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

// contactAs is like contact but assigns a UserID, so it is detected as "self"
// when that uid is signed in.
func contactAs(id, name, userID string, roles ...string) firestoredb.Contact {
	c := contact(id, name, roles...)
	c.Contact.UserID = userID
	return c
}

// fakeDeleter records DeleteContact calls and can return a canned error.
type fakeDeleter struct {
	calls []string // "spaceID/contactID" per call
	err   error
}

func (f *fakeDeleter) DeleteContact(_ context.Context, spaceID, contactID string) error {
	f.calls = append(f.calls, spaceID+"/"+contactID)
	return f.err
}

func twoSpaces() map[string]any {
	return map[string]any{
		"fam":  map[string]any{"title": "Family", "type": "family", "status": "active", "roles": []any{"member"}},
		"priv": map[string]any{"title": "Private", "type": "private", "status": "active"},
	}
}

// famContacts builds a Family space fixture with the given contacts.
func famContacts(cs ...firestoredb.Contact) *fakeContacts {
	return &fakeContacts{bySpace: map[string][]firestoredb.Contact{"fam": cs}}
}

// newHarness builds a navtest harness already showing a loaded Spaces list.
func newHarness(t *testing.T, spaces map[string]any, contacts *fakeContacts) *navtest.Harness {
	t.Helper()
	return newHarnessWith(t, spaces, contacts, nil, "uid")
}

func newHarnessWith(t *testing.T, spaces map[string]any, contacts *fakeContacts, deleter ContactDeleter, uid string) *navtest.Harness {
	t.Helper()
	a := &app{
		spaces:   fakeSpaces{spaces: spaces},
		contacts: contacts,
		deleter:  deleter,
		uid:      uid,
		cache:    map[string][]firestoredb.Contact{},
	}
	h := navtest.New(t, nav.Page{Title: "Spaces", Content: newSpacesScreen(a)},
		navtest.WithNav(nav.WithoutLogin()),
		navtest.WithSize(80, 24),
	)
	// Init already ran loadSpaces; deliver its result if still pending.
	// navtest.New runs Init, so spacesLoadedMsg should already be applied.
	return h
}

// openContacts navigates spaces → space → Contacts and returns the harness
// sitting on the loaded contacts screen.
func openContacts(t *testing.T, h *navtest.Harness) *navtest.Harness {
	t.Helper()
	h.Press("enter") // enter space (fam sorts first)
	h.Press("down")  // move to "Contacts" menu item
	h.Press("enter") // open Contacts
	if _, ok := h.Model().Content().(*contactsScreen); !ok {
		t.Fatalf("expected contacts screen, got %T", h.Model().Content())
	}
	return h
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
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
	members := contactItemsFrom(cs, true, "uid")
	if len(members) != 2 {
		t.Fatalf("members count = %d, want 2", len(members))
	}
	alice := menuItemRef(members[0]).(contactItem)
	if hasMemberRole(alice.roles) {
		t.Errorf("member role should be stripped, got %v", alice.roles)
	}
	if len(alice.roles) != 1 || alice.roles[0] != "parent" {
		t.Errorf("alice roles = %v, want [parent]", alice.roles)
	}

	all := contactItemsFrom(cs, false, "uid")
	if len(all) != 3 {
		t.Fatalf("all count = %d, want 3", len(all))
	}
	if !hasMemberRole(menuItemRef(all[0]).(contactItem).roles) {
		t.Errorf("full list should keep member role, got %v", menuItemRef(all[0]).(contactItem).roles)
	}
}

func TestSpaceItemsFrom_SortedAndMapped(t *testing.T) {
	spaces := map[string]any{
		"z1": map[string]any{"title": "Zeta", "type": "family", "status": "active"},
		"a1": map[string]any{"title": "Alpha", "type": "private", "status": "active"},
	}
	items := spaceItemsFrom(spaces)
	if len(items) != 2 || menuItemRef(items[0]).(spaceItem).id != "a1" {
		t.Fatalf("expected sorted by id, got %v", items)
	}
	if menuItemRef(items[0]).(spaceItem).title != "Alpha" {
		t.Errorf("title = %q", menuItemRef(items[0]).(spaceItem).title)
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
	if got := contactItemsFrom(cs, false, "uid"); len(got) != 1 {
		t.Errorf("nil contact not skipped: %d items", len(got))
	}
}

func TestContactItemsFrom_MarksSelf(t *testing.T) {
	cs := []firestoredb.Contact{
		contactAs("me", "Me", "u1", "member"),
		contact("c2", "Bob", "child"),
	}
	items := contactItemsFrom(cs, false, "u1")
	if !menuItemRef(items[0]).(contactItem).isSelf {
		t.Error("contact with matching UserID should be isSelf")
	}
	if menuItemRef(items[1]).(contactItem).isSelf {
		t.Error("other contact should not be isSelf")
	}
	if menuItemRef(contactItemsFrom(cs, false, "")[1]).(contactItem).isSelf {
		t.Error("empty uid must not mark an empty-UserID contact as self")
	}
}

func TestItemAndScreenMetadata(t *testing.T) {
	a := &app{cache: map[string][]firestoredb.Contact{}}
	sp := spaceItem{id: "fam", title: "Family"}
	cases := map[string]string{
		newSpacesScreen(a).Title():                                    "Spaces",
		newSpaceScreen(a, sp).Title():                                 "Family",
		newContactsScreen(a, sp, true).Title():                        "Members",
		newContactsScreen(a, sp, false).Title():                       "Contacts",
		newContactCardScreen(a, sp, contactItem{title: "Al"}).Title(): "Al",
		newConfirmDeleteScreen(a, sp, contactItem{}, false).Title():   "Delete contact",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("Title = %q, want %q", got, want)
		}
	}
	if (spaceItem{id: "y"}).name() != "y" {
		t.Error("space name falls back to id")
	}
	if (spaceItem{id: "x", title: "X", spaceType: "family", status: "active"}).detail() != "family · active" {
		t.Error("space detail")
	}
	if (contactItem{ctype: "person", gender: "female"}).detail() != "person · female" {
		t.Error("contact detail")
	}
}

// --- navigation tests ---

func TestNavigation_SpacesToSpaceToMembersToCard(t *testing.T) {
	fc := &fakeContacts{bySpace: map[string][]firestoredb.Contact{
		"fam": {contact("c1", "Alice", "member", "parent"), contact("c2", "Bob", "child")},
	}}
	h := newHarness(t, twoSpaces(), fc)
	h.RequireContains("Family")

	h.Press("enter") // enter fam
	spaceScr, ok := h.Model().Content().(*spaceScreen)
	if !ok {
		t.Fatalf("top is %T, want *spaceScreen", h.Model().Content())
	}
	if spaceScr.space.id != "fam" {
		t.Fatalf("entered space %q, want fam", spaceScr.space.id)
	}
	if spaceScr.count != 2 {
		t.Fatalf("space contact count = %d, want 2", spaceScr.count)
	}

	h.Press("enter") // Members
	cs, ok := h.Model().Content().(*contactsScreen)
	if !ok {
		t.Fatalf("top is %T, want *contactsScreen", h.Model().Content())
	}
	if !cs.membersOnly {
		t.Error("expected membersOnly screen")
	}
	if len(cs.list.Items()) != 1 {
		t.Fatalf("members list has %d items, want 1", len(cs.list.Items()))
	}

	h.Press("enter") // card
	card, ok := h.Model().Content().(*contactCardScreen)
	if !ok {
		t.Fatalf("top is %T, want *contactCardScreen", h.Model().Content())
	}
	if card.contact.title != "Alice" {
		t.Errorf("card contact = %q, want Alice", card.contact.title)
	}
}

func TestNavigation_BackPopsAndRootQuits(t *testing.T) {
	fc := &fakeContacts{bySpace: map[string][]firestoredb.Contact{"fam": {contact("c1", "Alice", "member")}}}
	h := newHarness(t, twoSpaces(), fc)

	h.Press("enter")
	if h.Model().Depth() != 2 {
		t.Fatalf("stack depth = %d, want 2", h.Model().Depth())
	}

	h.Press("esc")
	if h.Model().Depth() != 1 {
		t.Fatalf("stack depth after esc = %d, want 1", h.Model().Depth())
	}
	if _, ok := h.Model().Content().(*spacesScreen); !ok {
		t.Fatalf("top is %T, want *spacesScreen", h.Model().Content())
	}

	h.Press("esc")
	if !h.Quit() {
		t.Fatal("esc at spaces screen should quit")
	}
}

func TestNavigation_ContactsCachePreventsRefetch(t *testing.T) {
	fc := &fakeContacts{bySpace: map[string][]firestoredb.Contact{
		"fam": {contact("c1", "Alice", "member"), contact("c2", "Bob", "child")},
	}}
	h := newHarness(t, twoSpaces(), fc)

	h.Press("enter") // space; Init loads contacts once
	h.Press("enter") // Members
	h.Press("esc")   // back to space
	h.Press("down")  // Contacts
	h.Press("enter")

	full, ok := h.Model().Content().(*contactsScreen)
	if !ok {
		t.Fatalf("top is %T, want *contactsScreen", h.Model().Content())
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
	a := &app{spaces: fakeSpaces{err: errors.New("boom")}, cache: map[string][]firestoredb.Contact{}}
	h := navtest.New(t, nav.Page{Title: "Spaces", Content: newSpacesScreen(a)},
		navtest.WithNav(nav.WithoutLogin()),
		navtest.WithSize(80, 24),
	)
	h.RequireContains("boom")
}

func TestViews_RenderAcrossScreens(t *testing.T) {
	fc := &fakeContacts{bySpace: map[string][]firestoredb.Contact{
		"fam": {contact("c1", "Alice", "member", "parent")},
	}}
	h := newHarness(t, twoSpaces(), fc)
	h.RequireContains("Family")

	h.Press("enter")
	h.RequireContains("id:")
	h.RequireContains("Members")
	h.RequireContains("fam")

	h.Press("enter")
	h.RequireContains("Alice")

	h.Press("enter")
	h.RequireContains("Alice")
	h.RequireContains("roles")
	h.RequireContains("parent")

	h.Press("left")
	if _, ok := h.Model().Content().(*contactsScreen); !ok {
		t.Fatalf("left should return to contacts, got %T", h.Model().Content())
	}
}

func TestSpaceScreen_ReentryUsesCache(t *testing.T) {
	fc := &fakeContacts{bySpace: map[string][]firestoredb.Contact{"fam": {contact("c1", "Al", "member")}}}
	h := newHarness(t, twoSpaces(), fc)
	h.Press("enter")
	h.Press("esc")
	h.Press("enter")
	if got := h.Model().Content().(*spaceScreen).count; got != 1 {
		t.Errorf("re-entry count = %d, want 1", got)
	}
	if fc.calls["fam"] != 1 {
		t.Errorf("ListContacts called %d times, want 1", fc.calls["fam"])
	}
}

func TestContactCard_ShowsFields(t *testing.T) {
	fc := &fakeContacts{bySpace: map[string][]firestoredb.Contact{"fam": {contact("c1", "Alice", "member")}}}
	h := newHarness(t, twoSpaces(), fc)
	h.Press("enter", "enter", "enter")
	h.RequireContains("Alice")
	h.RequireContains("id")
}

func TestQuitKeyAndResize(t *testing.T) {
	h := newHarness(t, twoSpaces(), &fakeContacts{})
	h.Press("q")
	if h.Quit() {
		t.Error("q must not quit (reserved for filtering)")
	}
	h.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !h.Quit() {
		t.Error("ctrl+c should quit")
	}
	h2 := newHarness(t, twoSpaces(), &fakeContacts{})
	h2.Resize(120, 40)
	if _, ok := h2.Model().Content().(*spacesScreen); !ok {
		t.Fatalf("after resize top is %T", h2.Model().Content())
	}
}

func TestContactsScreen_LoadErrorView(t *testing.T) {
	fc := &fakeContacts{err: errors.New("net down")}
	h := newHarness(t, twoSpaces(), fc)
	h.Press("enter")
	h.RequireContains("net down")
}

func TestDelete_SelfIsRefused(t *testing.T) {
	fc := famContacts(contactAs("me", "Me", "u1", "member"), contact("c2", "Bob", "child"))
	del := &fakeDeleter{}
	h := newHarnessWith(t, twoSpaces(), fc, del, "u1")
	openContacts(t, h)

	h.Press("delete")
	if _, ok := h.Model().Content().(*confirmDeleteScreen); ok {
		t.Fatal("deleting self must not open the confirm screen")
	}
	h.RequireContains("Cannot delete yourself")
	if len(del.calls) != 0 {
		t.Errorf("deleter must not be called for self, got %v", del.calls)
	}
}

func TestDelete_ConfirmAndSucceed(t *testing.T) {
	fc := famContacts(contact("c1", "Alice", "parent"), contact("c2", "Bob", "child"))
	del := &fakeDeleter{}
	h := newHarnessWith(t, twoSpaces(), fc, del, "u1")
	openContacts(t, h)

	h.Press("delete")
	confirm, ok := h.Model().Content().(*confirmDeleteScreen)
	if !ok {
		t.Fatalf("top is %T, want *confirmDeleteScreen", h.Model().Content())
	}
	if confirm.contact.id != "c1" {
		t.Fatalf("confirming delete of %q, want c1", confirm.contact.id)
	}

	before := fc.calls["fam"]
	h.Press("enter") // confirm → delete → unwind + reload
	if _, ok := h.Model().Content().(*contactsScreen); !ok {
		t.Fatalf("after delete top is %T, want *contactsScreen", h.Model().Content())
	}
	if len(del.calls) != 1 || del.calls[0] != "fam/c1" {
		t.Fatalf("deleter calls = %v, want [fam/c1]", del.calls)
	}
	if fc.calls["fam"] != before+1 {
		t.Errorf("expected one reload, calls went %d -> %d", before, fc.calls["fam"])
	}
}

func TestDelete_ErrorShownInline(t *testing.T) {
	fc := famContacts(contact("c1", "Alice", "parent"))
	del := &fakeDeleter{err: errors.New("api down")}
	h := newHarnessWith(t, twoSpaces(), fc, del, "u1")
	openContacts(t, h)

	h.Press("delete")
	h.Press("enter")
	confirm, ok := h.Model().Content().(*confirmDeleteScreen)
	if !ok {
		t.Fatalf("on error we must stay on confirm, got %T", h.Model().Content())
	}
	if confirm.err == nil {
		t.Error("confirm screen should record the delete error")
	}
	h.RequireContains("api down")
}

func TestDelete_CancelPops(t *testing.T) {
	fc := famContacts(contact("c1", "Alice", "parent"))
	h := newHarnessWith(t, twoSpaces(), fc, &fakeDeleter{}, "u1")
	openContacts(t, h)

	h.Press("delete")
	h.Press("esc")
	if _, ok := h.Model().Content().(*contactsScreen); !ok {
		t.Fatalf("esc on confirm should pop to contacts, got %T", h.Model().Content())
	}
}

func TestDelete_FromCard(t *testing.T) {
	fc := famContacts(contact("c1", "Alice", "parent"))
	del := &fakeDeleter{}
	h := newHarnessWith(t, twoSpaces(), fc, del, "u1")
	openContacts(t, h)
	h.Press("enter")
	if _, ok := h.Model().Content().(*contactCardScreen); !ok {
		t.Fatalf("top is %T, want *contactCardScreen", h.Model().Content())
	}
	h.Press("backspace")
	if _, ok := h.Model().Content().(*confirmDeleteScreen); !ok {
		t.Fatalf("backspace on card should open confirm, got %T", h.Model().Content())
	}
}

func TestDelete_NilDeleterIsNoop(t *testing.T) {
	fc := famContacts(contact("c1", "Alice", "parent"))
	h := newHarnessWith(t, twoSpaces(), fc, nil, "u1")
	openContacts(t, h)
	h.Press("delete")
	if _, ok := h.Model().Content().(*confirmDeleteScreen); ok {
		t.Fatal("with no deleter, delete must be a no-op")
	}
}

func TestDelete_FromCard_SucceedsAndUnwindsPastCard(t *testing.T) {
	fc := famContacts(contact("c1", "Alice", "parent"))
	del := &fakeDeleter{}
	h := newHarnessWith(t, twoSpaces(), fc, del, "u1")
	openContacts(t, h)
	h.Press("enter") // card
	h.Press("delete")
	h.Press("enter") // confirm
	if _, ok := h.Model().Content().(*contactsScreen); !ok {
		t.Fatalf("after delete from card top is %T, want *contactsScreen", h.Model().Content())
	}
	if len(del.calls) != 1 || del.calls[0] != "fam/c1" {
		t.Fatalf("deleter calls = %v, want [fam/c1]", del.calls)
	}
}

func TestNew_PublicConstructor(t *testing.T) {
	m := New(fakeSpaces{spaces: twoSpaces()}, &fakeContacts{}, nil, "uid")
	if m.Depth() != 1 {
		t.Fatalf("Depth = %d, want 1", m.Depth())
	}
	if _, ok := m.Content().(*spacesScreen); !ok {
		t.Fatalf("Content = %T, want *spacesScreen", m.Content())
	}
}
