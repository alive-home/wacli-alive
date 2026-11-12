---
title: Install
description: "Build the read-only wacli from source and put it on your PATH."
---

# Install

This read-only build of `wacli` is built from source. There is no Homebrew formula and no release archive for it.

Do not install `wacli` with Homebrew, `go install`, or upstream's release archives. Those are the upstream build, which can send.

## Build from source

`wacli` requires Go 1.27.0 or newer and uses `go-sqlite3`, so builds require cgo and a C toolchain:

- macOS: Xcode Command Line Tools.
- Debian / Ubuntu: `sudo apt install build-essential`.
- Fedora / RHEL: `sudo dnf groupinstall "Development Tools"`.

From a checkout of this repository:

```bash
mkdir -p dist
CGO_ENABLED=1 CGO_CFLAGS="-Wno-error=missing-braces" \
  go build -tags sqlite_fts5 -o dist/wacli ./cmd/wacli
install -m 755 dist/wacli ~/.local/bin/wacli
```

Any directory on your `PATH` works in place of `~/.local/bin`.

The `sqlite_fts5` build tag is required for `messages search` to use the FTS5 index. Without it, search falls back to `LIKE`.

GCC 15 has stricter brace-init warnings; the `-Wno-error=missing-braces` flag keeps the `go-sqlite3` build green there. macOS / clang and older GCC do not need it.

## Docker

The `Dockerfile` builds the same read-only binary into an Alpine image with the store under `/data`:

```bash
docker build -t wacli-readonly .
docker run --rm -it -v wacli-data:/data wacli-readonly auth
docker run --rm -v wacli-data:/data wacli-readonly sync --follow
```

## Verify the install

```bash
wacli --version
wacli doctor
wacli --help
```

`wacli --version` ends in `-readonly`, and `wacli --help` lists no `send` command. If either is different, another `wacli` is earlier on your `PATH`: check with `which -a wacli`.

`wacli doctor` checks the store directory, database integrity, FTS5 availability, and (with `--connect`) live connectivity to WhatsApp. See [Doctor](doctor.md).

## Updating

`git pull`, build again, and replace the binary. The local store format is forward-compatible across point releases; routine upgrades do not require re-pairing.

## Development builds

Install Node.js 24 or newer and the pnpm version pinned in `package.json`. Corepack users can run `corepack pnpm install --frozen-lockfile` from the checkout to download and verify that pinned version.

```bash
make build
make check
./dist/wacli --version
```

The Makefile is a thin wrapper over the pnpm scripts. `make build` writes `./dist/wacli`; `make check` runs the complete local gate.

Development and Docker builds select Go 1.27.3 for its compiler, runtime, cgo, and standard-library security fixes. The `toolchain` directive in `go.mod` selects that build version without raising the Go 1.27.0 source minimum; normal Go toolchain auto-selection downloads it when needed.

## Storage

- Default store directory: `~/.local/state/wacli` on Linux (XDG state dir), `~/.wacli` on macOS / Windows. Existing Linux `~/.wacli` directories keep working.
- Override with `--store DIR` or `WACLI_STORE_DIR`.
- The store contains `session.db` (whatsmeow keys), `wacli.db` (messages + FTS), `media/`, and a `LOCK` file. See [Spec](spec.md#storage-layout) for the layout.
- Permissions are owner-only (`0700` on the directory, `0600` on files). Do not relax these: they protect your WhatsApp session keys.
- A store made by upstream `wacli` 0.26.0 or older works with this build as it is.

## Related pages

- [Quickstart](quickstart.md): pair, sync, and search.
- [Auth](auth.md): `wacli auth`, `auth status`.
- [Sync](sync.md): bootstrap and follow-mode sync, refresh flags.
- [Doctor](doctor.md): self-checks and connectivity probe.
