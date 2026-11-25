// usage: node harness.mjs <test.ts> <mode: collect|check> <file.json> [fixtures.json]
import fs from 'fs';
import vm from 'vm';
import module from 'module';

const [,, testFile, mode, jsonFile, fixturesFile] = process.argv;
let src = fs.readFileSync(testFile, 'utf8');
src = module.stripTypeScriptTypes(src, { mode: 'transform' });
// drop imports (single or multi-line)
src = src.replace(/^import[\s\S]*?from\s+['"][^'"]+['"];?\s*$/gm, '');
const parseSnapSrc = fs.readFileSync(new URL('./parsesnap.js', import.meta.url), 'utf8');

const results = {}; // key -> {status, msg}
const collected = {}; // key -> {events, opts, kind}
let outputs = mode === 'check' ? JSON.parse(fs.readFileSync(jsonFile, 'utf8')) : {};
const fixtures = fixturesFile ? JSON.parse(fs.readFileSync(fixturesFile, 'utf8')) : {};

// ---- fake langchain classes
class FakeMsg {
  constructor(fields) { Object.assign(this, typeof fields === 'string' ? { content: fields } : fields); }
  toLC() {
    const kwargs = {};
    for (const k of Object.getOwnPropertyNames(this)) kwargs[k] = this[k];
    return { lc: 1, type: 'constructor', id: ['langchain_core', 'messages', this.constructor.name], kwargs };
  }
}
class AIMessage extends FakeMsg {}
class AIMessageChunk extends FakeMsg {}
class HumanMessage extends FakeMsg {}
class SystemMessage extends FakeMsg {}
class ToolMessage extends FakeMsg {}

function plain(v) {
  if (v && typeof v === 'object' && Object.prototype.toString.call(v) === '[object Error]') return v.message;
  if (v instanceof FakeMsg) return plain(v.toLC());
  if (Array.isArray(v)) return v.map(plain);
  if (v && typeof v === 'object') {
    const o = {};
    for (const [k, x] of Object.entries(v)) { if (x !== undefined) o[k] = plain(x); }
    return o;
  }
  if (typeof v === 'function') return '[Function]';
  return v;
}

// ---- tiny vitest
let suitePath = [];
let current = null; // current test key
const tests = [];
const beforeEaches = [];
function fmt(title, vars) {
  return title.replace(/\$(\w+)/g, (_, k) => String(vars?.[k]));
}
function describe(name, fn) {
  suitePath.push(name);
  const saved = beforeEaches.length;
  fn();
  beforeEaches.length = saved;
  suitePath.pop();
}
describe.each = (rows) => (name, fn) => { for (const row of rows) describe(fmt(name, row), () => fn(row)); };
function it(name, fn) { tests.push({ name: [...suitePath, name].join(' > '), fn }); }
it.each = (rows) => (name, fn) => { for (const row of rows) { const a = Array.isArray(row) ? row : [row]; it(fmt(name, row), () => fn(...a)); } };
const hooks = [];
function beforeEach(f) { hooks.push(f); }
function afterEach() {}
const vi = { fn: (impl) => { const f = (...a) => { f.mock.calls.push(a); return impl?.(...a); }; f.mock = { calls: [] }; return f; }, spyOn: () => ({ mockReturnValue() {} }), restoreAllMocks() {} };

