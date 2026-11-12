# Repository Guidelines

> **READ-ONLY FORK.** This fork of openclaw/wacli must never send to or change
> WhatsApp. It is handed to customers so they can read their own WhatsApp.
> Three places enforce that, and each fails closed when upstream adds something:
>
> 1. `internal/app/guard.go`: `OpenWA()` in `internal/app/app.go` wraps every
>    WhatsApp client in `guardWA`. The guard writes out every `WAClient` method,
>    so a method upstream adds does not compile until it is classified there and
>    in the lists in `guard_test.go`. Sends and changes return
>    `errWAMutationBlocked`, presence is dropped, and connecting never announces
>    the device as online. When in doubt, block.
> 2. `cmd/wacli/readonly.go`: `readOnlyCommands` is the allowlist of commands
>    that run. Everything else upstream registers becomes a stub that says it is
>    not available. A command upstream adds stays out until it is listed.
>    `auth logout` is replaced by a local one that clears `session.db` and
>    never contacts WhatsApp.
> 3. `cmd/wacli/readonly.go`: `readOnlyDelegateKinds` is the allowlist of
>    requests a running `sync --follow` answers over its local socket
>    (`media download`, `contacts check`). Everything else is refused in
>    `executeDelegatedSend` before any work, so nothing reaches the
>    link-preview fetcher.
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
>
> Do not remove, weaken or bypass any of these. Only list a command, a delegate
> kind or a pass-through method after reading what it does on the wire.
>
> **Local only.** Upstream's `.github/` workflows, `.crabbox.yaml`, the crabbox
> skill, `docs/CNAME`, `.ci-policy.md` and the release-publishing scripts
> (`scripts/release-local.mjs`, `verify-release-candidate.mjs`,
> `download-release-candidate.mjs`, `*-cross-platform-assets.mjs`,
> `check-macos-minimum.sh`, the `.goreleaser*.yaml` configs) are deleted on
> purpose: they dispatch to openclaw infrastructure, publish through it, or
> build upstream's release. There is no CI; run the gate locally.
>
> **Merging upstream.** `git merge <upstream tag>`, then: keep the deleted files
> deleted, keep `guardWAClient(cli)` in `OpenWA()` and `applyReadOnlyBuild` at
> the end of `newRootCmd`, classify what the compiler and
> `TestGuardWAClassifiesEveryMethod` / `TestReadOnlyBuildRunsOnlyListedCommands`
> report, delete upstream tests for commands this build does not have, bring
> `README.md` and `docs/` in line with the command list, and set
> `sourceVersion` in `cmd/wacli/root.go` to `<upstream version>-readonly`.

## Project Structure
- `cmd/wacli/`: CLI command wiring (auth, sync, messages, media, contacts, chats, groups, history, doctor). Upstream's send, presence and other mutating command code is still compiled so merges stay small, but `readonly.go` keeps it out of the command tree.
- `internal/app/`: core app, whatsmeow event handling, backfill, sync idle logic.
- `internal/store/`: SQLite schema, migrations, FTS5 search, chats/contacts/groups/messages/media queries.
- `internal/wa/`: whatsmeow client wrapper, JID resolution, message parsing (text, business, media, context).
- `internal/config/`: store-dir resolution (`WACLI_STORE_DIR` env → XDG state dir on Linux → `~/.wacli`).
- `internal/lock/`: platform-specific LOCK-file locking; acquired before all write commands.
- `internal/out/`: JSON + table output helpers; all human text goes through here.
- `internal/fsutil/`: enforces 0700/0600 owner-only permissions on store files.
- `internal/pathutil/`: sanitises StorePath; rejects `?` and `#` to prevent URI injection.
- `internal/sqliteutil/`: sqlite file helpers.
- Tests sit next to the code they cover (`*_test.go`).

## Key Architectural Facts
- **Two databases**: `session.db` (managed by whatsmeow) + `wacli.db` (app data, FTS5 search).
- **FTS5 table** is a separate trigger-synced table in `wacli.db`; requires `-tags sqlite_fts5` at build time.
- **Store lock**: a `LOCK` file in the store dir is acquired before any write operation; `--lock-wait` controls the wait timeout.
- **Read-only mode**: `--read-only` flag or `WACLI_READONLY=1` env; commands that write the local store exit immediately with a clear error. This is on top of the read-only build, which holds without the flag.
- **Store path precedence**: `--store` flag → `WACLI_STORE_DIR` env → XDG `~/.local/state/wacli` on Linux (legacy `~/.wacli` fallback) → `~/.wacli` elsewhere.

## Build, Test, and Development Commands
- Build: `pnpm build` — compiles with `-tags sqlite_fts5` and `CGO_CFLAGS=-Wno-error=missing-braces` (required for GCC 15+).
- Run: `pnpm wacli -- <args>` — rebuilds then runs.
- Test: `pnpm test` — runs plain and FTS Go tests, the Windows lock/store test cross-compiles, cgo-required build check, and Node documentation/script tests.
- Lint: `pnpm lint` — `go vet ./...`; `make lint` also runs vulnerability checks and `pnpm lint:deadcode` for production and test reachability.
- Format fix: `pnpm format` — `gofmt -w .`.
- Format check: `pnpm format:check` — fails if any file would change.
- **Full gate** (must pass before every PR): `pnpm format:check && pnpm lint && pnpm lint:deadcode && pnpm test && pnpm build && pnpm docs:site && git diff --check`.

## Coding Style
- Standard `gofmt` formatting; run `pnpm format` before committing.
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
