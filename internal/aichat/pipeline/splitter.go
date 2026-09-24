package pipeline

import (
	"encoding/json"
	"fmt"
	"strings"
)

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
			s.pending = s.pending[len(s.pending)-hold:]
			return out.String()
		}
		out.WriteString(s.pending[:idx])
		s.pending = s.pending[idx+len(startTag):]
		s.inAction = true
	}
}

// Finish flushes any trailing plain text (buffered because it could have
// been the start of a tag that never completed) and returns the parsed
// action, if the stream carried a well-formed one. An unterminated
// <sneat-action> block is reported as an error; a well-formed block with
// invalid JSON is reported as an error too, with Action left nil -- callers
// treat both like "no action" and still show the text collected so far.
func (s *Splitter) Finish() (trailing string, action *Action, err error) {
	if s.inAction {
		return "", nil, fmt.Errorf("pipeline: stream ended inside an unterminated %s block", startTag)
	}
	trailing, s.pending = s.pending, ""
	if s.err != nil {
		return trailing, nil, s.err
	}
	if s.actionOK {
		a := s.action
		return trailing, &a, nil
	}
	return trailing, nil, nil
}

func (s *Splitter) parseAction(raw string) {
	if s.actionOK || s.err != nil {
		// Only one action block per turn is part of the convention; a second
		// one is ignored rather than overwriting the first silently.
		return
	}
	var a Action
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		s.err = fmt.Errorf("pipeline: malformed %s block: %w", startTag, err)
		return
	}
	s.action = a
	s.actionOK = true
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
