package harness

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	jsonschema "github.com/digitallysavvy/go-ai/pkg/schema"
)

type r83carrySpecInput struct {
	Zip     string `json:"zip" validate:"required"`
	City    string `json:"city,omitempty"`
	Country string `json:"country,omitempty"`
}

// r83carryToolSpecJSON runs one turn with the tools and returns each tool
// spec's inputSchema as sent to the adapter (JSON).
func r83carryToolSpecJSON(t *testing.T, userTools map[string]types.Tool) map[string]string {
	t.Helper()
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	a, session := newTestAgent(t, mock, userTools, Callbacks{}, ToolApprovalConfiguration{})
	if _, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: session}); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, spec := range mock.turnSettings[0].Tools {
		b, err := json.Marshal(spec.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		out[spec.Name] = string(b)
	}
	return out
}

// TS _toToolSpecs sends asSchema(inputSchema).jsonSchema, whose keys keep
// the zod object's (or the MCP server's) order. A Go struct schema, a bare
// struct validator and an MCP schema map with its ParameterKeyOrder keep it
// too, and a bare validator is not dropped.
func TestR83carryToolSpecSchemaKeyOrder(t *testing.T) {
	structSchema := jsonschema.NewSimpleStructSchema(reflect.TypeOf(r83carrySpecInput{}))
	want, _ := json.Marshal(structSchema)
	mcpRaw := []byte(`{"type":"object","properties":{"zeta":{"type":"string"},"alpha":{"type":"number"}},"required":["zeta"]}`)
	order, _ := jsonorder.Parse(mcpRaw)
	var mcpMap map[string]interface{}
	if err := json.Unmarshal(mcpRaw, &mcpMap); err != nil {
		t.Fatal(err)
	}
	got := r83carryToolSpecJSON(t, map[string]types.Tool{
		"struct": {Description: "s", Parameters: structSchema},
		"bare":   {Description: "b", Parameters: jsonschema.NewStructSchema(reflect.TypeOf(r83carrySpecInput{}))},
		"mcp":    {Description: "m", Parameters: mcpMap, ParameterKeyOrder: order},
		"plain":  {Description: "p", Parameters: map[string]interface{}{"type": "object"}},
		"none":   {Description: "n"},
	})
	for name, w := range map[string]string{
		"struct": string(want),
		"bare":   string(want),
		"mcp":    string(mcpRaw),
		"plain":  `{"type":"object"}`,
		"none":   `null`,
	} {
		if got[name] != w {
			t.Errorf("%s inputSchema\n got %s\nwant %s", name, got[name], w)
		}
	}
}
