---
format: https://specscore.md/feature-specification
status: Implementing
---

# Feature: Chat AI Pipeline

> [SpecScore.**Studio**](https://specscore.studio): | [Explore](https://specscore.studio/app/github.com/sneat-co/sneat-cli/spec/features/chat-ai?op=explore) | [Edit](https://specscore.studio/app/github.com/sneat-co/sneat-cli/spec/features/chat-ai?op=edit) | [Ask question](https://specscore.studio/app/github.com/sneat-co/sneat-cli/spec/features/chat-ai?op=ask) | [Request change](https://specscore.studio/app/github.com/sneat-co/sneat-cli/spec/features/chat-ai?op=request-change) |
**Status:** Implementing
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

### Contacts

#### REQ: contacts-lookup-resolves-not-guesses

`pipeline.findOrShowContact` (find_contact/show_contact) MUST resolve a contact reference against real space data via `sneat-ai-backend/resolve.Resolve`, falling back to `Resolver`'s title search when `resolve.Resolve` finds nothing, and MUST NOT execute through `Executor` — these are read-only lookups. One resolved contact MUST render a `ContactCard` with no raw entity keys; several MUST render a `ContactsGrid` choice.

### Diagnostics

#### REQ: full-decision-trace-is-logged

`pipeline.Output.Trace` MUST carry `decision.Chain.Decide`'s full `Trace` (every provider's outcome and latency, not just which one decided) on every turn, and `chatapp`'s diagnostics logging MUST log it — including a labelled path (`deterministic` when the Sneat rules provider or no provider at all decided, `decision` when any other provider such as Jev decided) — behind the existing debug-only logger, never user text.

### Interactive shell

#### REQ: chatshell-cutover

`sneat chat` MUST run `internal/aichat/pipeline.Pipeline` through `strongo/aichat`'s `tui/chatshell.Model`, not a Sneat-specific terminal UI. Slash commands (`/spaces`, `/space`, `/contacts`, `/who-am-i`, `/version`, `/help`) MUST still route to the existing `chat.Processor` (chat-messenger#req:processor-seam) rather than being reimplemented.

#### REQ: controls-are-transcript-blocks

A presentation MUST render as a `tui/transcript.Block`: `ContactsGrid` MUST reuse `tui/grid` directly (the same generic grid DataTug uses) rather than a competing grid, except that a search narrowed to exactly one contact MUST render as a `controls.CardBlock` instead of a one-row grid. Every other list-shaped presentation (`day_calendar`, `week_calendar`, `happenings_list`, `todo_list`, `buy_list`) MUST render as `controls.ListBlock`. Both `ListBlock` and `CardBlock` expose `Current()` (`transcript.EntityBlock`) so the focused item can be pinned to the sidebar or resolved by a later pronoun.

#### REQ: card-edit-appends-not-replaces

