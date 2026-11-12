---
title: Quickstart
description: "Pair as a linked WhatsApp Web device, sync, and search your messages in under five minutes."
---

# Quickstart

Five minutes from a clean machine to authenticated sync and search. This build reads WhatsApp; it cannot send or change anything. For deeper reading, follow the links at the bottom of each step.

## 1. Install

```bash
mkdir -p dist
CGO_ENABLED=1 CGO_CFLAGS="-Wno-error=missing-braces" \
  go build -tags sqlite_fts5 -o dist/wacli ./cmd/wacli
install -m 755 dist/wacli ~/.local/bin/wacli
wacli --version
```

Run this from a checkout of this repository. Requirements and platform notes are on [Install](install.md).

## 2. Pair as a linked device

```bash
wacli auth
```

`auth` prints a QR code in your terminal. On your phone, open WhatsApp → **Linked devices** → **Link a device**, scan the QR, and approve. As soon as pairing succeeds, `auth` immediately starts the initial sync — keep it running until it idles out or press `Ctrl+C` once it has caught up.

For someone who would rather not use a terminal, `wacli auth --open` shows the same QR code on a local page in the default browser, with the steps and a plain explanation of what the link can read and where the data is stored.

If the terminal QR does not scan, try `--qr-format text` and render that raw QR payload in another app, or pair via phone-number code with `--phone +15551234567`.

> The session lasts as long as the linked device stays linked on your phone. Unlinking it there (WhatsApp → **Linked devices**) ends the session and requires a fresh QR. `wacli auth logout` clears the local session only; it does not remove the device on your phone.

Verify:

```bash
wacli auth status
```

## 3. Keep the store warm

```bash
wacli sync --follow
```

`sync` never shows a QR; it requires a previously paired session and runs until you stop it. `--once` exits after one idle window; `--follow` reconnects on errors. Both honor `--max-messages` / `--max-db-size` (and the `WACLI_SYNC_MAX_*` env equivalents) so the local store stays bounded.

See [Sync](sync.md) for refresh-contacts/refresh-groups, `--download-media`, and the idle-exit knobs.

## 4. Search and read

```bash
# Full-text search (FTS5 when the binary was built with -tags sqlite_fts5; LIKE otherwise)
wacli messages search "meeting"

# Search media-bearing messages
wacli messages search "meeting" --has-media

# List recent messages from a chat, oldest first
wacli messages list --chat 1234567890@s.whatsapp.net --asc

# Show a single message
wacli messages show --chat 1234567890@s.whatsapp.net --id <message-id>

# Show context around a message
wacli messages context --chat 1234567890@s.whatsapp.net --id <message-id> --before 5 --after 5
```

`--json` produces a stable envelope; `--full` keeps full IDs in tables. See [Messages](messages.md) for every filter.

## 5. Backfill older history (optional, best-effort)

`sync` only stores what WhatsApp Web pushes. To request older messages for a specific chat from your **primary device** (your phone), use:

```bash
wacli history coverage --include-blocked
wacli history fill --dry-run --limit 20
wacli history backfill --chat 1234567890@s.whatsapp.net --requests 10 --count 50
```

The phone must be online for `backfill`. WhatsApp may not return full history. See [History](history.md) for coverage planning, limits, and patterns.

## 6. Named accounts (optional)

If you run more than one WhatsApp number, named accounts give each an isolated store, session, and lock:

```bash
# Add a second account and pair it immediately
wacli accounts add work

# List all configured accounts
wacli accounts list

# Use a named account with any command
wacli --account work sync --follow
wacli --account personal messages search "invoice"
```

Use `--no-auth` to create the account entry without pairing immediately. Two accounts can sync concurrently — their locks are independent. See [Accounts](accounts.md) for YAML config and migration from manual `--store` paths.

## 7. Diagnostics and safety

```bash
wacli doctor
wacli doctor --connect

# Also keep commands from writing the local store
wacli --read-only messages search "invoice"
WACLI_READONLY=1 wacli store cleanup   # exits with a clear error
```

`doctor` checks the store, schema, FTS5 availability, and (with `--connect`) live connectivity. See [Doctor](doctor.md).

## 8. Shell completion (optional)

```bash
wacli completion bash    >> ~/.bash_completion
wacli completion zsh     >  "${fpath[1]}/_wacli"
wacli completion fish    >  ~/.config/fish/completions/wacli.fish
```

## Where next

- [Overview](overview.md) — global flags, store model, full command map.
- [Accounts](accounts.md) — named accounts, isolated stores, YAML config.
- [Channels](channels.md) — read WhatsApp Channels.
- [Groups](groups.md) — list, refresh, info, participants, invite links.
- [Spec](spec.md) — design notes, storage layout, locking model, non-goals.
- [Doctor](doctor.md) — self-checks and connectivity probe.
