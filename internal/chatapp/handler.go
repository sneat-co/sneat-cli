package chatapp

import (
	"context"
	"fmt"
	"iter"
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
//
// Concurrency (coordinator ruling): every mutation of state (the live
// *session.State a chatshell key handler can read at any moment) happens on
// the UI loop -- inside Submit itself (called synchronously from
// chatshell's Update) or inside OnMsg (also called from Update). Submit's
// own returned tea.Cmd runs pipeline.Turn/HandleAction on a COPY of state,
// never the live pointer, and hands the resulting copy back in a message;
// OnMsg is what writes it onto state. See Submit and handleTurn/handleSlash.
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
	// streamText/streamUser accumulate an in-flight stream's visible text and
	// the user turn that started it, so OnStreamDone can record the exchange
	// into history once the stream finishes (brief §7/S7 coordinator ruling:
	// "last N (8) turns of history" -- see pipeline.HistoryTurns).
	streamText map[string]*strings.Builder
	streamUser map[string]string
	// history is the bounded exchange log StreamRequest sends ahead of the
	// current turn. Pipeline itself is a stateless value (rebuilt per call),
	// so this is where the session's history actually lives.
	history []ai.Message
}

// appendHistory records one user/assistant exchange and trims history to
// pipeline.HistoryTurns exchanges, oldest first. An empty assistant text
// (e.g. a deterministic turn that only rendered a structured control) still
// records the user's side, so a later pronoun/continuation still has it in
// view even though there was no prose reply.
func (h *handler) appendHistory(user, assistant string) {
	if user == "" {
		return
	}
	h.history = append(h.history, ai.Message{Role: ai.RoleUser, Text: user})
	if assistant != "" {
		h.history = append(h.history, ai.Message{Role: ai.RoleAssistant, Text: assistant})
	}
	max := pipeline.HistoryTurns * 2
	if len(h.history) > max {
		h.history = h.history[len(h.history)-max:]
	}
}

// --- chatshell.Handler -----------------------------------------------------

// Submit runs on the UI loop (chatshell's handleInputKey calls it
// synchronously from Update), so it is where FOCUS is read (coordinator
// ruling: FocusedRef()/SelectionRefs() at Submit time, on the UI loop) and
// where the state snapshot handed to the background pipeline call is taken.
// The returned tea.Cmd's closure runs on a worker goroutine and must not
// touch h.state -- it works only on the snapshot, and its result message is
// applied back to h.state by OnMsg, which does run on the UI loop.
func (h *handler) Submit(text string) tea.Cmd {
	trimmed := strings.TrimSpace(text)

	// Sync the pipeline's space to whatever the slash-command Processor's
	// active space now is (coordinator ruling SPACE: /space is the single
	// source of truth once the Processor itself changes it -- see
	// chat.Processor.ActiveSpace).
	if active := h.processor.ActiveSpace(); active != "" {
		h.spaceID = active
	}

	if strings.HasPrefix(trimmed, "/") {
		return func() tea.Msg {
			replies, err := h.processor.SendText(h.ctx, trimmed)
			return slashMsg{replies: replies, err: err}
		}
	}

	// Snapshot state ON THE UI LOOP: a shallow copy isolates the background
	// call's Pending/Previous/LastShown/Sidebar/Selection/Focused
	// reassignments (pipeline never mutates a slice/struct in place, only
	// reassigns a State field) from the live h.state until OnMsg applies the
	// result back.
	snapshot := *h.state
	if ref := h.model.FocusedRef(); ref != nil {
		snapshot.Focus(ref)
	}
	if sel := h.model.SelectionRefs(); len(sel) > 0 {
		snapshot.Selection = sel
	}

	ctx, cancel := context.WithCancel(h.ctx)
	busyCmd := h.model.SetBusy(true)
	h.model.SetBusyCancel(cancel)

	work := func() tea.Msg {
		out, err := h.pipeline.Turn(ctx, trimmed, &snapshot, h.spaceID)
		return turnMsg{text: trimmed, output: out, err: err, state: snapshot}
	}
	if busyCmd == nil {
		return work
	}
	return tea.Batch(work, busyCmd)
}

// --- chatshell.SidebarObserver ---------------------------------------------

// OnSidebarChange fires from chatshell's Update (UI loop), so writing
// straight to h.state is safe here.
func (h *handler) OnSidebarChange(refs []session.EntityRef) {
	h.state.Sidebar = refs
}

// --- chatshell.StreamObserver ------------------------------------------------

func (h *handler) OnStreamEvent(id string, ev ai.Event) tea.Cmd {
	if ev.Type == ai.EventTextDelta {
		// Accumulate the visible (splitter-filtered) reply text for history
		// (S7): OnStreamDone records it as the assistant side of this turn's
		// exchange once the stream finishes.
		if b := h.streamText[id]; b != nil {
			b.WriteString(ev.Text)
		}
		return nil
	}
	if ev.Type != ai.EventCompleted {
		return nil
	}
	sp := h.splitters[id]
	if sp == nil {
		return nil
	}
	return func() tea.Msg {
		trailing, action, err := sp.Finish()
		if err != nil {
			// Splitter contract: a malformed/unterminated block is surfaced,
			// never silently dropped, and whatever text was already held
			// back is still shown (coordinator ruling SPLITTER).
			return actionMsg{parseErr: err, trailing: trailing}
		}
		if action == nil {
			return nil // a plain text-only answer
		}
		out, err := h.pipeline.HandleAction(h.ctx, *action, h.state, h.spaceID)
		return actionMsg{output: out, err: err}
	}
}

