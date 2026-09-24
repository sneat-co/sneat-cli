package pipeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrActionNotTrailing is returned by Finish when a well-formed
// <sneat-action> block parsed successfully but was NOT the trailing content
// of the reply -- non-whitespace prose followed it (S6 coordinator ruling
// SPLITTER: "a block followed by non-whitespace prose is NOT executed --
// only a trailing block counts"). The block itself is intentionally
// discarded (Action returns nil); the prose that invalidated it is still
// included in Finish's trailing text, so nothing is lost, only not acted on.
var ErrActionNotTrailing = errors.New("pipeline: <sneat-action> block was not the trailing content of the reply, ignored")

// startTag/endTag delimit the single semantic action block a main-LLM
// inference may emit at the end of its streamed answer, per the product
// convention in the sneat-chat MVP brief §4: the model streams user-facing
// text and may end with one <sneat-action>{json}</sneat-action> block.
const (
	startTag = "<sneat-action>"
	endTag   = "</sneat-action>"
)

// Action is the semantic action a main-LLM turn asks for, parsed out of its
// trailing <sneat-action> block. It never carries an entity ID: Reference is
// resolved against real data by pipeline.Resolver, exactly like a
// decision.Reference (never trust model-invented IDs).
type Action struct {
	Kind         string            `json:"kind"`
	Reference    string            `json:"reference,omitempty"`
	Pronoun      bool              `json:"pronoun,omitempty"`
	Slots        map[string]string `json:"slots,omitempty"`
	Presentation string            `json:"presentation,omitempty"`
}

// Splitter hides a <sneat-action>...</sneat-action> block from the visible
// transcript while a response streams, and parses it once the stream
// completes. It is a pure buffering state machine: Feed is called once per
// text delta and returns only the text that is safe to render immediately
// (a delta may split the start or end tag itself, so the last len(tag)-1
// bytes of plain text are always held back until they are confirmed not to
// be the beginning of a tag).
type Splitter struct {
	pending  string
	inAction bool
	actionOK bool
	action   Action
	err      error
	// postAction accumulates visible text emitted AFTER the most recently
	// parsed action block's closing tag (reset to empty by every new
	// parseAction call, success or failure). Non-whitespace content here at
	// Finish time means that block was not trailing -- see
	// ErrActionNotTrailing.
	postAction strings.Builder
}

// Feed appends the next streamed delta and returns the portion of it that is
// safe to render now. Text belonging to a <sneat-action> block is never
// returned.
func (s *Splitter) Feed(delta string) string {
	s.pending += delta
	var out strings.Builder
	for {
		if s.inAction {
			idx := strings.Index(s.pending, endTag)
			if idx < 0 {
				return out.String()
			}
			s.parseAction(s.pending[:idx])
			s.pending = s.pending[idx+len(endTag):]
			s.inAction = false
			continue
		}
		idx := strings.Index(s.pending, startTag)
		if idx < 0 {
			hold := partialTagSuffixLen(s.pending, startTag)
			safe := s.pending[:len(s.pending)-hold]
			out.WriteString(safe)
			s.recordPostAction(safe)
			s.pending = s.pending[len(s.pending)-hold:]
			return out.String()
		}
		out.WriteString(s.pending[:idx])
		s.recordPostAction(s.pending[:idx])
		s.pending = s.pending[idx+len(startTag):]
		s.inAction = true
	}
}

// recordPostAction tracks text that renders after the most recent parsed
// action block, for the S6 "only a trailing block counts" rule.
func (s *Splitter) recordPostAction(text string) {
	if s.actionOK {
		s.postAction.WriteString(text)
	}
}

// Finish flushes any trailing plain text (buffered because it could have
// been the start of a tag that never completed, or because it followed an
// already-parsed action block) and returns the parsed action, if the stream
// carried one that qualifies. Three cases return an error but still return
// EVERY byte of trailing text the stream produced -- S6 coordinator ruling
// SPLITTER: no data loss, ever, even when the action itself is discarded:
//   - an unterminated <sneat-action> block: everything since the (never
//     closed) start tag is shown as ordinary prose, Action is nil;
//   - a well-formed block with invalid JSON: reported, Action is nil;
//   - a well-formed, validly-parsed block that non-whitespace prose
//     followed (ErrActionNotTrailing): the block is discarded, but the
//     prose that invalidated it is included in trailing exactly as it
//     would have been shown had no action ever been emitted.
func (s *Splitter) Finish() (trailing string, action *Action, err error) {
	if s.inAction {
		// Unterminated: pending holds everything since the start tag (it
		// was never re-added to out because the loop returned early from
		// inside the `s.inAction` branch) -- show it as prose rather than
		// discarding it.
		trailing, s.pending = s.pending, ""
		return trailing, nil, fmt.Errorf("pipeline: stream ended inside an unterminated %s block", startTag)
	}
	trailing, s.pending = s.pending, ""
	s.recordPostAction(trailing)
	if s.err != nil {
		return trailing, nil, s.err
	}
	if s.actionOK {
		if strings.TrimSpace(s.postAction.String()) != "" {
			return trailing, nil, ErrActionNotTrailing
		}
		a := s.action
		return trailing, &a, nil
	}
	return trailing, nil, nil
}

// parseAction parses one <sneat-action> block's raw JSON body. The product
// convention is one block per turn AT THE END of the reply (coordinator
// ruling SPLITTER): a model that emits more than one -- e.g. it reconsiders
// mid-answer -- has its LAST block win, matching "the end of the reply is
// the model's final answer" rather than silently keeping the first. A
// successful later block also clears any error an earlier malformed block
// left, for the same reason. Every new block call also resets postAction:
// text after an EARLIER block does not disqualify a LATER, genuinely
// trailing one.
func (s *Splitter) parseAction(raw string) {
	s.postAction.Reset()
	var a Action
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		s.err = fmt.Errorf("pipeline: malformed %s block: %w", startTag, err)
		s.actionOK = false
		return
	}
	s.action = a
	s.actionOK = true
	s.err = nil
}

// partialTagSuffixLen returns the length of the longest suffix of s that is
// also a proper prefix of tag -- the bytes that must be held back because a
// future delta could complete them into tag.
func partialTagSuffixLen(s, tag string) int {
	max := len(s)
	if max > len(tag)-1 {
		max = len(tag) - 1
	}
	for l := max; l > 0; l-- {
		if strings.HasSuffix(s, tag[:l]) {
			return l
		}
	}
	return 0
}
