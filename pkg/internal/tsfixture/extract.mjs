// Extracts TS AI SDK test cases (canned wire response + call options + vitest
// snapshots) into JSON consumed by the Go tsfixture package.
//
//   cd <ai repo>/packages && node <this file> <test.ts> > cases.json
//
// Each case carries: test name, response (json body / SSE chunks / fixture
// file contents), call kind and options (evaluated literal), model id, the
// top-level describe, and the snapshots for the assertions it makes.
import fs from 'fs';
import vm from 'vm';
import path from 'path';

const testFile = process.argv[2];
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
{
  const re = /^(?:export )?(?:const|let) (\w+)(?::[^=]+)? = /gm;
  let m;
  while ((m = re.exec(src))) {
    const k = m.index + m[0].length;
    let j = k;
    while (j < src.length && /\s/.test(src[j])) j++;
    let end;
    if ('{[('.includes(src[j])) end = balanced(src, j); else end = src.indexOf('\n', j);
    let e2 = end;
    while (e2 < src.length && src[e2] !== ';' && src[e2] !== '\n') e2++;
    try { vm.runInContext(`var ${m[1]} = (${stripTS(src.slice(k, e2))})`, ctx); } catch (e) { /* skip */ }
  }
}
// strip TS-only syntax so the literal evaluates as JS
function stripTS(s) { return s.replace(/\s+satisfies\s+[\w.]+(<[^>]*>)?/g, '').replace(/\s+as\s+const\b/g, ''); }
function evalExpr(s) { return vm.runInContext('(' + stripTS(s) + ')', ctx); }

const re = /\n(\s*)(it|test)(?:\.each\([^)]*\))?\(\s*(['"`])((?:\\.|(?!\3).)*)\3/g;
const starts = [];
let m;
while ((m = re.exec(src))) starts.push({ idx: m.index, name: m[4], line: ' '.repeat(m[1].split('\n').pop().length) + 'it' });

const out = [];
for (let i = 0; i < starts.length; i++) {
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

  const mid = body.match(/(?:\.chat|\.deepseek|chatModel|languageModel|responses|\.completion|chatModelFor|createModel|\.responsesModel)\(\s*['"]([^'"]+)['"]/);
  const call = body.match(/do(Generate|Stream)\(/);
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
    exps.push({ expr: mm[1].trim(), snap: parseSnap(dedent(body.slice(s0, k))) });
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
  if (exps.length) out.push({ test: name, response, callKind, callArgs, call: callObj, modelId: mid ? mid[1] : null, suspect, topDescribe, exps });
}
console.log(JSON.stringify(out));