// OnStreamDone fires exactly once per StartStream call (success, fatal
// error, or cancellation) -- this is where the splitter entry for id is
// retired and the LLM-leg diagnostics are logged, regardless of outcome.
func (h *handler) OnStreamDone(id string, err error) tea.Cmd {
	delete(h.splitters, id)
	if b := h.streamText[id]; b != nil && err == nil {
		h.appendHistory(h.streamUser[id], b.String())
	}
	delete(h.streamText, id)
	delete(h.streamUser, id)
	if h.logger != nil {
		t := aidiag.Turn{Path: aidiag.PathLLMFallback}
		if err != nil {
			t.Errors = []string{aidiag.ErrorCode(err)}
		}
		aidiag.Log(h.ctx, h.logger, t)
	}
	return nil
}

// --- chatshell.MsgHandler ----------------------------------------------------

// OnMsg runs on the UI loop (chatshell's dispatchUnhandled calls it from
// Update): every write to h.state in this file happens from here or from
// Submit itself (before the async work is dispatched), never from inside a
// tea.Cmd closure.
func (h *handler) OnMsg(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case slashMsg:
		return h.handleSlash(m)
	case turnMsg:
		return h.handleTurn(m)
	case actionMsg:
		if m.parseErr != nil {
			if m.trailing != "" {
				h.model.AppendAssistant(m.trailing)
			}
			h.model.AppendSystem("(couldn't parse action)")
			if h.logger != nil {
				aidiag.Log(h.ctx, h.logger, aidiag.Turn{Path: aidiag.PathLLMFallback, Errors: []string{aidiag.ErrorCode(m.parseErr)}})
			}
			return nil
		}
		h.render(m.output, m.err)
		return nil
	case grid.RowActivatedMsg:
		if m.Row.Ref != nil {
			h.state.Focus(m.Row.Ref)
		}
		return nil
	case controls.ItemActivatedMsg:
		ref := m.Ref
		h.state.Focus(&ref)
		return nil
	}
	return nil
}

func (h *handler) handleSlash(m slashMsg) tea.Cmd {
	if active := h.processor.ActiveSpace(); active != "" {
		h.spaceID = active
	}
	if m.err != nil {
		h.model.AppendSystem("error: " + m.err.Error())
		return nil
	}
	for _, r := range m.replies {
		// The messenger Reply's Keyboard (buttons) has no chatshell
		// equivalent in this MVP slice -- slash commands still answer, just
		// as plain text, not as pressable buttons. See the final report
		// (coordinator ruling SLASH BUTTONS, declined this round).
		h.model.AppendAssistant(r.Text)
	}
	return nil
}

func (h *handler) handleTurn(m turnMsg) tea.Cmd {
	h.model.SetBusy(false)
	*h.state = m.state // apply the background work's result -- UI loop only
	if m.err != nil {
		h.model.AppendSystem("error: " + m.err.Error())
		h.logTurn(aidiag.PathDeterministic, m.output, m.err)
		return nil
	}
	if !m.output.NeedsLLM {
		h.render(m.output, nil)
		h.logTurn(aidiag.PathDeterministic, m.output, nil)
		h.appendHistory(m.text, m.output.Text)
		return nil
	}
	return h.startLLMStream(m.text, m.output.Decision)
}

func (h *handler) startLLMStream(text string, d *decision.Decision) tea.Cmd {
	focused := focusedScopes(h.state)
	history := append([]ai.Message(nil), h.history...)
	// StreamRequest itself does no I/O; DynamicBlocks reads happen inside
	// the open func below, which chatshell's StartStream runs under the
	// per-stream context it owns -- not on the UI loop, and cancellable via
	// Esc/Ctrl+C like the rest of the stream.
	stateSnapshot := *h.state
	id := h.nextStreamID()
	if h.splitters == nil {
		h.splitters = map[string]*pipeline.Splitter{}
	}
	if h.streamText == nil {
		h.streamText = map[string]*strings.Builder{}
		h.streamUser = map[string]string{}
	}
	h.streamText[id] = &strings.Builder{}
	h.streamUser[id] = text
	open := func(ctx context.Context) iter.Seq2[ai.Event, error] {
		req, report := h.pipeline.StreamRequest(ctx, text, &stateSnapshot, h.spaceID, d, focused, history)
		h.logStreamRequest(d, report)
		seq, splitter := h.pipeline.Stream(ctx, req)
		h.splitters[id] = splitter
		return seq
	}
	return h.model.StartStream(id, open)
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
// text.
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

// turnMsg carries the state SNAPSHOT the background pipeline.Turn call
// produced (see Submit's doc comment); handleTurn is the only place that
// writes it onto the live h.state, and it does so on the UI loop.
type turnMsg struct {
	text   string
	output pipeline.Output
	err    error
	state  session.State
}

type actionMsg struct {
	output   pipeline.Output
	err      error
	parseErr error
	trailing string
}
