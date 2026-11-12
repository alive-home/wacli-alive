# groups

Read when: listing, refreshing, inspecting, reading invite links, pruning stale local group rows, or listing group participants.

`wacli groups` combines local group rows with live WhatsApp operations. This build cannot create, change, join, or leave groups, or manage their participants.

## Commands

```bash
wacli groups list [--query TEXT] [--limit N]
wacli groups refresh
wacli groups info --jid GROUP_JID
wacli groups participants list --jid GROUP_JID
wacli groups requests list --jid GROUP_JID
wacli groups invite link get --jid GROUP_JID
wacli groups prune [--days N] [--left-only=false|--include-active] [--dry-run] [--confirm]
```

## Notes

- Group JIDs use the `...@g.us` server.
- `list` reads local rows and hides groups marked left. Human output includes the group type (`group`, `community`, or `subgroup`) and parent community JID when known.
- `list --json` includes `IsParent` for communities and `LinkedParentJID` for subgroups.
- `list` returns at most 50 matching groups by default; non-positive `--limit` values also use 50. When more matching groups exist, stderr warns that the result is truncated (a warning event with `--events`); increase `--limit` to see more. JSON and table output keep their existing shape.
- `refresh` fetches joined groups live and updates local rows, including WhatsApp Community hierarchy metadata exposed by whatsmeow.
- `participants list` reads the last participant snapshot in `wacli.db` without connecting to WhatsApp. It works in read-only mode.
- A participant result can be empty or stale. Run `wacli sync --once --refresh-groups` or `wacli groups refresh` to fetch joined-group info and replace the stored snapshots. Normal sync also refreshes a group snapshot when it stores a message from that group and the live group-info lookup succeeds. It keeps one answer per group for 10 minutes rather than asking with every message, asks again sooner when WhatsApp reports a change to the group or the sync reconnects, and retries a failed lookup after a minute.
- Participant `updated_at` values record when wacli stored the snapshot. They are not WhatsApp join times or membership-change times.
- `info` fetches one group live and persists it, including whether the chat is a Community parent or linked subgroup.
- `requests` lists pending join requests for groups with join approval enabled.
- `prune` only deletes local group/chat/message rows from `wacli.db`. It does not leave WhatsApp groups or delete anything from WhatsApp servers.
- `prune` defaults to groups marked left locally. `--days N` limits left-group pruning to groups left more than `N` days ago.
- `prune --include-active --days N` also targets active groups whose last known local message is older than `N` days. Groups with no known local activity timestamp are skipped.
- Use `prune --dry-run` before deleting and `--confirm` only after reviewing the target list.

## Examples

```bash
wacli groups list --query family
wacli groups refresh
wacli groups info --jid 123456789@g.us
wacli --read-only groups participants list --jid 123456789@g.us --json
wacli groups invite link get --jid 123456789@g.us
wacli groups prune --dry-run
```
