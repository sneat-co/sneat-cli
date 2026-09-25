package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Whoami prints the email of the currently signed-in user (no UID or Firebase
// project ID).
func Whoami(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Print the currently signed-in user",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sess, err := env.Store.Load()
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), sess.Email)
			return err
		},
	}
}