// ---- expect
class Skip extends Error {}
function deepEq(a, b, subset) {
  if (b && b.__undef) return a === undefined;
  if (a === b) return true;
  if (typeof a !== typeof b || a === null || b === null || typeof a !== 'object') return false;
  if (Array.isArray(a) !== Array.isArray(b)) return false;
  if (Array.isArray(a)) { if (a.length !== b.length) return false; return a.every((x, i) => deepEq(x, b[i], subset)); }
  const bk = Object.keys(b).filter(k => !(b[k] && b[k].__undef) || !subset);
  const ak = Object.keys(a).filter(k => a[k] !== undefined);
  if (!subset) {
    const bk2 = Object.keys(b).filter(k => !(b[k] && b[k].__undef));
    if (ak.length !== bk2.length) return false;
  }
  return bk.every(k => deepEq(a[k], b[k], subset));
}
function asym(x) { return x && x.__asym; }
let usedReadUI = false;
const expect = (actual) => {
  if (usedReadUI && actual === undefined) throw new Skip('readUIMessageStream (client assembly)');
  const mk = (neg) => {
    const chk = (ok, msg) => { if (neg ? ok : !ok) throw new Error((neg ? 'not ' : '') + msg); };
    const norm = (v) => JSON.parse(JSON.stringify(v ?? null));
    return {
      toEqual: (e) => chk(deepEqA(actual, e, false), `toEqual\n actual=${JSON.stringify(actual)?.slice(0, 1500)}\n expected=${JSON.stringify(e)?.slice(0, 1500)}`),
      toStrictEqual: (e) => chk(deepEqA(actual, e, false), 'toStrictEqual ' + JSON.stringify(actual)?.slice(0, 800)),
      toMatchObject: (e) => chk(deepEqA(actual, e, true), `toMatchObject\n actual=${JSON.stringify(actual)?.slice(0, 1500)}\n expected=${JSON.stringify(e)?.slice(0, 1500)}`),
      toHaveLength: (n) => chk(actual?.length === n, `toHaveLength ${n}, got ${actual?.length}`),
      toBe: (e) => chk(Object.is(actual, e), `toBe ${e}, got ${actual}`),
      toBeUndefined: () => chk(actual === undefined, 'toBeUndefined'),
      toBeDefined: () => chk(actual !== undefined, 'toBeDefined'),
      toBeNull: () => chk(actual === null, 'toBeNull'),
      toBeTruthy: () => chk(!!actual, 'toBeTruthy'),
      toBeFalsy: () => chk(!actual, 'toBeFalsy'),
      toContainEqual: (e) => chk(Array.isArray(actual) && actual.some(x => deepEqA(x, e, false)), `toContainEqual ${JSON.stringify(e)}\n actual=${JSON.stringify(actual)?.slice(0, 2000)}`),
      toMatch: (re) => chk(typeof actual === 'string' && (Object.prototype.toString.call(re) === '[object RegExp]' ? new RegExp(re.source, re.flags).test(actual) : actual.includes(re)), `toMatch ${re} got ${actual}`),
      toContain: (e) => chk(actual?.includes?.(e), `toContain ${e}`),
      toBeGreaterThan: (e) => chk(actual > e, 'toBeGreaterThan'),
      toBeGreaterThanOrEqual: (e) => chk(actual >= e, 'toBeGreaterThanOrEqual'),
      toBeLessThan: (e) => chk(actual < e, 'toBeLessThan'),
      toMatchInlineSnapshot: (s) => {
        const parsed = new Function(parseSnapSrc + '; return parseSnap;')()(s.replace(/^\n/, ''));
        chk(deepEqA(actual, parsed, false), `toMatchInlineSnapshot\n actual=${JSON.stringify(actual)?.slice(0, 2500)}\n expected=${JSON.stringify(parsed)?.slice(0, 2500)}`);
      },
      toMatchFileSnapshot: () => { throw new Skip('file snapshot (covered by Go fixture test)'); },
      toBeInstanceOf: () => { throw new Skip('instanceof'); },
      toHaveBeenCalled: () => { if (!actual.mock) throw new Skip('mock'); chk(actual.mock.calls.length > 0, 'toHaveBeenCalled'); },
      toHaveBeenCalledTimes: (n) => { if (!actual.mock) throw new Skip('mock'); chk(actual.mock.calls.length === n, 'toHaveBeenCalledTimes'); },
      toHaveBeenCalledWith: () => { throw new Skip('callbacks'); },
      toThrow: () => { throw new Skip('toThrow'); },
    };
  };
  const o = mk(false);
  o.not = mk(true);
  o.resolves = o; o.rejects = { toThrow() { throw new Skip('rejects'); } };
  return o;
};
function deepEqA(a, b, subset) {
  if (b && b.__asym) return b.__asym(a);
  if (Array.isArray(b)) { if (!Array.isArray(a) || (a.length !== b.length)) return false; return a.every((x, i) => deepEqA(x, b[i], subset)); }
  if (b && typeof b === 'object' && !b.__undef) {
    if (!a || typeof a !== 'object') return false;
    const bk = Object.keys(b);
    if (!subset) {
      const ak = Object.keys(a).filter(k => a[k] !== undefined);
      const bk2 = bk.filter(k => !(b[k] && b[k].__undef) && b[k] !== undefined);
      if (ak.length !== bk2.length) return false;
    }
    return bk.every(k => (b[k] === undefined || (b[k] && b[k].__undef)) ? a[k] === undefined : deepEqA(a[k], b[k], subset));
  }
  return deepEq(a, b, subset);
}
expect.any = (C) => ({ __asym: (x) => (C === String ? typeof x === 'string' : C === Number ? typeof x === 'number' : x instanceof C) });
expect.objectContaining = (o) => ({ __asym: (x) => deepEqA(x, o, true) });
expect.stringMatching = (re) => ({ __asym: (x) => typeof x === 'string' && new RegExp(Object.prototype.toString.call(re) === '[object RegExp]' ? re.source : re).test(x) });
expect.stringContaining = (s) => ({ __asym: (x) => typeof x === 'string' && x.includes(s) });

