# Sneat CLI (JSON) — design

Date: 2026-07-08
Status: Draft for review

Supersedes the earlier TUI-oriented draft. This repo becomes a **command-line
tool** (`sneat`) whose commands print **JSON** to stdout, backed by the shared
Firebase project `sneat-eur3-1`.

## 1. Goals

`sneat` is a scriptable CLI. A signed-in user can:

- `sneat auth login` — sign in via **browser** (any enabled provider: Google,
  GitHub, …) or headless **email + password**; `logout`; `whoami`.
- `sneat space list` — JSON of the user's spaces.
- `sneat contact list|get|add|delete|update` and comm-channel (phone/email)
  management within a space — reads as JSON, writes via the sneat-go API.
- `sneat calendar list` — JSON of recurring happenings in a space.

**Command noun convention:** singular resource + verb (`space list`,
`contact get`), matching specscore-cli / `gh` / `kubectl` / `docker` (the noun
names a *type*; the list shape is an output concern). A bare singular
(`sneat space`) shows help. A bare **plural** is a hidden
convenience alias for that resource's `list` action: `sneat spaces` ==
`sneat space list`, and `sneat contacts --space <id>` ==
`sneat contact list --space <id>` (hidden from `--help` to keep it uncluttered).

All command output is JSON on stdout; errors + diagnostics on stderr; non-zero
exit on failure.

### Priority / build order

1. **Auth foundation**: CLI skeleton (cobra) + `sneat auth login` (browser +
   email/password) + session store + `sneat whoami`. Emulator-first.
2. **First authenticated Firestore read via DALgo**: `sneat spaces list`
   (proves the user-token → DALgo → DBO stack end-to-end).
3. **Contacts read**: `list` + `get`.
4. **Contacts write**: `add` / `delete` / `update` / roles / phone / email
   (sneat-go API).
5. **Calendar**: `calendar list`.
6. **Extraction (optional, later)**: move contacts/calendar command logic into
   `contactus/cli` + `calendarius/cli` packages, and any Firestore-token DALgo
   capability into its own module.

## 2. Data & auth model (from research)

- One shared Firebase project **`sneat-eur3-1`**; public web API key
  `AIzaSyCeQu1WC182yD0VHrRm4nHUxVf27fY-MLQ`.
- **Reads = Firestore via DALgo**, authenticated as the user with their Firebase
  **ID token** (security rules enforced). See §5.
- **Writes = sneat-go HTTP API** at **`https://api.sneat.cloud/v0/`** (prod;
  changed recently from `api.sneat.ws`) / `http://localhost:4300/v0/` (dev), with
  the ID token as `Authorization: Bearer`.

Firestore document paths:

- User's spaces: `users/{uid}` → `spaces` map (`IUserSpaceBrief`: title, type,
  `userContactID`, …).
- Contacts: `spaces/{spaceID}/ext/contactus/contacts/*` (flat list =
  `status == "active"`, `parentContactID == ""`).
- Recurring happenings: `spaces/{spaceID}/ext/calendarius` →
  `recurringHappenings` map.

DBO/DTO reuse (toolchain Go 1.26.4; extensions are `go 1.26`):

- `github.com/sneat-co/contactus/backend/dbo4contactus` (`ContactDbo`,
  `ContactBrief`) + `.../dto4contactus` (write request bodies).
- `github.com/sneat-co/calendarius/backend/dbo4calendarius`
  (`CalendariusSpaceDbo`, `CalendarHappeningBrief`).

DALgo reads unmarshal Firestore docs directly into these DBOs
(`dalgo2firestore` calls `docSnapshot.DataTo(record.Data())`; the DBOs' existing
`firestore:` tags round-trip). No custom decoder.

## 3. CLI structure (patterns from ingitdb-cli)

`ingitdb-cli` is the template: cobra, Go 1.26.4, DALgo consumer, `yaml.v3` (no
viper), constructor-injected dependencies per command (no globals / no `init()`),
centralized `writeJSON` output helpers, a DB-factory seam for tests.

```
cmd/sneat/main.go              thin entrypoint: build root cmd, AddCommand, run
cmd/sneat/commands/            one file per command; func Xxx(deps...) *cobra.Command
  root.go  auth.go  whoami.go  spaces.go  contacts.go  calendar.go
  output.go                    writeJSON(w io.Writer, v any) + format dispatch
  flags.go                     shared flag helpers (--space, --project, --emulator, ...)
internal/config/               resolve config (flag > env > default)
internal/session/              token store (0600 JSON in os.UserConfigDir/sneat)
internal/sneatauth/            Firebase Auth REST: signInWithPassword, refresh
internal/browserauth/          loopback server + embedded Firebase-JS sign-in page
internal/tokensrc/             oauth2.TokenSource that refreshes the ID token
internal/firestoredb/          build *firestore.Client (user token) + dalgo2firestore
internal/sneatapi/             POST/DELETE to /v0/contactus/* with Bearer
```

