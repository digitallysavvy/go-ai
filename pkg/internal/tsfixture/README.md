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
(ai@7.0.140 plus main); the anthropic language/convert-prompt, bedrock
prepare-tools, google language and gemini JSON accumulator cases were
re-extracted at `e64ed82f27` (ai@7.0.153+16); the groq chat cases are
`groq/.../chat_asserts.json.gz` (below). The OpenAI chat and
Responses, xAI Responses and Azure provider cases and the OpenAI/xAI
function-level cases added in review round 77 are extracted at
`e64ed82f27` with `--asserts` (function-level ones with
`--expand-each --fn=NAME`); `openai/.../completion.json.gz` is extracted at `e64ed82f27` with
`--asserts` too. The Chat Completions frame of the Responses case "should
return helpful error when Chat Completions stream is received" is one TS
rejects on purpose; `providerutils.TestTSFixtureFramesAccepted` lists it.
Round 78 (prova) added, all extracted at `e64ed82f27`: `groq/.../chat_asserts.json.gz`,
`mistral/.../chat_asserts.json.gz`, `mistral/.../conversation.json.gz` and
`gateway/.../language.json.gz` with `--asserts`; `groq/.../convert_messages`,
`prepare_tools`, `convert_usage` and `mistral/.../convert_messages`, `prepare_tools`,
`convert_usage`, `finish_reason`, `conversation_inputs`, `conversation_tools` with
`--expand-each --fn=NAME`. The gateway case test supplies the default responses of the
TS `prepareJsonResponse` / `prepareStreamResponse` helpers, and the google
convert-messages case test restores `supportedFunctionResponseUrls` RegExp patterns from
the TS argument source (a RegExp evaluates to `{}` in the case JSON).
Round 78 (provb) added, extracted at `e64ed82f27` with `--expand-each --fn=NAME`:
`deepseek/.../convert_messages`, `prepare_tools`, `usage`; `alibaba/.../convert_messages`,
`prepare_tools`, `usage`; `moonshot/.../convert_messages`, `usage`,
`normalize_json_schema`.
Round 79 (provb) added, extracted at `e64ed82f27` with `--expand-each --fn=NAME`:
`openresponses/.../convert_input` (convertToOpenResponsesInput) and `finish_reason`
(mapOpenResponsesFinishReason).
Round 79 (prova) added, all extracted at `e64ed82f27`: `cohere/.../chat_asserts.json.gz`
with `--asserts`; `cohere/.../convert_messages`, `prepare_tools` and
`perplexity/.../convert_input`, `convert_usage`, `decision_state` with
`--expand-each --fn=NAME`; the empty `perplexity/.../chat.json.gz` was removed.
Round 80 (prova) filled the responses that TS test helpers build (google `language`,
`interactions`, `structured_output_with_tools`; bedrock `chat`; anthropic `language`) and
added `perplexity/.../language_asserts.json.gz` (`--asserts`), by running each TS test
file with stub vitest, test server and model that record the response each test prepared
before its model call. Since round 81 that script is committed as `capture.mjs`: re-extracting
these files with `extract.mjs` alone loses those responses (`response: null`), so run the
extractor and then the capture (see Regenerating).

The extractor serializes a `Buffer.from(...)` through `Buffer.prototype.toJSON`
(`{"type": "Buffer", "data": [...]}`); `WireBytes` reads that form as bytes
like the `__bytes` marker. Since round 79 it also records `const NAME =
server.calls[n].x;` (with or without `await`), `const { a, b } = await
model.doGenerate(...)` (each name is `result.<name>`) and `const NAME =
BASE.path;` over `result` or a known alias (casts dropped) as aliases, and a
`() => fn(args)` toThrow callback carries its own `callArgs` (an assertion
whose arguments do not evaluate is dropped instead of replayed against the
test's first call). `Run` logs how many snapshots a suite without
`EvalPathSnapshots` skipped.

## Regenerating

From a checkout of the TS AI SDK at the target commit:

```sh
cd <ai repo>/packages
node <go-ai repo>/pkg/internal/tsfixture/extract.mjs <path/to/provider test.ts> \
  | gzip -9n > <go-ai repo>/pkg/providers/<provider>/testdata/tscases/<name>.json.gz
```

For a test file whose responses come from helpers the extractor cannot
evaluate (`prepareJsonResponse`, `prepareStream(...)`, fixtures read by a
helper), fill them by running the TS test file under Node 24 with stub vitest,
test server and model (`capture.mjs`); it records, per test, the response the
test prepared before each model call and fills only cases whose `response` is
null:

```sh
cd <ai repo>/packages
node <go-ai repo>/pkg/internal/tsfixture/extract.mjs <path/to/test.ts> --asserts > /tmp/cases.json
node <go-ai repo>/pkg/internal/tsfixture/capture.mjs <path/to/test.ts> <ModelClassName> /tmp/cases.json \
  | gzip -9n > <go-ai repo>/pkg/providers/<provider>/testdata/tscases/<name>.json.gz
```

It reports on stderr the cases it could not fill (a custom `fetch`, an error
response of a `rejects` test, no model call). Round 81 checked that it
reproduces `perplexity/.../language_asserts.json.gz` byte for byte.

Use `gzip -9n` (no file name, no timestamp) so regenerating unchanged cases
produces identical bytes. Then run the provider's `ts_cases` tests and update
the Go skip lists for any case that changed.

## Response queues

A TS test that queues several responses (`server.urls[url].response =
[first, second, ...]`) is extracted with all of them: the first stays in
`response` and the rest go to `response.next` (`json`, `chunks`, `error` with
`status`/`text`, and `empty` entries). `Run` serves them in order, one per
request, and `404 {"error":"Not Found"}` after the last, as the TS test server
does. A case without a queue serves its one response to every request (and
fails after 20 requests instead of looping forever).

## Routes and response functions

With `--asserts`, a TS test that sets the responses of several URLs of its
test server (`server.urls[POST_URL].response = ...; server.urls[POLL_URL]
.response = ...`), or a response function (`.response = () => { ... }`), is
extracted with `routes`: one `{path, response}` per URL path, the last
assignment winning. A response function is called up to six times with the
counters the test declares before it (`let getCallCount = 0;`); its results
become the route's queue with `repeat: true` (the last one is served to every
later request, as the TS server calls the function per request), and each
counter becomes an alias of `server.hits['<path>']`, the number of requests
the route served. `Run` sends each request to the route whose TS path equals
or ends with the request path (the Go base URL replaces the TS one, so Go
`/interactions` matches TS `/v1beta/interactions`), the longest match
winning, and answers `404 {"error":"Not Found"}` when none matches. A test
that calls `provider.interactions({ agent: NAME })` gets the model id
`agent:NAME` (`managedAgent:ID` for `{ managedAgent }`); the Go test maps it
to the agent model.

