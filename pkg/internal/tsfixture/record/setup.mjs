import { beforeEach, afterEach, expect } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';

const OUT = process.env.REC_OUT;
let cur = null;

function ser(v, depth = 0, seen = new Set()) {
  if (v === undefined) return undefined;
  if (v === null || typeof v === 'string' || typeof v === 'number' || typeof v === 'boolean') return v;
  if (typeof v === 'bigint') return Number(v);
  if (typeof v === 'function') return { $fn: true };
  if (v instanceof Uint8Array) return { $bytes: Buffer.from(v).toString('base64') };
  if (v instanceof ArrayBuffer) return { $bytes: Buffer.from(v).toString('base64') };
  if (v instanceof URL) return { $url: v.href };
  if (v instanceof Date) return { $date: v.toISOString() };
  if (v instanceof Error) {
    const o = { $error: { name: v.name, message: v.message } };
    for (const k of ['statusCode', 'url', 'responseBody', 'isRetryable', 'data', 'type', 'code', 'param']) if (v[k] !== undefined) o.$error[k] = ser(v[k], depth + 1, seen);
    return o;
  }
  if (typeof v === 'object') {
    if (typeof v.aborted === 'boolean' && typeof v.addEventListener === 'function') return undefined; // AbortSignal
    if (v instanceof ReadableStream) return { $stream: true };
    if (typeof Blob !== 'undefined' && v instanceof Blob) return { $blob: true, size: v.size, type: v.type };
    if (seen.has(v) || depth > 14) return { $circular: true };
    seen.add(v);
    let out;
    if (Array.isArray(v)) out = v.map(x => ser(x, depth + 1, seen) ?? null);
    else if (v instanceof Map) out = { $map: [...v.entries()].map(([k, x]) => [ser(k, depth + 1, seen), ser(x, depth + 1, seen)]) };
    else {
      out = {};
      for (const k of Object.keys(v)) {
        const x = ser(v[k], depth + 1, seen);
        if (x !== undefined) out[k] = x;
      }
    }
    seen.delete(v);
    return out;
  }
  return String(v);
}
globalThis.__recSer = ser;

globalThis.__recHttpStart = (url, request) => {
  if (!cur) return null;
  const headers = {};
  request.headers.forEach((v, k) => { headers[k] = v; });
  const rec = { routeUrl: url, method: request.method, url: request.url, headers };
  cur.http.push(rec);
  return rec;
};
globalThis.__recHttpResponse = (rec, response) => {
  if (!rec) return;
  const r = { type: response.type, headers: response.headers ?? {} };
  if (response.type === 'json-value') r.body = ser(response.body);
  else if (response.type === 'stream-chunks') r.chunks = response.chunks;
  else if (response.type === 'binary') r.bodyB64 = Buffer.from(response.body).toString('base64');
  else if (response.type === 'error') { r.status = response.status ?? 500; r.body = response.body ?? 'Error'; }
  else if (response.type === 'empty') r.status = response.status ?? 200;
  rec.response = r;
};

// Some TS tests inject a mock `fetch` into the model config instead of using the
// test server. Wrap config.fetch once so those exchanges are recorded in the
// same shape the test server hook writes (marked mockFetch).
function recordMockFetch(cfg) {
  if (!cfg || typeof cfg.fetch !== 'function' || cfg.fetch.__recWrapped) return;
  const orig = cfg.fetch;
  const wrapped = async function (input, init) {
    const c = globalThis.__recCur;
    let rec = null;
    if (c) {
      try {
        const req = typeof Request !== 'undefined' && input instanceof Request ? input : null;
        const url = req ? req.url : input instanceof URL ? input.href : String(input);
        const headers = {};
        const h = init?.headers ?? req?.headers;
        if (h) {
          if (typeof h.forEach === 'function' && !Array.isArray(h)) h.forEach((v, k) => { headers[String(k).toLowerCase()] = v; });
          else if (Array.isArray(h)) for (const [k, v] of h) headers[String(k).toLowerCase()] = v;
          else for (const k of Object.keys(h)) headers[k.toLowerCase()] = h[k];
        }
        rec = { routeUrl: url, method: init?.method ?? req?.method ?? 'GET', url, headers, mockFetch: true };
        const body = init?.body;
        if (typeof body === 'string') rec.bodyB64 = Buffer.from(body).toString('base64');
        else if (body instanceof Uint8Array || body instanceof ArrayBuffer) rec.bodyB64 = Buffer.from(body).toString('base64');
        else if (typeof FormData !== 'undefined' && body instanceof FormData) {
          // serialize the multipart body the way fetch would send it
          const r = new Response(body);
          rec.headers['content-type'] = r.headers.get('content-type');
          rec.bodyB64 = Buffer.from(await r.arrayBuffer()).toString('base64');
        }
        c.http.push(rec);
      } catch (e) { rec = null; }
    }
    const res = await orig.apply(this, arguments);
    // The wrapped fetch may end in the test server, whose hook already
    // recorded the exchange: keep that one only.
    if (rec && globalThis.__recCur) {
      const list = globalThis.__recCur.http;
      const i = list.indexOf(rec);
      if (i >= 0 && list.slice(i + 1).some(x => !x.mockFetch)) {
        list.splice(i, 1);
        rec = null;
      }
    }
    if (rec) {
      try {
        const clone = res.clone();
        const rh = {};
        res.headers.forEach((v, k) => { rh[k] = v; });
        const ct = res.headers.get('content-type') ?? '';
        if (!res.ok) rec.response = { type: 'error', status: res.status, headers: rh, body: await clone.text() };
        else if (ct.includes('event-stream')) rec.response = { type: 'stream-chunks', headers: rh, chunks: [await clone.text()] };
        else if (ct.includes('json')) {
          const text = await clone.text();
          try { rec.response = { type: 'json-value', headers: rh, body: JSON.parse(text) }; }
          catch (e) { rec.response = { type: 'binary', headers: rh, bodyB64: Buffer.from(text).toString('base64') }; }
        } else rec.response = { type: 'binary', headers: rh, bodyB64: Buffer.from(await clone.arrayBuffer()).toString('base64') };
      } catch (e) { /* unreadable: the replay skips the test */ }
    }
    return res;
  };
  wrapped.__recWrapped = true;
  try { cfg.fetch = wrapped; } catch (e) { /* frozen config */ }
}

