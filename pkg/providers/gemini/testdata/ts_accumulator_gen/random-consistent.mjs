// Generates random sequences that never conflict (each path keeps one value
// kind) and records what the TS GoogleJSONAccumulator does with them.
//
//   cp <ai repo>/packages/google/src/google-json-accumulator.ts acc.ts
//   node random-consistent.mjs <seed> <count> out.jsonl   (Node >= 22.6 strips the types)
//
// Part of testdata/ts_accumulator_cases.jsonl.gz (TS e64ed82f27).
import { GoogleJSONAccumulator } from './acc.ts';
import fs from 'node:fs';
let seed = Number(process.argv[2] || 7);
const rnd = () => { seed = (seed * 1664525 + 1013904223) >>> 0; return seed / 2 ** 32; };
const pick = a => a[Math.floor(rnd() * a.length)];
// path -> value kind, fixed so sequences never conflict
const P = [
  ['$.a','s'],['$.b','n'],['$.c','b'],['$.d','z'],['$.e','e'],['$.f','a'],
  ['$.o.p','s'],['$.o.q','n'],['$.o.r.s','s'],['$.o.t','e'],['$.o.u','a'],
  ['$.l[0]','s'],['$.l[1]','s'],['$.l[2]','n'],['$.l[3]','z'],
  ['$.r[0].x','s'],['$.r[0].y','n'],['$.r[1].x','s'],['$.r[1].y','b'],['$.r[2].x','s'],['$.r[0].z','a'],
  ['$.m[0][0]','n'],['$.m[0][1]','s'],['$.m[1][0]','b'],['$.m[1][1]','e'],['$.m[2]','a'],
  ['$.__proto__.z','s'],['$.constructor.prototype.q','n'],['$.hasOwnProperty','s'],
  ['$.ünï.çödé','s'],['$.q"r','s'],['$.a b','n'],['$.w[0].v[0]','s'],['$.w[0].v[1]','n'],['$.w[1].v[0]','e'],
];
const strs = ['', 'x', 'hello', 'a"b', 'line\nbreak', '  ', 'é😀', '\\', '</script>', 'a\u0000b'];
const nums = [0, 1, 50, -3, 1.5, 0.1, 100, 1e21, 1e-7, 123456789012, 9007199254740993, 5e-324, 1.7976931348623157e308];
function entries(path, kind) {
  const out = [];
  if (kind === 's') {
    const parts = 1 + Math.floor(rnd() * 3);
    for (let i = 0; i < parts; i++) {
      const a = { jsonPath: path, stringValue: pick(strs) };
      if (rnd() < 0.5) a.willContinue = i < parts - 1 || rnd() < 0.2;
      out.push(a);
    }
    if (rnd() < 0.2) out.push({ jsonPath: path, willContinue: false }); // terminator
  } else if (kind === 'n') out.push({ jsonPath: path, numberValue: pick(nums) });
  else if (kind === 'b') out.push({ jsonPath: path, boolValue: rnd() < 0.5 });
  else if (kind === 'z') out.push({ jsonPath: path, nullValue: null });
  else if (kind === 'e') out.push({ jsonPath: path, ...(rnd() < 0.3 ? { willContinue: false } : {}) });
  else if (kind === 'a') { out.push({ jsonPath: path + '[]' }); }
  return out;
}
const N = Number(process.argv[3] || 2000);
const out = [];
for (let i = 0; i < N; i++) {
  const n = 1 + Math.floor(rnd() * 8);
  const args = [];
  for (let j = 0; j < n; j++) { const [p, k] = pick(P); args.push(...entries(p, k)); }
  if (rnd() < 0.1) args.push({ jsonPath: pick(['$', '$.', '']) });
  const chunks = []; let cur = [];
  for (const a of args) { cur.push(a); if (rnd() < 0.35) { chunks.push(cur); cur = []; } }
  if (cur.length) chunks.push(cur);
  const acc = new GoogleJSONAccumulator();
  const steps = []; let err = null;
  for (const c of chunks) {
    try { const r = acc.processPartialArgs(JSON.parse(JSON.stringify(c))); steps.push({ current: JSON.parse(JSON.stringify(r.currentJSON)), delta: r.textDelta }); }
    catch (e) { steps.push({ err: String(e).slice(0, 80) }); err = true; break; }
  }
  let fin = null;
  if (!err) { const f = acc.finalize(); fin = { finalJSON: f.finalJSON, closing: f.closingDelta }; }
  out.push({ chunks, steps, fin });
}
fs.writeFileSync(process.argv[4] || 'corpus2.jsonl', out.map(o => JSON.stringify(o)).join('\n') + '\n');
