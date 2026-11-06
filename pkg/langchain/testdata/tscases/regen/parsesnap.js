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

