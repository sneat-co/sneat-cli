---
name: sneat
description: |
  ALWAYS use this skill — do not ask "would you like me to?" — whenever the
  user states or requests anything about shopping/lists, calendar/schedule,
  appointments, activities, contacts or birthdays, in English, Russian or a
  mix. Bare statements are requests to act: "Buy shoes", "Vasilisa basketball
  Friday", "Vanya's birthday is Friday", "Dentist Friday", "Купить ботинки",
  "У Василисы стоматолог в пятницу", "What do I need to buy for Vasilisa?".
  It turns what the user said into a Sneat action through the `sneat` CLI
  (`sneat action new/add/commit`, `sneat context`, `sneat query`); Sneat
  validates, asks only what is missing, and returns links to what changed.
---

# Sneat

You understand language. **Sneat understands Sneat.** Never decide yourself
who "Vasilisa" is, which date "Friday" is, or whether an action is complete —
send what you understood, read the validator's answer, ask exactly the
questions it returns, patch, commit.

```text
user text → semantic JSON → sneat action new → validation (questions?)
          → ask user → sneat action add <act_id> → validation → sneat action commit <act_id>
          → reply with what was done + links
```

## Act, don't ask whether to act

A statement like "Buy shoes" or "Vanya's birthday is Friday" IS the request.
Start the action immediately; the only questions you ask are the ones Sneat
returns in `validation.questions`. Never offer a menu of things you could do.

## Prerequisites

`sneat` CLI on PATH and signed in (`sneat whoami`; if not, ask the user to run
`sneat auth login` themselves). Default space is the family space; pass
`--space <id>` only when the user names another space.

## Step 0 — read early (cheap)

Run `sneat context` once per conversation (or when unsure). It returns the
Space's contacts (ids, names, gender, dob), lists, recurring happenings,
today's date and weekday, timezone and preferred language. Use it to
decompose confidently; do **not** paste it back to the user.

## Step 1 — decompose the utterance into a semantic action

Detect the message language (`en`, `ru`, or `mixed`). Keep the user's original
words in every `text` field with its `language`; add `localized.en` (or
`localized.ru`) when you can translate. IDs, dates, weekdays and recurrence
are language-neutral.

Four kinds — pick one:

| kind | when | shape |
|---|---|---|
| `buy` | things to buy / add to a shopping list | `operations[]` each with `objects[]`, optional `contacts[]`, `deadline`, `listID` |
| `schedule` | activity, appointment, class, event | `schedule{activity, kind?, contacts[], provider?, repeats?, slots[]}` |
| `birthday` | someone's birthday | `birthday{contact, date, year?}` |
| `clear` | empty a whole list | `operations[]` with exactly one entry, `listID` only |

Rules that matter:

- **Plurality and grouping.** "shoes and socks for Alice and Vasilisa" is ONE
  operation with two objects and two contacts. "shoes for Alice and a
  basketball for Vasilisa" is TWO operations. Never flatten who-gets-what.
- **Contacts are mentions.** Send `{"mention":"Василисе"}` exactly as
  written (any case, declension, nickname, "Mum", "папа", "me"). Sneat
  resolves them and returns candidates when ambiguous.
- **Dates are structure.** `{"weekday":"fr"}`, `{"date":"2026-09-30"}`,
  `{"relative":"end_of_month"}`, or anchors
  `{"before":{"birthdayOf":{"mention":"Vanya"}}}` /
  `{"before":{"nextOccurrenceOf":{"activity":{"text":"basketball"},"contact":{"mention":"Vasilisa"}}}}`.
  Only if you truly cannot structure it, send `{"text":"до пятницы"}`.
- **Times as HH:MM** (24h). "6pm" → `"18:00"`; "3 часа дня" → `"15:00"`.
  If the user gives an ambiguous bare hour ("at 6"), send `"6"` and let
  Sneat assume/warn.
- **Recurrence is never guessed.** "Vasilisa basketball Friday" → slot
  `{"weekday":"fr"}` with no `repeats` and no `time`; Sneat will ask. Say
  `repeats:"weekly"` only when the user said every/каждую.
- Several weekdays with different times ("Wed 3pm, Fri 6pm, Sat 10am") are
  **one** schedule with three slots.
