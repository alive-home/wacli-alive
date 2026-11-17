#!/usr/bin/env bun
// A receiver for `wacli push`, small enough to read in one go. It checks the
// signature and keeps what arrives in memory. The four functions under
// "storage" are the part a real receiver replaces with its own database.
//
//   bun scripts/push-receiver-example.mjs --key-file KEYFILE [--port 8787]
//   wacli push --url http://127.0.0.1:8787/ --key-file KEYFILE --allow-private
//
// It prints counts only, never what a message says.
import { createHash, createHmac, timingSafeEqual } from "node:crypto";
import { readFileSync } from "node:fs";

const args = process.argv.slice(2);
const flag = (name, fallback) => (args.includes(name) ? args[args.indexOf(name) + 1] : fallback);
const keyFile = flag("--key-file");
if (!keyFile) {
  console.error("usage: bun scripts/push-receiver-example.mjs --key-file KEYFILE [--port 8787]");
  process.exit(2);
}

// One key here. A receiver for a team keeps one per person and looks the
// person up by key id; who a request is from never comes from its body.
const key = readFileSync(keyFile, "utf8").trim();
const keys = new Map([[createHash("sha256").update(key).digest("hex").slice(0, 16), { key, owner: "me" }]]);

// --- storage ---------------------------------------------------------------
const messages = new Map(); // owner + chat_jid + msg_id -> message
const calls = new Map(); // owner + chat_jid + call_id + event_type -> call event
const senders = new Map(); // owner -> last sign of life

function upsertMessages(owner, list) {
  for (const m of list) messages.set(`${owner}|${m.chat_jid}|${m.msg_id}`, { ...m, removed_at: null });
}

function removeMessages(owner, list) {
  for (const r of list) {
    const id = `${owner}|${r.chat_jid}|${r.msg_id}`;
    if (r.moved_to_msg_id) messages.delete(id); // the same message arrives under its new id
    else if (messages.has(id)) messages.set(id, { chat_jid: r.chat_jid, msg_id: r.msg_id, removed_at: new Date(), reason: r.reason });
  }
}

function upsertCalls(owner, list) {
  for (const c of list) calls.set(`${owner}|${c.chat_jid}|${c.call_id}|${c.event_type}`, c);
}

function touchSender(owner, body) {
  senders.set(owner, { seen_at: new Date(), linked: body.linked, last_sync_at: body.last_sync_at, caught_up: !body.more, wacli_version: body.wacli_version });
}
// ---------------------------------------------------------------------------

function verified(request, raw) {
  const entry = keys.get(request.headers.get("x-wacli-key-id") ?? "");
  if (!entry) return null;
  const want = Buffer.from("sha256=" + createHmac("sha256", entry.key).update(raw).digest("hex"));
  const got = Buffer.from(request.headers.get("x-wacli-signature") ?? "");
  return got.length === want.length && timingSafeEqual(got, want) ? entry.owner : null;
}

const server = Bun.serve({
  hostname: "127.0.0.1",
  port: Number(flag("--port", "8787")),
  maxRequestBodySize: 8 << 20,
  async fetch(request) {
    if (request.method !== "POST") return new Response(null, { status: 405 });
    const raw = Buffer.from(await request.arrayBuffer()); // the signature covers these exact bytes
    const owner = verified(request, raw);
    if (!owner) return new Response(null, { status: 401 });
    const body = JSON.parse(raw);
    if (body.schema !== 1) return new Response(null, { status: 400 });
    // A copy of an old request is refused; wacli signs sent_at with the rest.
    if (Math.abs(Date.now() - Date.parse(body.sent_at)) > 5 * 60_000) return new Response(null, { status: 401 });

    upsertMessages(owner, body.messages);
    removeMessages(owner, body.removed);
    upsertCalls(owner, body.calls);
    touchSender(owner, body);

    const removed = [...messages.values()].filter((m) => m.removed_at).length;
    console.log(
      `in: ${body.messages.length} messages, ${body.removed.length} removed, ${body.calls.length} calls, more=${body.more}` +
        ` | stored: ${messages.size - removed} messages, ${removed} removed, ${calls.size} calls`,
    );
    return new Response(null, { status: 204 });
  },
});
console.log(`listening on http://127.0.0.1:${server.port}/`);
