// Replays every recorded chunk sequence of ts_accumulator_cases.jsonl through
// the TS GoogleJSONAccumulator at the current checkout and re-records the
// results; appends the sequences of tests added upstream.
import { GoogleJSONAccumulator } from './acc.ts';
import fs from 'node:fs';
const [inFile, outFile] = process.argv.slice(2);
const clone = x => JSON.parse(JSON.stringify(x === undefined ? null : x));
const lines = fs.readFileSync(inFile, 'utf8').split('\n').filter(Boolean).map(l => JSON.parse(l));
const added = [
  { name: 'ts-test-38', kind: 'match', fin: true, chunks: [[{ jsonPath: '$.location', stringValue: 'Bos', willContinue: true }], [{ jsonPath: '$.location', stringValue: 'ton' }]] },
  { name: 'ts-test-39', kind: 'match', fin: true, chunks: [[{ jsonPath: '$.location', stringValue: 'Boston', willContinue: true }], [{ jsonPath: '$.location', stringValue: '' }]] },
  { name: 'ts-test-40', kind: 'match', fin: true, chunks: [[{ jsonPath: '$.location', stringValue: 'Boston', willContinue: true }], [{ jsonPath: '$.location' }]] },
];
const out = [];
for (const c of [...lines, ...added]) {
  const acc = new GoogleJSONAccumulator();
  const steps = [];
  let threw = false;
  for (const chunk of c.chunks) {
    try {
      const r = acc.processPartialArgs(clone(chunk));
      steps.push({ current: clone(r.currentJSON), delta: r.textDelta });
    } catch (e) {
      steps.push({ err: String(e) });
      threw = true;
      break;
    }
  }
  let fin = null;
  if (!threw && (c.fin === true || c.fin != null)) {
    const r = acc.finalize();
    fin = { finalJSON: r.finalJSON, closing: r.closingDelta };
  }
  out.push(JSON.stringify({ name: c.name, kind: threw ? 'error' : 'match', chunks: c.chunks, steps, fin }));
}
fs.writeFileSync(outFile, out.join('\n') + '\n');
