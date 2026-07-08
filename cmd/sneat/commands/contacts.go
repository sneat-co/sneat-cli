package commands

import (
	"context"
	"strings"

	"github.com/sneat-co/sneat-cli/internal/firestoredb"
	"github.com/spf13/cobra"
)

// ContactsReader reads contacts within a space.
type ContactsReader interface {
	ListContacts(ctx context.Context, spaceID string) ([]firestoredb.Contact, error)
	GetContact(ctx context.Context, spaceID, contactID string) (firestoredb.Contact, error)
}

// Contact builds the `sneat contact` command group. Bare `sneat contact` shows
// help; the plural `sneat contacts` (see Contacts) aliases `contact list`.
func Contact(env Env) *cobra.Command {
	cmd := &cobra.Command{Use: "contact", Short: "Work with contacts in a space"}
	cmd.AddCommand(
		contactListCmd(env, "list", "List a space's contacts as JSON"),
		contactGet(env),
		contactAdd(env),
		contactDelete(env),
	)
	return cmd
}

// Contacts is the top-level `sneat contacts` — an alias for `sneat contact list`.
func Contacts(env Env) *cobra.Command {
	cmd := contactListCmd(env, "contacts", "List a space's contacts (alias for 'contact list')")
	cmd.Hidden = true
	return cmd
}

func contactListCmd(env Env, use, short string) *cobra.Command {
	var space string
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, _ []string) error {
			spaceID, err := resolveSpaceID(cmd, env, space)
			if err != nil {
				return err
			}
			reader, err := env.NewContactsReader(configFromCmd(cmd, env.Getenv))
			if err != nil {
				return err
			}
			contacts, err := reader.ListContacts(cmd.Context(), spaceID)
			if err != nil {
				return err
			}
			headers, rows := contactsTable(contacts)
			return output(cmd, contacts, headers, rows)
		},
	}
	cmd.Flags().StringVar(&space, "space", "", "space id, or 'family'/'private' (default: current space or family)")
	return cmd
}

var contactHeaders = []string{"ID", "NAME", "TYPE", "GENDER", "STATUS", "ROLES"}

func contactsTable(cs []firestoredb.Contact) (headers []string, rows [][]string) {
	rows = make([][]string, 0, len(cs))
	for _, c := range cs {
		rows = append(rows, contactRow(c))
	}
	return contactHeaders, rows
}

func contactRow(c firestoredb.Contact) []string {
	d := c.Contact
	if d == nil {
		return []string{c.ID, "", "", "", "", ""}
	}
	return []string{c.ID, d.GetTitle(), string(d.Type), string(d.Gender), string(d.Status), strings.Join(d.Roles, ",")}
}

func contactGet(env Env) *cobra.Command {
	var space, id string
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get one contact by id as JSON",
		RunE: func(cmd *cobra.Command, _ []string) error {
			spaceID, err := resolveSpaceID(cmd, env, space)
			if err != nil {
				return err
			}
			reader, err := env.NewContactsReader(configFromCmd(cmd, env.Getenv))
			if err != nil {
				return err
			}
			contact, err := reader.GetContact(cmd.Context(), spaceID, id)
			if err != nil {
				return err
			}
			return output(cmd, contact, contactHeaders, [][]string{contactRow(contact)})
		},
	}
	cmd.Flags().StringVar(&space, "space", "", "space id, or 'family'/'private' (default: current space or family)")
	cmd.Flags().StringVar(&id, "id", "", "contact id")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}
