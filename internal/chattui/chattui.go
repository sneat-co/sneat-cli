// Package chattui renders a Sneat chat session inline in the terminal. It is
// launched by `sneat chat`.
//
// It is a package of its own rather than a screen inside internal/tui: that
// package's screen stack serves list navigation, while a chat has no sibling
// screens to navigate to and a focus model spanning a text input and a block of
// buttons.
//
// It depends on internal/chat for the Processor interface only. The concrete
// processor is unexported in that package, so this one cannot name an
// implementation even by accident (chat-messenger#req:processor-seam); the
// composition root that builds one is the RunChat closure in cmd/sneat/main.go.
package chattui

import (
	"context"
	"strings"

	"github.com/bots-go-framework/bots-go-core/botkb"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sneat-co/sneat-cli/internal/chat"
)

// focusTarget names what currently takes keys: the input line, or the live
// reply's button block. Focus is only ever one of the two (REQ: focus-and-keys).
type focusTarget int

const (
	focusInput focusTarget = iota
	focusButtons
)

// Model is the root bubbletea model for a chat session.
//
// It holds only what can still change. The transcript is not a field: committed
// turns are handed to the terminal as ordinary scrollback and never repainted
// (REQ: scrollback-commit), so the terminal owns them, not this model. What
// remains here is the live region — the reply whose buttons are still
// focusable, the input, and where focus sits.
type Model struct {
	proc  chat.Processor
	input textinput.Model

	// live is the most recent bot reply while its buttons are still
	// focusable, or nil when there is none. A reply carrying no keyboard is
	// never live: it commits on arrival.
	live *chat.Reply

	// focus is what keys go to. It is focusButtons only while live is non-nil.
	focus focusTarget

	// row and col are the focused button's position in live's keyboard, read
	// only while focus is focusButtons.
	row, col int

	// pending reports whether a reply is in flight. While set, keys other than
	// ctrl+c are ignored (REQ: input-locked-while-pending) and the typing
	// indicator renders (REQ: pending-is-visible).
	pending bool

	// spin animates the typing indicator. It is ticked only while pending, so an
	// idle session schedules no work.
	spin spinner.Model

	// width is the terminal width the live region renders to.
	width int
}

// Model must satisfy tea.Model for tea.NewProgram to take it.
var _ tea.Model = Model{}

// New builds the root model for a chat session processed by proc.
//
// It takes the chat.Processor interface, never a concrete type
// (chat-messenger#req:processor-seam).
func New(proc chat.Processor) Model {
	in := textinput.New()
	in.Placeholder = "Type a message"
	in.Prompt = inputPrompt
	in.Width = inputWidth(defaultWidth)
	in.Focus()
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = pendingStyle
	return Model{
		proc:  proc,
		input: in,
		spin:  sp,
		width: defaultWidth,
		focus: focusInput,
	}
}

// inputWidth converts a terminal width into the width the text input renders
// at: the terminal less the prompt it sits behind.
//
// Setting it is not cosmetic. textinput.placeholderView sizes its buffer as
// `make([]rune, Width+1)` and copies the placeholder into it, so a zero Width
// silently truncates the placeholder to one rune — the input renders "> T"
// rather than "> Type a message". The floor keeps that from recurring on a
// terminal too narrow to hold the placeholder.
func inputWidth(terminal int) int {
	w := terminal - lipgloss.Width(inputPrompt)
	if min := lipgloss.Width("Type a message"); w < min {
		return min
	}
	return w
}

// defaultWidth is the assumed terminal width until the first WindowSizeMsg
// arrives. Bubble Tea sends one on startup, so this only covers the first
// frame.
const defaultWidth = 80

// Init starts the input's cursor blinking.
func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

// --- turns ---

// repliesMsg carries a Processor's answer to a turn back onto the UI thread.
type repliesMsg struct{ replies []chat.Reply }

