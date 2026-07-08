package commands

import "github.com/spf13/cobra"

// Whoami prints the currently signed-in user as JSON.
func Whoami(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Print the currently signed-in user",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sess, err := env.Store.Load()
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), map[string]string{
				"uid": sess.UID, "email": sess.Email, "project": sess.Project,
			})
		},
	}
}
