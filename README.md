# wacli, read-only build

Read your WhatsApp from the terminal. `wacli` pairs as a linked device, mirrors your messages into a local SQLite database, and lets you search and read them.

This build cannot send messages or change anything on WhatsApp.

> `wacli` uses the WhatsApp Web protocol through [`whatsmeow`](https://github.com/tulir/whatsmeow). It is not affiliated with WhatsApp or Meta. This is a fork of [openclaw/wacli](https://github.com/openclaw/wacli) with everything that sends or changes WhatsApp taken out.

## What it does

- Pairs with your phone as a linked device and syncs your messages.
- Searches and reads messages, chats, contacts, groups, channels, calls, and polls.
- Downloads media attached to your messages.
- Asks your phone for older history, per chat.

## What it cannot do

- Send messages, files, reactions, polls, or status updates.
- Mark messages as read, show typing, or show you as online.
- Edit, delete, or forward messages.
- Archive, pin, or mute chats.
- Change groups, channels, contacts, or your profile.
- Remove itself from your account. To do that, open WhatsApp on your phone, go to **Linked devices**, and remove it there. `wacli auth logout` only clears the session on this machine.

These commands are not in the binary. Running one prints that it is not available and exits with an error. Underneath the commands, every call to WhatsApp goes through one guard that refuses anything that sends or changes state, so a command that slipped through would still be refused.

## What leaves your machine

Only the WhatsApp connection itself: the linked-device session and media downloads from WhatsApp's servers. There is no telemetry and no update check.

One opt-in exception: `wacli sync --webhook URL` posts each new message to the URL you pass. Without that flag, nothing is posted anywhere.

Your messages and session keys stay in the store directory on your disk, readable only by your user.

## Install

Build it from this repository. You need Go 1.27.0 or newer and a C compiler.

```sh
mkdir -p dist
CGO_ENABLED=1 CGO_CFLAGS="-Wno-error=missing-braces" \
  go build -tags sqlite_fts5 -o dist/wacli ./cmd/wacli
install -m 755 dist/wacli ~/.local/bin/wacli
wacli --version
```

The version ends in `-readonly`. If it does not, another `wacli` is earlier on your `PATH`.

Do not install `wacli` with Homebrew, `go install`, or upstream's release archives. Those are the upstream build, which can send.

See the [installation guide](docs/install.md) for platform notes and Docker.

## Quick start

Pair by scanning the QR code from WhatsApp's **Linked devices** screen. `wacli auth` prints it in the terminal; `wacli auth --open` shows it on a local page in your browser, with the steps and a plain explanation of what the link can read. `auth` runs the first sync after pairing.

```sh
wacli auth --open
wacli sync --follow
wacli messages search "meeting"
```

The [quickstart](docs/quickstart.md) covers phone-number pairing, named accounts, media, and diagnostics.

## Keep messages in sync

```sh
wacli sync --follow
```

Search reads the local database, so it works without a live WhatsApp connection:

```sh
wacli messages search "invoice" --has-media
wacli --json messages list --limit 20
```

WhatsApp Web provides history on a best-effort basis. Use [`history coverage`](docs/history.md) to see what is available locally before requesting older messages from your phone.

## Use wacli from scripts

Tables are the default. Use `--json` for one-shot commands and `--events` for NDJSON lifecycle events from long-running commands. Progress and errors stay on stderr.

`--read-only` or `WACLI_READONLY=1` goes one step further than this build already does: it also rejects commands that write the local store.

```sh
wacli --read-only --json messages search "invoice"
WACLI_READONLY=1 wacli --json doctor
```

While `sync --follow` runs it holds the store lock. `media download` and `contacts check` from another terminal are handed to the running sync. See [companion integrations](docs/integrations.md) for webhooks and read-only SQLite access.

## Commands

| Area | What it covers |
| --- | --- |
| [`auth`](docs/auth.md), [`accounts`](docs/accounts.md) | Pair a linked device and keep separate stores per account. |
| [`sync`](docs/sync.md), [`history`](docs/history.md) | Mirror new events and request older per-chat history. |
| [`messages`](docs/messages.md), [`calls`](docs/calls.md) | Search, read, and export local records. |
| [`media`](docs/media.md) | Download media from synced messages. |
| [`contacts`](docs/contacts.md), [`chats`](docs/chats.md) | Find people and chats; keep local aliases and tags. |
| [`groups`](docs/groups.md), [`channels`](docs/channels.md) | Read groups, participants, and channels. |
| [`profile`](docs/profile.md) | Read profile details. |
| [`store`](docs/store.md), [`doctor`](docs/doctor.md) | Inspect local storage and diagnose the setup. |

Run `wacli help <command>` for the full reference of the installed binary.

## Configuration

The default store is `~/.local/state/alive-wapp` on Linux and `~/.alive-wapp` elsewhere. It is not upstream's `~/.wacli`: a store left there is not read. Override it with `--store DIR` or `WACLI_STORE_DIR`. Use named accounts when each WhatsApp number needs its own session, database, and lock.

```sh
wacli accounts add work
wacli --account work sync --follow
```

See [accounts](docs/accounts.md) for store selection and [sync](docs/sync.md) for storage limits, media downloads, and webhooks.

## Development

Development uses the Go 1.27.3 toolchain selected by `go.mod`, Node.js 24 or newer, pnpm, cgo, and a C compiler.

```sh
pnpm install --frozen-lockfile
pnpm build
pnpm format:check && pnpm lint && pnpm test
```

[AGENTS.md](AGENTS.md) describes how the read-only guarantee is enforced and how to merge upstream without losing it.

## Credits

Built on [openclaw/wacli](https://github.com/openclaw/wacli), created by [@steipete](https://github.com/steipete) and maintained by [@dinakars777](https://github.com/dinakars777), which was inspired by [`whatsapp-cli`](https://github.com/vicentereig/whatsapp-cli) by Vicente Reig.

## License

[MIT](LICENSE).
