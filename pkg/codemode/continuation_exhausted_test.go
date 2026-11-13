package codemode

import (
	"context"
	"errors"
	"testing"
)

// TS prepareContinuation reports a continuation with no pending entry at the
// next index with the ordinary mismatch error and the interruptId details.
func TestRunCodeModeFullyResolvedContinuationIsMismatch(t *testing.T) {
	security, err := resolveContinuationSecurity(ContinuationSecurityOptions{SigningKey: []byte("k")})
	if err != nil {
		t.Fatal(err)
	}
	pending := PendingInterruption{RunInterruptionID: "interrupt-0", InterruptID: "outer:tool-0:interrupt", ToolName: "a", ToolCallID: "outer:tool-0", Payload: InterruptPayload{"kind": "custom"}}
	signed, err := signContinuation(Continuation{
		Version: 2, JS: "return 1", OuterToolCallID: "outer", ToolNames: []string{}, Token: "t",
		PendingInterruptions: []PendingInterruption{pending},
		Resolutions:          []PendingResolution{{RunInterruptionID: "interrupt-0", Value: 1}},
	}, security)
	if err != nil {
		t.Fatal(err)
	}
	_, err = RunCodeMode(context.Background(), RunInput{
		JS: "return 1", Tools: ToolSet{},
		Options:             &Options{ContinuationSecurity: &ContinuationSecurityOptions{SigningKey: []byte("k")}},
		Continuation:        &signed,
		InterruptResolution: &InterruptResolution{InterruptID: "x", Resolution: 2},
	})
	var perr *ProtocolError
	if !errors.As(err, &perr) || perr.Message != "Interrupt resolution does not match the next pending code-mode interruption." {
		t.Fatalf("err = %v", err)
	}
	if d, _ := perr.Details.(map[string]interface{}); d["interruptId"] != "x" {
		t.Fatalf("details = %#v", perr.Details)
	}
}

// TS messages interpolate the tool name inside literal quotes
// (`Tool "${toolName}" ...`); Go's %q would escape quotes and backslashes.
func TestToolErrorMessagesQuoteNamesLikeTS(t *testing.T) {
	name := `a"b\c`
	if got, want := NewToolApprovalRequiredError(name, nil, "id").Message, `Tool "a"b\c" requires approval before execution.`; got != want {
		t.Fatalf("required = %q, want %q", got, want)
	}
	if got, want := NewToolApprovalDeniedError(name, nil, "id", "").Message, `Tool "a"b\c" approval was denied.`; got != want {
		t.Fatalf("denied = %q, want %q", got, want)
	}
	if got, want := NewToolApprovalDeniedError(name, nil, "id", "no").Message, `Tool "a"b\c" approval was denied: no`; got != want {
		t.Fatalf("denied reason = %q, want %q", got, want)
	}
}
