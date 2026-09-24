---
format: https://specscore.md/feature-specification
status: Draft
---

# Feature: Chat AI Pipeline

> [SpecScore.**Studio**](https://specscore.studio): | [Explore](https://specscore.studio/app/github.com/sneat-co/sneat-cli/spec/features/chat-ai?op=explore) | [Edit](https://specscore.studio/app/github.com/sneat-co/sneat-cli/spec/features/chat-ai?op=edit) | [Ask question](https://specscore.studio/app/github.com/sneat-co/sneat-cli/spec/features/chat-ai?op=ask) | [Request change](https://specscore.studio/app/github.com/sneat-co/sneat-cli/spec/features/chat-ai?op=request-change) |
**Status:** Draft
**Source Ideas:** —

## Summary

`internal/aichat` is the sneat-chat MVP's AI processing pipeline: Sneat's decision taxonomy and deterministic rules on top of `strongo/aichat`'s shared `LLMProvider`/`DecisionProvider`/`ai/aiconfig`/`ai/ctxmgr`/`ai/diag` contracts, entity resolution against real Firestore/API data, pending-confirmation and undo bookkeeping via real sneat-go HTTP mutations (calendarius/listus), the `<sneat-action>` stream-splitter convention for a main-LLM turn, and flag/env/file configuration for Sneat AI Cloud and BYOK. `internal/chatapp` is its composition root: `sneat chat` runs it through `strongo/aichat`'s `tui/chatshell`, replacing `internal/chattui` entirely (deleted).

## Problem

`sneat chat` today (chat-messenger) is a deterministic slash-command processor with free text explicitly deferred. The sneat-chat MVP brief asks for a conversational layer that still prefers deterministic handling and a fast decision provider (Jev) before ever falling back to a main LLM call, resolves what the user refers to against real data rather than trusting a model-supplied ID, and never depends on Jev being available. Building that as Sneat-specific code entangled with a terminal renderer would make none of it reusable by DataTug or a future product; building it directly against `strongo/aichat`'s product-neutral contracts (`ai.LLMProvider`, `decision.Provider`, `decision.Chain`, `session.State`) keeps Sneat's part to naming (the taxonomy) and resolving (real data), which is what this Feature specifies.

## Behavior

### Decision taxonomy

#### REQ: taxonomy

`sneatdomain.Taxonomy()` MUST declare every module (`calendar`, `todo`, `contacts`, `general`), intent, presentation, data kind and entity type this pipeline understands, as a `decision.Taxonomy`, so any `decision.Provider` — the deterministic rules provider, a future Jev call, or a test double — validates against the same vocabulary (`decision.Validate`).

### Deterministic rules

#### REQ: rules-provider-first

The processing chain MUST run a deterministic `decision.Provider` (`internal/aichat/rules`, built on `strongo/aichat/ai/decision/rules`) before any other provider, per the founder's addendum: "a chain of decision providers and the first that decided we use." It MUST be a small exact-phrase table, not a regex NLP engine, and MUST abstain (not guess) on text it does not recognise.

#### REQ: confirmation-requires-pending

The rules provider's confirmation, rejection/cancellation and undo rules MUST fire only when `session.State.Pending` (confirmation/rejection) or `session.State.Previous` (undo) is set. An unambiguous "yes" with nothing pending MUST abstain rather than being misread as agreement to nothing.

### Pipeline

#### REQ: chain-first-decides

`pipeline.Pipeline.Turn` MUST run `decision.Chain.Decide` and, when a provider decides, dispatch on its `Interaction` and `Presentation`/`Module`+`Intent` without a main-LLM call. When no provider decides, `Turn` MUST return `Output.NeedsLLM = true` rather than guessing or erroring.

#### REQ: resolver-never-trusts-a-model-id

`pipeline.Resolver.Resolve` MUST resolve a `decision.Reference` against real data (`internal/aichat/data` readers) or, for a pronoun reference, against `session.State.Candidates()` filtered by entity type. It MUST NOT accept an entity ID from a `decision.Decision` or a `<sneat-action>` block — neither carries one, by contract.

#### REQ: ambiguous-reference-is-a-choice

When resolution finds more than one candidate, the pipeline MUST return them as a choice (`Output.Entities`) and MUST NOT execute, stage a pending action, or guess. When it finds none, it MUST ask the user to clarify.

#### REQ: destructive-actions-confirm

Rescheduling or cancelling a happening and deleting a todo MUST be staged as `session.State.Pending` with a human-readable `Summary` and MUST NOT execute until a deterministic or LLM-driven confirmation resolves it. A non-destructive resolved action (e.g. completing a todo) executes immediately.

#### REQ: undo-when-supported

An executed action whose `Executor.Execute` returns a non-nil undo action MUST be recorded as `session.State.Previous` with that `Undo`. `Turn`'s undo interaction MUST execute `Previous.Undo` when set and MUST answer that nothing can be undone otherwise.

#### REQ: sneat-action-splitter

`pipeline.Splitter` MUST hide a trailing `<sneat-action>{json}</sneat-action>` block from the text a main-LLM turn streams to the user, MUST tolerate the tags and JSON body splitting across any number of stream deltas, and MUST report an unterminated block or malformed JSON as an error rather than silently dropping or emitting it as visible text. `pipeline.Pipeline.HandleAction` MUST resolve and confirm/execute a parsed action through the same policy as a deterministic command (REQ: destructive-actions-confirm).

### Configuration

#### REQ: no-jev-still-works

`--no-jev` (mapped by `cmd/sneat/commands/chat.go` onto `aiconfig.Config.Decision.Provider = "disabled"`, and passed independently as `aiconfig.Deps.DisableCloudDecision`) MUST remove only the cloud decision provider from the chain; the deterministic rules provider (`aiconfig.Deps.ExtraDecision`) MUST remain, so every deterministic scenario keeps working with Jev disabled, unavailable, or never configured.

#### REQ: byok-independent-of-decision

`aiconfig.Build` MUST be able to select a BYOK main-LLM provider (`--ai-llm byok`, `--byok-protocol openai-compatible|anthropic`, `--byok-endpoint`, `--byok-model`, `--byok-api-key-env`) independently of whether the cloud decision provider is in the chain, so a user may use Sneat AI Cloud for Jev while answering with their own key.

### Mutations

#### REQ: mutations-via-sneat-go-http

Every action `pipeline.SneatExecutor` executes MUST go through a sneat-go HTTP endpoint (calendarius's `update_slot`/`cancel_happening`/`revoke_happening_cancellation`, listus's `list_items_create`/`list_items_set_is_done`/`list_items_delete`, via `internal/sneatapi`) and MUST NOT write Firestore directly. Reschedule and cancel-happening actions MUST read the happening's current state first (for the slot's duration and to build the undo action) rather than trusting the resolved reference's cached Title/Keys alone.

