package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"context"

	"github.com/sneat-co/sneat-cli/cmd/sneat/commands"
	"github.com/sneat-co/sneat-cli/internal/browserauth"
	"github.com/sneat-co/sneat-cli/internal/chat"
	"github.com/sneat-co/sneat-cli/internal/chatapp"
	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/deviceflow"
	"github.com/sneat-co/sneat-cli/internal/firestoredb"
	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/sneat-co/sneat-cli/internal/sneatapi"
	"github.com/sneat-co/sneat-cli/internal/sneatauth"
	"github.com/sneat-co/sneat-cli/internal/tokensrc"
	"github.com/sneat-co/sneat-cli/internal/tui"
	"github.com/strongo/buildinfo"
	"github.com/strongo/buildinfo/cobracmd"
	"github.com/strongo/deviceauth"
	"golang.org/x/term"
)

// userConfigDir is a seam over os.UserConfigDir so tests can force the rare
// "no config dir available" error path (newEnv's two DefaultPath/
// DefaultMetadataPath calls) without touching the real HOME/config dir.
// Production default is the real os.UserConfigDir.
var userConfigDir = os.UserConfigDir

// osExit is a seam over os.Exit so main itself can be driven directly in a
// test: swapped for a code-capturing stub that doesn't terminate the test
// binary, restored with t.Cleanup. Production default is the real os.Exit.
var osExit = os.Exit

// newKeyringStore is SecureStore's lazy credential-store factory, split out
// to a named seam so it can be invoked directly in a test: deviceauth.
// NewKeyringStore only constructs a *KeyringStore handle (service/account
// name validation), it never touches the real OS keychain until Save/Load
// is called on the result, so calling it here is safe and hermetic.
// Production default/behaviour is unchanged.
var newKeyringStore = func() (deviceauth.Store, error) {
	return deviceauth.NewKeyringStore("sneat-cli", "https://auth.sneat.co|sneat-cli")
}

func main() {
	osExit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main's testable body: it builds the composition-root Env, wires the
// cobra command tree, executes it against args, and returns the process exit
// code (0 on success). main itself stays a one-line os.Exit(run(...)) seam so
// main is covered by driving run directly in tests.
func run(args []string, stdout, stderr io.Writer) int {
	// info resolves this build's version/commit/date once, from the
	// github.com/strongo/buildinfo link-time vars stamped by
	// .goreleaser.yaml's ldflags (falling back to runtime/debug.BuildInfo
	// for `go run`/`go test`/an unstamped build). Both `sneat --version`
	// and `sneat version` are wired from this single value below so they
	// can never disagree.
	info := buildinfo.Get("sneat")
	env, err := newEnv(info)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "sneat:", err)
		return 1
	}
	root := commands.Root(env)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	// WireCobra registers the `version` subcommand and wires cobra's own
	// --version/-v flag from the same Info, so `sneat --version` and
	// `sneat version` can never disagree
	// (github.com/strongo/buildinfo/cobracmd.WireCobra).
	cobracmd.WireCobra(root, info)
	root.AddCommand(
		commands.Auth(env),
		commands.Whoami(env),
		commands.Space(env),
		commands.Spaces(env),
		commands.Ui(env),
		commands.Chat(env),
		commands.Contact(env),
		commands.Contacts(env),
		commands.Convo(env),
		commands.Action(env),
		commands.Context(env),
		commands.Query(env),
	)
	if err := root.Execute(); err != nil {
		_, _ = fmt.Fprintln(stderr, "sneat:", err)
		return exitCodeFor(err)
	}
	return 0
}

// exitCodeFor is run's error-to-exit-code mapping, split out so the
// *commands.ExitCodeError branch (an error that carries its own process exit
// code, used by the Action Protocol) can be unit-tested directly against a
// synthetic error instead of needing a real command failure to construct one.
func exitCodeFor(err error) int {
	code := 1
	var exitErr *commands.ExitCodeError
	if errors.As(err, &exitErr) {
		code = exitErr.Code
	}
	return code
}

