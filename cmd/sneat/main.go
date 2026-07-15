package main

import (
	"fmt"
	"os"
	"time"

	"context"

	"github.com/sneat-co/sneat-cli/cmd/sneat/commands"
	"github.com/sneat-co/sneat-cli/internal/browserauth"
	"github.com/sneat-co/sneat-cli/internal/chat"
	"github.com/sneat-co/sneat-cli/internal/chattui"
	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/firestoredb"
	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/sneat-co/sneat-cli/internal/sneatapi"
	"github.com/sneat-co/sneat-cli/internal/sneatauth"
	"github.com/sneat-co/sneat-cli/internal/tokensrc"
	"github.com/sneat-co/sneat-cli/internal/tui"
	"golang.org/x/term"
)

// Build metadata, overridable via -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	path, err := session.DefaultPath(os.UserConfigDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sneat:", err)
		os.Exit(1)
	}
	store := session.NewStore(path)
	env := commands.Env{
		Getenv: os.Getenv,
		Now:    time.Now,
		Store:  store,
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
		NewSpacesReader: func(cfg config.Config) (commands.SpacesReader, error) {
			auth := sneatauth.New(sneatauth.Options{APIKey: cfg.APIKey, AuthEmulatorHost: cfg.AuthEmulatorHost})
			ts := tokensrc.New(context.Background(), store, auth, time.Now)
			return firestoredb.NewSpacesReader(cfg, ts), nil
		},
		NewContactsReader: func(cfg config.Config) (commands.ContactsReader, error) {
			auth := sneatauth.New(sneatauth.Options{APIKey: cfg.APIKey, AuthEmulatorHost: cfg.AuthEmulatorHost})
			ts := tokensrc.New(context.Background(), store, auth, time.Now)
			return firestoredb.NewContactsReader(cfg, ts), nil
		},
		NewContactWriter: func(cfg config.Config) (commands.ContactWriter, error) {
			auth := sneatauth.New(sneatauth.Options{APIKey: cfg.APIKey, AuthEmulatorHost: cfg.AuthEmulatorHost})
			ts := tokensrc.New(context.Background(), store, auth, time.Now)
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
		RunChat: func(spaces commands.SpacesReader, uid string) error {
			return chattui.Run(chat.NewProcessor(spaces, uid))
		},
	}
	root := commands.Root(env)
	root.AddCommand(
		commands.Version(version, commit, date),
		commands.Auth(env),
		commands.Whoami(env),
		commands.Space(env),
		commands.Spaces(env),
		commands.Ui(env),
		commands.Chat(env),
		commands.Contact(env),
		commands.Contacts(env),
		commands.Convo(env),
	)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "sneat:", err)
		os.Exit(1)
	}
}
