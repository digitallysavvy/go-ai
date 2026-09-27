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

func TestRunCodeMode_InterruptModeUnsupported(t *testing.T) {
	tools := ToolSet{"guarded": {
		Name:          "guarded",
		Parameters:    map[string]interface{}{"type": "object"},
		NeedsApproval: true,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, nil
		},
	}}
	_, err := RunCodeMode(context.Background(), RunInput{
		JS:      "return await tools.guarded({});",
		Tools:   tools,
		Options: &Options{Approval: &ApprovalOptions{Mode: ApprovalModeInterrupt}},
	})
	var protoErr *ProtocolError
	if !errors.As(err, &protoErr) {
		t.Fatalf("expected *ProtocolError, got %#v (%v)", err, err)
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
	SetMaxWorkers(1)
	defer SetMaxWorkers(0)

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