## Plain assertions and function-level tests

`extract.mjs` has two more modes. Neither changes the output of the default
mode, so existing case files regenerate byte for byte.

- `--asserts` also extracts the plain `expect(expr).toEqual/toBe/toMatchObject/
  toHaveLength/toBeUndefined/...` assertions of each case (`asserts` and
  `aliases` in the JSON), the response headers the test server sends, the model
  id of the `model` variable a test calls, and snapshots whose `expect(...)`
  argument spans lines. `Run` evaluates simple access expressions over
  `result`, `server.calls[n].requestBodyJson|requestHeaders|requestUserAgent`
  and `requestBody` aliases (`asserts.go`), plus the stream-part filters TS
  tests write over `const parts = await convertReadableStreamToArray(stream)`
  (`.filter(p => p.f === 'v')`, `.some(p => p.f === 'v')`,
  `.find(p => p.f === 'v')`, `.map(p => p.f)`), aliases of such calls
  (`const types = parts.map(p => p.type)`), `const calls = server.calls`,
  `new URL(x).searchParams.get('k')`, and `expect.objectContaining` /
  `expect.arrayContaining` arguments, and `.at(n)`. Plain assertions compare the raw Go stream (block and wire
  source ids kept, a `StreamProviderError` object as the error part's `error`); a snapshot of
  a path over the stream is canonicalized with `CanonStream` on both sides. Assertions over another test-server
  variable (`pollServer.calls`) read as `server.*`, and TS casts
  (`(p as { delta: string }).delta`, nested object types included) are dropped. Anything else is counted
  as "not evaluated" in the test log.
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

## Wire replay

`wire.go` / `wire_run.go` replay recordings of the whole TS test run
(requests, responses and call results) against Go providers; see
`record/README.md`.

## Stateful replays

`google/testdata/tscases/realtime_stateful.json.gz` holds every
`parseServerEvent` / `serializeClientEvent` call the TS realtime event mapper
tests make, in order, with the number of the mapper instance (`inst`) each ran
on. `TestTSCasesRealtimeStateful` feeds them to one Go mapper per instance, so
id counters and transcription accumulation that depend on earlier events are
checked. The recorder (`record/setup.mjs`) captures these two methods like the
`do*` calls; record them with
`REC_OUT=<dir> npx vitest run --config ../../rec/vitest.config.mjs src/realtime/google-realtime-event-mapper.test.ts`
in the `google` package, then gzip the `.jsonl` file with `gzip -9n` and name it `*.json.gz` (`TestCaseFilesAreCompressed` checks the name).

## Large payloads

Recorded image payloads would make case files huge (the TS image fixtures
are 5 to 6 MB each). `google/testdata/tscases/interactions.json.gz` uses the
same shrink rule as `pkg/providers/google/testdata/ts/interactions/README.md`:
every base64 JPEG run of 100,000 characters or more is replaced by a 1x1
JPEG, and every other run that long (thought signatures) by its first 64
characters, everywhere it occurs (response bodies and snapshots alike). Apply
the same rule after regenerating, before gzipping, so no file passes 1 MB.
`openai/testdata/tscases/responses.json.gz` replaces every run of 20,000 or
more base64 characters (the compaction fixtures' encrypted content) by its
first 64 characters the same way.
A test whose point is the size itself cannot be shrunk: the two Google batch
tests that build a 20 MB creation body (`reaches 20 MB` and the file-input
variant) are left out of `testdata/tswire/google.jsonl.gz`.

## Prompt building

`BuildPromptMessages` (used by both the case replay and the wire replay)
accepts text, reasoning, tool-call, tool-result, file, reasoning-file, custom
and tool-approval-response parts. File data is built by `fileDataFromTS`
(base64 strings, recorded `Uint8Array` bytes, URLs with a gs: `originalUrl`,
provider references and text). Tool-result outputs decode from their TS JSON
form; content outputs are built block by block so recorded bytes survive. Provider option keys a TS literal sets to `undefined` are dropped (TS
option schemas read them as absent).
