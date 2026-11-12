# chats

Read when: listing known chats, filtering chat state, or pruning stale local chat rows.

`wacli chats` reads chat rows from `wacli.db`. It can use session-backed PN/LID mappings to make historical `@lid` chat rows display as phone-number chats when possible. This build cannot archive, pin, mute, or mark chats read or unread.

## Commands

```bash
wacli chats list [--query TEXT] [--limit N] [--archived|--no-archived] [--pinned|--no-pinned] [--muted|--no-muted] [--unread|--no-unread]
wacli chats show --jid JID
wacli chats cleanup [--days N] [--jid JID] [--dry-run] [--confirm]
```

## Notes

- `list` is local and sorted by pinned chats first, then newest known message timestamp.
- During sync, a new incoming content message unarchives its chat when WhatsApp's **Keep chats archived** setting is off. History imports, duplicate messages, edits, reactions, and outgoing messages do not trigger this. The incoming message must be newer than both the archive action and the account setting; within the same timestamp second, a first live delivery after a known action wins, including actions queued behind startup recovery. The message and boundary are stored atomically, and an older archive replay cannot undo the resulting unarchive.
- On upgrade, sync recovers the account setting and archive timestamps from `regular_low` before applying automatic unarchives. Unknown settings preserve the archived state. If full replay is known to fail verification, sync requests a phone recovery snapshot and keeps the recovery intent until it arrives; no incremental delta is treated as proof of the missing setting.
- WhatsApp system events, such as a changed security code or a group notice, arrive as payloads with no content and are stored as `(message)` rows. They do not set that timestamp, so they cannot move a chat up the list; a chat that holds nothing else has no timestamp and sorts last.
- On the next writable open, activity matching the newest locally stored message is recomputed from stored content. Activity newer than every local message is preserved. If an unstored message advertised by history has the exact same second as a local placeholder, the repair uses local content; syncing that message restores its activity.
- `--query` filters by chat name or JID.
- `list --json` and `show --json` include `archived`, `pinned`, `muted_until`, `unread`, and `unread_count`; `unread` is true for counted unread messages and marker-only unread chats, while `unread_count` only counts unread messages.
- `list --unread` matches counted and marker-only unread chats; `list --no-unread` excludes both.
- Replayed read signals reduce the unread count only through the messages they cover; older reads cannot restore already-read messages. Known message IDs distinguish arrivals in the same second, using their local insertion order. Without a known ID boundary, messages at the cutoff second remain unread. Content-free system events, reactions, and revocations do not add to live unread counts.
- A fresh message this account sends (from the phone or another linked device) acknowledges the chat through that reply. The unread count only decreases; arrivals beyond the reply remain unread, including later rows in the same second. A manual marker clears only when no readable messages remain beyond that boundary. Reactions, edits, revocations, deletions, poll votes, calls, and content-free protocol rows are not replies. Replaying a stored reply, including a deleted or revoked one, changes nothing. History imports never mark a chat read because of an own message, including newly imported or decoded replies; explicit conversation unread state is still synced. This updates local metadata, not outbound read receipts.
- Reading a chat on the phone clears it here too while `sync` is connected. WhatsApp reports that read as a `read-self` receipt only while read receipts are turned off; with them on it arrives as an ordinary read receipt sent by this account, and both are honoured.
- `show` accepts the stored JID. If a phone JID maps to a historical `@lid` row, it can show that row too.
- Run `wacli sync` to catch up chat-state changes made on other devices; run `wacli contacts refresh` to improve chat names.
- `cleanup` only deletes local `wacli.db` rows. It does not delete chats or messages from WhatsApp.
- `cleanup --days N` skips chats with no known local activity timestamp; use `--jid` for an explicit local row.
- Use `cleanup --dry-run` before deleting and `--confirm` only for scripts that already reviewed the target list.

## Read receipts

Read events carrying message IDs are applied as bounded ranges, including live echoes and later recovery replay. If newer readable messages remain, an existing marker-only unread state is preserved even when the event came from another device. Events containing only a timestamp keep their existing clearing behavior. Replaying an older range never restores a marker that has already been cleared.

This build never sends read receipts and never marks a chat read on WhatsApp.

## Examples

```bash
wacli chats list
wacli chats list --query family --limit 20
wacli chats list --pinned
wacli chats show --jid 1234567890@s.whatsapp.net
wacli chats cleanup --days 365 --dry-run
```

When the primary phone answers an app-state recovery after the command times out, a running process continues waiting for up to 24 hours and persists that snapshot. Closing the process stops that wait; durable recovery intents remain available for a later retry. A collection whose full snapshot fails hash verification retains a durable broken-snapshot flag. This does not guarantee that the local chat index already includes every server-side mutation.