- **`clear` empties a list and is never inferred.** Use it only for an
  explicit instruction to empty one ("clear the groceries list", "очисти
  список продуктов"). Send one operation with `listID` when the user named a
  list, or no `listID` at all for the default shopping list. It cannot remove
  a single item: "take the milk off the list" is not a `clear`, and Sneat
  refuses one carrying `objects`. Never use it to "start fresh" on your own
  initiative.
- Do not invent optional fields (duration, listID, kind); Sneat defaults them.

Get the full field list, importance levels and 15 worked EN/RU examples with
`sneat action schema` (or see [reference/examples.md](reference/examples.md)).

## Step 2 — submit and read the validation

```bash
sneat action new --utterance "Vasilisa basketball Friday" --language en \
  --json '{"kind":"schedule","schedule":{"activity":{"text":"basketball","language":"en"},"contacts":[{"mention":"Vasilisa"}],"slots":[{"weekday":"fr"}]}}'
```

The reply carries `actionID` (`act_…`) and `validation`:

- `validation.canCommit` — **only Sneat decides this**;
- `validation.questions[]` — already prioritised and combined (e.g. "What
  time is basketball on Friday, and is it every week or just this once?").
  Ask them verbatim-in-spirit, in the reply language, at most two at a time;
  `blocking:false` questions are optional — mention them once, never insist;
- `validation.issues[]` — `warn` items (e.g. "I read 6 as 18:00") must be
  mentioned to the user; `info` items may be;
- `validation.semantic` — the resolved candidate (contact IDs, absolute
  dates). Keep working with the same `actionID`.

## Step 3 — patch the SAME action with what the user adds or corrects

Send only the delta. Scalars replace, contacts/objects/slots merge by key:

```bash
sneat action add act_7f3k9m2x --utterance "Every Friday at six" \
  --json '{"schedule":{"slots":[{"weekday":"fr","time":"6","repeats":"weekly"}]}}'
```

Ambiguous contact → the question comes with `options[{id,label}]`; patch with
the chosen `contactID`: `{"schedule":{"contacts":[{"mention":"Alice","contactID":"c123"}]}}`.
"Add it anyway" after a duplicate warning → `{"overrides":["duplicate_happening"]}`.

## Step 4 — commit when allowed

```bash
sneat action commit act_7f3k9m2x
```

Commit only when `canCommit` is true (the CLI exits with code 2 and prints
the pending question otherwise). Committing twice is safe (idempotent).
`result.entities[]` and `result.links[]` tell you what was created and where.

**Corrections after commit** use the same action: "Actually Saturday is at
11" → `sneat action add act_… --json '{"schedule":{"slots":[{"weekday":"sa","time":"11:00"}]}}'`
then `sneat action commit act_…` amends the existing happening (no
duplicate). "This Saturday at 12 only" → `{"schedule":{"exceptions":[{"weekday":"sa","time":"12:00"}]}}`.
Cancelling one occurrence is not supported yet; say so and offer the calendar link.

## Step 5 — reply

Reply in the language of the user's message (else their preferred language,
else English). State what was done in one or two sentences, show the original
wording (e.g. "молоко (milk)"), and always include the returned links so the
user can check or edit in Sneat. Never claim success without a commit
result.

## Queries

"What do I need to buy for Vasilisa?" / "Что нужно купить Василисе?" /
"What do I need to do before the end of the month?":

```bash
sneat query --json '{"contacts":[{"mention":"Василисе"}],"before":{"relative":"end_of_month"}}'
```

Answer from `items[]` (with `deadline`, `link`) and `happenings[]` (with
`next`, `link`).

## Confirmation policy

Creating list items, happenings and birthdays is safe and reversible: commit
without asking for confirmation once validation passes. Confirm only when
Sneat asks (ambiguity, duplicate) or the user seems unsure. Never call the
older `sneat convo` sandbox for real data.

**`clear` is the exception.** It deletes every item in a list and there is no
undo. Say which list and how many items will go, and commit only once the
user has confirmed — even when validation passes. The commit result names
every deleted item, so quote them back if the user asks what was lost.

## When something is missing from the CLI

Report the gap plainly; do not simulate the operation or write to Sneat by
other means.
