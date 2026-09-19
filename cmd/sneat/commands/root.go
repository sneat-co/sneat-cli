package commands

import (
	"context"
	"io"
	"time"

	"github.com/sneat-co/sneat-ai-backend/actionspec"
	"github.com/sneat-co/sneat-ai-backend/dto4sneatai"
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

// DeviceFlow runs the shared browser-approved device authorization and returns
// a normal Firebase session for the existing CLI clients.
type DeviceFlow interface {
	Run(ctx context.Context, output, errorOutput io.Writer) (sneatauth.Result, error)
}

// Env holds injected process dependencies so commands stay unit-testable.
type Env struct {
	Getenv            func(string) string
	Now               func() time.Time
	Store             SessionStore
	NewInsecureStore  func() SessionStore
	NewAuthClient     func(cfg config.Config) AuthClient
	NewBrowserFlow    func(cfg config.Config) BrowserFlow
	NewDeviceFlow     func(cfg config.Config, issuer string) (DeviceFlow, error)
	NewSpacesReader   func(cfg config.Config) (SpacesReader, error)
	NewContactsReader func(cfg config.Config) (ContactsReader, error)
	NewContactWriter  func(cfg config.Config) (ContactWriter, error)
	// NewActionsAPI builds the client for the `sneat action`/`context`/`query`
	// command families (Action Protocol, sneat-ai-backend's `/v0/sneatai/*`).
	NewActionsAPI func(cfg config.Config) (ActionsAPI, error)
	// IsTerminal reports whether interactive prompts are possible (stdin is a TTY).
	IsTerminal func() bool
	// RunContactForm collects contact fields interactively.
	RunContactForm func(*contactInput) error
	// RunTUI launches the interactive terminal UI.
	RunTUI func(spaces SpacesReader, contacts ContactsReader, deleter ContactDeleter, uid string) error
	// RunChat launches the interactive chat session. It is the composition root:
	// it builds the concrete chat processor from the reader and uid.
	RunChat func(spaces SpacesReader, contacts ContactsReader, uid, email string) error
}

// ContactDeleter deletes a contact by space and id. It is the id-based view of
// the contact write path that the interactive UI needs.
type ContactDeleter interface {
	DeleteContact(ctx context.Context, spaceID, contactID string) error
}

// ActionsAPI is the Action Protocol HTTP surface the `sneat action`,
// `sneat context` and `sneat query` command families call. It is the
// commands package's view of internal/sneatapi.Client (a subset interface so
// commands stay unit-testable against a fake).
type ActionsAPI interface {
	ActionCreate(ctx context.Context, req dto4sneatai.CreateActionRequest) (dto4sneatai.ActionResponse, error)
	ActionPatch(ctx context.Context, req dto4sneatai.PatchActionRequest) (dto4sneatai.ActionResponse, error)
	ActionGet(ctx context.Context, spaceID, actionID string) (dto4sneatai.ActionResponse, error)
	ActionValidate(ctx context.Context, req dto4sneatai.ValidateActionRequest) (dto4sneatai.ActionResponse, error)
	ActionCommit(ctx context.Context, req dto4sneatai.CommitActionRequest) (dto4sneatai.CommitActionResponse, error)
	ActionCancel(ctx context.Context, req dto4sneatai.ActionRequest) error
	Context(ctx context.Context, spaceID string) (dto4sneatai.ContextResponse, error)
	Query(ctx context.Context, req dto4sneatai.QueryRequest) (dto4sneatai.QueryResponse, error)
	Schema(ctx context.Context) (actionspec.Schema, error)
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
	cmd.PersistentFlags().String("auth-host", "", "shared authorization server URL (default https://auth.sneat.co; HTTP loopback only for development)")
	cmd.PersistentFlags().String("api-base-url", "", "sneat-go API base URL")
	cmd.PersistentFlags().String("auth-emulator", "", "Firebase Auth emulator host, e.g. localhost:9099")
	cmd.PersistentFlags().String("firestore-emulator", "", "Firestore emulator host, e.g. localhost:8080")
	cmd.PersistentFlags().Bool("emulator", false, "use local Auth+Firestore emulators on default ports")
	addFormatFlags(cmd)
	return cmd
}
