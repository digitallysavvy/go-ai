package tool

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestFormatsAndParsers(t *testing.T) {
	tools := []types.Tool{
		{Name: "lookup", Description: "lookup", Parameters: nil},
	}
	google := ToGoogleFormat(tools)
	if len(google) != 1 {
		t.Fatalf("ToGoogleFormat length = %d", len(google))
	}
	if _, ok := google[0]["parameters"].(map[string]interface{}); !ok {
		t.Fatalf("expected default parameter schema, got %+v", google[0]["parameters"])
	}

	args, err := ParseToolCallArguments(`{"city":"nyc"}`)
	if err != nil || args["city"] != "nyc" {
		t.Fatalf("ParseToolCallArguments(string) failed: args=%+v err=%v", args, err)
	}
	args, err = ParseToolCallArguments([]byte(`{"x":1}`))
	if err != nil || args["x"].(float64) != 1 {
		t.Fatalf("ParseToolCallArguments(bytes) failed: args=%+v err=%v", args, err)
	}
	if _, err := ParseToolCallArguments(123); err == nil {
		t.Fatal("expected unsupported type error")
	}
}

func TestToolLookupAndChoiceConverters(t *testing.T) {
	available := []types.Tool{{Name: "search"}, {Name: "weather"}}
	if err := ValidateToolCall(types.ToolCall{ToolName: "search"}, available); err != nil {
		t.Fatalf("ValidateToolCall known tool failed: %v", err)
	}
	if err := ValidateToolCall(types.ToolCall{ToolName: "missing"}, available); err == nil {
		t.Fatal("expected unknown tool error")
	}

	tool, err := FindTool("weather", available)
	if err != nil || tool.Name != "weather" {
		t.Fatalf("FindTool() failed: tool=%+v err=%v", tool, err)
	}
	if _, err := FindTool("missing", available); err == nil {
		t.Fatal("expected not found error")
	}

	openAI := ConvertToolChoiceToOpenAI(types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "search"})
	openAIMap, ok := openAI.(map[string]interface{})
	if !ok || openAIMap["type"] != "function" {
		t.Fatalf("unexpected openai tool choice: %#v", openAI)
	}
	if ConvertToolChoiceToGoogle(types.ToolChoice{Type: types.ToolChoiceRequired}) != "ANY" {
		t.Fatal("expected ANY for required choice")
	}
	anthropic := ConvertToolChoiceToAnthropic(types.ToolChoice{Type: types.ToolChoiceNone})
	if anthropic != nil {
		t.Fatalf("expected nil anthropic choice for none, got %#v", anthropic)
	}
}

func TestAnthropicSchemaSanitizerHelpers(t *testing.T) {
	if !supportedAnthropicStringFormat("email") || supportedAnthropicStringFormat("bitcoin") {
		t.Fatal("supportedAnthropicStringFormat mismatch")
	}
	if formatAnthropicConstraintName("exclusiveMaximum") != "exclusive maximum" {
		t.Fatalf("formatAnthropicConstraintName unexpected result")
	}
	if got := formatAnthropicConstraintValue(map[string]int{"a": 1}); got == "" {
		t.Fatal("formatAnthropicConstraintValue should not be empty")
	}
	desc := anthropicConstraintDescription(map[string]interface{}{
		"minimum": 1,
		"format":  "custom-format",
	})
	if desc == "" {
		t.Fatal("expected anthropicConstraintDescription output")
	}
}
