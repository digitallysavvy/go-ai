package codemode

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS invokeCodeModeTool sanitizes a plain tool error to
// RunError('Host tool failed.', 'CODE_MODE_HOST_TOOL_ERROR').
func TestRunCodeMode_HostToolFailureCode(t *testing.T) {
	tools := ToolSet{"fail": {
		Name:       "fail",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, errors.New("tool exploded")
		},
	}}
	_, err := RunCodeMode(context.Background(), RunInput{JS: "return await tools.fail({});", Tools: tools})
	var te *ToolError
	if !errors.As(err, &te) {
		t.Fatalf("want *ToolError, got %T %v", err, err)
	}
	if te.Message != "Host tool failed." || te.Code != "CODE_MODE_HOST_TOOL_ERROR" {
		t.Fatalf("got %q %q", te.Message, te.Code)
	}
}

// TS preserves a CodeModeError thrown by a host tool (codeModeErrors /
// findPreservedCodeModeError) instead of sanitizing it: e.g.
// experimental_requestCodeModeInterrupt with a malformed payload throws
// CodeModeProtocolError, which reaches the caller unchanged.
func TestRunCodeMode_HostToolCodeModeErrorPreserved(t *testing.T) {
	tools := ToolSet{"bad": {
		Name:       "bad",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, RequestCodeModeInterrupt(InterruptPayload{"noKind": true})
		},
	}}
	_, err := RunCodeMode(context.Background(), RunInput{JS: "return await tools.bad({});", Tools: tools})
	var pe *ProtocolError
	if !errors.As(err, &pe) {
		t.Fatalf("want *ProtocolError, got %T %v", err, err)
	}
	if pe.Message != "Code mode interrupt payload must include a string kind." {
		t.Fatalf("got %q", pe.Message)
	}

	// A guest that catches the rejection sees the preserved message too.
	got, err := RunCodeMode(context.Background(), RunInput{
		JS:    "try { await tools.bad({}); } catch (e) { return e.message; }",
		Tools: tools,
	})
	if err != nil || got != "Code mode interrupt payload must include a string kind." {
		t.Fatalf("got %v %v", got, err)
	}
}

// A typed tool error the script caught is not the reason the script failed
// later: TS findPreservedCodeModeError only matches the run's own error.
func TestRunCodeMode_CaughtToolErrorNotReportedForLaterFailure(t *testing.T) {
	tools := ToolSet{"fail": {
		Name:       "fail",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, errors.New("tool exploded")
		},
	}}
	for _, js := range []string{
		"try { await tools.fail({}); } catch (e) {} throw new Error('guest own');",
		"try { await tools.nope({}); } catch (e) {} throw new Error('guest own');",
	} {
		_, err := RunCodeMode(context.Background(), RunInput{JS: js, Tools: tools})
		var cm CodeModeError
		if err == nil || errors.As(err, &cm) || !strings.Contains(err.Error(), "guest own") {
			t.Fatalf("%s: got %T %v", js, err, err)
		}
	}

	// A CPU timeout after a caught tool error is reported as the timeout.
	_, err := RunCodeMode(context.Background(), RunInput{
		JS:      "try { await tools.nope({}); } catch (e) {} while (true) {}",
		Tools:   tools,
		Options: &Options{ExecutionPolicy: &ExecutionPolicy{CPUTimeoutMs: 200}},
	})
	var ce *CPUTimeoutError
	if !errors.As(err, &ce) {
		t.Fatalf("want *CPUTimeoutError, got %T %v", err, err)
	}

	// Rethrowing the caught error still reports the typed tool error.
	_, err = RunCodeMode(context.Background(), RunInput{
		JS:    "try { await tools.nope({}); } catch (e) { throw e; }",
		Tools: tools,
	})
	var te *ToolError
	if !errors.As(err, &te) || te.Message != "Unknown tool: nope" {
		t.Fatalf("want *ToolError Unknown tool, got %T %v", err, err)
	}
}

// TS approval errors extend CodeModeToolError (instanceof matches).
func TestToolApprovalErrorsMatchToolError(t *testing.T) {
	for _, err := range []error{
		NewToolApprovalRequiredError("x", nil, "c"),
		NewToolApprovalDeniedError("x", nil, "c", "no"),
	} {
		var te *ToolError
		if !errors.As(err, &te) || te.Code != err.(CodeModeError).ErrorCode() || te.Message != err.Error() {
			t.Fatalf("%T: errors.As *ToolError failed", err)
		}
		var pe *ProtocolError
		if errors.As(err, &pe) {
			t.Fatalf("%T matched *ProtocolError", err)
		}
	}
}
