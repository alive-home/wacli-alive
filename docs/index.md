---
title: Overview
permalink: /
description: "This read-only build of wacli pairs as a linked WhatsApp Web device, mirrors message history into local SQLite with FTS5 search, and exposes search, media, contact, and group reads for terminals, scripts, and coding agents. It cannot send or change anything on WhatsApp."
---

# wacli

A script-friendly WhatsApp CLI built on [`whatsmeow`](https://github.com/tulir/whatsmeow). One binary pairs as a linked WhatsApp Web device, syncs messages and status broadcasts into a local SQLite store, and exposes search, media, contact, and group commands with predictable output for terminals, shell pipelines, and coding agents.

This is a read-only build. It cannot send messages or change anything on WhatsApp: those commands are not in the binary, and every call to WhatsApp passes a guard that refuses anything that sends or changes state.

## Why wacli

- **Local mirror, fast search.** All synced messages land in a SQLite store with an FTS5 index; offline `messages search` returns hits in milliseconds.
- **Reads only.** Nothing is sent, nothing is marked as read, and the device never shows as typing or online. `chats list` still filters by the archive, pin, mute, and unread state your phone sets.
- **Stable output.** Human-readable tables by default, `--json` to stdout for scripts, NDJSON `--events` for long-running commands. Human progress, prompts, and errors stay on stderr so pipes stay clean.
- **Single binary.** No daemon, no plugin host. Run `wacli auth`, then `wacli sync --follow` to keep the store warm.
- **Built for agents.** An agent with this binary cannot send as you. `--read-only` (or `WACLI_READONLY=1`) additionally blocks every command that writes local state. Store locks prevent two instances from racing on the same device identity.
- **Boundable storage.** `sync` warns when storage is uncapped; `--max-messages` / `--max-db-size` cap local growth. Media downloads cap at 100 MiB.
- **Best-effort history.** `history coverage` shows local anchors, `history fill --dry-run` plans candidate chats, and `history backfill` requests older messages per chat from your primary device.

## Pick your path

- **Trying it.** Read [Install](install.md), then [Quickstart](quickstart.md). Pair, sync, and search in under five minutes.
- **Using multiple WhatsApp accounts.** Read [Accounts](accounts.md) for named account stores and `--account`.
- **Searching old chats.** Read [Sync](sync.md) for the sync model and [History](history.md) for coverage planning and on-demand backfill.
- **Browsing chats.** Read [Chats](chats.md) for listing and filtering chats.
- **Managing local storage.** Read [Store](store.md) for stats, dry-run cleanup, and local-only pruning.
- **Mirroring address-book names.** Read [Contacts import-system](contacts-import-system.md) to import macOS Contacts display names into local wacli metadata.
- **Wiring up an agent.** Pair `--read-only`, `--json`, and `--events` from [Overview](overview.md); read [Doctor](doctor.md) for self-checks.
- **Building companion tools.** Read [Companion integrations](integrations.md) for safe read-only SQLite and JSON integration patterns.
- **Looking up a flag.** Open the per-command pages from [Overview](overview.md).

## Status

Core implementation is in place. `CHANGELOG.md` in the repository tracks shipped behavior. WhatsApp Web is not a published API; expect occasional breakage from upstream protocol changes — `wacli` follows `whatsmeow` upstream.

## Out of scope

- Sending, or changing anything on WhatsApp.
- Guaranteed full-history export (WhatsApp Web history is best-effort).
- A daemon, MCP server, web UI, or GUI.
- End-to-end "contact creation" inside WhatsApp; local aliases and tags only.

## Disclaimer

`wacli` is a third-party tool that uses the WhatsApp Web protocol via `whatsmeow`. It is **not affiliated with WhatsApp or Meta**. Use at your own risk; pairing as a linked device is subject to WhatsApp's terms.

A fork of [openclaw/wacli](https://github.com/openclaw/wacli). Released under the MIT license.
