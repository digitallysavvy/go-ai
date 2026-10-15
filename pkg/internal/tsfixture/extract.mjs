// Extracts TS AI SDK test cases (canned wire response + call options + vitest
// snapshots) into JSON consumed by the Go tsfixture package.
//
//   cd <ai repo>/packages && node <this file> <test.ts> | gzip -9n > cases.json.gz
//
// Cases are committed gzipped (see README.md in this directory).
//
// Each case carries: test name, response (json body / SSE chunks / fixture
// file contents), call kind and options (evaluated literal), model id, the
// top-level describe, and the snapshots for the assertions it makes.
import fs from 'fs';
import vm from 'vm';
import path from 'path';

const testFile = process.argv[2];
const wantAsserts = process.argv.includes('--asserts') || process.argv.some(a => a.startsWith('--fn='));
const fnName = (process.argv.find(a => a.startsWith('--fn=')) || '').slice(5) || null;
const src = fs.readFileSync(testFile, 'utf8');
const snapPath = path.join(path.dirname(testFile), '__snapshots__', path.basename(testFile) + '.snap');
let extSnaps = {};
if (fs.existsSync(snapPath)) {
  const exportsObj = {};
  try { vm.runInNewContext(fs.readFileSync(snapPath, 'utf8'), { exports: exportsObj }); } catch (e) { console.error('snap err', e.message); }
  extSnaps = exportsObj;
}

function dedent(t) {
  const lines = t.split('\n');
  let min = 1e9;
  for (const l of lines) { if (l.trim() === '') continue; const m = /^( *)/.exec(l)[1].length; if (m < min) min = m; }
  if (min === 1e9 || min === 0) return t;
  return lines.map(l => l.slice(Math.min(min, /^( *)/.exec(l)[1].length))).join('\n');
}

// pretty-format parser (vitest snapshots do not escape quotes inside strings)
function parseSnap(text) {
  let i = 0;
  const s = text;
  const ws = () => { while (i < s.length && /\s/.test(s[i])) i++; };
  function str() {
    i++;
    const st = i;
    while (i < s.length) {
      if (s[i] === '"') {
        const j = i + 1;
        let k = j;
        while (k < s.length && (s[k] === ' ' || s[k] === '\t')) k++;
        if (k >= s.length) { const v = s.slice(st, i); i = j; return v; }
        if (s[k] === ',' || s[k] === '\n') {
          let q = k + 1;
          if (s[k] === ',') q = k + 2;
          while (q < s.length && /\s/.test(s[q])) q++;
          if (q >= s.length || '"}]{['.includes(s[q])) { const v = s.slice(st, i); i = j; return v; }
        }
      }
      i++;
    }
    throw new Error('unterminated string');
  }
  function val() {
    ws();
    const c = s[i];
    if (c === '"') return str();
    if (c === '{') {
      i++;
      const o = {};
      for (;;) {
        ws();
        if (s[i] === '}') { i++; break; }
        if (s[i] === ',') { i++; continue; }
        if (s[i] !== '"') throw new Error('bad key at ' + i + ': ' + s.slice(i, i + 30));
        i++;
        const ks = i;
        const ke = s.indexOf('": ', i);
        const key = s.slice(ks, ke);
        i = ke + 3;
        o[key] = val();
      }
      return o;
    }
    if (s.startsWith('Set {', i)) {
      i += 5;
      const items = [];
      for (;;) {
        ws();
        if (s[i] === '}') { i++; break; }
        if (s[i] === ',') { i++; continue; }
        items.push(val());
      }
      return { __set: items };
    }
    if (c === '[' && /^\[[A-Za-z]\w*(Error)?:/.test(s.slice(i, i + 60))) {
      const e = s.indexOf('],\n', i);
      const v = s.slice(i, e + 1);
      i = e + 1;
      return v;
    }
    if (c === '[') {
      i++;
      const a = [];
      for (;;) {
        ws();
        if (s[i] === ']') { i++; break; }
        if (s[i] === ',') { i++; continue; }
        a.push(val());
      }
      return a;
    }
    const m = /^(\d{4}-\d\d-\d\dT[\d:.]+Z|-?\d[\d.eE+-]*|null|undefined|true|false|\[Function[^\]]*\]|\[Circular[^\]]*\]|Any<[^>]*>)/.exec(s.slice(i, i + 80));
    if (m) {
      i += m[0].length;
      const t = m[0];
      if (t === 'null') return null;
      if (t === 'undefined') return { __undef: true };
      if (t === 'true') return true;
      if (t === 'false') return false;
      if (/^-?\d/.test(t) && !/T/.test(t)) return Number(t);
      return t;
    }
    throw new Error('bad value at ' + i + ': ' + s.slice(i, i + 30));
  }
  try { return val(); } catch (e) { return { __err: e.message }; }
}

