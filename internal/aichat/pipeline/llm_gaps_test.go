package pipeline

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"strings"
	"testing"

	"github.com/strongo/aichat/ai"
	"github.com/strongo/aichat/ai/ctxmgr"
	"github.com/strongo/aichat/ai/decision"
	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/data"
	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

// TestDynamicBlocks_ReaderErrors covers the calendar and contacts scopes'
// own read-error branches (each drops that scope's block rather than
// erroring the whole call).
func TestDynamicBlocks_ReaderErrors(t *testing.T) {
	wantErr := errors.New("boom")
	p := Pipeline{
		Readers: data.Readers{
			Happenings: errFindByTitleHappenings{err: wantErr},
			Contacts:   errContactsReader{err: wantErr},
		},
		Now: fixedNow,
	}
	blocks := p.DynamicBlocks(context.Background(), "sp1", []string{sneatdomain.ModuleCalendar, sneatdomain.ModuleContacts}, false)
	for _, b := range blocks {
		if b.Name == "relevant_happenings" || b.Name == "contacts" {
			t.Fatalf("expected the errored scope's block to be dropped, got %+v", b)
		}
	}
}

// TestDynamicBlocks_TodosCapped covers the todo/buy combined cap's own
// truncation branches (distinct from TestDynamicBlocks_CapsHappeningsAndTodos,
// which only caps happenings).
func TestDynamicBlocks_TodosCapped(t *testing.T) {
	var items []data.Todo
	for i := 0; i < maxDynamicItems+10; i++ {
		items = append(items, data.Todo{SpaceID: "sp1", ID: fmt.Sprintf("t%d", i), List: data.ListKindDo, Title: fmt.Sprintf("T%d", i)})
	}
	p := Pipeline{Readers: data.Readers{Todos: &data.FakeTodos{Items: items}}, Now: fixedNow}
	blocks := p.DynamicBlocks(context.Background(), "sp1", []string{sneatdomain.ModuleTodo}, false)
	block := findBlock(t, blocks, "todos")
	if got := strings.Count(block.Text, "- [todo] T"); got != maxDynamicItems {
		t.Fatalf("listed %d todos, want the cap of %d", got, maxDynamicItems)
	}
	if !strings.Contains(block.Text, fmt.Sprintf("(showing the first %d)", maxDynamicItems)) {
		t.Fatalf("text = %q, want the truncation notice", block.Text)
	}
}

// TestEntityTitles_EmptyTitleFallsBackToType covers the per-ref fallback
// directly.
func TestEntityTitles_EmptyTitleFallsBackToType(t *testing.T) {
	got := entityTitles([]session.EntityRef{{Type: "happening"}, {Title: "Named"}})
	if got != "happening, Named" {
		t.Fatalf("got = %q", got)
	}
}

// TestStreamRequest_CtxMgrSelectAllAndSelect cover StreamRequest's own
// p.CtxMgr branches: SelectAll (d==nil) and Select (d!=nil), using a real
// *ctxmgr.Manager (the same construction chatapp.go's Run uses) rather than
// the nil-CtxMgr passthrough every other llm_test.go case relies on.
func TestStreamRequest_CtxMgrSelectAllAndSelect(t *testing.T) {
	mgr := ctxmgr.NewManager(ctxmgr.Policy{})
	p := Pipeline{Now: fixedNow, CtxMgr: mgr}

	req, _ := p.StreamRequest(context.Background(), "hello", &session.State{}, "sp1", nil, nil, nil)
	if len(req.Context) == 0 {
		t.Fatal("SelectAll path (d==nil): expected some context blocks")
	}

	d := &decision.Decision{RequiredScopes: []string{sneatdomain.ModuleCalendar}}
	req2, _ := p.StreamRequest(context.Background(), "hello", &session.State{}, "sp1", d, nil, nil)
	if len(req2.Context) == 0 {
		t.Fatal("Select path (d!=nil): expected some context blocks")
	}
}