// errMsg carries a Processor failure back onto the UI thread. The Processor
// returns a bare error by contract
// (chat-messenger#req:errors-are-returned-not-formatted): wording it for a
// terminal is this package's job, not its.
type errMsg struct{ err error }

// sendText hands a typed message to the Processor from a command, so the call
// runs off the UI thread. SendText does network I/O; calling it inline in Update
// would freeze the live region until the backend answered.
func sendText(p chat.Processor, text string) tea.Cmd {
	return func() tea.Msg {
		replies, err := p.SendText(context.Background(), text)
		if err != nil {
			return errMsg{err}
		}
		return repliesMsg{replies}
	}
}

// pressButton hands a pressed button's callback data to the Processor from a
// command, for the same reason sendText does: PressButton does network I/O, and
// the live region must not block on it.
func pressButton(p chat.Processor, data string) tea.Cmd {
	return func() tea.Msg {
		replies, err := p.PressButton(context.Background(), data)
		if err != nil {
			return errMsg{err}
		}
		return repliesMsg{replies}
	}
}

// Update handles the terminal's size, the key table, and the Processor's answer,
// and passes everything else to the input.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.input.Width = inputWidth(msg.Width)
	case spinner.TickMsg:
		// Only keep the animation going while a turn is in flight. Once it
		// resolves the tick stops rescheduling itself and the loop goes quiet.
		if !m.pending {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case repliesMsg:
		return m.receive(msg.replies)
	case errMsg:
		return m.fail(msg.err)
	case tea.KeyMsg:
		next, cmd := m.handleKey(msg)
		return next.syncInputCursor(cmd)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// --- keys (REQ: focus-and-keys, REQ: input-locked-while-pending) ---

// The keys the requirement names, spelled as bubbletea names them
// (tea.KeyMsg.String) rather than as tea.KeyType values, so the tables below can
// be read against the requirement line by line.
const (
	keyCtrlC = "ctrl+c"
	keyEnter = "enter"
	keyEsc   = "esc"
	keyUp    = "up"
	keyDown  = "down"
	keyLeft  = "left"
	keyRight = "right"
)

// handleKey gives a key press its one meaning.
//
// Which meaning that is depends on two things, applied here in the order that
// makes each of them absolute: ctrl+c first, because nothing may take the exit
// away; then the pending lock, because it has to be able to refuse the very keys
// focus would otherwise act on. Only what survives both reaches a focus table,
// and each key appears in exactly one of those.
func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	// ctrl+c quits from either focus, and even while a reply is in flight
	// (REQ: input-locked-while-pending). It is matched ahead of the lock for
	// precisely that reason.
	if msg.String() == keyCtrlC {
		return m, tea.Quit
	}
	// While a reply is in flight every other key is ignored, so nothing the user
	// does can race the answer they are waiting for.
	//
	// internal/tui's confirm screen guards its in-flight delete the same way, but
	// it returns before ctrl+c too. That is safe for a screen resolving a single
	// round trip; a chat session may block on a slow backend for as long as it
	// likes, so the carve-out above is where this deliberately parts company with
	// it — the user must always retain a way out.
	if m.pending {
		return m, nil
	}
	if m.focus == focusButtons {
		return m.handleButtonKey(msg)
	}
	return m.handleInputKey(msg)
}

