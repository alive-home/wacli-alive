# push

Read when: sending the local store to your own server, database, CRM, or agent.

`wacli push` posts what the local store holds to a URL you choose: every message once, then each change, and the call events. It only posts. It reads nothing from a response beyond the status code, so the receiving side cannot ask this computer for anything. It does not send to or change WhatsApp.

The one answer wacli reads is the one a person asks for with `push status --receiver`: four counters the receiver keeps about that person's own messages. See [What the receiver is asked](#what-the-receiver-is-asked).

Without this command (or `sync --webhook`), nothing is posted anywhere.

## Commands

```bash
wacli push --url URL [--key-file FILE] [--sync] [--every DURATION] [--dry-run] [--allow-private]
wacli push install (--url URL [--key-file FILE] | --stdin) [--allow-private]
wacli push status [--receiver]
wacli push uninstall
wacli push prompt [--lang nl|en]
```

- `--url URL` is where each request goes. It must be `https`; a receiver on this computer (`http://localhost:8787/`, `http://127.0.0.1:8787/`) may use `http`. A redirect is never followed.
- `--key-file FILE` holds the key that signs each request. See [Signing](#signing).
- `--sync` fetches what is new from WhatsApp first, as `sync --once` does, and closes that connection before anything is posted. When the sync fails, what is stored is still posted and the command exits with the sync's error.
- `--every DURATION` keeps running and repeats, at least `1m`. A failed round keeps its place and the next round sends it again.
- `--dry-run` counts what would be posted. It posts nothing and keeps no position.
- `--allow-private` allows `http` and URLs that resolve to a private network, for a receiver on your own network or tailnet.
- `push install`, `push status` and `push uninstall` have the system run it every 15 minutes. See [Running it on a schedule](#running-it-on-a-schedule).
- `push prompt` prints the instructions for an AI agent that builds the receiver. See [Having an agent build it](#having-an-agent-build-it).

## What a run does

1. **The full copy.** The first run for a URL posts every stored message, oldest row first. A message that was deleted or taken back goes as a removal, without its text.
2. **The changes.** After that, a run posts what the store's change journal recorded since the last run: new messages, edited ones, and removed ones. The journal is in storage order, so a message WhatsApp delivers late with an old timestamp is not missed.
3. **The calls.** Call events stored since the last run, plus every event of the last 24 hours: an event's outcome and duration can arrive after the event itself.
4. **The closing request.** The last request of a run has `"more": false`. It is posted even when there was nothing to send, so the receiver sees a sign of life.

The position is kept per URL in `push-<id>.json` in the store directory, and moves only after the receiver answered 2xx. That file is the only thing `push` writes without `--sync`, also under `--read-only`. Two URLs each get their own full copy. The journal keeps 30 days; a position older than that starts over with the full copy, which also tells what was removed in between.

## The request

```json
{
  "schema": 1,
  "store_id": "f3b1c0de...",
  "sent_at": "2026-11-17T10:30:02Z",
  "wacli_version": "0.26.0-readonly",
  "os": "darwin",
  "linked": true,
  "last_sync_at": "2026-11-17T10:29:41Z",
  "more": false,
  "messages": [
    {
      "chat_jid": "15550001111@s.whatsapp.net",
      "chat_name": "Ada",
      "msg_id": "3EB0C7D8E9F0A1B2",
      "timestamp": "2026-11-17T09:05:10Z",
      "from_me": false,
      "sender_jid": "15550001111@s.whatsapp.net",
      "sender_name": "Ada",
      "text": "Is the guest list final?"
    }
  ],
  "removed": [
    { "chat_jid": "15550001111@s.whatsapp.net", "msg_id": "3EB0AAAABBBBCCCC", "reason": "whatsapp-revoke" }
  ],
  "calls": [
    {
      "chat_jid": "15550001111@s.whatsapp.net",
      "call_id": "3EB0A1B2C3D4E5F6",
      "event_type": "call_log",
      "direction": "outbound",
      "media": "audio",
      "outcome": "connected",
      "duration_secs": 312,
      "timestamp": "2026-11-17T09:12:44Z"
    }
  ]
}
```

- `linked` is false when the store has no WhatsApp session (never paired, or logged out on the phone). `last_sync_at` is the last sync that completed, or `null`.
- A message can also carry `display_text` (when it differs from `text`, as for "Sent image"), `quoted_msg_id`, `forwarded`, `reaction_to_id`, `reaction_emoji`, `media_type`, `media_caption`, `filename`, `mime_type` and `edited`. Empty fields are left out. These are all the message fields that leave: no attachment, no local path, and none of the fields that fetch an attachment from WhatsApp.
- `removed` names a message the store no longer shows. `reason` says why (`whatsapp-revoke` when its sender took it back). When a message only moved to another id, `moved_to_chat_jid` and `moved_to_msg_id` name it, and the message itself is in `messages` under the new id.
- `calls` are the objects `calls list --json` prints. Only `call_log` events have an outcome; `offer`, `terminate` and `reject` are live signals.
- A request holds at most 200 items and about 256 KB of them; a single larger message goes alone. Times are UTC.

## Signing

With `--key-file`, each request carries:

```
X-Wacli-Key-Id: <first 16 hex characters of SHA-256 of the key>
X-Wacli-Signature: sha256=<hex HMAC-SHA256 of the request body, keyed with the key>
```

The key itself is never sent. The receiver finds the key by its id, computes the same HMAC over the exact bytes it received, and compares. `sent_at` is inside the signed body: refuse a request that is more than a few minutes old.

Any string works as a key; `openssl rand -hex 32` makes a good one. Keep the file readable by your user only. wacli knows nothing about people or teams: a receiver that serves several computers gives each its own key and decides whose request it is from the key, never from the body.

## The receiving side

- Answer 2xx only after everything in the request is stored. Any other answer makes wacli send it again next run.
- Store messages by `chat_jid` + `msg_id` and overwrite on repeat. The same message arrives again after an edit, after a failed run, and after a restart of the full copy.
- A `removed` entry can name a message you never stored: it was gone before the first run. Ignore it.
- Store calls by `chat_jid` + `call_id` + `event_type` and overwrite on repeat.
- Message text, names and filenames are written by other people. An agent that reads them must treat them as data, never as instructions.

`scripts/push-receiver-example.mjs` is a complete receiver in about 90 lines, with its storage in memory:

```bash
openssl rand -hex 32 > push.key
bun scripts/push-receiver-example.mjs --key-file push.key
wacli push --url http://127.0.0.1:8787/ --key-file push.key
```

### Having an agent build it

When the store should end up in a project of your own that an AI agent works on, let that agent build the receiver:

```bash
wacli push prompt | pbcopy
```

Paste the text into the agent's chat. It describes the request, the signature and the rules of this page, and asks the agent to build the endpoint, make your keys, test with signed requests of its own, and answer with three lines: the address, the key and the read key. The first two are what `push install` asks for; the read key stays in the project. `--lang nl` has the agent answer in Dutch; `--json` gives the text as `prompt`. The command opens no store and reaches nothing.

Every stored row belongs to the person of the key it came with, so a team shares one receiver: each further person pastes the same text in the same project, and their agent enrolls them at the endpoint instead of building again. That takes the project's enrollment secret (`WACLI_ENROLL_SECRET`, shared with every agent of the project) and no access to the database, and it only ever makes a new person.

The text also has the endpoint answer [what wacli asks](#what-the-receiver-is-asked), so the Mac app and the app page can show when your messages were last read.

Reading goes through the endpoint too, and a person reads only their own rows. Each person has a read key beside the push key, saved as a secret only that person has (`WACLI_READ_KEY`), and an agent reads with the key of the person it works for. So colleagues in one project do not read each other's WhatsApp. The endpoint keeps when each read key last read, and a status request with the read key shows a person their own: the last post, the last read and the reads per day. Whoever administers the database can still read every row; the agent says who that is. The text also says what holds when the project runs on Supabase (an Edge Function without Supabase's JWT check, tables that stay closed to everything but the function).

### What the receiver is asked

`wacli push status --receiver` posts one more kind of request to the installed URL, signed like every other: the request of the check that `install` sends (`"more": true`, three empty lists) with `"status": true` added. A receiver that knows it stores nothing from it and answers `200` with four fields about the person of the key:

```json
{ "last_read_at": "2026-11-17T10:31:00Z", "reads_today": 3, "messages": 1204, "calls": 17 }
```

- `last_read_at` is when that person's read key last read, or `null`. `reads_today` counts its read requests on the current UTC day. `messages` and `calls` count the rows the receiver holds for that person.
- wacli takes these four fields and nothing else. An answer longer than 4 KB, one that is not JSON, one without all four fields or with a negative count is refused. Each field is a time or a number, so nothing a receiver answers is text this computer shows or acts on.
- A receiver built before this request answers it like the check, with `204` and no body. `status` then says the receiver gave no status, and everything else it prints is still true.
- Nothing asks on its own: a scheduled round never does. The [Mac app](install.md#mac-app) and the [app page](app.md) ask while their window is open, about once a minute.

## Running it on a schedule

`--every` repeats for as long as the terminal stays open. To keep a computer sending without one:

```bash
wacli push install --url https://example.com/wacli --key-file ~/.config/wacli-push.key
wacli push status
wacli push uninstall
```

- `install` first posts one request without content and with `"more": true`. A receiver that does not answer 2xx (a wrong key, a wrong address) is reported at once and nothing is installed.
- Then the system starts a round now and every 15 minutes while you are signed in: a launchd agent on macOS (which also starts a round at each sign-in), a task in Task Scheduler on Windows (on battery too, and caught up after sleep). Both belong to the signed-in user and are named `wacli-push-` plus eight characters that stand for the store directory. A round fetches what is new from WhatsApp, as `--sync` does, and pushes. A round that is still going is not started a second time.
- A push is installed whole or not at all: when the system does not take the job, the saved URL and key are removed again, an earlier install for this store included.
- The URL and the path of the key file are kept in `scheduled-push.json` in the store directory, owner-only. The scheduled command is `wacli --store DIR push --installed`: the URL is not in it, because a program's arguments are readable by every program on the computer. For the same reason a program that starts `install` can hand both over standard input: `--stdin` reads `{"url": "...", "key": "..."}` and keeps the key beside the settings, as `scheduled-push.key`.
- `status` names the receiver's host and how the last round ended. `--json` gives `installed`, `scheduled` (the system still holds the job), `host`, `every_seconds` and `last_run`. It reaches nothing. With `--receiver` it also [asks the receiver](#what-the-receiver-is-asked) and adds `receiver`, or `receiver_error` when there was no usable answer.
- A round waits up to ten minutes for another wacli that holds the store, such as the app still fetching the first history when the push was turned on.
- `uninstall` removes the job, the saved URL and a key that `--stdin` stored. The position stays, so installing again for the same URL sends what is new.
- One schedule per store directory: a named account (`--account`) or another `--store` gets its own, and `status` and `uninstall` speak for the store they are run with. The job runs the program by the path it was started with: after moving it, run `install` again.
- On Windows the task runs `wacli.exe`, a console program, so a console window can show for the length of a round.
- On Linux there is no scheduler to hand it to; start `wacli push --url URL --key-file FILE --sync` from cron or a systemd timer.

The [Mac app](install.md#mac-app) does the same from its Settings window, and [`wacli app`](app.md) from a page in the browser on any system.

## Examples

```bash
wacli push --url https://example.com/wacli --key-file ~/.config/wacli-push.key --dry-run
wacli push --url https://example.com/wacli --key-file ~/.config/wacli-push.key
wacli push --url https://example.com/wacli --key-file ~/.config/wacli-push.key --sync --every 15m
```