Commands take injected collaborators (e.g. `newDB`, `store`, `authClient`,
`getenv`, `openBrowser`) so every command is unit-testable with fakes; `main`
wires the real implementations. Output writers take `io.Writer`
(`cmd.OutOrStdout()`).

The existing tview TUI (`sneatui` package) and `tcell`/`tview` deps are
**removed**. This repo's Go module is renamed `github.com/sneat-co/sneat-tui` →
`github.com/sneat-co/sneat-cli` (go.mod + imports + README).

## 4. Configuration & emulator switching

Precedence **flag > env > default**. Uses standard Firebase emulator env vars.

| Concern | Flag | Env | Default |
|---|---|---|---|
| Project | `--project` | `SNEAT_FIREBASE_PROJECT` | `sneat-eur3-1` |
| Web API key | `--api-key` | `FIREBASE_API_KEY` | public web key |
| Auth domain | `--auth-domain` | `FIREBASE_AUTH_DOMAIN` | `sneat.app` |
| Auth emulator | `--auth-emulator` | `FIREBASE_AUTH_EMULATOR_HOST` | off |
| Firestore emulator | `--firestore-emulator` | `FIRESTORE_EMULATOR_HOST` | off |
| Both emulators | `--emulator` | — | auth `localhost:9099`, firestore `localhost:8080` |
| API base URL | `--api-base-url` | `SNEAT_API_BASE_URL` | `https://api.sneat.cloud/v0/` |

The Firestore client honors `FIRESTORE_EMULATOR_HOST` natively; when set it skips
auth, so reads work against the emulator with the same DALgo code path.

## 5. Firestore reads via DALgo as the user

The crux: read Firestore **as the signed-in user** (rules enforced), through
DALgo, so DBO structs are reused.

```go
// internal/tokensrc: an oauth2.TokenSource backed by the stored session,
// refreshing the Firebase ID token via securetoken when near expiry.
type Source struct { store *session.Store; auth *sneatauth.Client }
func (s *Source) Token() (*oauth2.Token, error) // {AccessToken: idToken, Expiry: ...}

// internal/firestoredb:
func Open(ctx context.Context, cfg config.Config, ts oauth2.TokenSource) (dal.DB, error) {
    var opts []option.ClientOption
    if cfg.FirestoreEmulatorHost == "" {         // prod: authenticate as the user
        opts = append(opts, option.WithTokenSource(ts))
    }                                             // emulator: env var, no creds
    client, err := firestore.NewClient(ctx, cfg.Project, opts...)
    if err != nil { return nil, err }
    return dalgo2firestore.NewDatabase(cfg.Project, client), nil
}
```

Reads use DALgo idioms into DBOs, e.g. spaces:

```go
rec := dal.NewRecordWithData(dal.NewKeyWithID("users", uid), &userRecord{})
err := db.RunReadonlyTransaction(ctx, func(ctx context.Context, tx dal.ReadTransaction) error {
    return tx.Get(ctx, rec)
})
```

and contacts via a collection query on `spaces/{spaceID}/ext/contactus/contacts`
with `status == "active"` and `parentContactID == ""` into `[]*ContactDbo`.

**Capability spike (build-order §2): RESOLVED — it works.** Firestore honors a
Firebase ID token (rules enforced) via the gRPC client + `option.WithTokenSource`.
Verified against prod on 2026-07-08: `firestore.NewClient(project,
option.WithTokenSource(<idToken>))` read `users/{uid}` as the user and returned
the caller's own `spaces`. `dalgo2firestore.NewDatabase` wrapping that client
reads the same doc via `DataTo` into a `*map[string]any`. The REST-based DALgo
fallback is therefore **not needed**.

## 6. Authentication

### 6.1 Browser sign-in (default) — local page + Firebase JS SDK

`sneat auth login` starts a loopback server on `127.0.0.1:<port>` and opens the
browser to a locally-served page that:

- loads the Firebase JS SDK (compat build from `gstatic.com`) and initializes
  with the public config (`apiKey`, `authDomain`, `projectId`);
- if an auth emulator host is configured, calls `connectAuthEmulator`;
- offers provider buttons (`signInWithPopup(GoogleAuthProvider|GithubAuthProvider)`);
- on success reads `user.getIdToken()` + `user.refreshToken` + `user.uid` +
  `user.email` and `POST`s them to the CLI's `/callback` (same-origin loopback);
