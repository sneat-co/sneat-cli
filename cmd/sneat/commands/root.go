package commands

import (
	"context"
	"time"

	"github.com/sneat-co/sneat-cli/internal/browserauth"
	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/sneat-co/sneat-cli/internal/sneatauth"
	"github.com/spf13/cobra"
)

// SessionStore persists the authenticated session.
type SessionStore interface {
	Save(session.Session) error
	Load() (session.Session, error)
	Clear() error
}

// AuthClient performs Firebase auth REST calls.
type AuthClient interface {
	SignInWithPassword(ctx context.Context, email, password string) (sneatauth.Result, error)
	Refresh(ctx context.Context, refreshToken string) (sneatauth.Result, error)
}

// BrowserFlow runs one interactive browser sign-in.
type BrowserFlow interface {
	Run(ctx context.Context) (browserauth.Result, error)
}

// Env holds injected process dependencies so commands stay unit-testable.
type Env struct {
	Getenv          func(string) string
	Now             func() time.Time
	Store           SessionStore
	NewAuthClient   func(cfg config.Config) AuthClient
	NewBrowserFlow  func(cfg config.Config) BrowserFlow
	NewSpacesReader func(cfg config.Config) (SpacesReader, error)
}

// Root builds the top-level `sneat` command.
func Root(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "sneat",
		Short:         "Sneat.app command-line interface",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.PersistentFlags().String("project", "", "Firebase project id (default sneat-eur3-1)")
	cmd.PersistentFlags().String("api-key", "", "Firebase web API key")
	cmd.PersistentFlags().String("auth-domain", "", "Firebase auth domain for browser sign-in (default sneat.app)")
	cmd.PersistentFlags().String("api-base-url", "", "sneat-go API base URL")
	cmd.PersistentFlags().String("auth-emulator", "", "Firebase Auth emulator host, e.g. localhost:9099")
	cmd.PersistentFlags().String("firestore-emulator", "", "Firestore emulator host, e.g. localhost:8080")
	cmd.PersistentFlags().Bool("emulator", false, "use local Auth+Firestore emulators on default ports")
	return cmd
}
