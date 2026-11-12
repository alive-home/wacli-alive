# auth

Read when: pairing a store, checking auth state, or choosing QR vs phone pairing.

`wacli auth` connects interactively and bootstraps sync after successful pairing. `wacli sync` never shows a QR code, so use `auth` first for a new store or named account.

## Commands

```bash
wacli auth [--open] [--follow] [--idle-exit 30s] [--download-media] [--qr-format terminal|text] [--phone PHONE] [--events]
wacli auth status
wacli auth logout
wacli --account work auth status
```

## Notes

- Default pairing prints a terminal QR code.
- `--open` shows the QR code on a page in the default browser instead of in the terminal, for people who would rather not use one. See [Pairing page](#pairing-page).
- `--qr-format text` prints the raw QR payload for external renderers.
- `--phone PHONE` uses WhatsApp phone-number pairing instead of QR pairing.
- `--history-days N` asks the primary device for the last N days of history instead of its whole recent window (about three months), and `--history-max-per-chat M` caps how many messages each chat brings. Both are sent in the pairing handshake, so they apply to this pairing only: they do nothing on a device that is already linked, and the primary decides whether to honour them. Use them when linking again to repair a gap, where the whole window would re-import messages you already hold.
- `--full-history` asks the primary device for a full history sync instead of its recent window of about three months; `--history-days N` then bounds how far back it goes. `--history-size-mb N` sets how many megabytes the primary may put into that full sync, which otherwise follows the primary's own default. Like the limits above, they travel in the pairing handshake and the primary decides what to send: a personal account was seen stopping at about 50 MB without `--history-size-mb`, and at three years back whatever `--history-days` asked.
- A full sync arrives in chunks over a long time, and the primary can pause it for minutes or hours (seen while the phone was on mobile data). Bootstrap auth exits once nothing has arrived for `--idle-exit`, which does not mean the primary has finished: the archive may still be incomplete. Pair with `--follow` to keep receiving it, or run `wacli sync --follow` on the same store afterwards. The chunks the primary has not sent yet arrive on later connections, without pairing again.
- Transient websocket drops before pairing completes are retried with a fresh QR/code.
- Passkey-gated pairing is not yet supported. If WhatsApp requests passkey verification or confirmation, auth stops with an actionable error instead of continuing to rotate unusable QR codes.
- After pairing, auth runs bootstrap sync until idle unless `--follow` is set.
- Bootstrap sync honors `WACLI_SYNC_MAX_MESSAGES` and `WACLI_SYNC_MAX_DB_SIZE` to cap local history growth.
- `--events` emits NDJSON lifecycle events on stderr, including raw QR and phone-pairing codes for external renderers.
- `auth status` reports whether the local store is authenticated. A recorded remote logout overrides a stale device row until WhatsApp confirms a new login.
- `auth logout` clears the local session so the store can pair again, for example after WhatsApp revoked it. It does not contact WhatsApp: to remove the device there, open WhatsApp on the phone and remove it under Linked devices. Synced messages in `wacli.db` are kept.
- For multiple accounts, prefer `wacli accounts add NAME`; it creates an isolated account store and runs the same auth/bootstrap flow.

## Pairing page

`wacli auth --open` writes `pair.html` and `pair.js` to the store directory (owner-only, like the rest of the store) and opens `pair.html` in the default browser. Nothing is served: the browser reads the files from disk, and the page loads nothing from the network.

- The page shows the QR code with the four steps to scan it, replaces the code when WhatsApp rotates it, and says when WhatsApp is linked, how many messages were fetched, or that pairing stopped. It is in Dutch or English, after the browser's language.
- Below that it tells the person what the link can read, how it works, where the data is stored, and where the source can be read. It ends with a text they can copy into their AI assistant, which explains that this build only reads and lists the commands to start with.
- The page follows the running command. When the command ends or is killed, the page says so within a minute; start `wacli auth --open` again for a new code.
- With `--events`, a `pair_page` event carries the path and whether a browser was opened, and `qr_code` events are still emitted. Without `--events`, the path is printed on stderr, also when no browser could be opened.
- `--open` cannot be combined with `--phone` or `--qr-format text`.
- A browser that is sandboxed away from hidden folders (some Linux snap and flatpak browsers) cannot read the page; pair in the terminal there.

## Examples

```bash
wacli auth
wacli auth --open
wacli auth --qr-format text
wacli auth --phone "+1 (234) 567-8900"
wacli auth --download-media
wacli auth --history-days 1
wacli auth --full-history --history-days 3650 --history-size-mb 2048 --follow
wacli auth status --json
```
