package commands

import (
	"errors"
	"fmt"
	"io"
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
			store, err := authStore(env, *insecureStorage)
			if err != nil {
				return err
			}
			var tok authTokens
			deviceLogin := email == ""
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
				flow, err := env.NewDeviceFlow(cfg, issuer, store)
				if err != nil {
					return err
				}
				res, err := flow.Run(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr())
				if err != nil {
					return err
				}
				tok = authTokens{res.IDToken, res.RefreshToken, res.UID, res.Email, res.ExpiresIn}
				printWarnings(cmd.ErrOrStderr(), res.Warnings)
			}
			if *insecureStorage {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Warning: --insecure-storage writes the Firebase session unencrypted to the local session file.")
			}
			if deviceLogin {
				return printSignedIn(cmd, tok.Email)
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
			return printSignedIn(cmd, sess.Email)
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
	return printSignedIn(cmd, tok.Email)
}

// printSignedIn reports a signed-in session in plain words. It deliberately
// shows no JSON, UID or Firebase project ID (founder, 2026-09-25): those are
// implementation details, not something a person signing in needs to see.
func printSignedIn(cmd *cobra.Command, email string) error {
	line := "Signed in."
	if email != "" {
		line = "Signed in as " + email + "."
	}
	_, err := fmt.Fprintln(cmd.OutOrStdout(), line)
	return err
}

func authLogout(env Env, insecureStorage *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored Sneat CLI session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := authStore(env, *insecureStorage)
			if err != nil {
				return err
			}
			sess, err := store.Load()
			if errors.Is(err, session.ErrNoSession) {
				return nil
			}
			if err != nil {
				return err
			}
			// Password sessions have no device grant to revoke. Their ordinary
			// Firebase session can be removed locally; device sessions always
			// revoke remotely before Client.Logout deletes local storage.
			if sess.Issuer == "" || sess.ClientID == "" {
				return store.Clear()
			}
			flow, err := env.NewDeviceFlow(configFromCmd(cmd, env.Getenv), sess.Issuer, store)
			if err != nil {
				return err
			}
			return flow.Logout(cmd.Context())
		},
	}
}

func printWarnings(output io.Writer, warnings []error) {
	for range warnings {
		// Warning causes may contain transport detail. Keep command output useful
		// without risking credential values or response bodies on stderr.
		_, _ = fmt.Fprintln(output, "Warning: the previous device login could not be revoked; it may remain active until it expires.")
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
