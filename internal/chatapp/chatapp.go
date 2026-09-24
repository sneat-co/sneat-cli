// Package chatapp is the composition root for `sneat chat`'s interactive
// session: it builds the aichat MVP pipeline (internal/aichat) and runs it
// through strongo/aichat's tui/chatshell, replacing internal/chattui
// entirely (brief §11 full cutover).
package chatapp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/strongo/aichat/ai/aiconfig"
	"github.com/strongo/aichat/ai/ctxmgr"
	"github.com/strongo/aichat/ai/decision"
	"github.com/strongo/aichat/ai/session"
	"github.com/strongo/aichat/tui/chatshell"
	"golang.org/x/oauth2"

	"github.com/sneat-co/sneat-cli/internal/aichat/data"
	"github.com/sneat-co/sneat-cli/internal/aichat/pipeline"
	sneatrules "github.com/sneat-co/sneat-cli/internal/aichat/rules"
	"github.com/sneat-co/sneat-cli/internal/chat"
	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/sneatapi"
)

// Deps are what Run needs to build and launch one chat session.
type Deps struct {
	Spaces   chat.SpacesReader
	Contacts chat.ContactsReader
	UID      string
	Email    string
	Version  string
	Cfg      config.Config
	AIConfig aiconfig.Config
	NoJev    bool
	// TokenSource yields the signed-in user's bearer token, for Firestore
	// reads, sneatapi mutations, and (unless BYOK) the LLM/decision cloud
	// calls.
	TokenSource oauth2.TokenSource
	// Debug enables ai/diag logging at slog.LevelDebug to stderr.
	Debug bool
}

// Product identifies this CLI to Sneat AI Cloud (ai.ChatRequest.Product /
// X-AI-Product), per brief §11.
const Product = "sneat"

// Run builds the pipeline and launches the interactive chatshell. It blocks
// until the user quits.
func Run(deps Deps) error {
	ctx := context.Background()
	httpClient := http.DefaultClient

	processor := chat.NewProcessor(chat.Deps{
		Spaces: deps.Spaces, Contacts: deps.Contacts, UID: deps.UID, Email: deps.Email, Version: deps.Version,
	})

	spaceID := defaultSpaceID(ctx, deps.Spaces, deps.UID)

	readers := data.Readers{
		Happenings: data.NewFirestoreHappenings(deps.Cfg, deps.TokenSource),
		Todos:      data.NewFirestoreTodos(deps.Cfg, deps.TokenSource),
		Contacts:   data.NewFirestoreContacts(deps.Cfg, deps.TokenSource),
	}

	api := sneatapi.New(deps.Cfg.APIBaseURL, deps.TokenSource, httpClient)
	executor := pipeline.SneatExecutor{Calendar: api, Todo: api, Happenings: readers.Happenings, Now: time.Now}

	cloudToken := func(context.Context) (string, error) {
		tok, err := deps.TokenSource.Token()
		if err != nil {
			return "", err
		}
		return tok.AccessToken, nil
	}

	providers, err := aiconfig.Build(deps.AIConfig, aiconfig.Deps{
		Product: Product, CloudToken: cloudToken, CloudBaseURL: deps.Cfg.APIBaseURL,
		EnvPrefix: "SNEAT_", HTTPClient: httpClient,
		ExtraDecision:        []decision.Provider{sneatrules.New()},
		DisableCloudDecision: deps.NoJev,
	})
	if err != nil {
		return fmt.Errorf("aichat: %w", err)
	}

	var logger *slog.Logger
	if deps.Debug {
		logger = slog.Default()
	}

	pl := pipeline.Pipeline{
		Chain:    decision.Chain{Providers: providers.Decision},
		Resolver: pipeline.Resolver{Readers: readers},
		Executor: executor,
		Readers:  readers,
		Now:      time.Now,
		LLM:      providers.LLM,
		CtxMgr:   ctxmgr.NewManager(ctxmgr.Policy{}),
		Product:  Product,
	}

	h := &handler{
		ctx: ctx, pipeline: pl, processor: processor, state: &session.State{}, spaceID: spaceID, logger: logger,
	}
	model := chatshell.New(h,
		chatshell.WithTitle("sneat chat"),
		chatshell.WithContext(ctx),
		chatshell.WithCommands(slashCommands(processor.Commands())),
		chatshell.WithSidebarRenderer(sidebarRender),
	)
	h.model = model

	// AltScreen is set by chatshell's own View() (tea.View.AltScreen), not a
	// tea.NewProgram option, in bubbletea v2.
	_, err = tea.NewProgram(model).Run()
	return err
}

// defaultSpaceID picks the user's first space (by the same ordering
// spaces/list uses elsewhere would be nicer, but that helper lives in
// cmd/sneat/commands as an unexported function) -- KNOWN MVP LIMITATION: no
// `/space` picker wired into the aichat pipeline yet, see the final report.
// A read failure or an account with no spaces leaves spaceID empty; every
// pipeline call that needs one then answers "no space" rather than panicking
// (Pipeline's readers/executor all validate spaceID through the normal
// sneat-go 400 path).
func defaultSpaceID(ctx context.Context, spaces chat.SpacesReader, uid string) string {
	if spaces == nil {
		return ""
	}
	m, err := spaces.ListSpaces(ctx, uid)
	if err != nil {
		return ""
	}
	for id := range m {
		return id // map iteration order is arbitrary; any one space is a reasonable MVP default
	}
	return ""
}

func slashCommands(cmds []chat.CommandInfo) []chatshell.Command {
	out := make([]chatshell.Command, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, chatshell.Command{Name: c.Name, Help: c.Summary})
	}
	return out
}

func sidebarRender(ref session.EntityRef, width int) string {
	title := ref.Title
	if title == "" {
		title = ref.Type
	}
	icon := "•"
	switch ref.Type {
	case "happening":
		icon = "📅"
	case "todo":
		icon = "☑"
	case "contact":
		icon = "👤"
	}
	line := icon + " " + title
	if len(line) > width && width > 1 {
		line = line[:width]
	}
	return line
}
