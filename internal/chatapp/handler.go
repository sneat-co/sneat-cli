package chatapp

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"

	"github.com/strongo/aichat/ai"
	"github.com/strongo/aichat/ai/ctxmgr"
	"github.com/strongo/aichat/ai/decision"
	aidiag "github.com/strongo/aichat/ai/diag"
	"github.com/strongo/aichat/ai/session"
	"github.com/strongo/aichat/tui/chatshell"
	"github.com/strongo/aichat/tui/grid"
	"github.com/strongo/aichat/tui/transcript"

	"github.com/sneat-co/sneat-cli/internal/aichat/controls"
	"github.com/sneat-co/sneat-cli/internal/aichat/pipeline"
	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
	"github.com/sneat-co/sneat-cli/internal/chat"
)

// handler implements chatshell.Handler, chatshell.MsgHandler,
// chatshell.StreamObserver and chatshell.SidebarObserver: it is the one
// place that turns the aichat pipeline's Output into chatshell calls.
type handler struct {
	ctx       context.Context
	pipeline  pipeline.Pipeline
	processor chat.Processor
	state     *session.State
	spaceID   string
	logger    *slog.Logger

	model *chatshell.Model

	streamSeq int64
	// splitters maps an in-flight stream's id to its Splitter, so
	// OnStreamEvent's EventCompleted can retrieve the parsed <sneat-action>
	// once the stream that produced it is known to have finished.
	splitters map[string]*pipeline.Splitter
}

// --- chatshell.Handler -----------------------------------------------------

func (h *handler) Submit(text string) tea.Cmd {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "/") {
		return func() tea.Msg {
			replies, err := h.processor.SendText(h.ctx, trimmed)
			return slashMsg{replies: replies, err: err}
		}
	}
	return func() tea.Msg {
		out, err := h.pipeline.Turn(h.ctx, trimmed, h.state, h.spaceID)
		return turnMsg{text: trimmed, output: out, err: err}
	}
}

// --- chatshell.SidebarObserver ---------------------------------------------

func (h *handler) OnSidebarChange(refs []session.EntityRef) {
	h.state.Sidebar = refs
}

// --- chatshell.StreamObserver ------------------------------------------------

func (h *handler) OnStreamEvent(id string, ev ai.Event) tea.Cmd {
	if ev.Type != ai.EventCompleted {
		return nil
	}
	sp := h.splitters[id]
	delete(h.splitters, id)
	if sp == nil {
		return nil
	}
	return func() tea.Msg {
		_, action, err := sp.Finish()
		if err != nil || action == nil {
			return nil // a plain text-only answer, or a malformed block already reported by the transcript's error entry
		}
		out, err := h.pipeline.HandleAction(h.ctx, *action, h.state, h.spaceID)
		return actionMsg{output: out, err: err}
	}
}

// --- chatshell.MsgHandler ----------------------------------------------------

func (h *handler) OnMsg(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case slashMsg:
		return h.handleSlash(m)
	case turnMsg:
		return h.handleTurn(m)
	case actionMsg:
		h.render(m.output, m.err)
		return nil
	case grid.RowActivatedMsg:
		if m.Row.Ref != nil {
			h.state.Focus(m.Row.Ref)
		}
		return nil
	}
	return nil
}

func (h *handler) handleSlash(m slashMsg) tea.Cmd {
	if m.err != nil {
		h.model.AppendSystem("error: " + m.err.Error())
		return nil
	}
	for _, r := range m.replies {
		// The messenger Reply's Keyboard (buttons) has no chatshell
		// equivalent in this MVP slice -- slash commands still answer, just
		// as plain text, not as pressable buttons. See the final report.
		h.model.AppendAssistant(r.Text)
	}
	return nil
}

func (h *handler) handleTurn(m turnMsg) tea.Cmd {
	if m.err != nil {
		h.model.AppendSystem("error: " + m.err.Error())
		return nil
	}
	if !m.output.NeedsLLM {
		h.render(m.output, nil)
		h.logTurn(aidiag.PathDeterministic, m.output, nil)
		return nil
	}
	return h.startLLMStream(m.text, m.output.Decision)
}

func (h *handler) startLLMStream(text string, d *decision.Decision) tea.Cmd {
	focused := focusedScopes(h.state)
	req, report := h.pipeline.StreamRequest(h.ctx, text, h.state, h.spaceID, d, focused)
	h.logStreamRequest(d, report)
	seq, splitter := h.pipeline.Stream(h.ctx, req)
	id := h.nextStreamID()
	if h.splitters == nil {
		h.splitters = map[string]*pipeline.Splitter{}
	}
	h.splitters[id] = splitter
	return h.model.StartStream(id, seq)
}

func (h *handler) nextStreamID() string {
	return "turn-" + strconv.FormatInt(atomic.AddInt64(&h.streamSeq, 1), 10)
}

// render turns a deterministic/action Output into chatshell calls: a
// structured control (when Presentation+Entities are set) and/or plain text.
func (h *handler) render(out pipeline.Output, err error) {
	if err != nil {
		h.model.AppendSystem("error: " + err.Error())
		return
	}
	if len(out.Entities) > 0 {
		h.model.AppendBlock(blockFor(out.Presentation, out.Entities))
	}
	if out.Text != "" {
		h.model.AppendAssistant(out.Text)
	}
}