`chat.Reply.Edit` (chat-messenger#req:card-edit) is a known, documented MVP limitation of the chatshell renderer: `strongo/aichat/tui/chatshell` has no in-place transcript-entry replacement API this round, so `chatapp.appendReplies` renders every reply -- `Edit` set or not -- as a new transcript block rather than replacing the pressed message's own block. A space→Contacts→← Spaces navigation therefore still works (each press still runs `chat.Processor.PressButton` and renders its reply), it just grows the transcript by one entry per hop instead of staying one message, unlike `chat-tui`'s (deprecated) in-place renderer. Replacing this with real in-place editing needs a chatshell API this Feature does not currently have; it is out of scope for the sneat-chat MVP.

#### REQ: sidebar-and-focus-feed-session-state

Pinning an entity to the chatshell sidebar (`Model.PinToSidebar`, driven by a control's own "+ add to sidebar" or `tui.AddToSidebarMsg`) MUST update `session.State.Sidebar` via the `SidebarObserver` callback, and a focused block's entity MUST be resolvable the same way a decision's pronoun reference resolves against `session.State.Candidates()` (chat-ai#req:resolver-never-trusts-a-model-id) -- focusing or pinning an entity is what makes "move it to Friday" or "mark it done" resolvable without the user repeating the entity's name.

#### REQ: calendar-and-todo-presentations-carry-real-data

A `day_calendar`/`week_calendar`/`happenings_list` presentation MUST render each happening's actual start (and end, when known) time, and a `todo_list`/`buy_list` presentation MUST render each item's done state — `pipeline.Output.HappeningRows`/`TodoRows` carry this data alongside `Entities` specifically so the rendered control is not title-only.

#### REQ: bot-keyboard-buttons-are-focusable

A `chat.Reply` carrying a `Keyboard` (a slash command's or a button press's own reply) MUST render as a focusable block whose buttons a user can navigate and press through the chatshell's normal focus/key handling, dispatching a data button through `chat.Processor.PressButton` and a text button through `SendText` -- the chat-messenger slash-command surface's buttons (e.g. `/spaces`' space picker) MUST keep working after the chatshell cutover, not degrade to plain text.

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

### AC: calendar-week-and-todo-views-show-real-data

**Requirements:** chat-ai#req:calendar-and-todo-presentations-carry-real-data

**Given** a space with one happening at a known time and a todo list with one done and one open item
**When** the user asks for today's calendar, this week's calendar, and their todos, through a headless `chatshell.Model`
**Then** the rendered day view, week view, and todo view each show the happening's real time and the todo's done/open marker, not just titles

### AC: bot-keyboard-button-press-dispatches-like-a-slash-command

**Requirements:** chat-ai#req:bot-keyboard-buttons-are-focusable

**Given** `/spaces` has rendered its space-picker buttons in a headless `chatshell.Model`
**When** the user focuses the buttons block and presses Enter over a space button
**Then** the press dispatches through `chat.Processor.PressButton` and the resulting space card appears in the transcript, the same outcome typing the equivalent command would produce

### AC: contact-lookup-resolves-or-offers-choices

**Requirements:** chat-ai#req:contacts-lookup-resolves-not-guesses

**Given** a space with a uniquely-named contact, two similarly-named contacts, and an unmatched name
**When** the user asks to find each of them by name
**Then** the unique name renders a `ContactCard`, the ambiguous name renders both candidates as a choice, and the unmatched name answers plainly that nothing was found -- none of the three executes an Executor action

## Out of Scope

Deferred to follow-on work (m11: kept honest against the current code, not the state at initial spec time):

- **The main-LLM leg's live network verification.** `pipeline.StreamRequest`/`Stream` are exercised against real `ai/openaicompat` and `ai/cloud` providers over `httptest` (scenarios 11/12), and `chatapp`'s free-form explain path (scenario 1) and no-LLM-configured path (scenario 13) are both exercised headlessly through the real `chatshell.Model`. A real OpenAI-compatible/Anthropic endpoint or a live `api.sneat.cloud` was not used.
- **Full F6/Shift+Right sidebar-toggle keyboard navigation exercised end-to-end.** Shift+Up/Down and Enter/"+" over a rendered block ARE now driven through the real `chatshell.Model.Update` key sequence in several tests (focusing an item, pinning to the sidebar, pressing a bot-keyboard button); F6 (sidebar visibility) and Shift+Right (moving focus into the sidebar itself) are not exercised by an `internal/chatapp` test — `tui/focus`'s and `tui/chatshell`'s own package tests cover the ring and those keys directly.
- **Firestore-backed happenings/todos/contacts readers' collection paths.** `internal/aichat/data`'s Firestore readers mirror `internal/firestoredb`'s existing contact-reading convention and are unit-tested against real `dbo4calendarius`/`dbo4listus` structs, but the collection PATH itself was not verified against a live or emulated space.
- **Recurring happening occurrence expansion is partial, not general.** A day/week view now excludes a recurring happening on a day its rule does not hit (S6), but only for a WEEKLY rule with explicit `Weekdays` — daily/monthly/yearly recurrence still falls back to being excluded rather than computed, and `HappeningsReader.Get`/`FindByTitle` still return the stored template slot, not a resolved next-occurrence; see `internal/aichat/data`'s and `controls.filterRecurringToWindow`'s doc comments.
- **Contact relationship-word resolution ("my mum", "my wife") only matches via the flat `RelatedAs` label, not the deeper cross-contact relationship graph.** `contacts.find_contact`/`show_contact` resolve through `sneat-ai-backend/resolve.Resolve` (m11: `internal/aichat/data.Contact` now carries `RelatedAs`/`Gender` from `dbo4contactus.ContactDbo`, so a word like "my wife" DOES resolve, via `resolve.Resolve`'s legacy-`RelatedAs` fallback when a contact's flat `relatedAs` field equals the role and its gender agrees). `resolve.Space.RelationsOf` (the deeper `dbo4linkage.WithRelatedAndIDs` cross-contact edge graph, e.g. contact A's own `related`/`relatedIDs` pointing at contact B) is still not wired -- a space that only links contacts through that graph, never setting the flat `relatedAs` label, still falls through to a plain name search; see `pipeline.findOrShowContact`'s doc comment.
- **A bot-keyboard button press always appends a new transcript entry.** `chat.Reply.Edit` (re-render the pressed card in place — e.g. a space card's own buttons replacing themselves) has no `chatshell` equivalent yet; see REQ: card-edit-appends-not-replaces above and `chatapp.appendReplies`'s doc comment.
- **Per-day adjustments and cancellations on a recurring happening are not reflected in any presentation.** `HappeningsReader`/`happeningRowsInWindow`/`DynamicBlocks` all project a recurring happening's UNMODIFIED rule (see the recurring-occurrence-expansion bullet above); a single occurrence individually rescheduled or cancelled via `dbo4calendarius`'s per-date Adjustments still shows at its original rule-computed time in a day/week/upcoming view and in the LLM's dynamic context, until `internal/aichat/data`/`sneatexecutor.go` read and apply Adjustments during projection.
- **Moving a recurring happening's single occurrence to a DIFFERENT calendar day is refused, not supported.** `sneatexecutor.rescheduleHappening` (fix round r4 review, BLOCKER) sends one `adjust_slot` call with `Date=`the original occurrence and the slot payload's own `Start`/`End` carrying the new time -- this LOOKS like it should support a cross-day move, but calendarius's real read paths never honour it (`calendar-day.ts`'s `joinRecurringsWithSinglesAndEmit` attaches the deviation to its `Date`, i.e. the ORIGINAL day, regardless of `Slot.Start`'s own date, and its timing badge reads only start/end TIME; `dbo4calendarius.Occurrences` ignores `Date` entirely too). Both `rescheduleHappening` and `rescheduleSummary` now refuse such a move outright ("I can only retime this occurrence on the same day...") rather than silently sending a mutation calendarius's own UI can never show correctly. Same-day retime, and moving the whole series (already out of scope), are unaffected.

Now DONE, no longer deferred (kept here only as history — see the requirements above for the current behaviour):

- Timezone handling: a slot's own `TimeZone`/`UTCOffset` is honoured when present, else the session's IANA zone (`Pipeline.TZ`, `--tz`/env override).
- A `/space` picker for the aichat pipeline: `chatapp.Submit` syncs the pipeline's active space from the slash-command `Processor.ActiveSpace()` on every turn, so `/space`/`/spaces` and the pipeline never disagree about which space is active.
- A reschedule/cancel confirmation and an ambiguous HAPPENING reference's choice list now render `controls.NewHappeningCard`/time-bearing `HappeningRow`s (the fully resolved new time/occurrence, computed before asking), not a raw-key card or a title-only list; an ambiguous TODO/CONTACT reference gets its own kind-appropriate presentation instead of always being labelled "Happenings".
- `internal/aichat/data`'s three Firestore readers (Happenings/Todos/Contacts) now share ONE `firestoredb.Session`/client per chat session (m6): `NewFirestoreHappenings`/`NewFirestoreTodos`/`NewFirestoreContacts` each take an already-open `*firestoredb.Session` instead of opening their own, and `chatapp.Run` builds one session and closes it once on shutdown (every return path, including an early `aiconfig.Build` failure).
- `data.Contact` carries dbo4contactus's human-readable fields (m11): first/last/nick/full name, gender, date of birth, the flat `RelatedAs` relationship label, and email/phone channels (primary first) -- reused via `firestoredb.Contact`'s existing `*dbo4contactus.ContactDbo`, never re-derived. `controls.NewContactCard` renders whichever of relationship/birthday/emails/phones a `ContactRow` actually has (never a raw entity key), covering `find_contact`/`show_contact`'s single-match card, `list_contacts`, and an ambiguous contacts choice list (`Pipeline.candidateContactRows`, one `ContactsReader.Get` per candidate).
- The occurrence a REFERENCE itself names ("cancel Friday's yoga", "move Friday's yoga to 16:00") is carried through resolution (fix round r4, M1): `Resolver.Resolve` attaches the matched day window to `EntityRef.Keys["date"]`, and `occurrenceAnchor` lets cancel/reschedule prefer it over `recurringAnchor`'s own "next occurrence from now" guess when the `when` slot itself carries no date. A week/day view's per-occurrence rows also carry their own `Keys["date"]` now, instead of one ref shared by every occurrence a recurring happening contributes to that window.
- The action phase (`chatapp`'s `OnStreamEvent` `EventCompleted` branch) now owns busy state for its own duration (fix round r4/r5, M2): a background `HandleAction` running after its stream already finished no longer lets the composer re-enable and accept a new turn while a possibly-destructive action is still executing. Fix round r5's review correction: `strongo/aichat/tui/chatshell`'s own `handleStreamDone` unconditionally clears busy the instant a stream's `DoneMsg` is handled (which fires right after `EventCompleted`, before the action work even starts) -- `OnStreamDone` now checks `h.pendingAction[id]` and re-asserts `SetBusy(true)`/`SetBusyCancel` for as long as that stream's action result hasn't landed. A stale `actionMsg` whose action actually ran (`m.parseErr == nil`) is also no longer dropped outright: `applyUndoDelta` still records `Previous`/undo and renders the result, skipping only `Focused`/`Selection`/`LastShown`/`Pending` (a newer, current turn may already hold its own fresher values there).
- "cancel Wednesday's yoga" against a happening that only recurs Mon/Fri now refuses with a specific reason ("Yoga doesn't happen on Wednesday.") instead of resolving to a phantom occurrence or falling back to a generic "couldn't find" text that would make a real happening sound nonexistent (fix round r5 minor). `Resolver.Resolve`'s happening branch checks a checkable (weekly, explicit `Weekdays`) recurring rule against the reference's named day via a new `excludeWrongWeekday`; a rule this MVP slice cannot check (daily/monthly/yearly, or no `Weekdays`) is still conservatively kept, matching `filterByWindow`'s own existing scope.
- `SneatExecutor.Now`/`Pipeline.Now`/`Resolver.Now` resolve "now" in the session's `--tz`/`SNEAT_TZ` zone (fix round r4, m1), not the process's own -- previously only DISPLAY timestamps honoured `--tz` while "today"/"this week"/`recurringAnchor` kept resolving against the server's own zone.
- A sidebar pin from a space the session has since left is excluded from pronoun resolution and from the LLM's session context (fix round r4, m3) -- `applySpaceChange` deliberately keeps other-space pins visible (so switching back doesn't lose them), but neither `Resolver.resolvePronoun` nor `Pipeline.sessionBlock` acts on or names one anymore.
- The LLM's dynamic todo context now includes the buy list alongside the do list (combined, capped together, sorted not-done first), and the calendar context projects a recurring happening's real upcoming occurrence date via the same projection a calendar presentation uses, not its stored template date.
- Switching the active space (`/space` or a space button) now clears `Focused`/`Selection`/`LastShown`/`Pending`/`Previous` so a bare pronoun or "undo" can never resolve against an entity from the space just left; sidebar pins are kept (not dropped) and are never actable across spaces because `Resolver`/`SneatExecutor` refuse a cross-space target defensively.

## Open Questions

None at this time.

---
*This document follows the https://specscore.md/feature-specification*