// handleInputKey gives a key its meaning while the input holds focus. Keys it
// does not name are the input's to interpret: they are text.
func (m Model) handleInputKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case keyEsc:
		// esc quits from the input. From the button block it means something else
		// entirely — which is why the two tables are separate rather than one
		// table asking where focus is at every entry.
		return m, tea.Quit
	case keyEnter:
		return m.submitText()
	case keyUp:
		// The block renders above the input, so up is the key that reaches it,
		// and it lands on the LAST row — the button physically nearest the cursor
		// that just left. Entering at row 0 would jump focus to the button
		// furthest from where the user is looking.
		//
		// With no live reply there is no block to enter — focus is focusButtons
		// only while live is non-nil — so the key is the input's, like any other.
		if rows := m.liveButtonRows(); len(rows) > 0 {
			m.focus = focusButtons
			m.row = len(rows) - 1
			m.col = clampCol(0, len(rows[m.row]))
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// handleButtonKey gives a key its meaning while the button block holds focus.
//
// Keys it does not name do nothing at all. They are not passed to the input: the
// input does not hold focus, and exactly one target does
// (AC: interaction-is-unambiguous).
func (m Model) handleButtonKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	rows := m.liveButtonRows()
	if len(rows) == 0 {
		// Focus is in a block that does not exist. Nothing reaches this: focus
		// becomes focusButtons only over a block with rows, and commitLive returns
		// focus with the reply it commits. If anything ever did, the input is
		// where focus belongs, rather than a cursor over nothing.
		m.focusInputLine()
		return m, nil
	}
	switch msg.String() {
	case keyEsc:
		// esc leaves the block for the input. It does not quit: quitting is what
		// esc means once focus is back on the input, one press later.
		m.focusInputLine()
	case keyUp:
		// up moves deeper into the block, toward its top. It stops at row 0: it
		// is already the key that entered the block, and a key that both entered
		// and left it would have two meanings for the same focus.
		if m.row > 0 {
			m.row--
			m.col = clampCol(m.col, len(rows[m.row]))
		}
	case keyDown:
		// down is how focus leaves the block, back to the input below it — the
		// mirror of the up that entered. From any row above the last it is a move
		// within the block.
		if m.row >= len(rows)-1 {
			m.focusInputLine()
			break
		}
		m.row++
		m.col = clampCol(m.col, len(rows[m.row]))
	case keyLeft:
		// left and right move within the row and stop at its ends. Wrapping would
		// give either a second meaning — "go to the other end" — for the same key
		// and the same focus.
		if m.col > 0 {
			m.col--
		}
	case keyRight:
		if m.col < len(rows[m.row])-1 {
			m.col++
		}
	case keyEnter:
		return m.pressFocusedButton()
	}
	return m, nil
}

// pressFocusedButton makes the turn the focused button stands for.
//
// A press is user input exactly as a submitted line is, so it commits the same
// way submitText does and in the same order: the live reply first, then an echo
// of what the user did, and only then does the turn go to the Processor
// (REQ: scrollback-commit). The reply it commits is the very one carrying the
// button being pressed, which is what makes a press always end the previous
// reply's focusability — no live reply is ever stranded, because the only way to
// press a button is from the block that commits with it.
//
// The echo names the button's label rather than its callback data: the label is
// what the user saw and chose, and `space?id=family1` is an implementation detail
// of the seam it travels over.
//
// Both commits are joined into a single Println, and the press is sequenced after
// it rather than batched alongside — see submitText for why the difference
// matters.
func (m Model) pressFocusedButton() (Model, tea.Cmd) {
	btn, ok := m.focusedButton()
	if !ok {
		return m, nil
	}
	data, ok := buttonData(btn)
	if !ok {
		// A button carrying no callback data names no turn: chat.Processor
		// identifies a press by its data and there is none
		// (chat-messenger#req:processor-seam). Nothing is committed, because
		// nothing happened: no turn means the reply is not superseded, and its
		// buttons are still the user's to press. It must not go pending either —
		// nothing would answer, and the lock would never lift.
		return m, nil
	}

	var blocks []string
	if live := m.commitLive(); live != "" {
		blocks = append(blocks, live)
	}
	blocks = append(blocks, renderUserEcho(btn.GetText()))

	m.pending = true
	// The spinner starts inside the sequence, after the commit — not batched
	// around it. Batching would let the tick race the commit, and the commit
	// being ordered first is the whole point of the sequence
	// (REQ: scrollback-commit).
	return m, tea.Sequence(commit(blocks), tea.Batch(m.spin.Tick, pressButton(m.proc, data)))
}

