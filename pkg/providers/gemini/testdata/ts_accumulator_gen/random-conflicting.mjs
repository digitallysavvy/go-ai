// Generates random sequences whose paths may conflict (a key under a string,
// an index on an object, ...) and records what the TS GoogleJSONAccumulator
// does: it throws a TypeError, or coerces (null + "x", "[object Object]x").
// Only the throwing sequences are kept in testdata/ts_accumulator_cases.jsonl.gz.
//
//   cp <ai repo>/packages/google/src/google-json-accumulator.ts acc.ts
//   node random-conflicting.mjs <seed> <count> out.jsonl
import { GoogleJSONAccumulator } from './acc.ts';
import fs from 'node:fs';
let seed = Number(process.argv[2] || 1);
const rnd = () => { seed = (seed * 1664525 + 1013904223) >>> 0; return seed / 2 ** 32; };
const pick = a => a[Math.floor(rnd() * a.length)];
const paths = ['$.a','$.b','$.a.b','$.a.c','$.l[0]','$.l[1]','$.l[2]','$.l[0].x','$.l[1].x','$.l[0].y','$.l[]','$.a[]','$.a.b[]','$.o.l[0]','$.o.l[1].n','$.o.l[]','$.o','$.o.p','$.m[0][0]','$.m[0][1]','$.m[1][0]','$.m[0][]','$.m[]','$.l[0][0]','$.a.b.c','$.__proto__.z','$.constructor.prototype.q','$.s','$','$.','$.x y','$.q\"r','$.l[5]','$.é'];
const strs = ['', 'x', 'hello', 'a"b', 'line\nbreak', ' ', 'é😀', '\\'];
const nums = [0, 1, 50, -3, 1.5, 1e21, 1e-7, 123456789012];
function arg() {
  const a = { jsonPath: pick(paths) };
  const k = rnd();
  if (k < 0.3) { a.stringValue = pick(strs); if (rnd() < 0.5) a.willContinue = rnd() < 0.7; }
  else if (k < 0.45) a.numberValue = pick(nums);
  else if (k < 0.55) a.boolValue = rnd() < 0.5;
  else if (k < 0.62) a.nullValue = null;
  else if (k < 0.7) { a.stringValue = null; a.numberValue = pick(nums); }
  else if (k < 0.75) { a.stringValue = pick(strs); a.numberValue = 5; }
  else if (k < 0.8) { a.boolValue = false; }
  else { if (rnd() < 0.3) a.willContinue = rnd() < 0.5; }
  return a;
}
const N = Number(process.argv[3] || 3000);
const out = [];
for (let i = 0; i < N; i++) {
  const nargs = 1 + Math.floor(rnd() * 9);
  const chunks = []; let cur = [];
  for (let j = 0; j < nargs; j++) { cur.push(arg()); if (rnd() < 0.4) { chunks.push(cur); cur = []; } }
  if (cur.length) chunks.push(cur);
  const acc = new GoogleJSONAccumulator();
  const steps = []; let err = null;
  for (const c of chunks) {
    try { const r = acc.processPartialArgs(JSON.parse(JSON.stringify(c))); steps.push({ current: JSON.parse(JSON.stringify(r.currentJSON)), delta: r.textDelta }); }
    catch (e) { steps.push({ err: String(e).slice(0, 80) }); err = true; break; }
  }
  let fin = null;
  if (!err) { try { const f = acc.finalize(); fin = { finalJSON: f.finalJSON, closing: f.closingDelta }; } catch (e) { fin = { err: String(e) }; } }
  out.push({ chunks, steps, fin });
}
fs.writeFileSync(process.argv[4] || 'corpus.jsonl', out.map(o => JSON.stringify(o)).join('\n') + '\n');