### Interactive shell

#### REQ: chatshell-cutover

`sneat chat` MUST run `internal/aichat/pipeline.Pipeline` through `strongo/aichat`'s `tui/chatshell.Model`, not a Sneat-specific terminal UI. Slash commands (`/spaces`, `/space`, `/contacts`, `/who-am-i`, `/version`, `/help`) MUST still route to the existing `chat.Processor` (chat-messenger#req:processor-seam) rather than being reimplemented.

#### REQ: controls-are-transcript-blocks

A presentation MUST render as a `tui/transcript.Block`: `ContactsGrid` MUST reuse `tui/grid` directly (the same generic grid DataTug uses) rather than a competing grid, except that a search narrowed to exactly one contact MUST render as a `controls.CardBlock` instead of a one-row grid. Every other list-shaped presentation (`day_calendar`, `week_calendar`, `happenings_list`, `todo_list`, `buy_list`) MUST render as `controls.ListBlock`. Both `ListBlock` and `CardBlock` expose `Current()` (`transcript.EntityBlock`) so the focused item can be pinned to the sidebar or resolved by a later pronoun.

#### REQ: sidebar-and-focus-feed-session-state

Pinning an entity to the chatshell sidebar (`Model.PinToSidebar`, driven by a control's own "+ add to sidebar" or `tui.AddToSidebarMsg`) MUST update `session.State.Sidebar` via the `SidebarObserver` callback, and a focused block's entity MUST be resolvable the same way a decision's pronoun reference resolves against `session.State.Candidates()` (chat-ai#req:resolver-never-trusts-a-model-id) -- focusing or pinning an entity is what makes "move it to Friday" or "mark it done" resolvable without the user repeating the entity's name.

## Acceptance Criteria

### AC: deterministic-scenarios-need-no-llm

**Requirements:** chat-ai#req:rules-provider-first, chat-ai#req:chain-first-decides, chat-ai#req:no-jev-still-works

**Given** the deterministic rules provider is the only provider in the chain
**When** the user sends "show my calendar today", "this week", "my todos", "contacts", or "help"
**Then** `Turn` answers with `NeedsLLM = false` and the matching presentation, with no main-LLM call

### AC: reference-resolution-never-guesses

**Requirements:** chat-ai#req:resolver-never-trusts-a-model-id, chat-ai#req:ambiguous-reference-is-a-choice

**Given** a space with two happenings whose titles both contain "dentist"
**When** a reschedule action references "dentist"
**Then** the pipeline returns both as candidates and does not stage a pending action or execute anything

### AC: pending-confirm-cancel-undo

**Requirements:** chat-ai#req:destructive-actions-confirm, chat-ai#req:undo-when-supported, chat-ai#req:confirmation-requires-pending

**Given** a destructive action has been resolved to one candidate
**When** the user replies "yes"
**Then** the executor runs exactly once, `session.State.Previous` is set with its `Undo`, and a later "undo" runs the undo action and clears `Previous`

### AC: action-block-splits-across-deltas

**Requirements:** chat-ai#req:sneat-action-splitter

**Given** a streamed main-LLM answer whose `<sneat-action>{...}</sneat-action>` block's tags and JSON body each arrive split across separate deltas
**When** the stream is fed through `Splitter.Feed` and finished with `Splitter.Finish`
**Then** the visible text excludes the block entirely and the action parses correctly

### AC: mutations-are-http-and-confirmed

**Requirements:** chat-ai#req:mutations-via-sneat-go-http, chat-ai#req:destructive-actions-confirm

**Given** a resolved reschedule, cancel, complete/reopen, add, or delete action
**When** it executes
**Then** `SneatExecutor` sends exactly the expected sneat-go HTTP request (method, path, body) and never touches Firestore directly

### AC: chatshell-renders-and-resolves-session-context

**Requirements:** chat-ai#req:chatshell-cutover, chat-ai#req:controls-are-transcript-blocks, chat-ai#req:sidebar-and-focus-feed-session-state

**Given** a headless `chatshell.Model` (no real terminal) with a focused happening, or an entity pinned to its sidebar
**When** a `<sneat-action>` referencing "it" is handled, or the model renders a deterministic Output
**Then** the focused/pinned entity resolves the pronoun and the matching control (`ListBlock`/`CardBlock`/`tui/grid`) appears in the transcript

## Out of Scope

Deferred to follow-on work:

- **The main-LLM leg's live network verification.** `pipeline.StreamRequest`/`Stream` are exercised against real `ai/openaicompat` and `ai/cloud` providers over `httptest` (scenarios 11/12), and `chatapp`'s stream wiring is exercised headlessly with no LLM configured (proving the clean "not available" path, scenario 13). A real OpenAI-compatible/Anthropic endpoint or a live `api.sneat.cloud` was not used.
- **Full Shift-Arrow/F6 keyboard navigation exercised end-to-end.** The headless chatshell tests drive `Submit`/`OnMsg`/`OnStreamEvent`/`PinToSidebar`/`HandleAction` directly rather than through the actual focus-ring key sequence (Shift+Up/Down/Left/Right, F6); `tui/focus`'s own package tests cover the ring itself.
- **Firestore-backed happenings/todos/contacts readers' collection paths.** `internal/aichat/data`'s Firestore readers mirror `internal/firestoredb`'s existing contact-reading convention and are unit-tested against real `dbo4calendarius`/`dbo4listus` structs, but the collection PATH itself was not verified against a live or emulated space.
- **Recurring happening occurrence expansion.** `HappeningsReader` returns a recurring happening's stored template slot, not a resolved next-occurrence; see `internal/aichat/data`'s doc comment.
- **Timezone handling.** `internal/aichat/data`'s slot date/time parsing and `pipeline.parseWhen` treat calendarius's date+time strings as the server's local `time.Location`, not the space's own stored timezone.
- **A `/space` picker for the aichat pipeline.** The (currently unwired) `chatapp.defaultSpaceID` picks an arbitrary one of the user's spaces (Go map iteration order); it does not read or drive the existing `/space`/`/spaces` active-space selection the slash-command Processor already has.

## Open Questions

None at this time.

---
*This document follows the https://specscore.md/feature-specification*