// focusInputLine puts focus back on the input, leaving no cursor behind in the
// button block.
//
// Focus is focusButtons only while there is a block to point into, and up
// re-enters at the last row, so a cursor left where focus used to be would be
// state nothing reads and every path would have to remember to reset.
func (m *Model) focusInputLine() {
	m.focus = focusInput
	m.row, m.col = 0, 0
}

// syncInputCursor points the input's own cursor at whether the input holds focus,
// and passes cmd through.
//
// The input draws a blinking cursor while it is focused, and the live region
// marks the focused button while the block holds focus. Both at once would name
// two focus holders, and exactly one ever holds it
// (AC: interaction-is-unambiguous).
//
// It runs once after every key rather than at each place focus moves, so focus —
// the one field that says where keys go — stays the only thing a key handler has
// to get right, and the cursor follows it wherever it goes.
func (m Model) syncInputCursor(cmd tea.Cmd) (Model, tea.Cmd) {
	switch {
	case m.focus == focusInput && !m.input.Focused():
		// Focus returns the cursor's blink command, which has to reach the event
		// loop or the cursor sits still.
		return m, tea.Batch(cmd, m.input.Focus())
	case m.focus == focusButtons && m.input.Focused():
		m.input.Blur()
	}
	return m, cmd
}

// liveButtonRows returns the rows of buttons the live reply carries, or nil when
// nothing is live.
//
// Focus moves over exactly what View draws, because both read the block through
// this: a cursor indexing rows the renderer skipped would mark one button and
// press another.
func (m Model) liveButtonRows() [][]botkb.Button {
	if m.live == nil {
		return nil
	}
	return buttonRows(*m.live)
}

// focusedButton returns the button focus sits on, and whether there is one.
func (m Model) focusedButton() (botkb.Button, bool) {
	rows := m.liveButtonRows()
	if m.row < 0 || m.row >= len(rows) {
		return nil, false
	}
	row := rows[m.row]
	if m.col < 0 || m.col >= len(row) {
		return nil, false
	}
	return row[m.col], true
}

// buttonData returns b's callback data, and whether it carries any.
//
// botkb.Button exposes only its text and its type, so reading callback data means
// asserting the concrete botkb.DataButton — in both its forms, since its methods
// take a value receiver and either a value or a pointer can be stored in the
// interface, exactly as buttonRows does for the keyboard. Any other kind of
// button (a URL, a text button) identifies no turn to a chat.Processor, which
// names a press by its data.
func buttonData(b botkb.Button) (string, bool) {
	switch b := b.(type) {
	case *botkb.DataButton:
		if b == nil {
			return "", false
		}
		return b.Data, true
	case botkb.DataButton:
		return b.Data, true
	}
	return "", false
}

// clampCol keeps the button cursor inside a row of width buttons.
//
// Rows need not be the same width, so moving between them can leave the column
// past the end of the row it lands in: a cursor marking no button, which enter
// could not press. buttonRows drops empty rows, so width is at least 1 and there
// is always a button to land on.
func clampCol(col, width int) int {
	if col >= width {
		return width - 1
	}
	if col < 0 {
		return 0
	}
	return col
}

// submitText commits the turn's user input and starts the Processor call.
//
// The order is the one REQ: scrollback-commit fixes: the live reply commits
// first, then the echo of what the user submitted, and only then does the turn
// go to the Processor. Both commits are joined into a single Println so nothing
// can interleave them, and the send is sequenced after that Println rather than
// batched alongside it — tea.Batch runs its commands concurrently, which would
// let a fast Processor land its answer in scrollback ahead of the line that
// prompted it.
func (m Model) submitText() (Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		// A bare enter is not a turn: it would reach the Processor with nothing
		// and leave a blank echo in the transcript.
		return m, nil
	}
	m.input.SetValue("")

	var blocks []string
	if live := m.commitLive(); live != "" {
		blocks = append(blocks, live)
	}
	blocks = append(blocks, renderUserEcho(text))

	m.pending = true
	return m, tea.Sequence(commit(blocks), tea.Batch(m.spin.Tick, sendText(m.proc, text)))
}

