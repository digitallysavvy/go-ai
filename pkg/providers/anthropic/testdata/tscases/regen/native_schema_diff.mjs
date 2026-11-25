// Differential recorder: runs TS hasCircularSchemaReference /
// supportsNativeStructuredOutputSchema (ai@30548ee7f9) over fixed and
// generated schemas and prints [{schema, circular, native}].
import fs from 'fs';
import { stripTypeScriptTypes } from 'node:module';
const base = '/Users/arlene/Dev/side-projects/go-ai/ai/packages/anthropic/src/';
const isRecord = v => v != null && typeof v === 'object' && !Array.isArray(v);
function load(file) {
  let src = fs.readFileSync(base + file, 'utf8');
  src = src.replace(/^import[\s\S]*?;\s*$/gm, '');
  src = stripTypeScriptTypes(src, { mode: 'strip' }).replace(/^export /gm, '');
  return src;
}
const code = load('has-circular-schema-reference.ts') + '\n' + load('supports-native-structured-output-schema.ts') +
  '\nreturn { hasCircularSchemaReference, supportsNativeStructuredOutputSchema };';
const { hasCircularSchemaReference, supportsNativeStructuredOutputSchema } = new Function('isRecord', code)(isRecord);

const strings = (n, p = 'f') => Object.fromEntries(Array.from({ length: n }, (_, i) => [`${p}${i}`, { type: 'string' }]));
const opt = (n, p = 'f') => ({ type: 'object', properties: strings(n, p), additionalProperties: false });
const fixed = [
  { type: 'object', properties: { answer: { type: 'string' } }, required: ['answer'], additionalProperties: false },
  { type: 'object', properties: { answer: { type: 'string' } } },
  { type: 'object', properties: strings(60), required: Object.keys(strings(60)), additionalProperties: false },
  { type: 'object', properties: { root: { $ref: '#/$defs/node' } }, $defs: { node: { type: 'object', properties: { children: { items: { $ref: '#/$defs/node' } } } } } },
  ...[{ type: 'object', additionalProperties: { type: 'number' } }, { type: 'object', additionalProperties: true }, { type: 'object' }, { type: 'object', patternProperties: { '^x-': { type: 'string' } } }]
    .map(field => ({ type: 'object', properties: { field }, required: ['field'], additionalProperties: false })),
  { type: 'object', properties: { scores: { $ref: '#/$defs/Scores' } }, required: ['scores'], additionalProperties: false, $defs: { Scores: { type: 'object', additionalProperties: { type: 'number' } } } },
  { type: 'object', additionalProperties: false },
  opt(10), opt(11),
  { type: 'object', properties: { a: { type: 'string' }, nested: opt(10) }, required: ['nested'], additionalProperties: false },
  { type: 'object', properties: { items: { type: 'array', items: opt(5, 'i') }, union: { anyOf: [opt(3, 'a'), opt(3, 'b')] } }, required: ['items', 'union'], additionalProperties: false },
  ...[5, 6].map(n => ({ type: 'object', properties: { x: { $ref: '#/$defs/D' }, y: { $ref: '#/$defs/D' } }, required: ['x', 'y'], additionalProperties: false, $defs: { D: opt(n, 'd') } })),
  { type: 'object', properties: { answer: { type: 'string' } }, required: ['answer'], additionalProperties: false, $defs: { Unused: opt(30, 'u') } },
  // pointer edge cases
  { $ref: '#', type: 'object', properties: {}, additionalProperties: false },
  { $ref: '#/definitions/A', definitions: { A: { $ref: '#/definitions/B' }, B: { $ref: '#/definitions/A' } } },
  { $ref: '#/definitions/A', definitions: { A: { type: 'object', properties: { a: { type: 'string' } }, required: ['a'], additionalProperties: false } } },
  { type: 'object', properties: { a: { $ref: '#/definitions/a~1b' } }, required: ['a'], additionalProperties: false, definitions: { 'a/b': { $ref: '#/definitions/a~1b' } } },
  { type: 'object', properties: { a: { $ref: '#/definitions/a%20b' } }, required: ['a'], additionalProperties: false, definitions: { 'a b': { $ref: '#/properties/a' } } },
  { type: 'object', properties: { a: { $ref: '#/definitions/%E0%A4%A' } }, required: ['a'], additionalProperties: false, definitions: {} },
  { type: 'object', properties: { a: { $ref: '#/allOf/0' } }, required: ['a'], additionalProperties: false, allOf: [{ $ref: '#/properties/a' }] },
  { type: 'object', properties: { a: { $ref: '#/allOf/ 0 ' } }, required: ['a'], additionalProperties: false, allOf: [{ $ref: '#/properties/a' }] },
  { type: 'object', properties: { a: { $ref: '#/allOf/' } }, required: ['a'], additionalProperties: false, allOf: [{ $ref: '#/properties/a' }] },
  { type: 'object', properties: { a: { $ref: '#/allOf/1.0' } }, required: ['a'], additionalProperties: false, allOf: [{}, { $ref: '#/properties/a' }] },
  { type: 'object', properties: { a: { const: { $ref: '#' } } }, required: ['a'], additionalProperties: false },
  { type: ['object', 'null'], properties: null },
  { type: ['object', 'null'], additionalProperties: false },
  { type: 'object', properties: { a: { type: 'string' } }, required: 'a', additionalProperties: false },
  { type: 'object', properties: { m: { type: 'object', patternProperties: {} , additionalProperties: false } }, required: ['m'], additionalProperties: false },
  { type: 'object', properties: { r: { $ref: 'other.json#/x' } }, required: ['r'], additionalProperties: false },
  { type: 'object', properties: { r: { $ref: '#/$defs/missing' } }, required: ['r'], additionalProperties: false },
  { type: 'object', properties: { t: { if: opt(4, 'i'), then: opt(4, 't'), else: opt(4, 'e') } }, required: ['t'], additionalProperties: false },
  { type: 'object', properties: { p: { type: 'array', prefixItems: [opt(6, 'p'), opt(6, 'q')] } }, required: ['p'], additionalProperties: false },
  { type: 'object', properties: { n: { not: { type: 'object' } } }, required: ['n'], additionalProperties: false },
  { type: 'object', properties: { d: { dependentSchemas: { x: opt(11, 'x') } } }, required: ['d'], additionalProperties: false },
];
// generated schemas
let seed = 85;
const rnd = n => { seed = (seed * 1103515245 + 12345) & 0x7fffffff; return seed % n; };
function gen(depth, defs) {
  const k = rnd(depth > 2 ? 4 : 9);
  switch (k) {
    case 0: return { type: 'string' };
    case 1: return { type: 'number' };
    case 2: return { $ref: '#/$defs/' + defs[rnd(defs.length)] };
    case 3: return { type: 'object', additionalProperties: rnd(3) === 0 ? true : false, ...(rnd(2) ? { properties: {} } : {}) };
    case 4: case 5: {
      const props = {}; const req = [];
      const n = rnd(6);
      for (let i = 0; i < n; i++) { props['p' + i] = gen(depth + 1, defs); if (rnd(2)) req.push('p' + i); }
      return { type: 'object', properties: props, required: req, ...(rnd(4) ? { additionalProperties: false } : {}) };
    }
    case 6: return { type: 'array', items: gen(depth + 1, defs) };
    case 7: return { anyOf: [gen(depth + 1, defs), gen(depth + 1, defs)] };
    default: return { allOf: [gen(depth + 1, defs)] };
  }
}
const generated = [];
for (let i = 0; i < 300; i++) {
  const names = ['A', 'B', 'C'];
  const $defs = {};
  for (const n of names) $defs[n] = gen(1, names);
  generated.push({ ...gen(0, names), $defs });
}
const out = [...fixed, ...generated].map(schema => ({
  schema, circular: hasCircularSchemaReference(schema), native: supportsNativeStructuredOutputSchema(schema),
}));
process.stdout.write(JSON.stringify(out));
