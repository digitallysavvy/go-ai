package harness

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

func r91carryFinishScript(func(string, interface{})) []StreamPart {
	return []StreamPart{
		&StreamStartPart{},
		&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
		&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
	}
}

// harness-01: TS harness-agent.test.ts "validates custom call options
// before preparing a turn" (harness-agent.ts 1042): invalid options fail
// with a TypeValidationError and PrepareCall is not called; valid options
// reach PrepareCall as the validated (defaulted) value.
func TestR91carryHarnessCallOptionsSchema(t *testing.T) {
	callOptionsSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"tenant": map[string]interface{}{"type": "string"},
			"tier":   map[string]interface{}{"type": "string", "default": "free"},
		},
		"required": []interface{}{"tenant"},
	})
	var prepared []interface{}
	mock := newMockHarness(mockHarnessOptions{script: r91carryFinishScript})
	a, err := NewAgent(AgentSettings{
		Harness:           mock.harness,
		CallOptionsSchema: callOptionsSchema,
		PrepareCall: func(_ context.Context, opts PrepareCallOptions) (PrepareCallResult, error) {
			prepared = append(prepared, opts.CallOptions)
			return PrepareCallResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "Hello", HarnessSession: session, CallOptions: map[string]interface{}{"tenant": 123}})
	var ve *providererrors.ValidationError
	if !errors.As(err, &ve) || ve.Context == nil || ve.Context.Field != "options" {
		t.Fatalf("err = %v, want a ValidationError for field options", err)
	}
	if len(prepared) != 0 {
		t.Fatalf("PrepareCall ran with invalid options: %v", prepared)
	}

	type opts struct {
		Tenant string `json:"tenant"`
	}
	if _, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "Hello", HarnessSession: session, CallOptions: opts{Tenant: "acme"}}); err != nil {
		t.Fatal(err)
	}
	if len(prepared) != 1 {
		t.Fatalf("PrepareCall calls = %d", len(prepared))
	}
	got, _ := prepared[0].(map[string]interface{})
	if got["tenant"] != "acme" || got["tier"] != "free" {
		t.Fatalf("PrepareCall CallOptions = %#v, want the validated value with defaults", prepared[0])
	}
}