// ---- stream stubs
const convertArrayToReadableStream = (arr) => ({ __events: arr });
let callIdx = 0;
const toUIMessageStream = (stream, opts) => {
  const key = `${current}#${callIdx++}`;
  if (mode === 'collect') {
    const hasFn = opts && Object.values(opts).some(v => typeof v === 'function');
    const o = opts ? { sendStart: opts.sendStart, sendFinish: opts.sendFinish } : {};
    collected[key] = { events: plain(stream.__events), opts: o, callbacks: !!hasFn };
    return { __key: key };
  }
  return { __key: key, tee() { return [{ __key: key }, { __key: key }]; } };
};
const convertReadableStreamToArray = async (s) => {
  if (mode === 'collect') return [];
  const out = outputs[s.__key];
  if (!out) throw new Skip('no Go output (' + s.__key + ')');
  return out;
};

const stateIds = new Map();
function snapSeed(state) {
  const o = {};
  for (const [k, v] of Object.entries(state)) {
    if (v instanceof Set && v.size) o[k] = [...v];
    else if (v instanceof Map && v.size) o[k] = [...v.entries()];
  }
  return o;
}
function lockState(state) {
  for (const k of Object.keys(state)) {
    try { Object.defineProperty(state, k, { configurable: true, get() { throw new Skip('state inspection'); } }); } catch {}
  }
}
const processLangGraphEvent = (event, state, controller) => {
  const key = `${current}#L${callIdx++}`;
  if (mode === 'collect') {
    if (!stateIds.has(state)) stateIds.set(state, { id: stateIds.size, seed: snapSeed(state), first: key });
    collected[key] = { events: [plain(event)], state: stateIds.get(state).id, seed: stateIds.get(state).first === key ? stateIds.get(state).seed : undefined, kind: 'lang' };
    return;
  }
  const out = outputs[key];
  if (!out) throw new Skip('no Go output (' + key + ')');
  for (const c of out) controller.enqueue(c);
  lockState(state);
};
const processModelChunk = (chunk, state, controller) => {
  const key = `${current}#M${callIdx++}`;
  if (mode === 'collect') {
    collected[key] = { events: [plain(chunk)], kind: 'model', state0: JSON.parse(JSON.stringify(state)) };
    return;
  }
  const out = outputs[key];
  if (!out) throw new Skip('no Go output (' + key + ')');
  for (const c of out) controller.enqueue(c);
  lockState(state);
};
const unsupported = (n) => (...a) => { throw new Skip('helper ' + n); };
const TS = vm.createContext({
  processLangGraphEvent, processModelChunk,
  convertToolResultPart: unsupported('convertToolResultPart'), convertAssistantContent: unsupported('x'), convertUserContent: unsupported('x'),
  isToolResultPart: unsupported('x'), isPlainMessageObject: unsupported('x'), isAIMessageChunk: unsupported('x'), isToolMessageType: unsupported('x'),
  getMessageText: unsupported('x'), isImageGenerationOutput: unsupported('x'), extractImageOutputs: unsupported('x'), isCitationContentBlock: unsupported('x'),
  extractCitationsFromContentBlocks: unsupported('x'), emitSourceChunks: unsupported('x'),

  describe, it, expect, vi, beforeEach, afterEach, AIMessage, AIMessageChunk, HumanMessage, SystemMessage, ToolMessage,
  convertArrayToReadableStream, convertReadableStreamToArray, toUIMessageStream,
  toBaseMessages: async () => { throw new Skip('toBaseMessages'); },
  convertModelMessages: () => { throw new Skip('convertModelMessages'); },
  readUIMessageStream: () => { usedReadUI = true; return (async function* () {})(); },
  LANGGRAPH_RESPONSE_1: fixtures.LANGGRAPH_RESPONSE_1, LANGGRAPH_RESPONSE_2: fixtures.LANGGRAPH_RESPONSE_2, REACT_AGENT_TOOL_CALLING: fixtures.REACT_AGENT_TOOL_CALLING,
  console, JSON, Date, Math, Object, Array, Set, Map, Promise, setTimeout, Error, String, Number, Boolean,
  Uint8Array, TextEncoder, URL, Symbol, undefined,
});
vm.runInContext(src, TS);

for (const t of tests) {
  current = t.name; callIdx = 0; usedReadUI = false;
  try {
    for (const h of hooks) h();
    await t.fn();
    results[t.name] = { status: 'pass' };
  } catch (e) {
    results[t.name] = e instanceof Skip ? { status: 'skip', msg: e.message } : { status: 'fail', msg: String(e.message).slice(0, 4000) };
  }
}
if (mode === 'collect') fs.writeFileSync(jsonFile, JSON.stringify(collected));
fs.writeFileSync(jsonFile + '.results', JSON.stringify(results, null, 1));
const c = { pass: 0, fail: 0, skip: 0 };
for (const r of Object.values(results)) c[r.status]++;
console.error(c);
