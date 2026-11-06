// Recorder for the TS accumulator tests: save as packages/google/src/rec-acc.ts,
// point a copy of google-json-accumulator.test.ts at './rec-acc' and run it with
// ACC_OUT=<file>.jsonl. The "ts-test-N" cases in ts_accumulator_cases.jsonl.gz
// are these recordings, one case per accumulator instance.
import { appendFileSync } from 'node:fs';
import { GoogleJSONAccumulator as Real, type PartialArg } from './google-json-accumulator';
export type { PartialArg };
let n = 0;
const OUT = process.env.ACC_OUT!;
const clone = (x: unknown) => JSON.parse(JSON.stringify(x === undefined ? null : x));
export class GoogleJSONAccumulator extends Real {
  inst = ++n;
  override processPartialArgs(a: PartialArg[]) {
    const args = clone(a);
    const r = super.processPartialArgs(a);
    appendFileSync(OUT, JSON.stringify({ inst: this.inst, op: 'process', args, current: clone(r.currentJSON), delta: r.textDelta, test: (globalThis as any).__t }) + '\n');
    return r;
  }
  override finalize() {
    const r = super.finalize();
    appendFileSync(OUT, JSON.stringify({ inst: this.inst, op: 'finalize', finalJSON: r.finalJSON, closing: r.closingDelta }) + '\n');
    return r;
  }
}
