# Sneat CLI Phase 1b: Browser Sign-in — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans. Steps use `- [ ]`.

**Goal:** `sneat auth login` (no `--email`) opens the browser to a locally-served Firebase-JS page, the user signs in with any enabled provider (Google/GitHub), and the CLI captures the resulting `idToken`/`refreshToken` over loopback and stores the session — with `--email/--password` still available headless.

**Architecture:** New `internal/browserauth` serves an embedded HTML page (Firebase compat SDK, `signInWithPopup`) on `127.0.0.1:<port>` and a `/callback` endpoint that receives the tokens as JSON. The `auth login` command chooses the browser flow by default and the password flow when `--email` is given.

**Tech Stack:** Go stdlib `net/http` + `go:embed`; Firebase JS compat SDK from gstatic (client-side only).

## Global Constraints

- Browser flow needs no OAuth client and no deployed page; `localhost` is a Firebase-authorized domain.
- Emulator: when an auth emulator host is configured, the page calls `firebase.auth().useEmulator(...)`.
- Go tests must not require a real browser: inject a fake `OpenBrowser` that POSTs a canned payload to `/callback`.

---

### Task 1: internal/browserauth loopback + embedded page

**Files:**
- Create: `internal/browserauth/browserauth.go`
- Create: `internal/browserauth/signin.html`
- Create: `internal/browserauth/open.go`
- Create: `internal/browserauth/browserauth_test.go`

**Interfaces:**
- Produces:
  - `browserauth.Result{ IDToken, RefreshToken, UID, Email string; ExpiresIn time.Duration }`
  - `browserauth.Flow{ APIKey, AuthDomain, Project, AuthEmulatorHost string; OpenBrowser func(url string) error }`
  - `(Flow).Run(ctx context.Context) (Result, error)`
  - `browserauth.OpenBrowser(url string) error` (GOOS dispatch)

Full implementation and test are written in the steps below.

- [ ] Step 1: Write `browserauth_test.go` (fake OpenBrowser POSTs canned JSON to `/callback`; assert `Run` returns it; plus a ctx-cancel test).
- [ ] Step 2: Run it, expect failure.
- [ ] Step 3: Implement `signin.html`, `browserauth.go`, `open.go`.
- [ ] Step 4: Run tests + lint; expect pass.
- [ ] Step 5: Commit.

---

### Task 2: wire browser flow into `sneat auth login`

**Files:**
- Modify: `cmd/sneat/commands/root.go` (extend `Env` with `NewBrowserFlow`, add `BrowserFlow` interface)
- Modify: `cmd/sneat/commands/auth.go` (browser default; password when `--email`)
- Modify: `cmd/sneat/commands/auth_test.go` (add browser-flow test)
- Modify: `cmd/sneat/main.go` (wire real browser flow + `OpenBrowser`)

**Interfaces:**
- Produces:
  - `type BrowserFlow interface { Run(ctx context.Context) (browserauth.Result, error) }`
  - `Env.NewBrowserFlow func(cfg config.Config) BrowserFlow`

- [ ] Step 1: Add browser-flow fake + failing test (login with no `--email` runs the browser flow and saves the session).
- [ ] Step 2: Run it, expect failure.
- [ ] Step 3: Extend `Env`, refactor `authLogin` to branch on `--email`, add a shared `saveAndPrint` helper.
- [ ] Step 4: Wire `main.go`.
- [ ] Step 5: Run build/vet/test/lint; expect pass.
- [ ] Step 6: Commit.
