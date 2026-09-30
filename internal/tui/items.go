package tui

import (
	"sort"
	"strings"

	"charm.land/bubbles/v2/list"
	"github.com/sneat-co/sneat-cli/internal/firestoredb"
	"github.com/tuigoff/tuigoff/pkg/widgets"
)

// spaceItem is the data behind a Spaces list row.
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

func (i spaceItem) detail() string {
	parts := make([]string, 0, 2)
	if i.spaceType != "" {
		parts = append(parts, i.spaceType)
	}
	if i.status != "" {
		parts = append(parts, i.status)
	}
	return strings.Join(parts, " · ")
}

// contactItem is the data behind a Contacts list row and a contact card.
type contactItem struct {
	id, title, ctype, gender, status, ageGroup string
	roles                                      []string
	emails, phones                             []string
	isSelf                                     bool // true when this contact is the signed-in user
}

func (i contactItem) detail() string {
	parts := []string{i.ctype}
	if i.gender != "" {
		parts = append(parts, i.gender)
	}
	if len(i.roles) > 0 {
		parts = append(parts, strings.Join(i.roles, ","))
	}
	return strings.Join(parts, " · ")
}

// spaceItemsFrom builds sorted Spaces menu items from a user's spaces map.
func spaceItemsFrom(spaces map[string]any) []list.Item {
	items := make([]list.Item, 0, len(spaces))
	for _, id := range sortedKeys(spaces) {
		b, _ := spaces[id].(map[string]any)
		sp := spaceItem{
			id:        id,
			title:     str(b["title"]),
			spaceType: str(b["type"]),
			status:    str(b["status"]),
			roles:     strList(b["roles"]),
		}
		items = append(items, widgets.MenuItem{
			ID:     sp.id,
			Label:  sp.name(),
			Detail: sp.detail(),
			Ref:    sp,
		})
	}
	return items
}

// contactItemsFrom builds Contacts menu items. When membersOnly is set it keeps
// only contacts holding the member role and strips the member role from each
// row's displayed roles; otherwise every contact and role is shown. A contact
// is marked isSelf when its UserID matches uid (the signed-in user).
func contactItemsFrom(contacts []firestoredb.Contact, membersOnly bool, uid string) []list.Item {
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
		ci := contactItem{
			id:       c.ID,
			title:    contactTitle(d),
			ctype:    string(d.Type),
			gender:   string(d.Gender),
			status:   string(d.Status),
			ageGroup: d.AgeGroup,
			roles:    roles,
			emails:   commChannelKeys(d.Emails),
			phones:   commChannelKeys(d.Phones),
			isSelf:   uid != "" && d.GetUserID() == uid,
		}
		items = append(items, widgets.MenuItem{
			ID:     ci.id,
			Label:  ci.title,
			Detail: ci.detail(),
			Ref:    ci,
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

// newFilterList builds a widgets.List with filtering enabled.
func newFilterList(id string, items ...list.Item) widgets.List {
	l := widgets.NewList(id, items...)
	l.SetFilteringEnabled(true)
	return l
}

// menuItemRef returns the Ref of a MenuItem, or nil.
func menuItemRef(item list.Item) any {
	if mi, ok := item.(widgets.MenuItem); ok {
		return mi.Ref
	}
	return nil
}
