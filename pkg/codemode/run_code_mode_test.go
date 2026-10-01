package codemode

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// Ports TypeScript's code-mode/src/core.test.ts ("core execution").

func TestRunCodeMode_ReturnsJSONValue(t *testing.T) {
	got, err := RunCodeMode(context.Background(), RunInput{JS: "return { answer: 40 + 2 };", Tools: ToolSet{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]interface{}{"answer": float64(42)}
	assertDeepEqual(t, got, want)
}

func TestRunCodeMode_UndefinedReturnValue(t *testing.T) {
	got, err := RunCodeMode(context.Background(), RunInput{JS: "const value = 1 + 1;", Tools: ToolSet{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %#v", got)
	}
}

func TestRunCodeMode_StripsSimpleTypeAnnotations(t *testing.T) {
	got, err := RunCodeMode(context.Background(), RunInput{JS: "const value: number = 7; return { value };", Tools: ToolSet{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertDeepEqual(t, got, map[string]interface{}{"value": float64(7)})
}

func TestRunCodeMode_StripsInterfaceAndSatisfies(t *testing.T) {
	js := `
          interface Item { value: number }
          const item = { value: 12 } satisfies Item;
          return item;
        `
	got, err := RunCodeMode(context.Background(), RunInput{JS: js, Tools: ToolSet{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertDeepEqual(t, got, map[string]interface{}{"value": float64(12)})
}

// Ports TypeScript's code-mode/src/run-compatibility.test.ts, "supports
// tool names that are not host-function identifiers". (Its other two cases,
// exercising interrupt resolutions and continuation signing keys, are
// ported in continuation_test.go.) The bridge Proxy's `get(_t, name)` trap
// in wrapCodeModeSource
// intercepts any property key, including one reached only through bracket
// notation, so a tool name need not be a valid JS identifier.
// The default outerToolCallId (used only when neither
// ToolExecutionOptions.ToolCallID nor a Continuation supplies one) must be
// distinct per invocation, mirroring TypeScript's lazily-incremented
// “ `code-mode-${++invocationCounter}` “ fallback -- not the same literal
// string reused by every anonymous invocation in the process, which would
// otherwise give two unrelated invocations' host tool calls colliding
// toolCallId values (e.g. both "code-mode-1:tool-1").
func TestRunCodeMode_DefaultOuterToolCallIdIsUniquePerInvocation(t *testing.T) {
	var toolCallIDs []string
	tools := ToolSet{"echo": {
		Name:       "echo",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			toolCallIDs = append(toolCallIDs, opts.ToolCallID)
			return "ok", nil
		},
	}}
	for i := 0; i < 2; i++ {
		if _, err := RunCodeMode(context.Background(), RunInput{JS: "return await tools.echo({});", Tools: tools}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if len(toolCallIDs) != 2 {
		t.Fatalf("expected 2 recorded tool call ids, got %#v", toolCallIDs)
	}
	if toolCallIDs[0] == toolCallIDs[1] {
		t.Fatalf("expected distinct default outerToolCallIds across invocations, both got %q", toolCallIDs[0])
	}
}

func TestRunCodeMode_SupportsToolNamesThatAreNotHostFunctionIdentifiers(t *testing.T) {
	tools := ToolSet{"lookup-user": {
		Name: "lookup-user",
		Parameters: map[string]interface{}{
			"type":                 "object",
			"required":             []interface{}{"id"},
			"properties":           map[string]interface{}{"id": map[string]interface{}{"type": "string"}},
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"id": input["id"]}, nil
		},
	}}
	got, err := RunCodeMode(context.Background(), RunInput{
		JS:    "return await tools['lookup-user']({ id: 'user-1' });",
		Tools: tools,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertDeepEqual(t, got, map[string]interface{}{"id": "user-1"})
}

func TestRunCodeMode_JSONParseStringify(t *testing.T) {
	js := `
          const parsed = JSON.parse('{"count":2,"items":["a","b"]}');
          parsed.items.push("c");
          return JSON.stringify(parsed);
        `
	got, err := RunCodeMode(context.Background(), RunInput{JS: js, Tools: ToolSet{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != `{"count":2,"items":["a","b","c"]}` {
		t.Fatalf("got %#v", got)
	}
}

func TestRunCodeMode_FreshGlobalScopePerInvocation(t *testing.T) {
	ctx := context.Background()
	if _, err := RunCodeMode(ctx, RunInput{JS: "globalThis.sharedValue = 123; return globalThis.sharedValue;", Tools: ToolSet{}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := RunCodeMode(ctx, RunInput{JS: "return globalThis.sharedValue ?? 'missing';", Tools: ToolSet{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "missing" {
		t.Fatalf("got %#v", got)
	}
}

func TestRunCodeMode_ManyRapidIndependentInvocations(t *testing.T) {
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make([]interface{}, 20)
	errs := make([]error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = RunCodeMode(ctx, RunInput{JS: fmt.Sprintf("return %d * %d;", i, i), Tools: ToolSet{}})
		}(i)
	}
	wg.Wait()
	for i := 0; i < 20; i++ {
		if errs[i] != nil {
			t.Fatalf("index %d: unexpected error: %v", i, errs[i])
		}
		if results[i] != float64(i*i) {
			t.Fatalf("index %d: got %#v, want %d", i, results[i], i*i)
		}
	}
}

// Regression coverage for a real, previously-observed failure mode: qjs
// v0.0.6 (github.com/fastschema/qjs, see engine.go's doc comment) is
// pre-1.0, and this package creates a fresh Runtime per invocation, so a
// bug that only manifests after many create/destroy cycles would not show
// up in a handful of calls. This asserts correctness -- not just "no
// crash" -- across 250 sequential invocations with results large enough
// (5-digit numbers) that a truncated/corrupted read would be caught, plus
// a nested tool call and a couple of timeouts interleaved to exercise
// runInSandbox's timeout/grace path repeatedly too. Keep this if a new
// package-level dependency is ever added to the strip/eval path (see
// stripTypeScriptAnnotations's package doc for what NOT to reach for
// without re-running a loop like this one first): a data-corruption
// interaction between it and the sandbox would otherwise be silent.
func TestRunCodeMode_ManyInvocationsRemainCorrect(t *testing.T) {
	tools := ToolSet{"echo": {
		Name:       "echo",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return input, nil
		},
	}}

	for i := 0; i < 250; i++ {
		var js string
		var want interface{}
		switch i % 37 {
		case 0:
			js = "return await tools.echo({ n: " + fmt.Sprintf("%d", i) + " });"
			want = map[string]interface{}{"n": float64(i)}
		default:
			js = fmt.Sprintf("return %d * %d;", 100+i, 100+i)
			want = float64((100 + i) * (100 + i))
		}
		got, err := RunCodeMode(context.Background(), RunInput{JS: js, Tools: tools})
		if err != nil {
			t.Fatalf("iter %d: unexpected error: %v", i, err)
		}
		assertDeepEqual(t, got, want)
	}

	for i := 0; i < 3; i++ {
		_, err := RunCodeMode(context.Background(), RunInput{
			JS:      "while(true){}",
			Tools:   ToolSet{},
			Options: &Options{ExecutionPolicy: &ExecutionPolicy{TimeoutMs: 100}},
		})
		if err == nil {
			t.Fatalf("timeout iter %d: expected an error", i)
		}
	}

	// One more ordinary invocation after the timeouts, to confirm the
	// engine is still usable (not wedged by the abandoned timed-out
	// goroutines; see runInSandbox's doc comment).
	got, err := RunCodeMode(context.Background(), RunInput{JS: "return 999 * 999;", Tools: ToolSet{}})
	if err != nil {
		t.Fatalf("post-timeout invocation: unexpected error: %v", err)
	}
	assertDeepEqual(t, got, float64(999*999))
}

// Ports TypeScript's code-mode/src/tool-invocation.test.ts ("AI SDK tool
// bridge"). "exports a serializable direct tool call marker" is pkg/ai's
// DirectToolCall constant, not pkg/codemode's, and is covered there.
// "uses the final output from async iterable tools" has no Go port: Go's
// types.Tool.Execute returns a single (interface{}, error), not a stream,
// so a host tool cannot be an async generator in the first place.
// "returns an AI SDK tool that executes code mode" is ported in
// code_mode_tool_test.go. "late-binds host tools through generateText" and
// "announces changed tools in conversation while keeping the model tool
// stable" exercise the generic experimental_toolCallers/PrepareModelMessage
// plumbing end-to-end through generateText; that plumbing itself (not
// specific to code mode) is already covered end-to-end by
// pkg/ai's TestGenerateText_ToolCallers_LateBindsLocalCaller and
// TestGenerateText_ToolCallers_AnnouncesLocalCallerInMessage, and this
// package's contribution to that wiring -- CodeModeTool's Bind and
// PrepareModelMessage callbacks -- is covered by
// TestCodeModeTool_BindReturnsWorkingTool and
// TestCodeModeTool_ConversationDiscoveryDescription (tool_prompt_test.go).

func TestRunCodeMode_ChainsMultipleToolCalls(t *testing.T) {
	tools := ToolSet{
		"add": {
			Name:       "add",
			Parameters: map[string]interface{}{"type": "object"},
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				a, _ := input["a"].(float64)
				b, _ := input["b"].(float64)
				return map[string]interface{}{"value": a + b}, nil
			},
		},
		"double": {
			Name:       "double",
			Parameters: map[string]interface{}{"type": "object"},
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				v, _ := input["value"].(float64)
				return map[string]interface{}{"value": v * 2}, nil
			},
		},
	}
	got, err := RunCodeMode(context.Background(), RunInput{
		JS:    "const first = await tools.add({ a: 2, b: 3 }); return await tools.double(first);",
		Tools: tools,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertDeepEqual(t, got, map[string]interface{}{"value": float64(10)})
}

func TestRunCodeMode_RoundTripsUndefinedToolOutputs(t *testing.T) {
	tools := ToolSet{"nothing": {
		Name:       "nothing",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, nil
		},
	}}
	got, err := RunCodeMode(context.Background(), RunInput{
		JS:    "const value = await tools.nothing({}); return { type: typeof value };",
		Tools: tools,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertDeepEqual(t, got, map[string]interface{}{"type": "undefined"})
}

// The sandboxed engine's clock must not go stale across a blocking host
// tool call (a category of bug some embedded JS engines have): the qjs
// runtime blocks synchronously on the Go host function while the host tool
// runs, so it should observe real wall-clock time before and after,
// matching (within a generous tolerance) the host's own clock read inside
// the tool.
func TestRunCodeMode_ResetsClockToHostTimeAfterAsyncToolCalls(t *testing.T) {
	tools := ToolSet{"wait": {
		Name:       "wait",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			time.Sleep(80 * time.Millisecond)
			return map[string]interface{}{"hostNow": float64(time.Now().UnixMilli())}, nil
		},
	}}
	js := `
          const before = Date.now();
          const toolResult = await tools.wait({});
          const after = Date.now();
          return { before, hostNow: toolResult.hostNow, after };
        `
	got, err := RunCodeMode(context.Background(), RunInput{JS: js, Tools: tools})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %#v", got)
	}
	before, _ := result["before"].(float64)
	after, _ := result["after"].(float64)
	hostNow, _ := result["hostNow"].(float64)
	if after-before < 30 {
		t.Fatalf("expected at least 30ms to elapse across the tool call, got %v -> %v", before, after)
	}
	if diff := after - hostNow; diff < -1000 || diff > 1000 {
		t.Fatalf("expected sandbox clock and host clock to agree within 1000ms, got after=%v hostNow=%v", after, hostNow)
	}
}

func TestRunCodeMode_ForwardsContextToNestedTools(t *testing.T) {
	var seenContext interface{}
	tools := ToolSet{"context": {
		Name:       "context",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			seenContext = opts.ToolContext
			return opts.ToolContext, nil
		},
	}}
	got, err := RunCodeMode(context.Background(), RunInput{
		JS:    "return await tools.context({});",
		Tools: tools,
		ToolExecutionOptions: &types.ToolExecutionOptions{
			ToolCallID:  "outer",
			ToolContext: map[string]interface{}{"requestId": "req-1"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertDeepEqual(t, got, map[string]interface{}{"requestId": "req-1"})
	assertDeepEqual(t, seenContext, map[string]interface{}{"requestId": "req-1"})
}

// Mirrors TypeScript's "passes abort signals to nested tools": canceling
// the outer context while a nested tool call is in flight aborts the
// invocation promptly instead of waiting for the tool to notice on its
// own. See raceAgainstAbort in tool_invocation.go.
func TestRunCodeMode_ContextCancellationAbortsInFlightNestedToolCall(t *testing.T) {
	started := make(chan struct{})
	tools := ToolSet{"wait": {
		Name:       "wait",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct {
		v   interface{}
		err error
	}, 1)
	go func() {
		v, err := RunCodeMode(ctx, RunInput{JS: "return await tools.wait({});", Tools: tools})
		done <- struct {
			v   interface{}
			err error
		}{v, err}
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("tool never started")
	}
	cancel()

	select {
	case out := <-done:
		assertErrMatches(t, out.err, `(?i)abort`)
	case <-time.After(5 * time.Second):
		t.Fatal("RunCodeMode did not return promptly after cancellation")
	}
}

// Ports TypeScript's code-mode/src/exceptions.test.ts ("exceptions and serialization").

func TestRunCodeMode_UnknownTool(t *testing.T) {
	_, err := RunCodeMode(context.Background(), RunInput{JS: "return await tools.nope({});", Tools: ToolSet{}})
	assertErrMatches(t, err, `Unknown tool: nope`)
}

func TestRunCodeMode_ToolWithoutExecute(t *testing.T) {
	tools := ToolSet{"manual": {Name: "manual", Parameters: map[string]interface{}{"type": "object"}}}
	_, err := RunCodeMode(context.Background(), RunInput{JS: "return await tools.manual({});", Tools: tools})
	assertErrMatches(t, err, `does not have execute`)
}

func TestRunCodeMode_ValidatesInputBeforeExecute(t *testing.T) {
	var called bool
	tools := ToolSet{"add": {
		Name: "add",
		Parameters: map[string]interface{}{
			"type":     "object",
			"required": []interface{}{"a", "b"},
			"properties": map[string]interface{}{
				"a": map[string]interface{}{"type": "number"},
				"b": map[string]interface{}{"type": "number"},
			},
		},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			called = true
			return map[string]interface{}{"sum": 0}, nil
		},
	}}
	_, err := RunCodeMode(context.Background(), RunInput{JS: "return await tools.add({ a: 'wrong', b: 2 });", Tools: tools})
	assertErrMatches(t, err, `Invalid input`)
	if called {
		t.Fatal("execute should not have been called")
	}
}

// TestRunCodeMode_AppliesSchemaDefaultsBeforeExecute verifies the bridge
// fills JSON Schema "default" values (SLICE SC) before validating and before
// the host tool's Execute runs -- mirroring TS's validateToolInput, which
// hands execute() the zod-parsed (and thus defaulted) value, not the raw
// input (code-mode/src/tool-invocation.ts).
func TestRunCodeMode_AppliesSchemaDefaultsBeforeExecute(t *testing.T) {
	var gotInput map[string]interface{}
	tools := ToolSet{"search": {
		Name: "search",
		Parameters: map[string]interface{}{
			"type":     "object",
			"required": []interface{}{"query"},
			"properties": map[string]interface{}{
				"query":  map[string]interface{}{"type": "string"},
				"region": map[string]interface{}{"type": "string", "default": "us-east-1"},
			},
		},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			gotInput = input
			return map[string]interface{}{"ok": true}, nil
		},
	}}
	_, err := RunCodeMode(context.Background(), RunInput{JS: "return await tools.search({ query: 'widgets' });", Tools: tools})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotInput["region"] != "us-east-1" {
		t.Fatalf("tool saw region = %v, want us-east-1 (default should be filled)", gotInput["region"])
	}
}

func TestRunCodeMode_DoesNotExecuteToolsThatRequireApproval(t *testing.T) {
	var called bool
	tools := ToolSet{"guarded": {
		Name:          "guarded",
		Parameters:    map[string]interface{}{"type": "object"},
		NeedsApproval: true,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			called = true
			return map[string]interface{}{"ok": true}, nil
		},
	}}
	_, err := RunCodeMode(context.Background(), RunInput{JS: "return await tools.guarded({});", Tools: tools})
	var approvalErr *ToolApprovalRequiredError
	if !errors.As(err, &approvalErr) {
		t.Fatalf("expected *ToolApprovalRequiredError, got %#v (%v)", err, err)
	}
	if called {
		t.Fatal("execute should not have been called")
	}
}

func TestRunCodeMode_SanitizesToolThrows(t *testing.T) {
	tools := ToolSet{"fail": {
		Name:       "fail",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, errors.New("tool exploded")
		},
	}}
	_, err := RunCodeMode(context.Background(), RunInput{JS: "return await tools.fail({});", Tools: tools})
	assertErrMatches(t, err, `Host tool failed\.`)
}

func TestRunCodeMode_PropagatesSyntaxErrors(t *testing.T) {
	_, err := RunCodeMode(context.Background(), RunInput{JS: "return ; }", Tools: ToolSet{}})
	assertErrMatches(t, err, `(?i)syntax|unexpected|expression expected`)
}

// TestRunCodeMode_UnsupportedSyntaxFallsBackToRawSource ports the behavior
// of TypeScript's stripSnippetTypes (run package,
// dist/utils/source-cache.js): it catches *any* stripper error and falls
// back to running the snippet unmodified, rather than rejecting it before
// execution. TypeScript syntax the stripper recognizes but cannot erase
// (enums, namespaces, constructor parameter properties) must therefore
// reach QuickJS unstripped and fail there as an ordinary JavaScript
// SyntaxError -- never as a distinct "unsupported syntax" error raised by
// RunCodeMode itself before the sandbox ever runs.
func TestRunCodeMode_UnsupportedSyntaxFallsBackToRawSource(t *testing.T) {
	cases := []struct {
		name string
		js   string
	}{
		{"enum", "enum Color { Red, Green } return Color.Red;"},
		{"namespace", "namespace NS { export const x = 1; } return NS.x;"},
		{"constructor parameter property", "class Box { constructor(public value) {} } return 1;"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := RunCodeMode(context.Background(), RunInput{JS: c.js, Tools: ToolSet{}})
			if err == nil {
				t.Fatalf("expected an error for unsupported TypeScript syntax %q, got none", c.js)
			}
			// A plain QuickJS engine failure, not a package CodeModeError:
			// the source reached the sandbox unstripped and failed to parse
			// there, exactly like TypeScript's fallback.
			if _, ok := err.(CodeModeError); ok {
				t.Fatalf("expected a plain engine syntax error, got a CodeModeError: %v", err)
			}
			assertErrMatches(t, err, `(?i)syntax`)
		})
	}
}

func TestRunCodeMode_PropagatesRuntimeExceptions(t *testing.T) {
	_, err := RunCodeMode(context.Background(), RunInput{JS: "throw new Error('sandbox exploded');", Tools: ToolSet{}})
	assertErrMatches(t, err, `sandbox exploded`)
}

func TestRunCodeMode_RejectsNonJSONSerializableResults(t *testing.T) {
	_, err := RunCodeMode(context.Background(), RunInput{JS: "return 1n;", Tools: ToolSet{}})
	assertErrMatches(t, err, `(?i)JSON-serializable|bigint`)
}

func TestRunCodeMode_NonFiniteNumericResults(t *testing.T) {
	got, err := RunCodeMode(context.Background(), RunInput{JS: "return { value: Infinity };", Tools: ToolSet{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertDeepEqual(t, got, map[string]interface{}{"value": nil})
}

func TestRunCodeMode_FunctionValuesOmitted(t *testing.T) {
	got, err := RunCodeMode(context.Background(), RunInput{JS: "return { value: () => 1 };", Tools: ToolSet{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertDeepEqual(t, got, map[string]interface{}{})
}

func TestRunCodeMode_RejectsCircularToolInputs(t *testing.T) {
	var called bool
	tools := ToolSet{"echo": {
		Name: "echo",
		Parameters: map[string]interface{}{
			"type":       "object",
			"required":   []interface{}{"value"},
			"properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}},
		},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			called = true
			return "should not run", nil
		},
	}}
	js := `
          const input = { value: "x" };
          input.self = input;
          return await tools.echo(input);
        `
	_, err := RunCodeMode(context.Background(), RunInput{JS: js, Tools: tools})
	assertErrMatches(t, err, `(?i)circular|json`)
	if called {
		t.Fatal("execute should not have been called")
	}
}

func TestRunCodeMode_EnforcesMaxResultSize(t *testing.T) {
	_, err := RunCodeMode(context.Background(), RunInput{
		JS:      "return 'abcdef';",
		Tools:   ToolSet{},
		Options: &Options{ExecutionPolicy: &ExecutionPolicy{MaxResultBytes: 4}},
	})
	assertErrMatches(t, err, `size limit`)
}

func TestRunCodeMode_EnforcesMaxToolInputSize(t *testing.T) {
	var called bool
	tools := ToolSet{"echo": {
		Name:       "echo",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			called = true
			return "should not run", nil
		},
	}}
	_, err := RunCodeMode(context.Background(), RunInput{
		JS:      "return await tools.echo({ value: 'abcdef' });",
		Tools:   tools,
		Options: &Options{ExecutionPolicy: &ExecutionPolicy{MaxToolInputBytes: 4}},
	})
	assertErrMatches(t, err, `size limit`)
	if called {
		t.Fatal("execute should not have been called")
	}
}

func TestRunCodeMode_EnforcesMaxToolOutputSize(t *testing.T) {
	tools := ToolSet{"large": {
		Name:       "large",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"value": "abcdef"}, nil
		},
	}}
	_, err := RunCodeMode(context.Background(), RunInput{
		JS:      "return await tools.large({});",
		Tools:   tools,
		Options: &Options{ExecutionPolicy: &ExecutionPolicy{MaxToolOutputBytes: 4}},
	})
	assertErrMatches(t, err, `size limit`)
}

func TestRunCodeMode_RejectsCircularToolOutputs(t *testing.T) {
	output := map[string]interface{}{"value": "x"}
	output["self"] = output
	tools := ToolSet{"circular": {
		Name:       "circular",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return output, nil
		},
	}}
	_, err := RunCodeMode(context.Background(), RunInput{JS: "return await tools.circular({});", Tools: tools})
	assertErrMatches(t, err, `(?i)circular|closes the circle|not JSON-serializable`)
}

func TestRunCodeMode_HostToolOutputDateFormatting(t *testing.T) {
	tools := ToolSet{"date": {
		Name:       "date",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return time.Unix(0, 0).UTC(), nil
		},
	}}
	got, err := RunCodeMode(context.Background(), RunInput{JS: "return await tools.date({});", Tools: tools})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1970-01-01T00:00:00.000Z" {
		t.Fatalf("got %#v", got)
	}
}

func TestRunCodeMode_OmitsNilPropertiesFromHostToolOutputs(t *testing.T) {
	tools := ToolSet{"catalog": {
		Name:       "catalog",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{
				"concepts": []interface{}{
					map[string]interface{}{"name": "first", "recommendedNextAction": "open"},
					map[string]interface{}{"name": "second", "recommendedNextAction": nil},
				},
			}, nil
		},
	}}
	got, err := RunCodeMode(context.Background(), RunInput{JS: "return await tools.catalog({});", Tools: tools})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]interface{}{
		"concepts": []interface{}{
			map[string]interface{}{"name": "first", "recommendedNextAction": "open"},
			map[string]interface{}{"name": "second"},
		},
	}
	assertDeepEqual(t, got, want)
}

// Additional Go-side coverage (policy/approval/abort semantics have no TS
// equivalent test to port directly, since they exercise the `run` engine
// rather than code-mode itself).

func TestRunCodeMode_Timeout(t *testing.T) {
	_, err := RunCodeMode(context.Background(), RunInput{
		JS:      "while(true){}",
		Tools:   ToolSet{},
		Options: &Options{ExecutionPolicy: &ExecutionPolicy{TimeoutMs: 200}},
	})
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("expected *TimeoutError, got %#v (%v)", err, err)
	}
}

func TestRunCodeMode_Aborted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := RunCodeMode(ctx, RunInput{JS: "return 1;", Tools: ToolSet{}})
	var abortedErr *AbortedError
	if !errors.As(err, &abortedErr) {
		t.Fatalf("expected *AbortedError, got %#v (%v)", err, err)
	}
}

func TestRunCodeMode_SourceTooLarge(t *testing.T) {
	_, err := RunCodeMode(context.Background(), RunInput{
		JS:      "return 1;",
		Tools:   ToolSet{},
		Options: &Options{ExecutionPolicy: &ExecutionPolicy{MaxSourceBytes: 4}},
	})
	var tooLarge *SourceTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("expected *SourceTooLargeError, got %#v (%v)", err, err)
	}
}

func TestRunCodeMode_ApprovalCallback_Approved(t *testing.T) {
	tools := ToolSet{"guarded": {
		Name:          "guarded",
		Parameters:    map[string]interface{}{"type": "object"},
		NeedsApproval: true,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"ok": true}, nil
		},
	}}
	got, err := RunCodeMode(context.Background(), RunInput{
		JS:    "return await tools.guarded({});",
		Tools: tools,
		Options: &Options{Approval: &ApprovalOptions{
			OnApprovalRequired: func(ctx context.Context, req ApprovalRequest) (ApprovalDecision, error) {
				if req.ToolName != "guarded" {
					t.Fatalf("unexpected tool name %q", req.ToolName)
				}
				return Approved(), nil
			},
		}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertDeepEqual(t, got, map[string]interface{}{"ok": true})
}

func TestRunCodeMode_ApprovalCallback_Denied(t *testing.T) {
	tools := ToolSet{"guarded": {
		Name:          "guarded",
		Parameters:    map[string]interface{}{"type": "object"},
		NeedsApproval: true,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"ok": true}, nil
		},
	}}
	_, err := RunCodeMode(context.Background(), RunInput{
		JS:    "return await tools.guarded({});",
		Tools: tools,
		Options: &Options{Approval: &ApprovalOptions{
			OnApprovalRequired: func(ctx context.Context, req ApprovalRequest) (ApprovalDecision, error) {
				return Denied("no"), nil
			},
		}},
	})
	var deniedErr *ToolApprovalDeniedError
	if !errors.As(err, &deniedErr) {
		t.Fatalf("expected *ToolApprovalDeniedError, got %#v (%v)", err, err)
	}
}

// ApprovalModeInterrupt is now implemented (see continuation_test.go and
// approval_continuation_test.go for its ported TypeScript coverage); this
// only checks that a tool requiring approval under it pauses with an
// *Interrupt instead of erroring, without exercising the full
// continuation round trip.
func TestRunCodeMode_InterruptModeReturnsInterrupt(t *testing.T) {
	tools := ToolSet{"guarded": {
		Name:          "guarded",
		Parameters:    map[string]interface{}{"type": "object"},
		NeedsApproval: true,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, nil
		},
	}}
	got, err := RunCodeMode(context.Background(), RunInput{
		JS:      "return await tools.guarded({});",
		Tools:   tools,
		Options: &Options{Approval: &ApprovalOptions{Mode: ApprovalModeInterrupt}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	interrupt, ok := got.(*Interrupt)
	if !ok {
		t.Fatalf("expected *Interrupt, got %#v", got)
	}
	if interrupt.ToolName != "guarded" || interrupt.Payload.Kind() != ToolApprovalKind {
		t.Fatalf("unexpected interrupt: %#v", interrupt)
	}
}

func TestRunCodeMode_UsesSchemaSchemaInterface(t *testing.T) {
	tools := ToolSet{"add": {
		Name:       "add",
		Parameters: schema.NewJSONSchema(map[string]interface{}{"type": "object"}),
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"ok": true}, nil
		},
	}}
	got, err := RunCodeMode(context.Background(), RunInput{JS: "return await tools.add({});", Tools: tools})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertDeepEqual(t, got, map[string]interface{}{"ok": true})
}

func TestSetMaxWorkers_RejectsOverCap(t *testing.T) {
	if err := SetMaxWorkers(1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() {
		if err := SetMaxWorkers(0); err != nil {
			t.Fatalf("unexpected error resetting maxWorkers: %v", err)
		}
	}()

	release, err := acquireWorkerSlot()
	if err != nil {
		t.Fatalf("unexpected error acquiring the first slot: %v", err)
	}
	defer release()

	_, err = RunCodeMode(context.Background(), RunInput{JS: "return 1;", Tools: ToolSet{}})
	var concurrencyErr *ConcurrencyError
	if !errors.As(err, &concurrencyErr) {
		t.Fatalf("expected *ConcurrencyError, got %#v (%v)", err, err)
	}
}

func assertErrMatches(t *testing.T, err error, pattern string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error matching %q, got nil", pattern)
	}
	matched, rerr := regexp.MatchString(pattern, err.Error())
	if rerr != nil {
		t.Fatalf("bad pattern %q: %v", pattern, rerr)
	}
	if !matched {
		t.Fatalf("error %q does not match pattern %q", err.Error(), pattern)
	}
}

func assertDeepEqual(t *testing.T, got, want interface{}) {
	t.Helper()
	if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
