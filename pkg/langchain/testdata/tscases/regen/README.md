# Regenerating the LangChain TS cases

The gzipped files in the parent directory come from the TS AI SDK at
`b658e8503c` (`packages/langchain/src`).

- `langgraph_fixtures.json.gz`: `node extract_fixtures.mjs <ai>/packages/langchain/src/__fixtures__/langgraph.ts | gzip -9n`
- `langgraph-hitl-request-*.json.gz`, `react-agent-tool-calling.json.gz`: the TS `__snapshots__/*.json` files, gzipped.
- `adapter_cases.json.gz` and `utils_cases.json.gz`: a vitest shim (`harness.mjs`, Node 24 `stripTypeScriptTypes`) runs the real TS test bodies twice. `collect` records each test's input stream (LangChain classes are serialized the way RemoteGraph sends them); one of the `gen_*_test.go.txt` generators (copy to a `_test.go` file in `pkg/langchain`, run with `LC_IN`/`LC_UIN` and `LC_OUT`) runs the Go adapter over those inputs; `check` replays the TS test bodies against the Go output. Only tests that pass their TS assertions (or that fail solely because they inspect TS internal state after the chunk assertions passed) are kept, with the Go output as the expected value.

```sh
node harness.mjs <ai>/packages/langchain/src/adapter.test.ts collect inputs.json fixtures.json
LC_IN=inputs.json LC_OUT=outputs.json go test -count=1 -run ZZGen ./pkg/langchain
node harness.mjs <ai>/packages/langchain/src/adapter.test.ts check outputs.json fixtures.json
```

(`fixtures.json` is the unzipped `langgraph_fixtures.json.gz`; `utils.test.ts` works the same with `LC_UIN` and `ZZUGen`.)
