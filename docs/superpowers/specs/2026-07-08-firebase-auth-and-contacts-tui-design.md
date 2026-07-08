# Sneat TUI: Firebase auth + spaces + Contacts — design

Date: 2026-07-08
Status: Draft for review

## 1. Overview & goals

Turn `sneat-cli` (this repo — a full-screen terminal UI; module currently still
named `sneat-tui`) from a UI-only scaffold into a working terminal client for
[Sneat.app](https://sneat.app), backed by the shared Firebase project
`sneat-eur3-1`.

A signed-in user can:

- Sign in with **Google** (primary), **GitHub**, or **email + password**.
- See their **spaces** and open one.
- Inside a space, use a left menu (**Contacts**, **Calendar**) with content in the
  main pane.
- **Contacts**: list, view, add, delete, edit (names, gender, roles) and manage
  comm channels (phone, email) — built out as far as the backend allows.
- **Calendar**: read-only list of recurring happenings.

### Priority / build order

1. **Google login working end-to-end** (the maintainer's own sign-in).
2. Spaces list + in-space shell (left menu + main pane).
3. Contacts **read** (list + detail).
4. Contacts **write** (add / delete / edit / roles / phone / email).
5. Email + password login.
6. GitHub login.
7. Calendar: recurring-happenings list.

Each phase is independently testable and shippable.

## 2. Data & auth model (established by research)

- One shared Firebase project **`sneat-eur3-1`**. Public web API key
  `AIzaSyCeQu1WC182yD0VHrRm4nHUxVf27fY-MLQ` (already in the deployed frontend) is
  sufficient for the Firebase Auth REST API.
- **Reads = direct Firestore** using the user's Firebase ID token as a Bearer
  token against the Firestore REST API; Firestore security rules enforce access.
- **Writes = sneat-go HTTP API** at **`https://api.sneat.cloud/v0/`** (prod; the
  base URL changed recently from `api.sneat.ws`) or `http://localhost:4300/v0/`
  in dev, with the ID token in `Authorization: Bearer`.

Firestore document paths:

- User's spaces: `users/{uid}` → `spaces` map (`IUserSpaceBrief`: title, type,
  `userContactID`, …).
- Contacts: `spaces/{spaceID}/ext/contactus/contacts/*` (flat list =
  `status == "active"` and `parentContactID == ""`). Space module doc
  `spaces/{spaceID}/ext/contactus` also holds a `contacts` briefs map + counts.
- Recurring happenings: `spaces/{spaceID}/ext/calendarius` → `recurringHappenings`
  map (keyed by happeningID).

DBO/DTO reuse (toolchain is Go 1.26.4, so the extensions' `go 1.26` is fine):

- `github.com/sneat-co/contactus/backend/dbo4contactus` (`ContactDbo`,
  `ContactBrief`) and `.../dto4contactus` (request bodies for writes).
- `github.com/sneat-co/calendarius/backend/dbo4calendarius`
  (`CalendariusSpaceDbo`, `CalendarHappeningBrief`, `HappeningDbo`).

Where a needed module is not tagged for `go get`, add a local `replace`
directive to the sibling repo path.

## 3. Architecture: host + extension TUI modules

Mirrors the backend extension pattern (`extension.Config` / `RegisterRoutes`).

Note: this repo is GitHub `sneat-cli` but its module/README currently say
`sneat-tui`. Phase 1 renames the module `github.com/sneat-co/sneat-tui` →
`github.com/sneat-co/sneat-cli` (go.mod + imports + README) to remove that
inconsistency. The empty `sneat-go-cli` repo is unrelated and retired/ignored.

### New shared module: `github.com/sneat-co/sneat-cli-core`

Imported by **both** the host and every extension `tui/` package (so extensions
never depend on the host app, symmetric with how backend extensions depend on
`sneat-go-core`). Contents:

- `config` — resolve project, API key, emulator hosts, API base URL
  (precedence: **flag > env > default**).
- `sneatauth` (client) — Firebase Auth REST: `signInWithIdp`,
  `signInWithPassword`, token refresh; provider-credential types.
- `oauth` — loopback browser flow for Google & GitHub → provider credential.
- `session` — token store (0600 JSON under `$XDG_CONFIG_HOME/sneat/`, default
  `~/.config/sneat/`) + transparent refresh before expiry.
- `firestoredb` (client) — Firestore REST: `GetDoc`, `RunQuery`, and a
  typed-value → struct decoder.
- `sneatapi` (client) — POST/DELETE to `/v0/...` mutation endpoints with Bearer.
- `tuicore` — the `ExtensionModule` interface + a `Services` bundle (auth token
  provider, firestore client, api client) + `SpaceContext` passed to views.

### Host: `github.com/sneat-co/sneat-cli` (this repo)

Owns auth, session, config, spaces selection, and the app shell (left menu +
main pane). Registers extension modules and mounts the selected one's view.

### Extension TUI modules (own `go.mod`, sibling to `backend/`)

- `github.com/sneat-co/contactus/tui` — Contacts view; reads Firestore, writes
  via API; reuses its own `dbo4contactus` / `dto4contactus`.
- `github.com/sneat-co/calendarius/tui` — recurring-happenings view.

Each exposes:

```go
func Module() tuicore.ExtensionModule
```

```go
type ExtensionModule interface {
    ID() string                 // "contactus"
    Title() string              // "Contacts"
    MenuItem() (label string, shortcut rune)
    NewView(space SpaceContext, services Services) tview.Primitive
}
```

Work spans repos: `sneat-cli-core` (new), `sneat-cli` (host, this repo),
`contactus`, `calendarius`. Each repo gets its own branch/PR.

## 4. Configuration & emulator switching

Uses the standard Firebase emulator env vars so it works with
`firebase emulators:start`.

| Concern | Flag | Env | Default |
|---|---|---|---|
| Project | `--project` | `SNEAT_FIREBASE_PROJECT` | `sneat-eur3-1` |
| Web API key | `--api-key` | `FIREBASE_API_KEY` | public web key |
| Auth emulator | `--auth-emulator` | `FIREBASE_AUTH_EMULATOR_HOST` | off |
| Firestore emulator | `--firestore-emulator` | `FIRESTORE_EMULATOR_HOST` | off |
| Both emulators | `--emulator` | — | auth `localhost:9099`, firestore `localhost:8080` |
| API base URL | `--api-base-url` | `SNEAT_API_BASE_URL` | `https://api.sneat.cloud/v0/` |
| Google OAuth client | `--google-client-id` | `SNEAT_GOOGLE_CLIENT_ID` | none (prod requires it) |
| Google OAuth secret | — | `SNEAT_GOOGLE_CLIENT_SECRET` | none |
| GitHub OAuth client | `--github-client-id` | `SNEAT_GITHUB_CLIENT_ID` | none |

## 5. Authentication

### 5.1 Google (Phase 1, primary)

**Prerequisite (prod only, maintainer action):** create a Google OAuth 2.0
Client ID of type **Desktop app** in the `sneat-eur3-1` GCP project
(console.cloud.google.com → APIs & Services → Credentials). Provide its
`client_id`/`client_secret` via config. The `client_secret` for a Desktop client
is not a true secret; the loopback + PKCE flow is safe without server-side
secrecy.

Flow:

1. Start a loopback HTTP server on `http://localhost:<random>`.
2. Open the browser to Google's authorization endpoint with PKCE,
   `scope=openid email profile`, `redirect_uri=http://localhost:<port>`.
3. Receive `code` on the loopback; exchange at `https://oauth2.googleapis.com/token`
   (with `code_verifier`) → Google `id_token`.
4. Firebase `POST accounts:signInWithIdp?key=API_KEY` with
   `postBody = "id_token=<google_id_token>&providerId=google.com"`,
   `requestUri = "http://localhost"`, `returnSecureToken = true` → Firebase
   `idToken`, `refreshToken`, `localId`.

**Emulator path (no OAuth client, no browser):** the Auth emulator's
`signInWithIdp` accepts a synthesized `id_token` JSON
(`{"sub":...,"email":...,"email_verified":true}`), so a `--dev-user <email>`
shortcut signs in locally without Google. Used for building/testing the plumbing
before the Desktop client exists.

### 5.2 Email + password (Phase 5)

`POST accounts:signInWithPassword?key=API_KEY` → `idToken`, `refreshToken`,
`localId`, `expiresIn`.

### 5.3 GitHub (Phase 6)

Requires a GitHub OAuth App (client id) — loopback OAuth → GitHub `access_token`
→ Firebase `signInWithIdp` (`providerId=github.com`,
`postBody=access_token=<gh>`). Works against the Auth emulator immediately.

### 5.4 Token lifecycle

- Persist `{ idToken, refreshToken, uid, expiresAt, project }` as 0600 JSON in
  `~/.config/sneat/session.json`.
- Refresh via `POST https://securetoken.googleapis.com/v1/token?key=API_KEY`
  (`grant_type=refresh_token`) shortly before expiry; the `Services` token
  provider refreshes transparently for Firestore/API calls.
- Sign-out clears the file.

Emulator hosts rewrite the REST base to
`http://<AUTH_EMULATOR_HOST>/identitytoolkit.googleapis.com/...` and
`.../securetoken.googleapis.com/...`.

## 6. Firestore reads & decoding

- Single doc: `GET https://firestore.googleapis.com/v1/projects/{project}/databases/(default)/documents/{path}`.
- List: `POST .../documents:runQuery` with a `structuredQuery` (collection
  `contacts` under parent `spaces/{spaceID}/ext/contactus`, `where status ==
  "active"` and `parentContactID == ""`).
- **Decoder:** Firestore REST returns typed values
  (`{"fields":{"title":{"stringValue":"…"}}}`). A generic converter maps typed
  values → plain `any`, marshals to JSON, then `json.Unmarshal` into the DBO
  (the DBOs' `firestore:` tag names equal their `json:` tags, so this round-trips
  cleanly). Table-tested against representative documents.
- Emulator: base host becomes `http://<FIRESTORE_EMULATOR_HOST>/v1/...`.

## 7. Contact writes (via API)

Bodies reuse `dto4contactus` request structs (each embeds
`dto4spaceus.SpaceRequest{ SpaceID }`). `Authorization: Bearer <idToken>`.

| Action | Endpoint |
|---|---|
| Add contact | `POST contactus/create_contact` |
| Edit (names, gender, roles) | `POST contactus/update_contact` |
| Delete | `DELETE contactus/delete_contact` |
| Add phone/email | `POST contactus/add_contact_comm_channel` |
| Update comm channel | `POST contactus/update_contact_comm_channel` |
| Delete comm channel | `POST contactus/delete_contact_comm_channel` |

Exact field names are pinned during implementation by reading `dto4contactus`.

## 8. TUI navigation & screens

`login` → `spaces` (pick a space) → `space` (left menu + main pane).

- **login**: three provider buttons (Google / GitHub / Email+Password) + an
  email/password form; errors shown inline.
- **spaces**: list of the user's spaces (title, type); Enter opens one.
- **space**: `tview.Flex` — left `List` (Contacts, Calendar) + main content.
- **Contacts**: list → detail; **add** form (name, gender, role, phone, email),
  **delete** with confirm modal, **edit** (names/gender/roles), comm-channel
  add/update/delete.
- **Calendar**: read-only list of recurring happenings (title, kind, cadence).

Global keys: `Ctrl+C` quits; `Esc` backs out one level. The existing `about`
screen is retained.

## 9. Testing

Follows the repo's high-coverage norm (`cover100.dev`).

- `httptest` servers for the auth, firestore, and api clients (happy path +
  error bodies + token refresh).
- Table tests for the typed-value decoder and for config precedence
  (flag > env > default).
- Constructor/navigation tests for screens (as today).
- Optional emulator-backed integration behind a build tag
  (`//go:build emulator`), runnable via `firebase emulators:start`.

## 10. Follow-ups (out of this repo's scope, noted)

- Frontend `@sneat/api` built artifact and any server docs still reference
  `https://api.sneat.ws/v0/`; update to `https://api.sneat.cloud/v0/` in
  `sneat-libs` / `sneat-go` respectively.
- No REST *list* endpoints exist for contacts or happenings (reads are Firestore
  by design); if server-side listing is later wanted, add GET handlers.

## 11. Out of scope (for now)

- Offline caching / sync.
- Creating/editing spaces, membership management beyond contacts.
- Calendar write operations.
- Non-person contact types beyond basic display.
