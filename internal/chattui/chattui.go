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
	// ctrl+c are ignored (REQ: input-locked-while-pending).
	pending bool

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
	in.Focus()
	return Model{
		proc:  proc,
		input: in,
		focus: focusInput,
	}
}

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

// Update handles the terminal's size, the always-available quit, submitting the
// input, and the Processor's answer, and passes everything else to the input.
//
// TODO(chat-tui Task 4): the rest of the key table (focus movement, esc) and the
// pending lock belong here and are not implemented — every key other than ctrl+c
// and enter currently reaches the input regardless of focus or pending, so a
// second submit can be made while the first is still in flight.
// TODO(chat-tui Task 5): pressing a focused button must commit the live reply
// and an echo of the button's label, then run proc.PressButton. The machinery is
// here: commitLive and renderUserEcho are what a press needs, and enter already
// dispatches on focus.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case repliesMsg:
		return m.receive(msg.replies)
	case errMsg:
		m.pending = false
		// TODO(chat-tui Task 6): render the error as a bot message in the
		// transcript and leave the session running
		// (REQ: errors-render-in-transcript). Until that lands the failure is
		// dropped on the floor: the turn answers with silence, which is wrong
		// and deliberately not faked here.
		_ = msg.err
		return m, nil
	case tea.KeyMsg:
		// ctrl+c quits from any focus, and even while a reply is in flight: a
		// slow backend must never trap the user (REQ: input-locked-while-pending).
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		// enter submits from the input. It means something else entirely from
		// the button block, which is Task 5's.
		if msg.Type == tea.KeyEnter && m.focus == focusInput {
			return m.submitText()
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
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
	return m, tea.Sequence(commit(blocks), sendText(m.proc, text))
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
		// Focus stays on the input: entering the button block is `down`
		// (REQ: focus-and-keys), not something a reply does to the user.
	}
	blocks := make([]string, 0, len(replies))
	for _, r := range replies {
		blocks = append(blocks, renderCommittedReply(r))
	}
	return m, commit(blocks)
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
	m.focus = focusInput
	m.row, m.col = 0, 0
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
	b.WriteString(m.input.View())
	b.WriteByte('\n')
	b.WriteString(footerStyle.Render(footerHelp))
	return b.String()
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
	return strings.Join(lines, "\n")
}

// renderUserEcho renders what the user did, for scrollback: the text they
// submitted, or — from Task 5 — the label of the button they pressed
// (REQ: scrollback-commit).
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
	userStyle          = lipgloss.NewStyle().Faint(true)
	buttonStyle        = lipgloss.NewStyle()
	inertButtonStyle   = lipgloss.NewStyle().Faint(true)
	focusedButtonStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	footerStyle        = lipgloss.NewStyle().Faint(true).Padding(0, 1)
)

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

// footerHelp is the live region's hint line (REQ: inline-rendering).
//
// TODO(chat-tui Task 4): the focus and button keys this must name are not
// implemented, so it lists only what works.
const footerHelp = "enter send · ^c quit"
