// Runs a TS provider test file with stubbed vitest / test-server / model and
// records, per test, the response the test prepared before each model call.
// usage: node capture.mjs <test.ts> <ModelClassName> <cases.json>  (writes merged JSON to stdout)
// See README.md ("Regenerating").
import fs from 'fs';
import path from 'path';
import vm from 'vm';
import { stripTypeScriptTypes } from 'node:module';

const [testFile, modelClass, casesFile] = process.argv.slice(2);
let src = fs.readFileSync(testFile, 'utf8');
const importedNames = [];
for (const m of src.matchAll(/^import\s+(?:type\s+)?([\s\S]*?)\s+from\s+['"][^'"]+['"];?/gm)) {
  const spec = m[1];
  for (const n of spec.replace(/[{}]/g, ',').split(',')) {
    const t = n.trim().replace(/^type\s+/, '').replace(/^\*\s+as\s+/, '');
    const alias = t.split(/\s+as\s+/).pop();
    if (/^[A-Za-z_$][\w$]*$/.test(alias)) importedNames.push(alias);
  }
}
src = src.replace(/^import[\s\S]*?from\s+['"][^'"]+['"];?\s*$/gm, '');
src = stripTypeScriptTypes(src, { mode: 'strip' });

process.on('unhandledRejection', () => {});
const tests = [];
const beforeEachFns = [];
const stack = [];
const SENTINEL = { sentinel: true };
let current = null;
function clone(v) { return v === undefined ? undefined : JSON.parse(JSON.stringify(v)); }
const server = { urls: new Proxy({}, { get(t, k) { if (!(k in t)) t[k] = {}; return t[k]; } }), calls: [] };
class Model {
  constructor(id) { this.modelId = id; }
  capture(kind) {
    const routes = {};
    for (const [u, v] of Object.entries(server.urls)) routes[u] = clone(v.response);
    const now = JSON.stringify(Object.entries(server.urls).map(([u, v]) => [u, v.response]));
    current.captures.push({ kind, modelId: this.modelId, routes, setByTest: now !== current.base || current.base === undefined });
    throw SENTINEL;
  }
  async doGenerate() { return this.capture('generate'); }
  async doStream() { return this.capture('stream'); }
}
const anyProxy = new Proxy(function () {}, { get: (t, k) => (k === 'then' ? undefined : anyProxy), apply: () => anyProxy });
const ctx = {
  describe: (name, fn) => { stack.push(name); fn(); stack.pop(); },
  it: (name, fn) => tests.push({ name, fn, describe: stack[0], path: stack.slice(), befores: [...beforeEachFns, ...(tests.nestedBefore ?? [])].filter(b => b.depth.every((d, i) => stack[i] === d)) }),
  expect: anyProxy,
  createTestServer: () => server,
  mockId: () => () => 'id',
  convertReadableStreamToArray: async () => [],
  readFileSync: (p, enc) => { const a = path.resolve(path.dirname(testFile), p); const b = path.resolve(testFile.replace(/\/src\/.*$/, ''), p); return fs.readFileSync(fs.existsSync(a) ? a : b, enc); },
  InvalidArgumentError: class extends Error {}, APICallError: class extends Error {},
  [modelClass]: Model,
  createAnthropic: () => { const f = id => new Model(id); f.languageModel = f; f.chat = f; f.messages = f; return f; },
  createGoogle: () => { const f = id => new Model(id); return new Proxy(f, { get: (t, k) => (k in t ? t[k] : f) }); },
  vi: anyProxy, beforeEach: fn => { (stack.length ? (tests.nestedBefore ??= []) : beforeEachFns).push({ depth: stack.slice(), fn }); }, afterEach: () => {}, beforeAll: () => {}, afterAll: () => {},
  fs: { readFileSync: (p, enc) => { const a = path.resolve(path.dirname(testFile), p); const b = path.resolve(testFile.replace(/\/src\/.*$/, ''), p); return fs.readFileSync(fs.existsSync(a) ? a : b, enc); } },
  console, JSON, Object, Array, String, Number, Math, Date, Promise, Error, Map, Set, Buffer, URL,
};
ctx.describe.each = () => () => {}; ctx.it.each = () => () => {}; ctx.it.skip = () => {}; ctx.describe.skip = () => {};
for (const n of importedNames) if (!(n in ctx)) ctx[n] = anyProxy;
vm.runInNewContext(src, ctx, { filename: testFile });
const captured = {};
for (const t of tests) {
  current = { captures: [] };
  for (const k of Object.keys(server.urls)) delete server.urls[k];
  try { for (const b of t.befores) await b.fn(); current.base = JSON.stringify(Object.entries(server.urls).map(([u, v]) => [u, v.response])); await t.fn(); } catch (e) { if (e !== SENTINEL) console.error('test', t.name, 'error', e && e.message); }
  (captured[t.describe + '\0' + t.name] ??= []).push(...current.captures);
}
function toResp(r) {
  if (!r) return null;
  if (r.type === 'json-value') return { kind: 'json', body: r.body, headers: r.headers };
  if (r.type === 'stream-chunks') return { kind: 'chunks', chunks: r.chunks, headers: r.headers };
  if (r.type === 'error') return { kind: 'error', status: r.status, text: r.body, headers: r.headers };
  console.error('unknown response type', r.type);
  return null;
}
const cases = JSON.parse(fs.readFileSync(casesFile, 'utf8'));
const used = {};
let filled = 0;
for (const c of cases) {
  const key = c.topDescribe + '\0' + c.test;
  const caps = captured[key] || [];
  const i = used[key] = (used[key] ?? -1) + 1;
  const cap = caps[i];
  if (cap && !c.modelId && cap.modelId) { c.modelId = cap.modelId; console.error('model id from capture', c.test, cap.modelId); }
  if (c.response || c.routes || !['generate','stream'].includes(c.callKind)) continue;
  if (!cap) { console.error('no capture for', c.test, i); continue; }
  if (!cap.setByTest) { console.error('response not set by the test', c.test); continue; }
  filled++;
  const entries = Object.entries(cap.routes).filter(([, r]) => r);
  if (entries.some(([, r]) => r.type === 'error')) { console.error('error response (rejects test)', c.test); continue; }
  if (entries.length === 1) c.response = toResp(entries[0][1]);
  else { c.routes = entries.map(([u, r]) => ({ path: decodeURIComponent(new URL(u).pathname), response: toResp(r) })); c.response = c.routes[0].response; }
}
console.error('filled', filled);
process.stdout.write(JSON.stringify(cases));