// TestSplitStream_ConsumerStopsEarly covers every "yield returned false"
// branch: the flush before a stream error, the visible-text flush, the
// EventCompleted trailing flush, and the final plain yield -- each reached
// by a consumer that stops after its first callback.
func TestSplitStream_ConsumerStopsEarly(t *testing.T) {
	t.Run("stops on first text delta", func(t *testing.T) {
		src := func(yield func(ai.Event, error) bool) {
			if !yield(ai.Event{Type: ai.EventTextDelta, Text: "hello"}, nil) {
				return
			}
		}
		sp := &Splitter{}
		seq := splitStream(iter.Seq2[ai.Event, error](src), sp)
		calls := 0
		seq(func(ev ai.Event, err error) bool {
			calls++
			return false // stop immediately
		})
		if calls != 1 {
			t.Fatalf("calls = %d, want exactly 1 (the consumer stopped itself)", calls)
		}
	})

	t.Run("stops on the pre-error flush", func(t *testing.T) {
		// The first delta is ENTIRELY a possible tag prefix (no visible
		// leading text), so splitStream's own EventTextDelta branch yields
		// nothing for it (Feed returns "") and the loop just continues; the
		// error event is what makes Finish() flush that held-back text, so
		// the flush-before-error branch (not the ordinary text-delta
		// branch) is the very first callback the consumer sees.
		src := func(yield func(ai.Event, error) bool) {
			if !yield(ai.Event{Type: ai.EventTextDelta, Text: "<sneat-a"}, nil) {
				return
			}
			yield(ai.Event{}, errors.New("boom"))
		}
		sp := &Splitter{}
		seq := splitStream(iter.Seq2[ai.Event, error](src), sp)
		var got []ai.Event
		calls := 0
		seq(func(ev ai.Event, err error) bool {
			calls++
			got = append(got, ev)
			return false // stop on the flushed-text callback itself
		})
		if calls != 1 {
			t.Fatalf("calls = %d, want exactly 1", calls)
		}
		if len(got) != 1 || got[0].Type != ai.EventTextDelta || got[0].Text != "<sneat-a" {
			t.Fatalf("got = %+v, want the held-back text flushed before the error", got)
		}
	})

	t.Run("stops on the EventCompleted trailing flush", func(t *testing.T) {
		src := func(yield func(ai.Event, error) bool) {
			if !yield(ai.Event{Type: ai.EventTextDelta, Text: "Hello <sneat-a"}, nil) {
				return
			}
			yield(ai.Event{Type: ai.EventCompleted}, nil)
		}
		sp := &Splitter{}
		seq := splitStream(iter.Seq2[ai.Event, error](src), sp)
		var got []ai.Event
		seq(func(ev ai.Event, err error) bool {
			got = append(got, ev)
			// Let the first (visible "Hello ") callback through, then stop
			// exactly on the SECOND callback -- EventCompleted's own
			// trailing-flush yield -- rather than the plain EventCompleted
			// yield after it.
			return len(got) < 2
		})
		if len(got) != 2 {
			t.Fatalf("got = %+v, want exactly 2: the visible text, then the flushed trailing text (stopping there)", got)
		}
		if got[1].Text != "<sneat-a" {
			t.Fatalf("got[1] = %+v, want the flushed trailing tag bytes", got[1])
		}
	})

	t.Run("stops before the final plain yield", func(t *testing.T) {
		src := func(yield func(ai.Event, error) bool) {
			yield(ai.Event{Type: ai.EventCompleted}, nil) // no held-back text: Finish() flushes nothing
		}
		sp := &Splitter{}
		seq := splitStream(iter.Seq2[ai.Event, error](src), sp)
		var got []ai.Event
		seq(func(ev ai.Event, err error) bool {
			got = append(got, ev)
			return false
		})
		if len(got) != 1 || got[0].Type != ai.EventCompleted {
			t.Fatalf("got = %+v, want just the EventCompleted itself", got)
		}
	})
}

// TestUniqueStrings covers the dedup/empty-skip branch directly.
func TestUniqueStrings(t *testing.T) {
	got := uniqueStrings([]string{"a", "", "b", "a", "b", "c"})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("got = %v, want %v", got, want)
		}
	}
}