- the CLI stores the session and the page shows "You can close this window".

`localhost` is an authorized domain in Firebase by default (fixes the
opener-origin `auth/unauthorized-domain` check), so this works against
`sneat-eur3-1` with **no deployed page and no Google Desktop OAuth client**.
The `authDomain` must be a domain whose `/__/auth/handler` is a registered
Google OAuth redirect URI for the project — `sneat.app` (confirmed in
sneat-app's `environment.ts`), **not** the `firebaseapp.com` default, which is
not registered here and yields `redirect_uri_mismatch`. Headless/SSH falls back
to email/password. The page HTML is embedded
via `go:embed`. Go tests inject a fake `openBrowser` that POSTs a canned payload
to `/callback`, exercising the loopback plumbing without a real browser.

### 6.2 Email + password (headless)

`sneat auth login --email <e> --password <p>` (password also promptable/stdin)
→ `accounts:signInWithPassword?key=API_KEY` → `{idToken, refreshToken, localId,
email, expiresIn}`.

### 6.3 Session & refresh

Session `{project, uid, email, idToken, refreshToken, expiresAt}` persisted as
0600 JSON in `os.UserConfigDir()/sneat/session.json`. `internal/tokensrc`
refreshes via `securetoken.googleapis.com/v1/token?key=API_KEY`
(`grant_type=refresh_token`) before expiry and rewrites the session. `logout`
clears the file; `whoami` prints `{uid, email, project}` as JSON.

Emulator hosts rewrite the REST base to
`http://<AUTH_EMULATOR_HOST>/identitytoolkit.googleapis.com/...` and
`.../securetoken.googleapis.com/...`.

## 7. Contact writes (sneat-go API)

Bodies reuse `dto4contactus` request structs (each embeds
`dto4spaceus.SpaceRequest{ SpaceID }`); `Authorization: Bearer <idToken>`.

| Command | Endpoint |
|---|---|
| `contacts add` | `POST contactus/create_contact` |
| `contacts update` (names, gender, roles) | `POST contactus/update_contact` |
| `contacts delete` | `DELETE contactus/delete_contact` |
| add phone/email | `POST contactus/add_contact_comm_channel` |
| update comm channel | `POST contactus/update_contact_comm_channel` |
| delete comm channel | `POST contactus/delete_contact_comm_channel` |

Exact field names are pinned during implementation from `dto4contactus`.

### 7.1 Contact command surface

Noun is singular `contact` (alias `contacts`). `--space` is required until
`sneat space use <id>` stores a default space.

```
sneat contact list   --space <id>                         # JSON array
sneat contact get    --space <id> --id <cid>              # JSON object
sneat contact add    --space <id> --name "Alex T" \
                     --email a@example.com --phone +3227035895 \
                     --role member --gender male --type person
sneat contact delete --space <id> --id <cid>
sneat contact update --space <id> --id <cid> [--name ... --gender ... --role ...]
```

- `--email` / `--phone` / `--role` are repeatable (StringArray).
- `--name` is the full name (split into first/last); `--first` / `--last`
  available for precision.
- `--type`: `person` (default) | `company` | `pet` | `location`.
- `--gender`: `male` | `female` | `other`.
- `add` = `create_contact` then one `add_contact_comm_channel` per email/phone;
  prints the created contact as JSON.

## 8. Testing

Mirrors the repo's high-coverage norm and ingitdb-cli's seam style.

- Command constructors take injected fakes (`newDB`, `store`, `openBrowser`,
  `getenv`, HTTP clients) → pure unit tests, output asserted via a buffer.
- `httptest` servers for `sneatauth`, `browserauth` loopback, and `sneatapi`.
- Table tests for config precedence and the JSON output helpers.
- Firestore-through-DALgo covered against the **Firestore emulator** behind a
  build tag (`//go:build emulator`); the token-auth spike (prod rules) is a
  manual/checklisted verification.

## 9. Dependencies added

`github.com/spf13/cobra`, `github.com/dal-go/dalgo`,
`github.com/dal-go/dalgo2firestore`, `cloud.google.com/go/firestore`,
`google.golang.org/api/option`, `golang.org/x/oauth2`, and the two DBO modules
(`contactus/backend`, `calendarius/backend`). Removed: `tcell`, `tview`.
`replace` directives point DBO modules at local sibling repos if not tagged.

## 10. Follow-ups / out of scope

- Update `api.sneat.ws` → `api.sneat.cloud` references in `sneat-libs` /
  server docs (other repos).
- Extraction of per-extension `cli` packages (build-order §6).
- Offline caching, space/membership management, calendar writes.
