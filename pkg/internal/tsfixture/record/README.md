# Wire replay recordings

`pkg/providers/testdata/tswire/*.jsonl.gz` hold what the TypeScript AI SDK
tests actually do. The recorder runs the real TS tests (`vitest`) with two
hooks and writes one JSON line per test:

- every `model.do*()` call: the arguments, the result (or error) and the model
  (class, id, provider name, plain config settings);
- every HTTP request the test server saw (method, URL, headers, body) and the
  response it served.

Go replays a recording through the Go provider of the same name
(`TestTSWire`, `tsfixture.RunWire`): the Go model gets the same call options,
a local server answers with the recorded responses, and the requests the Go
model sends and the values it returns are diffed against the TS ones. Unlike
`testdata/tscases` (extracted literals and vitest snapshots) this covers every
test that talks to the test server, including helper-built responses,
multi-request flows (async image/video, polling), multipart and binary bodies.

## Recording

```sh
pkg/internal/tsfixture/record/prepare.sh <ai repo at b658e8503c> /tmp/ts-scratch
cd /tmp/ts-scratch && ./rec/run.sh openai groq mistral ...      # -> rec/out/<pkg>.jsonl
python3 <go-ai>/pkg/internal/tsfixture/record/finish.py rec/out <go-ai>/pkg/providers/testdata/tswire
```

`prepare.sh` copies the packages into a scratch directory (the TS checkout is
not modified), installs dependencies with pnpm, and needs no build: the vitest
config resolves `@ai-sdk/*` to `src`. It also truncates two kinds of large
test data so the recordings stay small: the 40 KB sample mp3 files become 64
bytes, and JSON strings over 3000 characters in `__fixtures__` are cut to 300
(`trunc_fixtures.py`). Recorded TS results are computed from the truncated
inputs, so Go and TS see the same data.

`finish.py` gzips with a fixed mtime (reproducible bytes) and splits a package
into `<pkg>__<test-file>.jsonl.gz` when one file would exceed ~900 KB.

## Running

```sh
go test ./pkg/providers/ -run TestTSWire                # all recorded packages
TSWIRE_PKG=cohere go test ./pkg/providers/ -run TestTSWire
TSWIRE_REPORT=1 go test -json ./pkg/providers/ -run TestTSWire   # log every difference instead of failing
TSWIRE_ERRORS=1 ...                                    # also compare error wording
```

The Anthropic, Google, Vertex and Bedrock suites are registered in
`pkg/providers/ts_wire_anthgoog_test.go`. They pass the `responseFormat` schema
as raw JSON (`WireSuite.RawSchemas`) so schema key order is compared, replay
Vertex Anthropic through its sub-provider, and send the fixed Google speech and
Cloud TTS hosts to the replay server with a host-rewriting transport. The
`anthropic-aws` recording has no HTTP exchanges (its tests inject a mock fetch)
and is not kept; Bedrock `doStream` tests are skipped because the recording
stores the binary event-stream frames as text (the case suites replay them).

Packages whose Go provider needs package-private setup replay in-package:
`fal` (fixed hosts), `azure` (URL layout selected by the resource name) and
`openai` (typed provider tools, provider names).

## What is compared

- requests: method, path (Go may add a fixed version prefix, `WireSuite.GoAdds`),
  query, every header the TS request carried (except transport noise), the JSON
  body (null/empty values ignored, key order not significant), multipart parts
  (name, filename, content type, bytes; JSON text values compare as JSON);
- results: embeddings, rerank ranking, images/audio/transcripts, warnings,
  provider metadata, response headers/body/model id, language-model content,
  usage and finish reason (`WireSuite.LanguageResults`), video operations.

Known harness limits (tests are skipped or the field ignored, each with the
reason in `Skip` or in `wire_run.go`): operations are opaque, TS calls that pass
functions (webhook factories), responses the recorder cannot carry
(controlled streams), provider settings that are code (extensions,
`transformRequestBody`), JSON key order inside text the model sees (Go maps),
Go headers beyond the TS ones, error wording.