const names = ['doGenerate','doStream','doEmbed','doRerank','doStart','doStatus','doDecide','doGetBatchResults','doStartBatch','doGetBatchStatus','doCancelBatch','doCreateClientSecret','doCreateWebRTCSession','parseServerEvent','serializeClientEvent'];
let instSeq = 0;
for (const n of names) {
  Object.defineProperty(Object.prototype, '__cap_' + n, {
    enumerable: false, configurable: true, writable: true,
    value: function (...args) {
      const rec = { method: n, args: ser(args), model: undefined };
      try {
        rec.model = { modelId: this.modelId, provider: this.provider, cls: this.constructor?.name };
        if (n === 'parseServerEvent' || n === 'serializeClientEvent') { if (!this.__recInst) Object.defineProperty(this, '__recInst', { value: ++instSeq, enumerable: false }); rec.model.inst = this.__recInst; }
        if (this.config ?? this.model?.config) { // a decision model wraps a language model
          const c = this.config ?? this.model.config;
          rec.model.config = { provider: c.provider, baseURL: c.baseURL };
          // plain settings (booleans, numbers, strings, string lists) that change behavior
          for (const k of Object.keys(c)) {
            const v = c[k];
            if (['provider', 'baseURL', 'headers', 'url'].includes(k)) continue;
            if (['boolean', 'number', 'string'].includes(typeof v) || (Array.isArray(v) && v.every(x => typeof x === 'string'))) rec.model.config[k] = v;
          }
          rec.model.config.hasTransformRequestBody = typeof c.transformRequestBody === 'function';
          try { const h = typeof c.headers === 'function' ? c.headers() : c.headers; rec.model.config.headers = ser(h); } catch (e) {}
          try { if (typeof c.url === 'function') rec.model.config.url = c.url({ modelId: this.modelId, path: '/PATH' }); } catch (e) {}
        }
      } catch (e) {}
      if (cur) cur.calls.push(rec);
      try { recordMockFetch(this.config ?? this.model?.config); } catch (e) {} // decision models wrap a language model
      let r;
      try { r = this[n](...args); } catch (e) { rec.error = ser(e); throw e; }
      if (r && typeof r.then === 'function') {
        return r.then(v => {
          if (v && typeof v === 'object' && v.stream) { const { stream, ...rest } = v; rec.result = ser(rest); rec.streamed = true; }
          else rec.result = ser(v);
          return v;
        }, e => { rec.error = ser(e); throw e; });
      }
      rec.result = ser(r);
      return r;
    },
  });
}

beforeEach(() => { cur = { calls: [], http: [] }; globalThis.__recCur = cur; });
afterEach(() => {
  const st = expect.getState();
  const rel = path.relative(process.cwd(), st.testPath ?? 'unknown');
  const entry = { file: rel, test: st.currentTestName, calls: cur.calls, http: cur.http };
  fs.mkdirSync(OUT, { recursive: true });
  fs.appendFileSync(path.join(OUT, path.basename(process.cwd()) + '.jsonl'), JSON.stringify(entry) + '\n');
  cur = null;
});
