package commands

import (
	"fmt"
	"time"

	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/deviceflow"
	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/spf13/cobra"
)

// Auth builds the `sneat auth` command group.
func Auth(env Env) *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Sign in and out of Sneat.app"}
	var insecureStorage bool
	cmd.PersistentFlags().BoolVar(&insecureStorage, "insecure-storage", false, "store the Firebase session in a plaintext 0600 file for headless environments")
	cmd.AddCommand(authLogin(env, &insecureStorage), authStatus(env, &insecureStorage), authLogout(env, &insecureStorage))
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

func authLogin(env Env, insecureStorage *bool) *cobra.Command {
	var email, password string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in through auth.sneat.co (or use email+password)",
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
				if env.NewDeviceFlow == nil {
					return fmt.Errorf("device login is not configured")
				}
				issuer, err := cmd.Flags().GetString("auth-host")
				if err != nil {
					return err
				}
				if issuer == "" {
					issuer = deviceflow.DefaultIssuer
				}
				flow, err := env.NewDeviceFlow(cfg, issuer)
				if err != nil {
					return err
				}
				res, err := flow.Run(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr())
				if err != nil {
					return err
				}
				tok = authTokens{res.IDToken, res.RefreshToken, res.UID, res.Email, res.ExpiresIn}
			}
			store, err := authStore(env, *insecureStorage)
			if err != nil {
				return err
			}
			if *insecureStorage {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Warning: --insecure-storage writes the Firebase session unencrypted to the local session file.")
			}
			return saveAndPrint(cmd, store, env, cfg, tok)
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "account email (enables headless password sign-in)")
	cmd.Flags().StringVar(&password, "password", "", "account password (with --email)")
	return cmd
}

func authStatus(env Env, insecureStorage *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the current Sneat CLI login",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := authStore(env, *insecureStorage)
			if err != nil {
				return err
			}
			sess, err := store.Load()
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), map[string]string{
				"uid": sess.UID, "email": sess.Email, "project": sess.Project,
				"expiresAt": sess.ExpiresAt.UTC().Format(time.RFC3339),
			})
		},
	}
}

func saveAndPrint(cmd *cobra.Command, store SessionStore, env Env, cfg config.Config, tok authTokens) error {
	sess := session.Session{
		Project: cfg.Project, UID: tok.UID, Email: tok.Email,
		IDToken: tok.IDToken, RefreshToken: tok.RefreshToken,
		ExpiresAt: env.Now().Add(tok.ExpiresIn),
	}
	if err := store.Save(sess); err != nil {
		return err
	}
	return writeJSON(cmd.OutOrStdout(), map[string]string{
		"uid": tok.UID, "email": tok.Email, "project": cfg.Project,
	})
}

func authLogout(env Env, insecureStorage *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored Sneat CLI session",
		RunE: func(_ *cobra.Command, _ []string) error {
			store, err := authStore(env, *insecureStorage)
			if err != nil {
				return err
			}
			return store.Clear()
		},
	}
}

func authStore(env Env, insecure bool) (SessionStore, error) {
	if !insecure {
		return env.Store, nil
	}
	if env.NewInsecureStore == nil {
		return nil, fmt.Errorf("insecure session storage is not configured")
	}
	return env.NewInsecureStore(), nil
}
