# channels

Read when: listing or inspecting WhatsApp Channels.

`wacli channels` reads WhatsApp Channels, which `whatsmeow` calls newsletters. Commands use live WhatsApp APIs and require authentication. Commands that update the local chat cache require writable mode. This build cannot join, leave, or post to channels.

## Commands

```bash
wacli channels list
wacli channels info --jid CHANNEL_JID
```

## Notes

- Channel JIDs use the `...@newsletter` server.
- `channels list` fetches subscribed channels live and updates local chat rows with kind `newsletter`.
- `channels info` fetches one joined channel live and updates the local chat row.
- `sync --refresh-channels` refreshes subscribed channel names into the local chat cache.

## Examples

```bash
wacli channels list
wacli channels info --jid 123456789012345@newsletter
```
