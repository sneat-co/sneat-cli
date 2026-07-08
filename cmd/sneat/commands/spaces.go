package commands

import (
	"context"

	"github.com/spf13/cobra"
)

// SpacesReader lists the signed-in user's spaces.
type SpacesReader interface {
	ListSpaces(ctx context.Context, uid string) (map[string]any, error)
}

// Spaces builds the `sneat spaces` command group.
func Spaces(env Env) *cobra.Command {
	cmd := &cobra.Command{Use: "spaces", Short: "Work with your spaces"}
	cmd.AddCommand(spacesList(env))
	return cmd
}

func spacesList(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List your spaces as JSON",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := configFromCmd(cmd, env.Getenv)
			sess, err := env.Store.Load()
			if err != nil {
				return err
			}
			reader, err := env.NewSpacesReader(cfg)
			if err != nil {
				return err
			}
			spaces, err := reader.ListSpaces(cmd.Context(), sess.UID)
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), spaces)
		},
	}
}
