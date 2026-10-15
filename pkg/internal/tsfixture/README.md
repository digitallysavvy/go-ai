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

## Plain assertions and function-level tests

`extract.mjs` has two more modes. Neither changes the output of the default
mode, so existing case files regenerate byte for byte.

- `--asserts` also extracts the plain `expect(expr).toEqual/toBe/toMatchObject/
  toHaveLength/toBeUndefined/...` assertions of each case (`asserts` and
  `aliases` in the JSON), the response headers the test server sends, the model
  id of the `model` variable a test calls, and snapshots whose `expect(...)`
  argument spans lines. `Run` evaluates simple access expressions over
  `result`, `server.calls[n].requestBodyJson|requestHeaders|requestUserAgent`
  and `requestBody` aliases (`asserts.go`); anything else is counted as "not
  evaluated" in the test log.
- `--fn=NAME` extracts function-level tests (`expect(NAME(args))` or
  `const result = NAME(args)`), for converters, `prepareTools`, usage and
  schema helpers. Tests that call `NAME` more than once outside their
  assertions are skipped (stateful or comparing calls). `RunFn` hands the
  evaluated TS arguments to a Go adapter and evaluates the assertions over its
  result; `ErrUnsupported` skips a case. `WarningsToTS` renders warnings the TS
  way (Go also mirrors `details` into `message`).

Test hooks used by the provider tests: `ProviderToolHook` (and
`anthropictools.ProviderTool` for Anthropic tools), `ToolNameAlias` /
`PromptToolNameAlias` (Go names provider tools by id), `MetadataKeyAlias`,
`DropResponseTimestamp`, `DropResponseModelID`, `StreamBodyEncoder` (Bedrock's
AWS event stream), `BuildPromptMessages`, `BuildTools`, `BuildToolChoice`.
`TSFIXTURE_FULL=1` stops the diff from truncating long strings. A skip entry in
the `skip` map is keyed by the TS test name; `name#01` skips only the second
case with that name.
