package commands

import (
	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/spf13/cobra"
)

// Auth builds the `sneat auth` command group.
func Auth(env Env) *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Sign in and out of Sneat.app"}
	cmd.AddCommand(authLogin(env), authLogout(env))
	return cmd
}

func authLogin(env Env) *cobra.Command {
	var email, password string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in (email+password; browser flow added later)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := configFromCmd(cmd, env.Getenv)
			ac := env.NewAuthClient(cfg)
			res, err := ac.SignInWithPassword(cmd.Context(), email, password)
			if err != nil {
				return err
			}
			sess := session.Session{
				Project: cfg.Project, UID: res.UID, Email: res.Email,
				IDToken: res.IDToken, RefreshToken: res.RefreshToken,
				ExpiresAt: env.Now().Add(res.ExpiresIn),
			}
			if err := env.Store.Save(sess); err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), map[string]string{
				"uid": res.UID, "email": res.Email, "project": cfg.Project,
			})
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "account email")
	cmd.Flags().StringVar(&password, "password", "", "account password")
	_ = cmd.MarkFlagRequired("email")
	_ = cmd.MarkFlagRequired("password")
	return cmd
}

func authLogout(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Clear the stored session",
		RunE: func(_ *cobra.Command, _ []string) error {
			return env.Store.Clear()
		},
	}
}
