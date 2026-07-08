package commands

import (
	"github.com/sneat-co/contactus/backend/dto4contactus"
	"github.com/sneat-co/sneat-core-modules/spaceus/dto4spaceus"
	"github.com/sneat-co/sneat-go-core/coretypes"
	"github.com/spf13/cobra"
)

func contactDelete(env Env) *cobra.Command {
	var space, id string
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a contact by id",
		RunE: func(cmd *cobra.Command, _ []string) error {
			spaceID, err := resolveSpaceID(cmd, env, space)
			if err != nil {
				return err
			}
			writer, err := env.NewContactWriter(configFromCmd(cmd, env.Getenv))
			if err != nil {
				return err
			}
			req := dto4contactus.ContactRequest{
				SpaceRequest: dto4spaceus.SpaceRequest{SpaceID: coretypes.SpaceID(spaceID)},
				ContactID:    id,
			}
			if err := writer.DeleteContact(cmd.Context(), req); err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), map[string]string{"deleted": id, "space": spaceID})
		},
	}
	cmd.Flags().StringVar(&space, "space", "", "space id, or 'family'/'private' (default: current space or family)")
	cmd.Flags().StringVar(&id, "id", "", "contact id")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}
