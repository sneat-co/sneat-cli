package commands

import (
	"context"

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
	cmd.AddCommand(contactListCmd(env, "list", "List a space's contacts as JSON"), contactGet(env))
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
			reader, err := env.NewContactsReader(configFromCmd(cmd, env.Getenv))
			if err != nil {
				return err
			}
			contacts, err := reader.ListContacts(cmd.Context(), space)
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), contacts)
		},
	}
	cmd.Flags().StringVar(&space, "space", "", "space id")
	_ = cmd.MarkFlagRequired("space")
	return cmd
}

func contactGet(env Env) *cobra.Command {
	var space, id string
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get one contact by id as JSON",
		RunE: func(cmd *cobra.Command, _ []string) error {
			reader, err := env.NewContactsReader(configFromCmd(cmd, env.Getenv))
			if err != nil {
				return err
			}
			contact, err := reader.GetContact(cmd.Context(), space, id)
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), contact)
		},
	}
	cmd.Flags().StringVar(&space, "space", "", "space id")
	cmd.Flags().StringVar(&id, "id", "", "contact id")
	_ = cmd.MarkFlagRequired("space")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}
