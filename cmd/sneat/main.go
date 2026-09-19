package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"context"

	"github.com/sneat-co/sneat-cli/cmd/sneat/commands"
	"github.com/sneat-co/sneat-cli/internal/browserauth"
	"github.com/sneat-co/sneat-cli/internal/chat"
	"github.com/sneat-co/sneat-cli/internal/chattui"
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

func main() {
	// info resolves this build's version/commit/date once, from the
	// github.com/strongo/buildinfo link-time vars stamped by
	// .goreleaser.yaml's ldflags (falling back to runtime/debug.BuildInfo
	// for `go run`/`go test`/an unstamped build). Both `sneat --version`
	// and `sneat version` are wired from this single value below so they
	// can never disagree.
	info := buildinfo.Get("sneat")
	path, err := session.DefaultPath(os.UserConfigDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sneat:", err)
		os.Exit(1)
	}
	metadataPath, err := session.DefaultMetadataPath(os.UserConfigDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sneat:", err)
		os.Exit(1)
	}
	store := session.NewLazySecureStore(func() (deviceauth.Store, error) {
		return deviceauth.NewKeyringStore("sneat-cli", "https://auth.sneat.co|sneat-cli")
	}, path, metadataPath)
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
			return deviceflow.New(deviceflow.Options{
				Issuer:      issuer,
				OpenBrowser: deviceauth.OpenBrowser,
				Exchange: sneatauth.New(sneatauth.Options{
					APIKey:           cfg.APIKey,
					AuthEmulatorHost: cfg.AuthEmulatorHost,
				}),
				DeviceInfo: deviceflow.DeviceInfo(info.Version),
				Store:      selectedStore,
				Project:    cfg.Project,
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
		// RunChat is the chat session's composition root: the one place that
		// builds a concrete processor and hands the renderer only the
		// chat.Processor interface (chat-messenger#req:processor-seam).
		RunChat: func(spaces commands.SpacesReader, contacts commands.ContactsReader, uid, email string) error {
			return chattui.Run(chat.NewProcessor(chat.Deps{
				Spaces:   spaces,
				Contacts: chatContacts{contacts},
				UID:      uid,
				Email:    email,
				Version:  info.Version,
			}))
		},
	}
	root := commands.Root(env)
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
		fmt.Fprintln(os.Stderr, "sneat:", err)
		code := 1
		var exitErr *commands.ExitCodeError
		if errors.As(err, &exitErr) {
			code = exitErr.Code
		}
		os.Exit(code)
	}
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
