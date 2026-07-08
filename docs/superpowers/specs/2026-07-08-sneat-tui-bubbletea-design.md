# Sneat Interactive TUI (bubbletea) — Design

**Goal:** An interactive terminal UI to browse spaces, a space's members and contacts, and a contact card, launched from `sneat ui` or `sneat spaces --ui`.

## Entry points

- `sneat ui` — dedicated command, starts at the Spaces screen.
- `sneat spaces --ui` and `sneat space list --ui` — same program via a `--ui` flag.

The program only runs when stdin **and** stdout are a TTY; otherwise it errors clearly (`interactive UI requires a terminal`). Readers come from the existing `Env` (`NewSpacesReader`, `NewContactsReader`), so no new data plumbing.

## Architecture

New package `internal/tui`.

- `Model` (root) holds: navigation `stack []screen`, shared `spacesReader`/`contactsReader`, `uid`, terminal `width`/`height`, a spinner, and a per-space contact cache (`map[string][]firestoredb.Contact`) so switching Members↔Contacts within a space does not refetch.
- `screen` interface:
  - `Init(m *Model) tea.Cmd` — kick off any data load.
  - `Update(m *Model, msg tea.Msg) (screen, tea.Cmd)` — handle screen-local messages/keys; returns the (possibly mutated) screen.
  - `View(m *Model) string`
  - `Title() string` — breadcrumb/header label.
- Root `Update` handles global concerns first: window resize, quit (`q`, `ctrl+c`), and **back** (`esc`, `left`) which pops the stack and quits when the stack has one screen. Everything else delegates to the top screen. Descending pushes a new screen.
- Data loads are async via `tea.Cmd` returning typed messages (`spacesLoadedMsg`, `contactsLoadedMsg`, `errMsg`); a spinner shows while a screen is loading.

## Screens

1. **Spaces** (`spacesScreen`): `bubbles/list` of the user's spaces (title; description = `type · status`). `Enter`/`→` pushes the Space screen for the selected space brief.
2. **Space** (`spaceScreen`): header renders space details (title, type, status, id). Below it a menu list with two items: **Members** and **Contacts (N)** (N = contact count once loaded). `Enter` on Members pushes a Contacts screen filtered to members; on Contacts pushes the full list.
3. **Contacts** (`contactsScreen`): one type used for both menu items, parameterized by `membersOnly bool`.
   - `membersOnly`: include only contacts whose roles contain `member`; the `member` role is **excluded** from each row's displayed roles.
   - otherwise: all contacts, all roles shown.
   - `Enter`/`→` pushes the Contact card.
4. **Contact card** (`contactCardScreen`): read-only detail for one contact — name/title, type, gender, age group, status, roles, emails, phones (from the `dbo4contactus.ContactDbo`).

## Navigation

- `↑`/`↓` (and `j`/`k`): move selection (bubbles/list default).
- `Enter` / `→`: descend / open.
- `Esc` / `←`: back to parent screen; **exit the app when on the Spaces screen**.
- `q` / `Ctrl+C`: quit anywhere.
- `/`: bubbles/list incremental filter (kept); paging via default keys.

## Data & filtering

- Spaces come from `SpacesReader.ListSpaces(ctx, uid)` (`map[string]any` keyed by space id; each value a brief with `title`/`type`/`status`/`roles`).
- Contacts come from `ContactsReader.ListContacts(ctx, spaceID)` (`[]firestoredb.Contact`, each wrapping `*dbo4contactus.ContactDbo`). Cached per space in the model.
- Member filter: `hasRole(contact, "member")`. Displayed roles: `roles minus "member"`.

## Testing

`internal/tui` unit tests drive `Model.Update` with fake in-memory readers and synthetic key messages (no real TTY):

- Navigation: spaces → space → members → card; `Esc` pops each level; `Esc` at Spaces returns `tea.Quit`.
- Member filtering: only members listed; `member` omitted from displayed roles; full Contacts list unfiltered.
- Loading: `contactsLoadedMsg` populates the list; a second visit to the same space uses the cache (reader called once).
- Error: `errMsg` renders an error line without crashing.

Pure helpers (`filterMembers`, `displayRoles`, `spaceItemsFrom`, `contactItemsFrom`) are tested directly.

## Files

- `internal/tui/tui.go` — `Model`, `New`, root `Update`/`View`, `screen` interface, nav helpers, messages, spinner.
- `internal/tui/spaces_screen.go`, `space_screen.go`, `contacts_screen.go`, `contact_card.go`.
- `internal/tui/*_test.go`.
- `cmd/sneat/commands/spaces.go` — `--ui` flag + launch; new `Ui(env)` command wired in `main.go`.
- `cmd/sneat/commands/root.go` / `main.go` — an `Env` launcher hook (`RunTUI`) so the command stays unit-testable and the tui package is injected.
- `go.mod` — promote `bubbletea` and `bubbles` to direct dependencies.

## Out of scope (this iteration)

No mutations (add/delete/edit) inside the TUI — browse-only. Calendar/happenings not included. These can layer on later.
