---
title: Install
description: "Install the read-only wacli: the Mac app, or a build from source."
---

# Install

This read-only build of `wacli` comes as an app for the Mac, and as source to build on any system. There is no Homebrew formula for it.

Do not install `wacli` with Homebrew, `go install`, or the release archives of openclaw/wacli. Those are the upstream build, which can send.

## Mac app

Download [wacli.dmg](https://github.com/alive-home/wacli-alive/releases/latest/download/wacli.dmg), open it, drag wacli to Applications, and open it from there. It runs on macOS 13 or newer, on Apple Silicon and Intel Macs. The app is signed with a Developer ID and notarized by Apple, so macOS opens it without a warning.

- **The window links WhatsApp.** It shows the code to scan with your phone, then that WhatsApp is linked and how many messages came in. It is the [pairing page](auth.md#pairing-page) of `wacli auth`, in English or Dutch after the Mac's language. Opening the app again later fetches what is new.
- **Settings (⌘,) can send a copy to a server.** Fill in the address and the key of your receiver and turn it on: the app checks that the receiver takes them, then the Mac pushes every 15 minutes. It is [`push install`](push.md#running-it-on-a-schedule); Settings shows how the last run ended. Off by default.
- **The program is inside the app,** at `/Applications/wacli.app/Contents/Helpers/wacli`. The text for your AI assistant on the pairing page names it by that path. To type `wacli` in a terminal, link it into a directory on your `PATH`:

```bash
mkdir -p ~/.local/bin
ln -s /Applications/wacli.app/Contents/Helpers/wacli ~/.local/bin/wacli
```

The app never checks for updates. To update, download the disk image again and replace the app; the store and the link with your phone stay. To remove it: turn off sending in Settings, delete the app, delete the store directory (`~/.alive-wapp`), and log the device out in WhatsApp on your phone, under Linked devices.

To build the app yourself: `bun run pack:mac -- --dev` makes `dist/mac/wacli.app`, signed for your Mac alone (it needs Xcode besides the tools below). Without `--dev` the script signs with the Developer ID named in `scripts/pack-mac.mjs`, has Apple notarize the app, and builds `dist/mac/wacli.dmg`.

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

Install [Bun](https://bun.sh); it runs the scripts in `package.json`. There are no packages to install.

```bash
make build
make check
./dist/wacli --version
```

The Makefile is a thin wrapper over those scripts. `make build` writes `./dist/wacli`; `make check` runs the complete local gate.

Development and Docker builds select Go 1.27.3 for its compiler, runtime, cgo, and standard-library security fixes. The `toolchain` directive in `go.mod` selects that build version without raising the Go 1.27.0 source minimum; normal Go toolchain auto-selection downloads it when needed.

## Storage

- Default store directory: `~/.local/state/alive-wapp` on Linux (XDG state dir), `~/.alive-wapp` on macOS / Windows. A store upstream wacli left in `~/.wacli` is not read.
- Override with `--store DIR` or `WACLI_STORE_DIR`.
- The store contains `session.db` (whatsmeow keys), `wacli.db` (messages + FTS), `media/`, and a `LOCK` file. See [Spec](spec.md#storage-layout) for the layout.
- Permissions are owner-only (`0700` on the directory, `0600` on files). Do not relax these: they protect your WhatsApp session keys.
- A store made by upstream `wacli` 0.26.0 or older works with this build as it is.

## Related pages

- [Quickstart](quickstart.md): pair, sync, and search.
- [Auth](auth.md): `wacli auth`, `auth status`.
- [Sync](sync.md): bootstrap and follow-mode sync, refresh flags.
- [Doctor](doctor.md): self-checks and connectivity probe.