// receive renders the Processor's answer to a turn: every reply commits to
// scrollback except a trailing one carrying buttons, which stays live until
// something supersedes it (REQ: scrollback-commit).
//
// Only a trailing reply can be live because only the most recent message's
// buttons are focusable (REQ: focus-and-keys). A keyboard on any earlier reply of
// the same turn is already superseded by the time the turn is drawn, so it
// commits with the reply that carries it, inert.
func (m Model) receive(replies []chat.Reply) (Model, tea.Cmd) {
	m.pending = false
	if n := len(replies); n > 0 && len(buttonRows(replies[n-1])) > 0 {
		last := replies[n-1]
		m.live = &last
		m.row, m.col = 0, 0
		replies = replies[:n-1]
		// Focus stays on the input: entering the button block is `up`
		// (REQ: focus-and-keys), not something a reply does to the user.
	}
	blocks := make([]string, 0, len(replies))
	for _, r := range replies {
		blocks = append(blocks, renderCommittedReply(r))
	}
	return m, commit(blocks)
}

// fail renders a Processor failure as a bot message in the transcript and leaves
// the session running (REQ: errors-render-in-transcript).
//
// The failure commits to scrollback rather than being drawn in the live region,
// because it is a completed turn: it is how the turn ended, and nothing about it
// can still change (REQ: scrollback-commit). It joins the transcript exactly as a
// reply does, which is the point — a backend that failed is a thing the
// conversation records, not a thing that interrupts it.
//
// It must not quit, and that is the requirement's own reasoning rather than a
// preference: the transcript is what the user came away with, and a program that
// exits on a failed turn takes the session down over something the next turn may
// well recover from. So the failure lifts the pending lock the same way replies
// do — a failure is an answer — and the input goes on taking messages.
func (m Model) fail(err error) (Model, tea.Cmd) {
	m.pending = false
	return m, commit([]string{renderError(err)})
}

// commitLive renders the live reply for scrollback and clears it, returning the
// rendered block, or "" when nothing was live.
//
// The reply commits with its buttons inert: they stop being focusable at the
// moment it commits, which is precisely what completes the turn
// (REQ: scrollback-commit). Focus returns to the input with it — the block it
// pointed into no longer exists, and focus is focusButtons only while live is
// non-nil.
func (m *Model) commitLive() string {
	if m.live == nil {
		return ""
	}
	block := renderCommittedReply(*m.live)
	m.live = nil
	m.focusInputLine()
	return block
}

// commit hands rendered transcript blocks to the terminal as scrollback.
//
// tea.Println prints above the live region, and the program never repaints what
// it printed. That is what makes a committed turn ordinary terminal text —
// selectable, copyable and searchable with the terminal's own tooling, and still
// there after the session exits (REQ: scrollback-commit, REQ: inline-rendering).
//
// The blocks are joined into one Println rather than printed by several, so their
// order is fixed by the string rather than by the order the event loop happens to
// deliver them in.
func commit(blocks []string) tea.Cmd {
	if len(blocks) == 0 {
		return nil
	}
	return tea.Println(strings.Join(blocks, "\n"))
}

// View renders the live region — the only part of the chat that is repainted.
// Everything above it is committed scrollback the terminal owns
// (REQ: scrollback-commit).
//
// The region is the focusable reply and its buttons (when there is one), the
// input line, and the footer hint (REQ: inline-rendering). It is deliberately not
// the transcript: redrawing past turns here would fight the terminal for text it
// already owns.
func (m Model) View() string {
	var b strings.Builder
	if m.live != nil {
		b.WriteString(renderLiveReply(*m.live, m.focus == focusButtons, m.row, m.col))
		b.WriteByte('\n')
	}
	if m.pending {
		b.WriteString(m.spin.View())
		b.WriteString(pendingStyle.Render(pendingLabel))
		b.WriteByte('\n')
	}
	b.WriteString(m.input.View())
	b.WriteByte('\n')
	b.WriteString(footerStyle.Render(m.footerHelp()))
	return b.String()
}

