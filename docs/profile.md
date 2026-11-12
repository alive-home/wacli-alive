# profile

Read when: fetching profile metadata for a WhatsApp user.

`wacli profile` fetches profile metadata for a WhatsApp user. This build cannot change the linked account's name, About text, or picture.

## Command

```bash
wacli profile picture-info --jid <jid-or-phone> [--preview] [--existing-id ID]
wacli profile get-about --jid <jid-or-phone>
wacli profile business --jid <jid-or-phone>
```

## Notes

- `picture-info` fetches metadata and a CDN URL for the target's profile picture. Pass `--preview` for preview metadata or `--existing-id` to let WhatsApp report unchanged pictures.
- `get-about` fetches a target user's current About text.
- `business` fetches a target WhatsApp Business profile when available.
- Live fetch commands do not mutate WhatsApp profile settings, but they still open the WhatsApp session store and therefore cannot run with `--read-only`.

## Examples

```bash
wacli profile picture-info --jid +15551234567 --json
wacli profile get-about --jid 15551234567@s.whatsapp.net
wacli profile business --jid +15551234567 --json
```
