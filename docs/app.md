# app

Read when: using wacli without a terminal on Windows or Linux, or looking for what the Mac app does on another system.

`wacli app` opens wacli as a page in your browser. The page links WhatsApp, turns the scheduled push on and off, and shows how it goes. It is what the [Mac app](install.md#mac-app) is on a Mac, and like the Mac app it holds no logic of its own: the page is the [pairing page](auth.md#pairing-page) of `auth`, and its panel runs [`push install`, `push uninstall` and `push status`](push.md#running-it-on-a-schedule).

## Commands

```bash
wacli app [--lang nl|en] [--no-open] [--port PORT]
```

- On Windows, opening `wacli.exe` from Explorer or from your downloads runs `app`. See [Windows](install.md#windows).
- `--lang nl|en` sets the language of the page. Without it the page follows the browser: Dutch for a Dutch browser, English for every other.
- `--no-open` prints the address without opening a browser.
- `--port PORT` picks the port on `127.0.0.1`. Without it wacli takes a free one.

## The page

- **Link WhatsApp.** The top of the page is the pairing page: the code to scan, then that WhatsApp is linked and how many messages came in. Opening the page again later fetches what is new. When linking ends early the page says why and has a button to start again.
- **Send a copy to a server.** Fill in the address and the key of your receiver and turn it on. wacli checks that the receiver takes them, then the computer pushes every 15 minutes, also when the page is closed. "Copy instructions" puts the text of [`push prompt`](push.md#having-an-agent-build-it) on the clipboard, for the AI agent of your own project that builds the receiver.
- **How it goes.** With the push on, the page shows where it sends, how the last round ended, when the receiver last read your messages and how often today, and how many messages and calls the receiver holds. The last two come from the receiver; see [What the receiver is asked](push.md#what-the-receiver-is-asked). A receiver that gives no status leaves those two lines out.

## Where the page comes from

The page is served by wacli itself, on this computer only.

- wacli listens on `127.0.0.1`, never on the network, and the address holds a random token that is made at start and printed in the terminal: `http://127.0.0.1:PORT/TOKEN/`. A request without the token, or under another host name, gets a 404.
- A change (turn on, turn off, start linking again) is taken only from the page itself: a request from another origin, or without one, gets a 403.
- The page loads nothing from the network and talks to its own address only. The address and the key you fill in go to wacli on this computer, which hands them to `push install` over standard input.
- wacli keeps running while the page is open. It leaves when the page has been closed for about three minutes, or when you close its window or press Ctrl-C. A push that is turned on keeps running: the system starts it, not this command.
- A sync that is still fetching when you turn the push on does not fail the first round: a scheduled round waits up to ten minutes for another wacli that holds the store.

## Examples

```bash
wacli app
wacli app --lang nl
wacli --store ~/work-whatsapp app --no-open
```
