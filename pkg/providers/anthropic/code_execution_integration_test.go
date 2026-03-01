package anthropic

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

// TestAnthropicCodeExecution_Integration tests the code execution tool with a live API key.
// This test is skipped when ANTHROPIC_API_KEY is not set.
func TestAnthropicCodeExecution_Integration(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping: ANTHROPIC_API_KEY not set")
	}

	prov := New(Config{APIKey: apiKey})
	model, err := prov.LanguageModel("claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("Failed to create model: %v", err)
	}

	// Create the code execution tool
	codeExecTool := tools.CodeExecution20260120()

	maxTokens := 1024
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{
			Text: "Use the code execution tool to run: echo hello world",
		},
		Tools:     []types.Tool{codeExecTool},
		MaxTokens: &maxTokens,
	}

	result, err := model.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatalf("DoGenerate failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Check that the model made tool calls
	if len(result.ToolCalls) == 0 {
		t.Log("Model did not make tool calls (may be expected for some prompts)")
		return
	}

	// Verify the tool call input can be parsed
	for _, tc := range result.ToolCalls {
		t.Logf("Tool call: %s", tc.ToolName)

		inputJSON, err := json.Marshal(tc.Arguments)
		if err != nil {
			t.Errorf("Failed to marshal tool call arguments: %v", err)
			continue
		}

		input, err := tools.UnmarshalCodeExecutionInput(inputJSON)
		if err != nil {
			t.Errorf("Failed to unmarshal code execution input: %v", err)
			continue
		}

		t.Logf("Input type: %s", input.GetInputType())

		switch v := input.(type) {
		case *tools.BashCodeExecutionInput:
			t.Logf("Bash command: %s", v.Command)
		case *tools.ProgrammaticToolCallInput:
			t.Logf("Python code: %s", v.Code)
		case *tools.TextEditorInput:
			t.Logf("Text editor command: %s, path: %s", v.Command, v.Path)
		}
	}
}

// TestAnthropicCodeExecution_BetaHeaderInjected verifies the beta header is included
// in the request when the code execution tool is in the tool list.
func TestAnthropicCodeExecution_BetaHeaderInjected(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "claude-opus-4-6", nil)

	codeExecTool := tools.CodeExecution20260120()
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "test"},
		Tools:  []types.Tool{codeExecTool},
	}

	header := model.combineBetaHeaders(opts, false)
	if header == "" {
		t.Error("Expected beta header to be set when code execution tool is present")
	}
	if header != BetaHeaderCodeExecution {
		t.Errorf("Beta header = %q, want %q", header, BetaHeaderCodeExecution)
	}
}
