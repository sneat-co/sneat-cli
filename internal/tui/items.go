package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/sneat-co/sneat-cli/internal/firestoredb"
)

// spaceItem is a row in the Spaces list.
type spaceItem struct {
	id, title, spaceType, status string
	roles                        []string
}

func (i spaceItem) name() string {
	if i.title != "" {
		return i.title
	}
	return i.id
}
func (i spaceItem) Title() string { return i.name() }
func (i spaceItem) Description() string {
	parts := make([]string, 0, 2)
	if i.spaceType != "" {
		parts = append(parts, i.spaceType)
	}
	if i.status != "" {
		parts = append(parts, i.status)
	}
	return strings.Join(parts, " · ")
}
func (i spaceItem) FilterValue() string { return i.title + " " + i.id }

// menuItem is a row in a Space's menu (Members / Contacts).
type menuItem struct {
	label, desc string
	membersOnly bool
}

func (i menuItem) Title() string       { return i.label }
func (i menuItem) Description() string { return i.desc }
func (i menuItem) FilterValue() string { return i.label }

// contactItem is a row in a Contacts list and the source for a contact card.
type contactItem struct {
	id, title, ctype, gender, status, ageGroup string
	roles                                      []string
	emails, phones                             []string
}

func (i contactItem) Title() string { return i.title }
func (i contactItem) Description() string {
	parts := []string{i.ctype}
	if i.gender != "" {
		parts = append(parts, i.gender)
	}
	if len(i.roles) > 0 {
		parts = append(parts, strings.Join(i.roles, ","))
	}
	return strings.Join(parts, " · ")
}
func (i contactItem) FilterValue() string { return i.title }

// spaceItemsFrom builds sorted Spaces list items from a user's spaces map.
func spaceItemsFrom(spaces map[string]any) []list.Item {
	items := make([]list.Item, 0, len(spaces))
	for _, id := range sortedKeys(spaces) {
		b, _ := spaces[id].(map[string]any)
		items = append(items, spaceItem{
			id:        id,
			title:     str(b["title"]),
			spaceType: str(b["type"]),
			status:    str(b["status"]),
			roles:     strList(b["roles"]),
		})
	}
	return items
}

// contactItemsFrom builds Contacts list items. When membersOnly is set it keeps
// only contacts holding the member role and strips the member role from each
// row's displayed roles; otherwise every contact and role is shown.
func contactItemsFrom(contacts []firestoredb.Contact, membersOnly bool) []list.Item {
	items := make([]list.Item, 0, len(contacts))
	for _, c := range contacts {
		d := c.Contact
		if d == nil {
			continue
		}
		roles := d.Roles
		if membersOnly {
			if !hasMemberRole(roles) {
				continue
			}
			roles = withoutMemberRole(roles)
		}
		items = append(items, contactItem{
			id:       c.ID,
			title:    contactTitle(d),
			ctype:    string(d.Type),
			gender:   string(d.Gender),
			status:   string(d.Status),
			ageGroup: d.AgeGroup,
			roles:    roles,
			emails:   commChannelKeys(d.Emails),
			phones:   commChannelKeys(d.Phones),
		})
	}
	return items
}

// commChannelKeys returns the sorted keys (addresses/numbers) of a comm-channel map.
func commChannelKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// newList builds a bubbles list with our default delegate and title.
func newList(title string, items []list.Item) list.Model {
	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = title
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	return l
}
