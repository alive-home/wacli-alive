# wacli overview

Read when: you need the user-facing command map, global flags, store model, or links to command-specific docs.

`wacli` is a WhatsApp CLI built on `whatsmeow`. It pairs as a linked WhatsApp Web device, stores message metadata locally, supports offline search, and exposes media/group/contact reads for scripts and humans. This is a read-only build: it cannot send messages or change anything on WhatsApp, and running a command that would prints that it is not available. Named accounts let multiple WhatsApp identities use isolated stores via `--account`.

## Store and output

- Default store: `~/.local/state/wacli` on Linux, `~/.wacli` elsewhere.
- Existing Linux `~/.wacli` stores are reused when no XDG store exists.
- Override the store with `--store DIR` or `WACLI_STORE_DIR`.
- Human-readable tables are the default.
- Use `--json` for scriptable output.
- Libsignal warnings and errors go to stderr, not stdout. With `--events`, they use NDJSON `warning` events with `data.code=libsignal_diagnostic`, the original `level`, `source`, `caller`, and a safe `message`. These diagnostics can precede a successful fallback and do not themselves mean the command failed.
- Libsignal diagnostics retain known operation labels or safe error categories, redact dynamic details, and report unknown messages generically. Debug and info logging are disabled to avoid exposing cryptographic material; libsignal v0.2.2 has no production info calls.
- A successful JSON command exits successfully if its pipe reader closes early. Other output errors and command failures still return a nonzero exit status.
- Use `--full` to avoid table truncation.
- Write commands acquire the store lock; use `--lock-wait DURATION` to wait.
- Use `--read-only` or `WACLI_READONLY=1` to also reject commands that write local state.
- Use `sync --max-messages`, `sync --max-db-size`, `WACLI_SYNC_MAX_MESSAGES`, or `WACLI_SYNC_MAX_DB_SIZE` to bound local history growth.
- Use `store cleanup`, `chats cleanup`, and `groups prune` to preview and remove stale local rows after sync has already stored them.
- Authenticated startup resolves historical `@lid` chat/message rows to phone-number JIDs when the WhatsApp session store has the mapping.
- Companion tools should prefer `--json`, `--events`, webhooks, or read-only access to `wacli.db`; see [companion integrations](integrations.md).

## Command pages

- [auth](auth.md) - pair and inspect auth status.
- [accounts](accounts.md) - create and select named account stores.
- [sync](sync.md) - sync messages, contacts, groups, channels, and optional media.
- [messages](messages.md) - list, search, show, and contextualize stored messages.
- [calls](calls.md) - list stored WhatsApp call events.
- [media](media.md) - download media attached to stored messages.
- [contacts](contacts.md) - search contacts and manage local aliases/tags.
- [contacts import-system](contacts-import-system.md) - import macOS Contacts names into local contact metadata.
- [chats](chats.md) - list, show, and filter known chats.
- [groups](groups.md) - refresh and inspect groups, participants, invite links, and join requests.
- [store](store.md) - inspect local store stats and prune stale local rows.
- [channels](channels.md) - list and inspect WhatsApp Channels.
- [history](history.md) - inspect archive coverage and request older per-chat history from the primary device.
- [profile](profile.md) - fetch profile metadata, About text, and business profiles.
- [doctor](doctor.md) - diagnose store, auth, search, and optional live connectivity.
- [version](version.md) - print the CLI version.
- [completion](completion.md) - generate shell completion scripts.
- [help](help.md) - inspect command help from the CLI.
- [companion integrations](integrations.md) - build read-only local tools on top of synced data.

## Common flow

```bash
wacli auth
wacli sync --follow
wacli messages search "meeting"
wacli messages list --chat mom --limit 20
```

## Recipient formats

Commands that accept `PHONE_OR_JID` accept a WhatsApp JID like `1234567890@s.whatsapp.net`, a group JID like `123456789@g.us`, a channel JID like `123456789012345@newsletter`, or a phone number with common formatting such as `+1 (234) 567-8900`.

## History limits

WhatsApp Web history is best-effort. `wacli sync` stores events WhatsApp provides, and `wacli history backfill` can ask the primary phone for older messages per chat. It cannot guarantee a full account export.
