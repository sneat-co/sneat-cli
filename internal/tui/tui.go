// Package tui implements the interactive terminal UI for browsing spaces,
// their members and contacts, and a contact card. It is launched by
// `sneat ui` or `sneat spaces --ui`. Screens run inside the shared
// strongo-tui navigation shell (pkg/nav): push/pop, alerts and the actions
// bar come from the shell; list widgets come from pkg/widgets.
package tui

import (
	"context"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/sneat-co/contactus/backend/dbo4contactus"
	"github.com/sneat-co/sneat-cli/internal/firestoredb"
	"github.com/strongo/strongo-tui/pkg/nav"
)

// SpacesReader lists the signed-in user's spaces.
type SpacesReader interface {
	ListSpaces(ctx context.Context, uid string) (map[string]any, error)
}

// ContactsReader lists a space's contacts.
type ContactsReader interface {
	ListContacts(ctx context.Context, spaceID string) ([]firestoredb.Contact, error)
}

// ContactDeleter deletes a contact by space and id (via the sneat-go API).
type ContactDeleter interface {
	DeleteContact(ctx context.Context, spaceID, contactID string) error
}

// Model is the root bubbletea model: the shared navigation shell.
type Model = nav.Model

// app holds the shared readers, signed-in uid and the contacts cache that
// screens share. Screens keep a pointer to it; the shell owns navigation.
type app struct {
	spaces   SpacesReader
	contacts ContactsReader
	deleter  ContactDeleter
	uid      string
	cache    map[string][]firestoredb.Contact
}

// New builds the root model starting on the Spaces screen.
func New(spaces SpacesReader, contacts ContactsReader, deleter ContactDeleter, uid string) Model {
	a := &app{
		spaces:   spaces,
		contacts: contacts,
		deleter:  deleter,
		uid:      uid,
		cache:    map[string][]firestoredb.Contact{},
	}
	return nav.New(
		nav.Page{Title: "Spaces", Content: newSpacesScreen(a)},
		nav.WithoutLogin(),
	)
}

// data-load messages.
type spacesLoadedMsg struct{ spaces map[string]any }
type contactsLoadedMsg struct {
	spaceID  string
	contacts []firestoredb.Contact
}
type errMsg struct{ err error }

// delete-flow messages.
type contactDeletedMsg struct{ spaceID, contactID string }
type deleteErrMsg struct{ err error }

// deleteContact issues a delete and reports success or failure.
func deleteContact(d ContactDeleter, spaceID, contactID string) tea.Cmd {
	return func() tea.Msg {
		if err := d.DeleteContact(context.Background(), spaceID, contactID); err != nil {
			return deleteErrMsg{err}
		}
		return contactDeletedMsg{spaceID: spaceID, contactID: contactID}
	}
}

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
