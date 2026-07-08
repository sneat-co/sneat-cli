package commands

import (
	"time"

	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/spf13/cobra"
)

// Auth builds the `sneat auth` command group.
func Auth(env Env) *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Sign in and out of Sneat.app"}
	cmd.AddCommand(authLogin(env), authLogout(env))
	return cmd
}

// authTokens is the provider-agnostic result of a sign-in.
type authTokens struct {
	IDToken      string
	RefreshToken string
	UID          string
	Email        string
	ExpiresIn    time.Duration
}

func authLogin(env Env) *cobra.Command {
	var email, password string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in via browser (default) or email+password (--email/--password)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := configFromCmd(cmd, env.Getenv)
			var tok authTokens
			if email != "" {
				res, err := env.NewAuthClient(cfg).SignInWithPassword(cmd.Context(), email, password)
				if err != nil {
					return err
				}
				tok = authTokens{res.IDToken, res.RefreshToken, res.UID, res.Email, res.ExpiresIn}
			} else {
				res, err := env.NewBrowserFlow(cfg).Run(cmd.Context())
				if err != nil {
					return err
				}
				tok = authTokens{res.IDToken, res.RefreshToken, res.UID, res.Email, res.ExpiresIn}
			}
			return saveAndPrint(cmd, env, cfg, tok)
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "account email (enables headless password sign-in)")
	cmd.Flags().StringVar(&password, "password", "", "account password (with --email)")
	return cmd
}

func saveAndPrint(cmd *cobra.Command, env Env, cfg config.Config, tok authTokens) error {
	sess := session.Session{
		Project: cfg.Project, UID: tok.UID, Email: tok.Email,
		IDToken: tok.IDToken, RefreshToken: tok.RefreshToken,
		ExpiresAt: env.Now().Add(tok.ExpiresIn),
	}
	if err := env.Store.Save(sess); err != nil {
		return err
	}
	return writeJSON(cmd.OutOrStdout(), map[string]string{
		"uid": tok.UID, "email": tok.Email, "project": cfg.Project,
	})
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
