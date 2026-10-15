# tsfixture

`tsfixture` replays test cases extracted from the TypeScript AI SDK's provider
tests (canned wire response, call options, vitest snapshots) against the Go
language models, and compares the results with the TS snapshots. It only
supports parity tests. The Go tests read committed data only: they need
neither Node nor a TS checkout.

## Committed case files

Each provider keeps its cases in `pkg/providers/<provider>/testdata/tscases/`
(shared OpenAI-compatible cases are in `pkg/providers/testdata/tscases/`).
The files are committed gzipped as `*.json.gz` to keep the repository small.
`LoadCases` gunzips any path ending in `.gz`. `TestCaseFilesAreCompressed`
fails if an uncompressed `*.json` lands in a `testdata/tscases` directory.

The cases were extracted from the TS AI SDK at commit `b658e8503c`
(ai@7.0.140 plus main).

## Regenerating

From a checkout of the TS AI SDK at the target commit:

```sh
cd <ai repo>/packages
node <go-ai repo>/pkg/internal/tsfixture/extract.mjs <path/to/provider test.ts> \
  | gzip -9n > <go-ai repo>/pkg/providers/<provider>/testdata/tscases/<name>.json.gz
```

Use `gzip -9n` (no file name, no timestamp) so regenerating unchanged cases
produces identical bytes. Then run the provider's `ts_cases` tests and update
the Go skip lists for any case that changed.