// blockFor builds the transcript.Block for a presentation kind. ContactsGrid
// reuses tui/grid directly; every other list-shaped presentation renders as
// a controls.ListBlock (see internal/aichat/controls's doc comment).
func blockFor(presentation string, entities []session.EntityRef) transcript.Block {
	// A single contact renders as a card (brief §3/§18 scenario 7: "single
	// contact -> contact card"), a real search result as the grid -- the
	// same distinction a card/grid pair always makes.
	if presentation == sneatdomain.PresentationContactsGrid && len(entities) == 1 {
		return cardFor(entities[0])
	}
	if presentation == sneatdomain.PresentationContactsGrid {
		contacts := make([]controls.Contact, 0, len(entities))
		for _, e := range entities {
			contacts = append(contacts, controls.Contact{Name: e.Title, Ref: e})
		}
		return controls.NewContactsGrid("Contacts", contacts)
	}
	items := make([]controls.Item, 0, len(entities))
	for _, e := range entities {
		items = append(items, controls.Item{Title: e.Title, Ref: e})
	}
	return controls.NewListBlock(headingFor(presentation), items)
}

// cardFor builds the single-entity card for a happening or contact -- the
// fields available are only what session.EntityRef carries (Title and Keys);
// a richer card (address, time, attendees, ...) needs the resolved
// data.Happening/data.Contact record, which is a follow-up once a
// presentation asks for a card explicitly rather than this len==1 inference.
func cardFor(ref session.EntityRef) *controls.CardBlock {
	title := ref.Title
	if title == "" {
		title = ref.Type
	}
	var fields [][2]string
	for _, k := range []string{"spaceID", "happeningID", "contactID", "itemID", "list"} {
		if v := ref.Keys[k]; v != "" {
			fields = append(fields, [2]string{k, v})
		}
	}
	return controls.NewCardBlock(title, ref, fields...)
}

func headingFor(presentation string) string {
	switch presentation {
	case sneatdomain.PresentationDayCalendar:
		return "Today"
	case sneatdomain.PresentationWeekCalendar:
		return "This week"
	case sneatdomain.PresentationHappeningsList:
		return "Happenings"
	case sneatdomain.PresentationTodoList:
		return "Todos"
	case sneatdomain.PresentationBuyList:
		return "To buy"
	default:
		return "Results"
	}
}

// focusedScopes maps the session's focused/sidebar entity types to their
// owning module scope, for StreamRequest's pinnedScopes (brief §4/§7: the
// Context Manager includes dynamic data for focused/sidebar entities even
// when not required by a decision).
func focusedScopes(st *session.State) []string {
	seen := map[string]bool{}
	var out []string
	add := func(t string) {
		scope := moduleForEntityType(t)
		if scope == "" || seen[scope] {
			return
		}
		seen[scope] = true
		out = append(out, scope)
	}
	if st.Focused != nil {
		add(st.Focused.Type)
	}
	for _, r := range st.Sidebar {
		add(r.Type)
	}
	return out
}

func moduleForEntityType(t string) string {
	switch t {
	case sneatdomain.EntityHappening:
		return sneatdomain.ModuleCalendar
	case sneatdomain.EntityTodo:
		return sneatdomain.ModuleTodo
	case sneatdomain.EntityContact:
		return sneatdomain.ModuleContacts
	default:
		return ""
	}
}

// logTurn/logStreamRequest emit ai/diag.Turn records at Debug -- never user
// text (see internal/aichat/diag's removed stand-in's doc, now delegated to
// ai/diag directly).
func (h *handler) logTurn(path aidiag.Path, out pipeline.Output, err error) {
	if h.logger == nil {
		return
	}
	t := aidiag.Turn{Path: path, LLMSkipped: true}
	if out.Decision != nil {
		t.Module, t.Intent = out.Decision.Module.Value, out.Decision.Intent.Value
	}
	if err != nil {
		t.Errors = []string{aidiag.ErrorCode(err)}
	}
	aidiag.Log(h.ctx, h.logger, t)
}

func (h *handler) logStreamRequest(d *decision.Decision, report ctxmgr.Report) {
	if h.logger == nil {
		return
	}
	path := aidiag.PathLLMFallback
	var module, intent string
	if d != nil {
		path, module, intent = aidiag.PathDecision, d.Module.Value, d.Intent.Value
	}
	h.logger.DebugContext(h.ctx, "aichat.turn.llm",
		"path", string(path), "module", module, "intent", intent,
		"requiredScopes", fmt.Sprint(report.Required), "actualScopes", fmt.Sprint(append(report.Retained, report.Added...)))
}

// --- message types the pipeline hands back through tea.Cmd -----------------

type slashMsg struct {
	replies []chat.Reply
	err     error
}

type turnMsg struct {
	text   string
	output pipeline.Output
	err    error
}

type actionMsg struct {
	output pipeline.Output
	err    error
}
