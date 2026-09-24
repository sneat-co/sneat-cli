// Package chatapp is the composition root for `sneat chat`'s interactive
// session: it builds the aichat MVP pipeline (internal/aichat) and runs it
// through strongo/aichat's tui/chatshell, replacing internal/chattui
// entirely (brief §11 full cutover).
package chatapp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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
	// Debug enables ai/diag logging at slog.LevelDebug, written to
	// <UserCacheDir>/sneat/chat-debug.log -- never stderr, which chatshell's
	// alt-screen owns.
	Debug bool
	// CurrentSpace is the session's persisted default space (coordinator
	// ruling SPACE), read from session.Session.CurrentSpace. Empty means
	// "none set yet" -- defaultSpaceID falls back to a deterministic choice.
	CurrentSpace string
	// TZ is the IANA zone day/week windows and slot times resolve in
	// (coordinator ruling TIMEZONES). Empty defaults to the process's local
	// zone name.
	TZ string
}

// Product identifies this CLI to Sneat AI Cloud (ai.ChatRequest.Product /
// X-AI-Product), per brief §11.
const Product = "sneat"

// Run builds the pipeline and launches the interactive chatshell. It blocks
// until the user quits.
func Run(deps Deps) (err error) {
	ctx := context.Background()
	httpClient := http.DefaultClient

	spaceID := defaultSpaceID(ctx, deps.Spaces, deps.UID, deps.CurrentSpace)

	// m9 coordinator ruling: seed the Processor's own active space with the
	// pipeline's default space pick, so /space agrees with it from the
	// first turn instead of reporting "No space is selected" until the
	// user presses a space button.
	processor := chat.NewProcessor(chat.Deps{
		Spaces: deps.Spaces, Contacts: deps.Contacts, UID: deps.UID, Email: deps.Email, Version: deps.Version,
		CurrentSpace: spaceID,
	})

	readers := data.Readers{
		Happenings: data.NewFirestoreHappenings(deps.Cfg, deps.TokenSource),
		Todos:      data.NewFirestoreTodos(deps.Cfg, deps.TokenSource),
		Contacts:   data.NewFirestoreContacts(deps.Cfg, deps.TokenSource),
	}
	// m4/Readers.Close's own doc comment: the session that wires a Readers
	// (this Run call) owns closing it on shutdown, releasing the lazily-
	// opened, reused Firestore client(s) behind it -- on every return path,
	// including an early one from aiconfig.Build failing below.
	defer func() {
		if cerr := readers.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("aichat: closing readers: %w", cerr)
		}
	}()

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
		logger = newDebugLogger()
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

// defaultSpaceID chooses the pipeline's starting space (coordinator ruling
// SPACE): the session's persisted CurrentSpace wins outright when set; else
// the user's family space if one exists among their spaces; else the lowest
// space ID after sorting -- never map iteration order. A read failure or an
// account with no spaces leaves spaceID empty; every pipeline call that
// needs one then answers "no space" rather than panicking (Pipeline's
// readers/executor all validate spaceID through the normal sneat-go 400
// path).
func defaultSpaceID(ctx context.Context, spaces chat.SpacesReader, uid, currentSpace string) string {
	if currentSpace != "" {
		return currentSpace
	}
	if spaces == nil {
		return ""
	}
	m, err := spaces.ListSpaces(ctx, uid)
	if err != nil || len(m) == 0 {
		return ""
	}
	ids := make([]string, 0, len(m))
	for id, info := range m {
		ids = append(ids, id)
		if isFamilySpace(info) {
			return id
		}
	}
	slices.Sort(ids)
	return ids[0]
}

// isFamilySpace reports whether a space-list entry is the user's family
// space, matching internal/chat/processor.go's own "type" field convention
// (see resolveSpace) rather than a separately-invented shape.
func isFamilySpace(info any) bool {
	b, _ := info.(map[string]any)
	if b == nil {
		return false
	}
	t, _ := b["type"].(string)
	return strings.EqualFold(t, "family")
}

// newDebugLogger writes ai/diag's Debug-level JSON logs to
// <UserCacheDir>/sneat/chat-debug.log rather than stderr, which chatshell's
// alt-screen owns exclusively while the program runs. A failure to open the
// file falls back to a discarded logger rather than corrupting the screen.
func newDebugLogger() *slog.Logger {
	dir, err := os.UserCacheDir()
	if err != nil {
		return slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	dir = filepath.Join(dir, "sneat")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	f, err := os.OpenFile(filepath.Join(dir, "chat-debug.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	return slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug}))
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
	// m8: truncate by lipgloss's DISPLAY width, not len() (bytes) -- byte
	// truncation both cuts a multi-byte rune (icon, or any non-ASCII title)
	// mid-sequence and, for a wide rune, allows more bytes than the sidebar
	// column can actually display.
	if width <= 0 {
		return line
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(line)
}