// newEnv builds the composition-root commands.Env: the real, process-wide
// dependencies (secure session store, Firebase auth, Firestore readers, the
// sneat-go API client, the interactive TUI/chat runners). It is split out of
// run so its own construction errors (an unavailable user config dir) can be
// tested directly, and so each closure below can be invoked directly in
// tests without going through cobra at all.
func newEnv(info buildinfo.Info) (commands.Env, error) {
	path, err := session.DefaultPath(userConfigDir)
	if err != nil {
		return commands.Env{}, err
	}
	metadataPath, err := session.DefaultMetadataPath(userConfigDir)
	if err != nil {
		return commands.Env{}, err
	}
	store := session.NewLazySecureStore(newKeyringStore, path, metadataPath)
	env := commands.Env{
		Getenv: os.Getenv,
		Now:    time.Now,
		Store:  store,
		NewInsecureStore: func() commands.SessionStore {
			return session.NewInsecureStore(path, metadataPath)
		},
		NewAuthClient: func(cfg config.Config) commands.AuthClient {
			return sneatauth.New(sneatauth.Options{APIKey: cfg.APIKey, AuthEmulatorHost: cfg.AuthEmulatorHost})
		},
		NewBrowserFlow: func(cfg config.Config) commands.BrowserFlow {
			return browserauth.Flow{
				APIKey:           cfg.APIKey,
				AuthDomain:       cfg.AuthDomain,
				Project:          cfg.Project,
				AuthEmulatorHost: cfg.AuthEmulatorHost,
				OpenBrowser:      browserauth.OpenBrowser,
			}
		},
		NewDeviceFlow: func(cfg config.Config, issuer string, selectedStore commands.SessionStore) (commands.DeviceFlow, error) {
			firebaseAuth := sneatauth.New(sneatauth.Options{
				APIKey:           cfg.APIKey,
				AuthEmulatorHost: cfg.AuthEmulatorHost,
			})
			return deviceflow.New(deviceflow.Options{
				Issuer:      issuer,
				OpenBrowser: deviceauth.OpenBrowser,
				Exchange:    firebaseAuth,
				Refresh:     firebaseAuth,
				DeviceInfo:  deviceflow.DeviceInfo(info.Version),
				Store:       selectedStore,
				Project:     cfg.Project,
			})
		},
		NewSpacesReader: func(cfg config.Config) (commands.SpacesReader, error) {
			auth := sneatauth.New(sneatauth.Options{APIKey: cfg.APIKey, AuthEmulatorHost: cfg.AuthEmulatorHost})
			ts := tokensrc.FromEnvOrSession(os.Getenv, context.Background(), store, auth, time.Now)
			return firestoredb.NewSpacesReader(cfg, ts), nil
		},
		NewContactsReader: func(cfg config.Config) (commands.ContactsReader, error) {
			auth := sneatauth.New(sneatauth.Options{APIKey: cfg.APIKey, AuthEmulatorHost: cfg.AuthEmulatorHost})
			ts := tokensrc.FromEnvOrSession(os.Getenv, context.Background(), store, auth, time.Now)
			return firestoredb.NewContactsReader(cfg, ts), nil
		},
		NewContactWriter: func(cfg config.Config) (commands.ContactWriter, error) {
			auth := sneatauth.New(sneatauth.Options{APIKey: cfg.APIKey, AuthEmulatorHost: cfg.AuthEmulatorHost})
			ts := tokensrc.FromEnvOrSession(os.Getenv, context.Background(), store, auth, time.Now)
			return sneatapi.New(cfg.APIBaseURL, ts, nil), nil
		},
		NewActionsAPI: func(cfg config.Config) (commands.ActionsAPI, error) {
			auth := sneatauth.New(sneatauth.Options{APIKey: cfg.APIKey, AuthEmulatorHost: cfg.AuthEmulatorHost})
			ts := tokensrc.FromEnvOrSession(os.Getenv, context.Background(), store, auth, time.Now)
			return sneatapi.New(cfg.APIBaseURL, ts, nil), nil
		},
		IsTerminal:     func() bool { return term.IsTerminal(int(os.Stdin.Fd())) },
		RunContactForm: commands.RunContactForm,
		RunTUI: func(spaces commands.SpacesReader, contacts commands.ContactsReader, deleter commands.ContactDeleter, uid string) error {
			return tui.Run(spaces, contacts, deleter, uid)
		},
		// RunChat is the chat session's composition root: internal/chatapp
		// builds the aichat MVP pipeline (deterministic rules + decision
		// chain + resolver + Context Manager + main LLM) and runs it through
		// strongo/aichat's tui/chatshell.
		RunChat: func(args commands.RunChatArgs) error {
			auth := sneatauth.New(sneatauth.Options{APIKey: args.Cfg.APIKey, AuthEmulatorHost: args.Cfg.AuthEmulatorHost})
			ts := tokensrc.FromEnvOrSession(os.Getenv, context.Background(), store, auth, time.Now)
			return chatapp.Run(chatapp.Deps{
				Spaces:       args.Spaces,
				Contacts:     chatContacts{args.Contacts},
				UID:          args.UID,
				Email:        args.Email,
				Version:      info.Version,
				Cfg:          args.Cfg,
				AIConfig:     args.AIConfig,
				NoJev:        args.NoJev,
				TokenSource:  ts,
				Debug:        os.Getenv("SNEAT_DEBUG") != "",
				CurrentSpace: args.CurrentSpace,
				TZ:           args.TZ,
			})
		},
	}
	return env, nil
}

// chatContacts adapts the CLI's Firestore-backed contacts reader to the lean
// interface internal/chat consumes. It exists at the composition root, the one
// place that knows both the concrete reader and the chat seam, so the chat
// package stays a leaf that names no Firestore type.
type chatContacts struct{ r commands.ContactsReader }

func (c chatContacts) ListContacts(ctx context.Context, spaceID string) ([]chat.Contact, error) {
	cs, err := c.r.ListContacts(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	out := make([]chat.Contact, 0, len(cs))
	for _, x := range cs {
		out = append(out, chat.Contact{Name: contactDisplayName(x)})
	}
	return out, nil
}

// contactDisplayName reads a contact's display name the way the browsing UI
// does: an explicit title, else the full name, else empty for the chat package
// to render as "(unnamed)".
func contactDisplayName(c firestoredb.Contact) string {
	d := c.Contact
	if d == nil {
		return ""
	}
	if d.Title != "" {
		return d.Title
	}
	if d.Names != nil {
		if n := d.Names.GetFullName(); n != "" {
			return n
		}
	}
	return ""
}
