# Repository Guidelines

> **READ-ONLY FORK.** This fork of openclaw/wacli must never send to or change
> WhatsApp. It is handed to customers so they can read their own WhatsApp.
> The code that does it is not in this build: upstream's send, presence,
> chat-state, group-admin, profile, contact-save and logout code is deleted,
> not switched off. whatsmeow itself still has its send functions, and the Go
> linker keeps them in the binary; nothing calls them. Three places keep it
> that way, and each fails closed when upstream adds something:
>
> 1. `internal/app/app.go` and `internal/app/guard.go`: `WAClient` and
>    `wa.Client` have no method that sends to or changes WhatsApp, and no code
>    sends presence, so connecting never announces the device as online.
>    `OpenWA()` wraps every WhatsApp client in `guardWA`, which writes out every
>    `WAClient` method: a method upstream adds does not compile until it is
>    written there and listed in `guard_test.go`. `TestNoMutatingMethodExists`
>    holds the names that must never come back, and
>    `TestBuildCallsOnlyAllowedWhatsmeowMethods`
>    (`internal/wa/readonly_calls_test.go`) fails on every whatsmeow call that
>    is not on its allowlist. When in doubt, leave it out.
> 2. `cmd/wacli/readonly.go`: `readOnlyCommands` is the allowlist of commands
>    that run. `removedCommands` names upstream's sending commands; each is a
>    stub that says it is not available, and so is anything else upstream
>    registers. A command upstream adds stays out until it is listed.
>    `auth logout` is a local one that clears `session.db` and never contacts
>    WhatsApp.
> 3. `cmd/wacli/readonly.go`: `readOnlyDelegateKinds` is the allowlist of
>    requests a running `sync --follow` answers over its local socket
>    (`media download`, `contacts check`). Everything else is refused in
>    `executeDelegated` (`cmd/wacli/delegate.go`) before any work.
>
> One thing sits below the guard: whatsmeow answers a retry receipt by sending
> the message again on its own. `internal/wa/session.go` turns that off
> (`UseRetryMessageStore = false`, `PreRetryCallback = refuseRetry`); upstream
> turns the store on, so keep our side when that file conflicts.
>
> Known remainder: for a store a sending wacli used in the last 28 days,
> whatsmeow may send WhatsApp's server one privacy-token request when such a
> contact reinstalls. No person sees it. Protocol acknowledgements (delivery
> receipts of type `inactive`, key requests to the own phone) are not sends.
> The build itself asks the account's own phone for three things, pinned in
> `readonly_calls_test.go`: older history, an app-state snapshot, and media it
> could not download. None shows up in a chat.
>
> Do not remove, weaken or bypass any of these. Only list a command, a delegate
> kind, a `WAClient` method or a whatsmeow call after reading what it does on
> the wire.
>
> **Local only.** Upstream's `.github/` workflows, `.crabbox.yaml`, the crabbox
> skill, `docs/CNAME`, `.ci-policy.md` and the release-publishing scripts
> (`scripts/release-local.mjs`, `verify-release-candidate.mjs`,
> `download-release-candidate.mjs`, `*-cross-platform-assets.mjs`,
> `check-macos-minimum.sh`, `release-common.mjs`, `extract-release-notes.mjs`,
> `govulncheck-stdlib.mjs`, `release-version-inspector/`, the
> `.goreleaser*.yaml` configs) are deleted on purpose: they dispatch to
> openclaw infrastructure, publish through it, build upstream's release, or
> send the dependency list to a vulnerability service. So are the `docs`
> command (it opened upstream's site) and the web fonts of the docs site;
> `scripts/docs-site-render.test.mjs` holds that the generated site loads
> nothing from the network. The scripts run on Bun; there is no package to
> install and no lockfile. There is no CI; run the gate locally.
>
> **Merging upstream.** `git merge <upstream tag>`. Upstream's sending code
> comes back as conflicts and new files: delete it again (the `send*`,
> `presence`, `docs`, `messages_edit|forward|delete`, `contacts_save` and
> `media_roots` files in `cmd/wacli`; `send.go`, `contact_save.go`,
> `receipts.go` in `internal/wa`; `read_receipts.go` in `internal/app` and
> `internal/store`; `internal/linkpreview`, of which `internal/safehttp` keeps
> the webhook's HTTP client) together with its tests. Keep the deleted files
> deleted, keep `guardWAClient(cli)` in `OpenWA()` and `applyReadOnlyBuild` at
> the end of `newRootCmd`, and work through what the compiler,
> `TestGuardWAClassifiesEveryMethod`, `TestNoMutatingMethodExists`,
> `TestBuildCallsOnlyAllowedWhatsmeowMethods` and
> `TestReadOnlyBuildRunsOnlyListedCommands` report. Then bring `README.md` and
> `docs/` in line with the command list, and set `sourceVersion` in
> `cmd/wacli/root.go` to `<upstream version>-readonly`.

## Project Structure
- `cmd/wacli/`: CLI command wiring (auth, sync, messages, media, contacts, chats, groups, history, doctor). `readonly.go` holds the command and delegate allowlists; `delegate.go` is the local socket of `sync --follow`.
- `internal/app/`: core app, whatsmeow event handling, backfill, sync idle logic.
- `internal/store/`: SQLite schema, migrations, FTS5 search, chats/contacts/groups/messages/media queries.
- `internal/wa/`: whatsmeow client wrapper, JID resolution, message parsing (text, business, media, context).
- `internal/config/`: store-dir resolution (`WACLI_STORE_DIR` env → XDG state dir on Linux → `~/.alive-wapp`). The default is deliberately not upstream's `~/.wacli`, the path a scan of well-known locations looks at; keep it that way when merging.
- `internal/lock/`: platform-specific LOCK-file locking; acquired before all write commands.
- `internal/out/`: JSON + table output helpers; all human text goes through here.
- `internal/fsutil/`: enforces 0700/0600 owner-only permissions on store files.
- `internal/pathutil/`: sanitises StorePath; rejects `?` and `#` to prevent URI injection.
- `internal/pairpage/`: the local pairing page of `wacli auth --open` (`pair.html` + `pair.js` in the store directory). It is read from disk and must load nothing from the network; `TestPageLoadsNothingFromTheNetwork` holds that. What it tells customers must stay true of the build.
- `internal/sqliteutil/`: sqlite file helpers.
- `internal/safehttp/`: HTTP client that only connects to public addresses; the sync webhook posts through it.
- Tests sit next to the code they cover (`*_test.go`).

## Key Architectural Facts
- **Two databases**: `session.db` (managed by whatsmeow) + `wacli.db` (app data, FTS5 search).
- **FTS5 table** is a separate trigger-synced table in `wacli.db`; requires `-tags sqlite_fts5` at build time.
- **Store lock**: a `LOCK` file in the store dir is acquired before any write operation; `--lock-wait` controls the wait timeout.
- **Read-only mode**: `--read-only` flag or `WACLI_READONLY=1` env; commands that write the local store exit immediately with a clear error. This is on top of the read-only build, which holds without the flag.
- **Store path precedence**: `--store` flag → `WACLI_STORE_DIR` env → XDG `~/.local/state/alive-wapp` on Linux → `~/.alive-wapp` elsewhere. Upstream's `~/.wacli` is never read.

## Build, Test, and Development Commands
- The scripts in `package.json` run on Bun. Always `bun run <script>`: a bare `bun test` starts Bun's own test runner instead of the `test` script.
- Build: `bun run build` — compiles with `-tags sqlite_fts5` and `CGO_CFLAGS=-Wno-error=missing-braces` (required for GCC 15+).
- Run: `bun run wacli <args>` — rebuilds then runs.
- Test: `bun run test` — runs plain and FTS Go tests, the Windows lock/store test cross-compiles, cgo-required build check, and the documentation/script tests (`bun test scripts/`).
- Lint: `bun run lint` — `go vet ./...`; `make lint` also runs `bun run lint:deadcode` for production and test reachability. The dead-code checker is fetched once through the Go module proxy and verified against Go's checksum database, like every Go dependency.
- Format fix: `bun run format` — `gofmt -w .`.
- Format check: `bun run format:check` — fails if any file would change.
- **Full gate** (must pass before every PR): `bun run check` (format check, vet, dead code, tests, build, docs site, `git diff --check`).

## Coding Style
- Standard `gofmt` formatting; run `bun run format` before committing.
- Output: send structured data to stdout (`--json` / table); send human hints, progress, and errors to stderr via `internal/out`.
- Prefer explicit error returns over panics; write short, early-return functions.
- No build-time CGO beyond sqlite3; keep the dependency tree minimal.

## Testing Guidelines
- Every bug fix should ship with a regression test.
- FTS-sensitive tests must run under `-tags sqlite_fts5`; non-FTS path tests must also pass without the tag.
- Use `fake_wa_test.go` / table-driven tests for whatsmeow interaction; avoid hitting real WhatsApp in unit tests.
- Integration tests that need a live account are opt-in and not part of the standard gate.
- Local retrieval CLI scenarios: `WACLI_RETRIEVAL_ARTIFACTS=/outside/repository/artifacts go test ./internal/app -run '^TestRetrievalStage[123]CLI$' -count=1` (repeat with `-tags sqlite_fts5`). These build and execute the real CLI against disposable stores and save per-scenario transcripts.

## Commit & Pull Request Guidelines
- Follow Conventional Commits: `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `security:`, `ci:` with an imperative summary.
- Keep commits focused; avoid bundling unrelated changes.
- PRs should state: what changed, why, how it was tested, and any new flags or env vars.
- Run the full gate locally before opening a PR; there is no CI.
- New contributors: add `Co-authored-by:` trailers when building on their work.

## Agent Notes
- This repo uses `AGENTS.md` as its agent-instruction source; `CLAUDE.md` is explicitly ignored.
- For agent-safe execution, pass `--read-only` (or set `WACLI_READONLY=1`) to also prevent local store writes.
- Prefer `--json` output for machine-readable parsing.
- Do not add dependencies or change build tooling without confirming with the maintainer.