// balanced scan from index i (at an opening bracket) -> index after the match
function balanced(s, i) {
  let depth = 0;
  for (let k = i; k < s.length; k++) {
    const c = s[k];
    if (c === "'" || c === '"') { k++; while (k < s.length && s[k] !== c) { if (s[k] === '\\') k++; k++; } continue; }
    if (c === '`') { k = tpl(s, k + 1); continue; }
    if (c === '/' && s[k + 1] === '/') { while (k < s.length && s[k] !== '\n') k++; continue; }
    if (c === '/' && s[k + 1] === '*') { k = s.indexOf('*/', k + 2) + 1; continue; }
    if ('{[('.includes(c)) depth++;
    if ('}])'.includes(c)) { depth--; if (depth === 0) return k + 1; }
  }
  return -1;
}
function tpl(s, k) {
  for (; k < s.length; k++) {
    if (s[k] === '\\') { k++; continue; }
    if (s[k] === '`') return k;
    if (s[k] === '$' && s[k + 1] === '{') { const e = balanced(s, k + 1); k = e - 1; }
  }
  return k;
}

// top-level consts (TEST_PROMPT, TEST_TOOLS, ...)
const ctx = vm.createContext({ JSON, Date, Math, Buffer, Uint8Array, TextEncoder, URL, Object, Array, String, Number });
if (wantAsserts) {
  // helpers the converter tests build their arguments with, kept as plain data
  vm.runInContext(`
    var createToolNameMapping = (o) => ({ __toolNameMapping: { tools: (o && o.tools) || [], providerToolNames: (o && o.providerToolNames) || {} } });
    var CacheControlValidator = function () { return { __cacheControlValidator: true }; };
    var structuredClone = (v) => JSON.parse(JSON.stringify(v));
  `, ctx);
}
{
  const re = wantAsserts ? /^(?:export )?(?:const|let) (\w+)(?::[^=]+)?\s*=\s+/gm : /^(?:export )?(?:const|let) (\w+)(?::[^=]+)? = /gm;
  let m;
  while ((m = re.exec(src))) {
    const k = m.index + m[0].length;
    let j = k;
    while (j < src.length && /\s/.test(src[j])) j++;
    let end;
    if ('{[('.includes(src[j])) end = balanced(src, j); else end = src.indexOf('\n', j);
    let e2 = end;
    while (e2 < src.length && src[e2] !== ';' && src[e2] !== '\n') e2++;
    if (wantAsserts) { const se = stmtEnd(src, j); if (se > 0) e2 = se; }
    try { vm.runInContext(`var ${m[1]} = (${stripTS(trimSemi(src.slice(k, e2)))})`, ctx); } catch (e) { /* skip */ }
  }
}
// strip TS-only syntax so the literal evaluates as JS
function stripTS(s) { return s.replace(/\s+satisfies\s+[\w.]+(<[^>]*>)?/g, '').replace(/\s+as\s+const\b/g, ''); }
// end of the statement that starts at j: the next ';' outside brackets/strings
function stmtEnd(s, j) {
  for (let k = j; k < s.length; k++) {
    const c = s[k];
    if (c === "'" || c === '"') { k++; while (k < s.length && s[k] !== c) { if (s[k] === '\\') k++; k++; } continue; }
    if (c === '`') { k = tpl(s, k + 1); continue; }
    if ('{[('.includes(c)) { const e = balanced(s, k); if (e < 0) return -1; k = e - 1; continue; }
    if (c === ';') return k;
  }
  return -1;
}
// --asserts only: a one-line declaration's slice keeps its trailing ';'
function trimSemi(s) { return wantAsserts ? s.replace(/;\s*$/, '') : s; }
function evalExpr(s) { return vm.runInContext('(' + stripTS(s) + ')', ctx); }

