// Records the frames the real TS runBridge (ai@7.0.113 packages/harness/src/bridge/index.ts)
// sends to a host, for the Go bridge transport tests.
//
// Regenerating the *.ndjson fixtures in this directory:
//   1. In a scratch directory: `npm init -y && npm pkg set type=module && npm i ws@8`.
//   2. Copy `ai/packages/harness/src/bridge/index.ts` there as `bridge.ts`,
//      together with `harness-bridge-capability-unsupported-error.ts` (the
//      relative import `runBridge` pulls in).
//   3. In that copy of `bridge.ts`, change the
//      `./harness-bridge-capability-unsupported-error` import to use the
//      explicit `.ts` extension (Node's type-stripping loader needs it).
//   4. Copy this script into the same scratch directory and run
//      `node record.mts > rec.out` (Node >= 24, which strips types natively).
//   5. Split `rec.out` on the `### <name>` headers into `<name>.ndjson` files
//      here, and set the port in `ready.ndjson` to 4319 (a fixed, readable
//      value — the real recording used whatever ephemeral port the OS
//      assigned `runBridge`'s `port: 0`).
// The `{"_close":{...}}` lines are recorder annotations appended by this
// script (see `record('unauthorized', ...)`/`record('resume', ...)` below),
// not real bridge frames — Go's fixture-replay test skips them.
import { WebSocket } from 'ws';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { runBridge } from './bridge.ts';

const TOKEN = 'integ-token';
const out: Record<string, string[]> = {};

function record(name: string, url: string) {
  const frames: string[] = [];
  out[name] = frames;
  const ws = new WebSocket(url);
  const closed = new Promise<{ code: number; reason: string }>(resolve =>
    ws.on('close', (code, reason) => resolve({ code, reason: reason.toString() })),
  );
  ws.on('message', raw => frames.push(raw.toString()));
  const opened = new Promise<void>(r => ws.on('open', () => r()));
  return { ws, frames, closed, opened };
}

async function waitUntil(pred: () => boolean, ms = 3000) {
  const s = Date.now();
  while (!pred()) {
    if (Date.now() - s > ms) throw new Error('timeout');
    await new Promise(r => setTimeout(r, 5));
  }
}

let release!: () => void;
const gate = new Promise<void>(r => (release = r));
let steer!: () => void;
const steered = new Promise<void>(r => (steer = r));

const handle = await runBridge<{ type: 'start' }>({
  bridgeType: 'test',
  bridgeStateDir: mkdtempSync(join(tmpdir(), 'rec-')),
  port: 0,
  token: TOKEN,
  onExit: () => {},
  onStop: () => ({ threadId: 'thread-1' }),
  onStart: async (_s, turn) => {
    turn.emit({ type: 'stream-start', warnings: [] } as never);
    turn.emit({ type: 'text-start', id: 'm' } as never);
    turn.emit({ type: 'text-delta', id: 'm', delta: 'one' } as never);
    turn.emit({ type: 'text-delta', id: 'm', delta: 'two' } as never);
    await gate;
    turn.emit({ type: 'text-delta', id: 'm', delta: 'three' } as never);
    // accept one steering message
    const it = turn.experimental_userMessages[Symbol.asyncIterator]();
    steer();
    const next = await it.next();
    if (!next.done) next.value.accept();
    turn.emit({ type: 'text-end', id: 'm' } as never);
    turn.emit({
      type: 'finish',
      finishReason: { unified: 'stop', raw: 'stop' },
      totalUsage: {
        inputTokens: { total: 1 },
        outputTokens: { total: 2 },
      },
    } as never);
  },
});

const base = `ws://127.0.0.1:${handle.port}/`;

// unauthorized
const bad = record('unauthorized', `${base}?agent_bridge_token=wrong`);
const badClose = await bad.closed;
out['unauthorized'].push(JSON.stringify({ _close: badClose }));

// A: start, get first deltas, then drop.
const a = record('live', `${base}?agent_bridge_token=${TOKEN}`);
await a.opened;
await waitUntil(() => a.frames.length >= 1);
a.ws.send(JSON.stringify({ type: 'start', prompt: 'hi' }));
await waitUntil(() => a.frames.filter(f => f.includes('text-delta')).length === 2);
a.ws.terminate();

// B: reconnect with resume from seq 3 (after first delta), release turn, steer, finish.
const b = record('resume', `${base}?agent_bridge_token=${TOKEN}`);
await b.opened;
await waitUntil(() => b.frames.length >= 1);
b.ws.send(JSON.stringify({ type: 'resume', lastSeenEventId: 3 }));
await waitUntil(() => b.frames.length >= 2);
release();
await steered;
b.ws.send(JSON.stringify({ type: 'user-message', messageId: 'msg-1', text: 'Change course.' }));
await waitUntil(() => b.frames.some(f => f.includes('"finish"')));
// user-message outside a turn
b.ws.send(JSON.stringify({ type: 'user-message', messageId: 'msg-2', text: 'late' }));
await waitUntil(() => b.frames.some(f => f.includes('msg-2')));
b.ws.send(JSON.stringify({ type: 'stop' }));
const bClose = await b.closed;
out['resume'].push(JSON.stringify({ _close: bClose }));

await handle.close();
for (const [name, frames] of Object.entries(out)) {
  console.log(`### ${name}`);
  for (const f of frames) console.log(f);
}
process.exit(0);
