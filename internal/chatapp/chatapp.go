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
	"github.com/sneat-co/sneat-cli/internal/firestoredb"
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

// runDeps are Run's own dependencies on the outside world: opening the
// shared Firestore-backed readers, building the AI provider chain, and
// driving the terminal program. Each is an unexported func var (a seam) so
// a test can override every one of them with a fake and exercise Run's own
// wiring/error/cleanup logic without a real Firestore project, cloud
// credentials, or terminal. The real defaults below are what Run calls in
// production; runDeps.reset (in export_test.go) restores them after a test
// that overrides one.

// newReaders opens the shared Firestore session Run's three readers use and
// returns it as an io.Closer alongside them (m6: ONE Firestore client for
// the whole chat session, shared by all three readers, instead of each
// reader opening its own -- a prior version's Readers.Close() closed 3
// separate clients that all conversed with the same Firestore project as
// the same user; there was never a reason for more than one).
var newReaders = func(cfg config.Config, ts oauth2.TokenSource, loc *time.Location) (data.Readers, io.Closer) {
	fsSession := firestoredb.NewSession(cfg, ts)
	return data.Readers{
		Happenings: data.NewFirestoreHappenings(fsSession, data.WithLocation(loc)),
		Todos:      data.NewFirestoreTodos(fsSession),
		Contacts:   data.NewFirestoreContacts(fsSession),
	}, fsSession
}

// buildAIConfig wraps aiconfig.Build as a seam.
var buildAIConfig = aiconfig.Build

// runProgram drives model to completion -- the real implementation blocks
// on the actual terminal via tea.NewProgram(model).Run(); a test replaces
// it to avoid opening one. AltScreen is set by chatshell's own View()
// (tea.View.AltScreen), not a tea.NewProgram option, in bubbletea v2.
var runProgram = func(model tea.Model) error {
	_, err := tea.NewProgram(model).Run()
	return err
}

// cloudTokenFunc builds aiconfig.Deps' CloudToken callback over ts, pulled
// out of Run as its own named function so it has a direct unit test (both
// the success and the TokenSource-error branch) independent of the rest of
// Run's wiring.
func cloudTokenFunc(ts oauth2.TokenSource) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		tok, err := ts.Token()
		if err != nil {
			return "", err
		}
		return tok.AccessToken, nil
	}
}

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

	// S4/TIMEZONES coordinator ruling: deps.TZ (--tz > SNEAT_TZ >
	// time.Local's name, resolved once at the CLI's composition root -- see
	// cmd/sneat/commands/chat.go's tzFromCmd) is the single zone every part
	// of one session resolves "today"/"this week"/slot times in. An
	// unparseable zone name falls back to time.Local rather than failing the
	// whole session over a typo'd --tz.
	loc := locationFromName(deps.TZ)
	now := sessionClock(loc)

	readers, closer := newReaders(deps.Cfg, deps.TokenSource, loc)
	// m4/m6: the session that wires a Readers (this Run call) owns closing
	// the shared Firestore client on shutdown -- on every return path,
	// including an early one from buildAIConfig failing below.
	defer func() {
		if cerr := closer.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("aichat: closing Firestore session: %w", cerr)
		}
	}()

	api := sneatapi.New(deps.Cfg.APIBaseURL, deps.TokenSource, httpClient)
	executor := pipeline.SneatExecutor{Calendar: api, Todo: api, Happenings: readers.Happenings, Now: now}

	providers, err := buildAIConfig(deps.AIConfig, aiconfig.Deps{
		Product: Product, CloudToken: cloudTokenFunc(deps.TokenSource), CloudBaseURL: deps.Cfg.APIBaseURL,
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
		Resolver: pipeline.Resolver{Readers: readers, Now: now},
		Executor: executor,
		Readers:  readers,
		Now:      now,
		TZ:       loc.String(),
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

	return runProgram(model)
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

// sessionClock builds the "now" every part of one chat session resolves
// "today"/"this week"/"tomorrow"/a bare-time reschedule against --
// SneatExecutor.Now, Pipeline.Now, and Resolver.Now alike (m1, fix round r4
// review). A bare time.Now() carries the PROCESS's own zone (the server/
// container's local zone, e.g. UTC), not the user's chosen one: --tz/
// SNEAT_TZ would then only relabel timestamps for DISPLAY while the actual
// "what day is today" logic silently kept using the process's own zone --
// wrong whenever the two disagree (e.g. --tz America/New_York on a UTC
// server: for several hours a day, the process's own "today" is already
// the NEXT calendar day in New York). loc is deps.TZ resolved via
// locationFromName, so callers get one shared, correctly-zoned clock rather
// than each separately relocating a bare time.Now().
func sessionClock(loc *time.Location) func() time.Time {
	return func() time.Time { return time.Now().In(loc) }
}

// locationFromName resolves name (an IANA zone, e.g. "Europe/Berlin", or
// time.Local's own "Local"/short-form string) to a *time.Location, falling
// back to time.Local for an empty or unparseable name (S4/TIMEZONES: a
// typo'd --tz or SNEAT_TZ must not fail the whole chat session).
func locationFromName(name string) *time.Location {
	if name == "" || name == time.Local.String() {
		return time.Local
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.Local
	}
	return loc
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

// userCacheDir/mkdirAll/openLogFile are newDebugLogger's own seams over the
// os package, so a test can force each failure branch (a cache dir lookup
// failing, a read-only/unwritable parent, a file that can't be opened)
// without actually breaking the filesystem out from under the real
// os.UserCacheDir/os.MkdirAll/os.OpenFile.
var (
	userCacheDir = os.UserCacheDir
	mkdirAll     = os.MkdirAll
	openLogFile  = os.OpenFile
)

// newDebugLogger writes ai/diag's Debug-level JSON logs to
// <UserCacheDir>/sneat/chat-debug.log rather than stderr, which chatshell's
// alt-screen owns exclusively while the program runs. A failure to open the
// file falls back to a discarded logger rather than corrupting the screen.
func newDebugLogger() *slog.Logger {
	dir, err := userCacheDir()
	if err != nil {
		return slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	dir = filepath.Join(dir, "sneat")
	if err := mkdirAll(dir, 0o755); err != nil {
		return slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	f, err := openLogFile(filepath.Join(dir, "chat-debug.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
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
