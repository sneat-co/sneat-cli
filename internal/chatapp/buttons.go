// S10: restores chat-messenger's bot-keyboard behaviour (a Reply's
// Keyboard, e.g. /spaces' space-picker buttons) as a focusable
// transcript.Block, which the earlier tui/chatshell cutover left as plain
// text only (see the now-removed comment in handleSlash: "no chatshell
// equivalent in this MVP slice"). buttonsBlock renders a botkb.Keyboard's
// rows, arrow keys move a row/col cursor, and Enter activates the button
// under it via buttonActivatedMsg -- handler.OnMsg does the actual dispatch
// (chat.Processor.PressButton for a data button, chat.Processor.SendText for
// a text button), the same "Block emits, handler acts" shape as
// grid.RowActivatedMsg and controls.ItemActivatedMsg.
package chatapp

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/bots-go-framework/bots-go-core/botkb"
	"github.com/tuigoff/tuigoff/pkg/transcript"
)

// buttonActivatedMsg is emitted when Enter is pressed over a buttonsBlock's
// cursor button.
type buttonActivatedMsg struct {
	Button botkb.Button
}

// buttonsBlock renders one Reply's Keyboard. It implements transcript.Block
// (not EntityBlock: a button is not a session.EntityRef).
type buttonsBlock struct {
	rows     [][]botkb.Button
	row, col int
}

// newButtonsBlock builds a buttonsBlock from kb, or nil when kb is nil, not
// a *botkb.MessageKeyboard (the only Keyboard implementation this codebase
// builds -- see internal/chat), or has no rows -- a caller checks for nil
// before calling AppendBlock, the same way it already checks r.Keyboard !=
// nil.
func newButtonsBlock(kb botkb.Keyboard) *buttonsBlock {
	mk, ok := kb.(*botkb.MessageKeyboard)
	if !ok || mk == nil || len(mk.Buttons) == 0 {
		return nil
	}
	return &buttonsBlock{rows: mk.Buttons}
}

func (b *buttonsBlock) Focusable() bool { return b.currentButton() != nil }

func (b *buttonsBlock) currentButton() botkb.Button {
	if b.row < 0 || b.row >= len(b.rows) {
		return nil
	}
	row := b.rows[b.row]
	if b.col < 0 || b.col >= len(row) {
		return nil
	}
	return row[b.col]
}

func (b *buttonsBlock) clampCol() {
	row := b.rows[b.row]
	if b.col >= len(row) {
		b.col = len(row) - 1
	}
	if b.col < 0 {
		b.col = 0
	}
}

func (b *buttonsBlock) Update(msg tea.Msg) (transcript.Block, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return b, nil
	}
	switch key.String() {
	case "up", "k":
		if b.row > 0 {
			b.row--
			b.clampCol()
		}
	case "down", "j":
		if b.row < len(b.rows)-1 {
			b.row++
			b.clampCol()
		}
	case "left", "h":
		if b.col > 0 {
			b.col--
		}
	case "right", "l":
		if b.row >= 0 && b.row < len(b.rows) && b.col < len(b.rows[b.row])-1 {
			b.col++
		}
	case "enter":
		if btn := b.currentButton(); btn != nil {
			return b, func() tea.Msg { return buttonActivatedMsg{Button: btn} }
		}
	}
	return b, nil
}

func (b *buttonsBlock) View(width int, focused bool) string {
	var sb strings.Builder
	for ri, row := range b.rows {
		if ri > 0 {
			sb.WriteString("\n")
		}
		for ci, btn := range row {
			label := "[ " + btn.GetText() + " ]"
			if focused && ri == b.row && ci == b.col {
				// Shared-look cutover: a plain text cursor marker, not a
				// lipgloss.Reverse fill -- buttonsBlock is product content
				// inside a themed card (tui/theme.Card already shifts the
				// whole card's fill/accent-bar on focus); this row has no
				// close tui/theme counterpart of its own (SelectedRow is for
				// vertical list rows), so it stays colour-free rather than
				// hand-rolling a lipgloss style.
				label = "› " + label
			}
			sb.WriteString(label)
			if ci < len(row)-1 {
				sb.WriteString(" ")
			}
		}
	}
	return clampWidth(sb.String(), width)
}

// clampWidth matches internal/aichat/controls's own helper -- kept as a
// small unexported duplicate rather than exporting controls' version, since
// this package must not grow a dependency the other direction. It clamps
// each line independently (ansi.Truncate, a pure content operation) so a
// multi-line View is never widened past width on any one line.
func clampWidth(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "")
	}
	return strings.Join(lines, "\n")
}