const re = /\n(\s*)(it|test)(?:\.each\([^)]*\))?\(\s*(['"`])((?:\\.|(?!\3).)*)\3/g;
const starts = [];
let m;
while ((m = re.exec(src))) starts.push({ idx: m.index, name: m[4], line: ' '.repeat(m[1].split('\n').pop().length) + 'it' });

// Nested consts (a describe-local TEST_PROMPT shadowing the module one) are
// evaluated per case when their scope still encloses the test.
const nestedDecls = [];
{
  const reN = /^([ \t]+)(?:const|let) (\w+)(?::[^=]+)? = /gm;
  let mN;
  while ((mN = reN.exec(src))) {
    const k = mN.index + mN[0].length;
    let j = k;
    while (j < src.length && /\s/.test(src[j])) j++;
    let end;
    if ('{[('.includes(src[j])) end = balanced(src, j); else end = src.indexOf('\n', j);
    if (end < 0) continue;
    let e2 = end;
    while (e2 < src.length && src[e2] !== ';' && src[e2] !== '\n') e2++;
    if (wantAsserts) { const se = stmtEnd(src, j); if (se > 0) e2 = se; }
    nestedDecls.push({ pos: mN.index, indent: mN[1].length, name: mN[2], expr: src.slice(k, e2) });
  }
}
// A declaration is visible to a test when the block that declares it (from
// the previous less-indented line to the next one) contains the test, wherever
// in that block it sits: the test body runs after the whole block has run.
const srcLines = src.split('\n');
const lineStart = [];
{ let off = 0; for (const l of srcLines) { lineStart.push(off); off += l.length + 1; } }
function lineOf(pos) { let lo = 0, hi = lineStart.length - 1; while (lo < hi) { const mid = (lo + hi + 1) >> 1; if (lineStart[mid] <= pos) lo = mid; else hi = mid - 1; } return lo; }
const indentOfLine = l => /^[ \t]*/.exec(l)[0].length;
for (const d of nestedDecls) {
  const ln = lineOf(d.pos);
  let open = ln - 1;
  while (open >= 0 && (srcLines[open].trim() === '' || indentOfLine(srcLines[open]) >= d.indent)) open--;
  let close = ln + 1;
  while (close < srcLines.length && (srcLines[close].trim() === '' || indentOfLine(srcLines[close]) >= d.indent)) close++;
  d.openPos = open >= 0 ? lineStart[open] : 0;
  d.closePos = close < srcLines.length ? lineStart[close] : src.length;
}
function scopedDecls(testIdx, testIndent) {
  return nestedDecls
    .filter(d => d.indent <= testIndent && d.openPos <= testIdx && testIdx < d.closePos)
    .sort((x, y) => x.indent - y.indent || x.pos - y.pos);
}

function splitTopLevelArgs(inner) {
  const parts = [];
  let cur = '';
  for (let k = 0; k < inner.length; k++) {
    const c = inner[k];
    if (c === "'" || c === '"') { const q = c; let j = k + 1; while (j < inner.length && inner[j] !== q) { if (inner[j] === '\\') j++; j++; } cur += inner.slice(k, j + 1); k = j; continue; }
    if (c === '`') { const j = tpl(inner, k + 1); cur += inner.slice(k, j + 1); k = j; continue; }
    if ('{[('.includes(c)) { const j = balanced(inner, k); cur += inner.slice(k, j); k = j - 1; continue; }
    if (c === ',') { parts.push(cur); cur = ''; continue; }
    cur += c;
  }
  if (cur.trim() !== '') parts.push(cur);
  return parts.map(p => p.trim());
}
function evalArgsJSON(srcs) {
  return srcs.map(a => JSON.parse(JSON.stringify(evalExpr(a), (kk, v) => v instanceof Uint8Array ? { __bytes: Buffer.from(v).toString('base64') } : (Object.prototype.toString.call(v) === '[object Set]' ? { __set: [...v] } : (v === undefined ? { __undef: true } : v)))));
}

function topDescribeOf(idx) {
  const ms = [...src.slice(0, idx).matchAll(/^describe\(\s*(['"`])(.*?)\1/gm)];
  return ms.length ? ms[ms.length - 1][2] : '';
}

const out = [];
for (let i = 0; i < starts.length; i++) {
  const saved = {};
  for (const d of scopedDecls(starts[i].idx, starts[i].line.length - 2)) {
    if (!(d.name in saved)) saved[d.name] = ctx[d.name];
    try { vm.runInContext(`var ${d.name} = (${stripTS(trimSemi(d.expr))})`, ctx); } catch (e) { /* keep module value */ }
  }
  let body0 = src.slice(starts[i].idx, i + 1 < starts.length ? starts[i + 1].idx : src.length);
  { const op = body0.indexOf('('); const e = op >= 0 ? balanced(body0, op) : -1; if (e > 0) body0 = body0.slice(0, e); }
  const name = starts[i].name;
  let body = body0;
  if (!/prepare(?:Json|Chunks)FixtureResponse|type:\s*'(json-value|stream-chunks)'/.test(body0) || !/do(Generate|Stream)\(/.test(body0)) {
    // use the enclosing describe's beforeEach (same indentation as the it)
    const upto = src.slice(0, starts[i].idx + 1);
    const lines = upto.split('\n');
    const indent = starts[i].line.length - 2;
    let found = -1;
    for (let ln = lines.length - 1; ln >= 0; ln--) {
      const mm = /^(\s*)describe(?:\.\w+)?(?:\.each\([^)]*\))?\(/.exec(lines[ln]);
      if (mm && mm[1].length < indent) { found = ln; break; }
    }
    if (found >= 0) {
      const region = lines.slice(found).join('\n');
      const re2 = new RegExp('^ {' + indent + '}beforeEach\\(', 'gm');
      const be = [...region.matchAll(re2)];
      if (be.length) {
        const bi = be[be.length - 1].index;
        const e = balanced(region, bi + indent + 'beforeEach'.length);
        body = region.slice(bi, e) + '\n' + body0;
      }
    }
  }

  if (fnName) {
    // function-level test: expect(...) over the return value of fnName(args)
    const saved2 = {};
    const own = [];
    const reD = /(?:const|let)\s+(\w+)(?::[^=]+)?\s*=\s*/g;
    let dm;
    while ((dm = reD.exec(body0))) {
      const st = dm.index + dm[0].length;
      const se = stmtEnd(body0, st);
      if (se < 0) continue;
      own.push({ name: dm[1], expr: body0.slice(st, se) });
    }
    for (const d of own) {
      if (!(d.name in saved2)) saved2[d.name] = ctx[d.name];
      try { vm.runInContext(`var ${d.name} = (${stripTS(d.expr)})`, ctx); } catch (e) { /* not a literal */ }
    }
    const cm = new RegExp('\\b' + fnName + '\\(').exec(body0);
    let fnArgs = null, fnArgSrc = null;
    if (cm) {
      const st = cm.index + cm[0].length - 1;
      const e = balanced(body0, st);
      const inner = body0.slice(st + 1, e - 1);
      const parts = [];
      let depth = 0, cur = '';
      for (let k = 0; k < inner.length; k++) {
        const c = inner[k];
        if (c === "'" || c === '"') { const q = c; let j = k + 1; while (j < inner.length && inner[j] !== q) { if (inner[j] === '\\') j++; j++; } cur += inner.slice(k, j + 1); k = j; continue; }
        if (c === '`') { const j = tpl(inner, k + 1); cur += inner.slice(k, j + 1); k = j; continue; }
        if ('{[('.includes(c)) { const j = balanced(inner, k); cur += inner.slice(k, j); k = j - 1; continue; }
        if (c === ',') { parts.push(cur); cur = ''; continue; }
        cur += c;
      }
      if (cur.trim() !== '') parts.push(cur);
      fnArgSrc = parts.map(p => p.trim());
      try {
        fnArgs = fnArgSrc.map(a => JSON.parse(JSON.stringify(evalExpr(a), (kk, v) => v instanceof Uint8Array ? { __bytes: Buffer.from(v).toString('base64') } : (Object.prototype.toString.call(v) === '[object Set]' ? { __set: [...v] } : (v === undefined ? { __undef: true } : v)))));
      } catch (err) { fnArgs = null; }
    }
    const aliases2 = {};
    const dstr = new RegExp('(?:const|let)\\s*\\{([^}]*)\\}\\s*=\\s*await\\s+' + fnName + '\\(').exec(body0);
    if (dstr) for (const nm of dstr[1].split(',')) { const n = nm.trim().split(':').pop().trim(); if (n) aliases2[n] = 'result.' + nm.trim().split(':')[0].trim(); }
    const rv = new RegExp('(?:const|let)\\s+(\\w+)\\s*=\\s*await\\s+' + fnName + '\\(').exec(body0);
    if (rv) aliases2[rv[1]] = 'result';
    const asserts2 = [];
    const reE2 = /\bexpect\(/g;
    let em2;
    while ((em2 = reE2.exec(body0))) {
      const st = em2.index + 'expect'.length;
      const e = balanced(body0, st);
      if (e < 0) continue;
      const expr = body0.slice(st + 1, e - 1).trim().replace(/^await\s+/, '');
      let k = e;
      let not = false;
      let rejects = false;
      if (body0.startsWith('.rejects', k)) { rejects = true; k += 8; }
      if (body0.startsWith('.not', k)) { not = true; k += 4; }
      const mm3 = /^\.(toEqual|toStrictEqual|toBe|toMatchObject|toHaveLength|toBeUndefined|toBeNull|toBeDefined|toContain|toBeGreaterThan|toBeTruthy|toBeFalsy|toThrowError|toThrow)\(/.exec(body0.slice(k));
      if (!mm3) continue;
      const as = k + mm3[0].length - 1;
      const ae = balanced(body0, as);
      if (ae < 0) continue;
      const argSrc = body0.slice(as + 1, ae - 1).trim();
      let arg = null, ok = true;
      if (argSrc !== '') { try { arg = JSON.parse(JSON.stringify(evalExpr(argSrc), (kk, v) => v instanceof Uint8Array ? { __bytes: Buffer.from(v).toString('base64') } : (Object.prototype.toString.call(v) === '[object Set]' ? { __set: [...v] } : (v === undefined ? { __undef: true } : v)))); } catch (err) { ok = false; arg = argSrc; } }
      const matcher = (rejects ? 'rejects.' : '') + mm3[1];
      if (!ok && !/toThrow/.test(matcher)) continue;
      const a2 = { expr, matcher, not, arg: ok ? arg : null };
      if (new RegExp('^' + fnName + '\\(').test(expr)) {
        try {
          const open = expr.indexOf('(');
          a2.callArgs = evalArgsJSON(splitTopLevelArgs(expr.slice(open + 1, balanced(expr, open) - 1)));
          a2.callSuffix = expr.slice(balanced(expr, open)).replace(/[,\s]+$/, '');
        } catch (err) { continue; }
      }
      asserts2.push(a2);
    }
    {
      const reIS = /expect\(([^\n]*?)\)\s*\.toMatchInlineSnapshot\(\s*`/g;
      let is;
      while ((is = reIS.exec(body0))) {
        const s0 = is.index + is[0].length;
        const k = tpl(body0, s0);
        const snap = parseSnap(dedent(body0.slice(s0, k).replace(/\\([\\`$])/g, '$1')));
        if (snap && snap.__err) continue;
        const e2 = is[1].trim().replace(/^await\s+/, '');
        const a3 = { expr: e2, matcher: 'toEqual', not: false, arg: snap };
        if (new RegExp('^' + fnName + '\\(').test(e2)) {
          try { const open = e2.indexOf('('); a3.callArgs = evalArgsJSON(splitTopLevelArgs(e2.slice(open + 1, balanced(e2, open) - 1))); a3.callSuffix = e2.slice(balanced(e2, open)).replace(/[,\s]+$/, ''); } catch (err) { continue; }
        }
        asserts2.push(a3);
      }
    }
    for (const n of Object.keys(saved2)) { if (saved2[n] === undefined) delete ctx[n]; else ctx[n] = saved2[n]; }
    for (const n of Object.keys(saved)) { if (saved[n] === undefined) delete ctx[n]; else ctx[n] = saved[n]; }
    // a test that calls the function several times outside its assertions is
    // stateful or compares calls: only the first call is modeled, so skip it
    const totalCalls = (body0.match(new RegExp('\\b' + fnName + '\\(', 'g')) || []).length;
    const ownCalls = asserts2.filter(a => a.callArgs).length;
    if (totalCalls - ownCalls > 1) { for (const n of Object.keys(saved2)) { if (saved2[n] === undefined) delete ctx[n]; else ctx[n] = saved2[n]; } for (const n of Object.keys(saved)) { if (saved[n] === undefined) delete ctx[n]; else ctx[n] = saved[n]; } continue; }
    if ((cm && fnArgs || asserts2.some(a => a.callArgs)) && asserts2.length) out.push({ test: name, topDescribe: topDescribeOf(starts[i].idx), fnArgs: fnArgs || [], fnArgSrc: fnArgSrc || [], asserts: asserts2, aliases: aliases2 });
    continue;
  }

  let response = null;
  const fx = body.match(/prepare(Json|Chunks)FixtureResponse\(\s*['"`]([^'"`]+)['"`]/);
  if (fx) {
    response = { kind: fx[1] === 'Json' ? 'fixture-json' : 'fixture-chunks', name: fx[2] };
    const ext = fx[1] === 'Json' ? '.json' : '.chunks.txt';
    for (const d of [path.dirname(testFile), path.dirname(path.dirname(testFile)), path.dirname(path.dirname(path.dirname(testFile)))]) {
      const f = path.join(d, '__fixtures__', fx[2] + ext);
      if (fs.existsSync(f)) { response.raw = fs.readFileSync(f, 'utf8'); break; }
    }
    if (!response.raw) response.error = 'fixture missing';
  } else {
    const r = body.match(/type:\s*'(json-value|stream-chunks)'/);
    if (r) {
      if (r[1] === 'json-value') {
        const bi = body.indexOf('body:', r.index);
        if (bi >= 0) {
          let j = bi + 5;
          while (/\s/.test(body[j])) j++;
          const e = balanced(body, j);
          try { response = { kind: 'json', body: evalExpr(body.slice(j, e)) }; } catch (err) { response = { kind: 'json', error: err.message }; }
        }
      } else {
        const ci = body.indexOf('chunks:', r.index);
        if (ci >= 0) {
          let j = ci + 7;
          while (/\s/.test(body[j])) j++;
          if (body[j] === '[') {
            const e = balanced(body, j);
            try { response = { kind: 'chunks', chunks: evalExpr(body.slice(j, e)) }; } catch (err) { response = { kind: 'chunks', error: err.message }; }
          }
        }
      }
    }
  }

  // response headers the test server sends (--asserts only: they feed
  // result.response.headers and the response id/timestamp)
  if (wantAsserts && response) {
    try {
      let hdr = null;
      const fxh = /prepare(?:Json|Chunks)FixtureResponse\(\s*['"][^'"]+['"]\s*,\s*\{/.exec(body);
      if (fxh) {
        const os = fxh.index + fxh[0].length - 1;
        const objText = body.slice(os, balanced(body, os));
        const hm = /\bheaders:\s*\{/.exec(objText);
        if (hm) { const hs = hm.index + hm[0].length - 1; hdr = evalExpr(objText.slice(hs, balanced(objText, hs))); }
      } else {
        const tm = /type:\s*'(json-value|stream-chunks)'/.exec(body);
        if (tm) {
          const os = body.lastIndexOf('{', tm.index);
          const oe = balanced(body, os);
          if (os >= 0 && oe > 0) {
            const objText = body.slice(os, oe);
            const hm = /\bheaders:\s*\{/.exec(objText);
            if (hm) { const hs = hm.index + hm[0].length - 1; hdr = evalExpr(objText.slice(hs, balanced(objText, hs))); }
          }
        }
      }
      if (hdr && typeof hdr === 'object') response.headers = JSON.parse(JSON.stringify(hdr));
    } catch (err) { /* headers not evaluable */ }
  }

  const mid = body.match(/(?:\.chat|\.deepseek|chatModel|languageModel|responses|\.completion|chatModelFor|createModel|\.responsesModel)\(\s*['"]([^'"]+)['"]/) ||
    body.match(/\bprovider\(\s*['"]([^'"]+)['"]/) || body.match(/\bmodelId:\s*['"]([^'"]+)['"]/);
  const call = body.match(/do(Generate|Stream)\(/);
  // the model may be a variable declared earlier: const opusModel = provider('claude-opus-4-7')
  let midVar = null;
  {
    const cv = /\b(\w+)\.do(?:Generate|Stream)\(/.exec(body);
    if (cv && wantAsserts) {
      const sc = scopedDecls(starts[i].idx, starts[i].line.length - 2).filter(d => d.name === cv[1]);
      let last = sc.length ? sc[sc.length - 1].expr : null;
      // a declaration inside the test body wins
      const own = new RegExp('\\b(?:const|let)\\s+' + cv[1] + '\\s*=\\s*').exec(body0);
      if (own) { const st = own.index + own[0].length; const se = stmtEnd(body0, st); if (se > st) last = body0.slice(st, se); }
      if (last == null) {
        const mg = new RegExp('^(?:export )?(?:const|let)\\s+' + cv[1] + '(?::[^=]+)?\\s*=\\s*([^;]*?);', 'm').exec(src);
        if (mg) last = mg[1];
      }
      if (last) {
        const ids = [...last.matchAll(/\(\s*['"]([^'"]+)['"]\s*[,)]/g)];
        if (ids.length) midVar = ids[ids.length - 1][1];
        else {
          // new Model(modelIdConst, { ... }): the first argument is a const string
          const am = /\bnew\s+\w+\(\s*([A-Za-z_$][\w$]*)\s*[,)]/.exec(last);
          if (am) { try { const v = vm.runInContext(am[1], ctx); if (typeof v === 'string') midVar = v; } catch (e) { /* unresolved */ } }
        }
      }
    }
  }
  let callArgs = null, callKind = null, callObj = null;
  if (call) {
    callKind = call[1].toLowerCase();
    const st = call.index + call[0].length - 1;
    const e = balanced(body, st);
    const inner = body.slice(st + 1, e - 1).trim();
    callArgs = inner.replace(/\s+/g, ' ');
    try { callObj = JSON.parse(JSON.stringify(vm.runInContext('(' + stripTS(inner) + ')', ctx), (k, v) => v instanceof Uint8Array ? { __bytes: Buffer.from(v).toString('base64') } : v)); } catch (err) { callObj = null; }
  }

  const exps = [];
  const reI = /expect\(([^\n]*?)\)\s*\.toMatchInlineSnapshot\(\s*`/g;
  let mm;
  while ((mm = reI.exec(body))) {
    const s0 = mm.index + mm[0].length;
    const k = tpl(body, s0);
    exps.push({ expr: mm[1].trim(), snap: parseSnap(dedent(body.slice(s0, k).replace(/\\([\\`$])/g, '$1'))) });
  }
  // external snapshots are keyed "<describe path> > <name> <n>"
  const pathNames = [];
  {
    const pre = src.slice(0, starts[i].idx + 1).split('\n');
    let curIndent = starts[i].line.length - 2;
    for (let ln = pre.length - 1; ln >= 0; ln--) {
      const mm2 = /^(\s*)describe(?:\.\w+)?(?:\.each\([^)]*\))?\(\s*(['"`])(.*?)\2/.exec(pre[ln]);
      if (mm2 && mm2[1].length < curIndent) { pathNames.unshift(mm2[3]); curIndent = mm2[1].length; if (curIndent === 0) break; }
    }
  }
  const fullName = pathNames.concat([name]).join(' > ');
  let snapIdx = 0;
  const reS = /expect\(([^\n]*?)\)\s*\.toMatchSnapshot\(\s*\)/g;
  while ((mm = reS.exec(body))) {
    snapIdx++;
    const key = fullName + ' ' + snapIdx;
    if (extSnaps[key] !== undefined) exps.push({ expr: mm[1].trim(), snap: parseSnap(extSnaps[key].replace(/^\n/, '')) });
    else exps.push({ expr: mm[1].trim(), snap: { __err: 'no ext snapshot' } });
  }

  // --asserts: snapshot expectations whose expect(...) argument spans lines
  if (wantAsserts) {
    const seen = new Set(exps.map(e => e.expr));
    const reM = /\bexpect\(/g;
    let mm4;
    while ((mm4 = reM.exec(body))) {
      const st = mm4.index + 'expect'.length;
      const e = balanced(body, st);
      if (e < 0) continue;
      const rawInner = body.slice(st + 1, e - 1);
      if (!rawInner.includes('\n')) continue; // single-line ones are handled above
      const inner = rawInner.trim().replace(/^await\s+/, '').replace(/,\s*$/, '');
      const expr = inner.replace(/^await\s+/, '').replace(/\s+/g, ' ').trim();
      let k = e;
      const insn = /^\s*\.toMatchInlineSnapshot\(\s*`/.exec(body.slice(k));
      if (insn) {
        const s0 = k + insn[0].length;
        const k2 = tpl(body, s0);
        exps.push({ expr: ('await ' + expr).replace(/^await await /, 'await '), snap: parseSnap(dedent(body.slice(s0, k2).replace(/\\([\\`$])/g, '$1'))) });
        continue;
      }
      if (/^\s*\.toMatchSnapshot\(\s*\)/.test(body.slice(k))) {
        snapIdx++;
        const key = fullName + ' ' + snapIdx;
        const full = ('await ' + expr).replace(/^await await /, 'await ');
        if (extSnaps[key] !== undefined) exps.push({ expr: full, snap: parseSnap(extSnaps[key].replace(/^\n/, '')) });
        else exps.push({ expr: full, snap: { __err: 'no ext snapshot' } });
      }
    }
  }

  // guard against attaching the wrong canned response: the snapshot's ids
  // must appear in the response we picked
  let suspect = false;
  if (response) {
    const resp = JSON.stringify(response.body || response.chunks || response.raw || '');
    const ids = new Set();
    for (const e of exps) for (const x of JSON.stringify(e.snap).matchAll(/[a-z]{2,6}_[0-9a-zA-Z]{16,}/g)) ids.add(x[0]);
    if (ids.size > 0) { let hit = false; for (const id of ids) { if (resp.includes(id)) { hit = true; break; } } if (!hit) suspect = true; }
  }
  let topDescribe = '';
  {
    const pre = src.slice(0, starts[i].idx);
    const ms = [...pre.matchAll(/^describe\(\s*(['"`])(.*?)\1/gm)];
    if (ms.length) topDescribe = ms[ms.length - 1][2];
  }
  // plain expect(...).toEqual(...) style assertions (--asserts): expressions
  // are kept as source text and evaluated by the Go side over the call result
  const asserts = [];
  const aliases = {};
  if (wantAsserts) {
    for (const am of body.matchAll(/(?:const|let)\s+(\w+)\s*=\s*await\s+(server\.calls\[\d+\]\.\w+)\s*;/g)) aliases[am[1]] = am[2];
    for (const am of body.matchAll(/(?:const|let)\s+(\w+)\s*=\s*(result\.[\w.\[\]?']+)\s*;/g)) aliases[am[1]] = am[2];
    const reE = /\bexpect\(/g;
    let em;
    while ((em = reE.exec(body))) {
      const st = em.index + 'expect'.length;
      const e = balanced(body, st);
      if (e < 0) continue;
      let expr = body.slice(st + 1, e - 1).trim().replace(/^await\s+/, '');
      let k = e;
      let not = false;
      if (body.startsWith('.not', k)) { not = true; k += 4; }
      const mm3 = /^\.(toEqual|toStrictEqual|toBe|toMatchObject|toHaveLength|toBeUndefined|toBeNull|toBeDefined|toContain|toBeGreaterThan|toBeTruthy|toBeFalsy)\(/.exec(body.slice(k));
      if (!mm3) continue;
      const as = k + mm3[0].length - 1;
      const ae = balanced(body, as);
      if (ae < 0) continue;
      const argSrc = body.slice(as + 1, ae - 1).trim();
      let arg = null, ok = true;
      if (argSrc !== '') {
        try { arg = JSON.parse(JSON.stringify(evalExpr(argSrc), (kk, v) => v instanceof Uint8Array ? { __bytes: Buffer.from(v).toString('base64') } : (Object.prototype.toString.call(v) === '[object Set]' ? { __set: [...v] } : (v === undefined ? { __undef: true } : v)))); } catch (err) { ok = false; }
      }
      if (!ok) continue;
      asserts.push({ expr, matcher: mm3[1], not, arg });
    }
  }
  for (const n of Object.keys(saved)) { if (saved[n] === undefined) delete ctx[n]; else ctx[n] = saved[n]; }
  if (exps.length || asserts.length) out.push({ test: name, response, callKind, callArgs, call: callObj, modelId: midVar || (mid ? mid[1] : null), suspect, topDescribe, exps, ...(wantAsserts ? { asserts, aliases } : {}) });
}
console.log(JSON.stringify(out));