// footerHelp is the live region's hint line: the keys that work right now, each
// with the one meaning it has right now (REQ: inline-rendering).
//
// It is state-aware because the keys are. enter and esc each mean one thing on
// the input and another in the button block, and while a reply is in flight only
// ctrl+c means anything at all (REQ: focus-and-keys,
// REQ: input-locked-while-pending). A single fixed line could not name them
// without also naming meanings that do not apply here — leaving the user to work
// out which, which is the ambiguity a hint exists to remove.
func (m Model) footerHelp() string {
	switch {
	case m.pending:
		return footerHelpPending
	case m.focus == focusButtons:
		return footerHelpButtons
	case len(m.liveButtonRows()) > 0:
		// Offered against the same thing down checks, so the hint cannot promise
		// a key that would do nothing.
		return footerHelpInputLive
	default:
		return footerHelpInput
	}
}

// --- rendering ---

// buttonRows returns the rows of buttons r's keyboard carries, skipping empty
// rows, or nil when it carries none this renderer can draw.
//
// botkb.Keyboard exposes only its type, so reading the buttons means asserting
// the concrete botkb.MessageKeyboard — in both its forms, since its methods take
// a value receiver and either a value or a pointer can be stored in the
// interface. A keyboard of some other implementation has no buttons this renderer
// can see; a nil or empty one has none to show. All of them answer nil, which is
// what keeps a reply with nothing focusable out of the live slot: it has nothing
// to wait for, so it is complete on arrival (REQ: scrollback-commit).
func buttonRows(r chat.Reply) [][]botkb.Button {
	var rows [][]botkb.Button
	switch kb := r.Keyboard.(type) {
	case *botkb.MessageKeyboard:
		if kb == nil {
			return nil
		}
		rows = kb.Buttons
	case botkb.MessageKeyboard:
		rows = kb.Buttons
	default:
		return nil
	}
	out := make([][]botkb.Button, 0, len(rows))
	for _, row := range rows {
		if len(row) > 0 {
			out = append(out, row)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// renderLiveReply renders the reply whose buttons are still focusable, marking
// the focused one when focus is in its button block.
func renderLiveReply(r chat.Reply, buttonsFocused bool, row, col int) string {
	if !buttonsFocused {
		return renderReply(r, buttonStyle, noFocus, noFocus)
	}
	return renderReply(r, buttonStyle, row, col)
}

// renderCommittedReply renders a reply as it goes into scrollback: its buttons
// inert text, none of them focused (REQ: scrollback-commit). It takes no focus
// argument at all, so no caller can carry a focus mark into the transcript.
func renderCommittedReply(r chat.Reply) string {
	return renderReply(r, inertButtonStyle, noFocus, noFocus)
}

// renderReply renders a reply's text and its rows of buttons. Buttons that are
// not focused take the unfocused style; focusRow and focusCol name the focused
// one, and noFocus for either means none is.
func renderReply(r chat.Reply, unfocused lipgloss.Style, focusRow, focusCol int) string {
	lines := []string{r.Text}
	for i, row := range buttonRows(r) {
		cells := make([]string, 0, len(row))
		for j, btn := range row {
			if i == focusRow && j == focusCol {
				cells = append(cells, focusedButtonStyle.Render(focusedMarkLeft+btn.GetText()+focusedMarkRight))
				continue
			}
			cells = append(cells, unfocused.Render("[ "+btn.GetText()+" ]"))
		}
		lines = append(lines, strings.Join(cells, " "))
	}
	return replyStyle.Render(strings.Join(lines, "\n"))
}

// renderError renders a Processor failure as the bot message it becomes in the
// transcript (REQ: errors-render-in-transcript).
//
// The wording is this package's own. The Processor hands back a bare error by
// contract (chat-messenger#req:errors-are-returned-not-formatted), so naming it
// as a failure falls to the renderer — the only layer that knows how an error
// should look on a terminal. The prefix is the same one internal/tui puts on its
// error text, for the same reason the styles below are restated rather than
// imported: the two packages read alike without depending on each other.
//
// It is marked as a failure in text and not by colour alone, for the reason the
// focused button is: lipgloss emits no colour when it is not writing to a
// terminal, so a block distinguished only by colour would reach a piped or
// copied transcript looking exactly like an ordinary reply.
func renderError(err error) string {
	return errStyle.Render(errorPrefix + err.Error())
}

// renderUserEcho renders what the user did, for scrollback: the text they
// submitted, or the label of the button they pressed (REQ: scrollback-commit).
//
// Both read the same way, because to the transcript they are the same thing —
// the user's turn — and a reader scanning back through it should not have to
// know which kind of input a line came from to follow the conversation.
//
// It carries the input's own prompt, so the committed line reads exactly as the
// input line it replaces did.
func renderUserEcho(s string) string {
	return userStyle.Render(inputPrompt + s)
}

// --- styles ---
//
// Restated here rather than imported from internal/tui: those are that
// package's unexported vars, and the two packages stay independent.

var (
	userStyle = lipgloss.NewStyle().Faint(true)

	// replyStyle frames a bot message so the transcript reads as a conversation
	// rather than a wall of lines. The border is dimmed: it groups the message
	// with its buttons without competing with them for attention.
	//
	// Its characters are runes, not ANSI, so they survive a non-terminal writer
	// and appear in View() — which is why the tests that read committed blocks
	// look for their content rather than matching a whole frame.
	replyStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)

	// A button carries its state twice over: in glyphs, which any terminal
	// shows and a test can read, and in colour, which only a terminal shows.
	// The glyphs are the carrier of record (see focusedMarkLeft); these styles
	// are the part that makes it look like a button.
	buttonStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	inertButtonStyle   = lipgloss.NewStyle().Faint(true)
	focusedButtonStyle = lipgloss.NewStyle().Bold(true).Reverse(true)
	footerStyle        = lipgloss.NewStyle().Faint(true).Padding(0, 1)
	pendingStyle       = lipgloss.NewStyle().Faint(true)
	errStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

// errorPrefix names a committed failure as one (renderError). It carries the
// meaning on its own, without the colour errStyle adds: a transcript that has
// been piped, copied or redirected has no colour left in it, and the reader still
// has to be able to tell a failure from a reply.
const errorPrefix = "Error: "

// inputPrompt marks the input line, and with it the committed echo of a line
// submitted from there (renderUserEcho), so the two cannot drift apart.
const inputPrompt = "> "

// noFocus is a button position no button occupies. It renders a block with
// nothing focused, which is how a committed reply's inert buttons render.
const noFocus = -1

// The focused button is marked with glyphs rather than by colour alone. Colour
// is the wrong carrier for the only difference that matters here: a monochrome
// terminal drops it, and lipgloss emits none at all when it is not writing to a
// terminal, which would leave the mark invisible to a test. Both marks are two
// display cells wide, like the "[ " and " ]" they replace, so a row of buttons
// does not shift as focus moves along it.
const (
	focusedMarkLeft  = "▶ "
	focusedMarkRight = " ◀"
)

// The hint lines footerHelp chooses between, one per state the key table has.
// No one of them is a substring of another, so a test can assert both the hint a
// state shows and the hints it does not.
const (
	footerHelpInput     = "enter send · esc quit · ^c quit"
	footerHelpInputLive = "enter send · ↑ buttons · esc quit · ^c quit"
	footerHelpButtons   = "enter press · ↑↓←→ move · esc back · ^c quit"
	footerHelpPending   = "waiting for a reply · ^c quit"
)

// pendingLabel names what the typing indicator is waiting for. It sits beside
// the spinner: the words say what is happening, the motion says the session is
// working rather than hung (REQ: pending-is-visible).
const pendingLabel = "Sneat is typing…"
