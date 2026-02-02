package middleware

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestAddToolInputExamplesMiddleware_WithExamples(t *testing.T) {
	tool := types.Tool{
		Name:        "get_weather",
		Description: "Get weather for a location",
		InputExamples: []types.ToolInputExample{
			{
				Input: map[string]interface{}{
					"city": "New York",
				},
			},
			{
				Input: map[string]interface{}{
					"city": "London",
				},
			},
		},
	}

	mockModel := &mockLanguageModel{
		generateResult: &types.GenerateResult{
			Text: "test",
		},
	}

	middleware := AddToolInputExamplesMiddleware(nil)
	wrapped := WrapLanguageModel(mockModel, []*LanguageModelMiddleware{middleware}, nil, nil)

	opts := &provider.GenerateOptions{
		Tools: []types.Tool{tool},
	}

	_, err := wrapped.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The middleware should have modified the tools
	// Check that the original opts were not modified (middleware should return new params)
	if len(tool.InputExamples) == 0 {
		t.Error("original tool should not be modified")
	}
}

func TestAddToolInputExamplesMiddleware_DescriptionUpdate(t *testing.T) {
	tests := []struct {
		name               string
		tool               types.Tool
		options            *AddToolInputExamplesOptions
		expectedInDesc     []string
		shouldHaveExamples bool
	}{
		{
			name: "tool with existing description",
			tool: types.Tool{
				Name:        "get_weather",
				Description: "Get weather for a location",
				InputExamples: []types.ToolInputExample{
					{
						Input: map[string]interface{}{
							"city": "NYC",
						},
					},
				},
			},
			options: &AddToolInputExamplesOptions{
				Prefix: "Input Examples:",
				Remove: true,
			},
			expectedInDesc:     []string{"Get weather for a location", "Input Examples:", `"city":"NYC"`},
			shouldHaveExamples: false,
		},
		{
			name: "tool without description",
			tool: types.Tool{
				Name: "calculate",
				InputExamples: []types.ToolInputExample{
					{
						Input: map[string]interface{}{
							"x": 5,
							"y": 10,
						},
					},
				},
			},
			options: &AddToolInputExamplesOptions{
				Prefix: "Examples:",
				Remove: true,
			},
			expectedInDesc:     []string{"Examples:", `"x":5`, `"y":10`},
			shouldHaveExamples: false,
		},
		{
			name: "keep examples",
			tool: types.Tool{
				Name:        "test",
				Description: "Test tool",
				InputExamples: []types.ToolInputExample{
					{
						Input: map[string]interface{}{
							"value": "test",
						},
					},
				},
			},
			options: &AddToolInputExamplesOptions{
				Prefix: "Input Examples:",
				Remove: false,
			},
			expectedInDesc:     []string{"Test tool", "Input Examples:", `"value":"test"`},
			shouldHaveExamples: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockModel := &mockLanguageModel{
				generateResult: &types.GenerateResult{Text: "test"},
			}

			middleware := AddToolInputExamplesMiddleware(tt.options)
			_ = WrapLanguageModel(mockModel, []*LanguageModelMiddleware{middleware}, nil, nil)

			opts := &provider.GenerateOptions{
				Tools: []types.Tool{tt.tool},
			}

			// Get the transformed params by calling the middleware's TransformParams directly
			transformedOpts, err := middleware.TransformParams(context.Background(), "generate", opts, mockModel)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(transformedOpts.Tools) != 1 {
				t.Fatalf("expected 1 tool, got %d", len(transformedOpts.Tools))
			}

			transformedTool := transformedOpts.Tools[0]

			// Check description contains expected strings
			for _, expected := range tt.expectedInDesc {
				if !strings.Contains(transformedTool.Description, expected) {
					t.Errorf("description should contain %q, got: %q", expected, transformedTool.Description)
				}
			}

			// Check if examples were removed or kept
			hasExamples := len(transformedTool.InputExamples) > 0
			if hasExamples != tt.shouldHaveExamples {
				t.Errorf("shouldHaveExamples=%v, but hasExamples=%v", tt.shouldHaveExamples, hasExamples)
			}
		})
	}
}

func TestAddToolInputExamplesMiddleware_NoExamples(t *testing.T) {
	tool := types.Tool{
		Name:        "no_examples",
		Description: "Tool without examples",
	}

	mockModel := &mockLanguageModel{
		generateResult: &types.GenerateResult{Text: "test"},
	}

	middleware := AddToolInputExamplesMiddleware(nil)

	opts := &provider.GenerateOptions{
		Tools: []types.Tool{tool},
	}

	transformedOpts, err := middleware.TransformParams(context.Background(), "generate", opts, mockModel)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Tool without examples should be unchanged
	if transformedOpts.Tools[0].Description != tool.Description {
		t.Errorf("tool without examples should not have description modified")
	}
}

func TestAddToolInputExamplesMiddleware_NoTools(t *testing.T) {
	mockModel := &mockLanguageModel{
		generateResult: &types.GenerateResult{Text: "test"},
	}

	middleware := AddToolInputExamplesMiddleware(nil)

	opts := &provider.GenerateOptions{
		Tools: []types.Tool{},
	}

	transformedOpts, err := middleware.TransformParams(context.Background(), "generate", opts, mockModel)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return same params
	if len(transformedOpts.Tools) != 0 {
		t.Error("expected no tools")
	}
}

func TestAddToolInputExamplesMiddleware_CustomFormat(t *testing.T) {
	tool := types.Tool{
		Name: "test",
		InputExamples: []types.ToolInputExample{
			{
				Input: map[string]interface{}{
					"value": "test",
				},
			},
		},
	}

	mockModel := &mockLanguageModel{
		generateResult: &types.GenerateResult{Text: "test"},
	}

	middleware := AddToolInputExamplesMiddleware(&AddToolInputExamplesOptions{
		Prefix: "Examples:",
		Format: func(example types.ToolInputExample, index int) string {
			return "CUSTOM"
		},
		Remove: true,
	})

	opts := &provider.GenerateOptions{
		Tools: []types.Tool{tool},
	}

	transformedOpts, err := middleware.TransformParams(context.Background(), "generate", opts, mockModel)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	desc := transformedOpts.Tools[0].Description
	if !strings.Contains(desc, "CUSTOM") {
		t.Errorf("description should contain custom format, got: %q", desc)
	}
}

func TestAddToolInputExamplesMiddleware_MultipleTools(t *testing.T) {
	tools := []types.Tool{
		{
			Name:        "tool1",
			Description: "First tool",
			InputExamples: []types.ToolInputExample{
				{Input: map[string]interface{}{"a": 1}},
			},
		},
		{
			Name:        "tool2",
			Description: "Second tool",
			// No examples
		},
		{
			Name:        "tool3",
			Description: "Third tool",
			InputExamples: []types.ToolInputExample{
				{Input: map[string]interface{}{"b": 2}},
			},
		},
	}

	mockModel := &mockLanguageModel{
		generateResult: &types.GenerateResult{Text: "test"},
	}

	middleware := AddToolInputExamplesMiddleware(nil)

	opts := &provider.GenerateOptions{
		Tools: tools,
	}

	transformedOpts, err := middleware.TransformParams(context.Background(), "generate", opts, mockModel)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(transformedOpts.Tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(transformedOpts.Tools))
	}

	// tool1 should have examples appended
	if !strings.Contains(transformedOpts.Tools[0].Description, "Input Examples:") {
		t.Error("tool1 should have examples in description")
	}

	// tool2 should be unchanged
	if transformedOpts.Tools[1].Description != "Second tool" {
		t.Error("tool2 description should be unchanged")
	}

	// tool3 should have examples appended
	if !strings.Contains(transformedOpts.Tools[2].Description, "Input Examples:") {
		t.Error("tool3 should have examples in description")
	}
}

func TestAddToolInputExamplesMiddleware_NilOptions(t *testing.T) {
	tool := types.Tool{
		Name: "test",
		InputExamples: []types.ToolInputExample{
			{Input: map[string]interface{}{"x": 1}},
		},
	}

	mockModel := &mockLanguageModel{
		generateResult: &types.GenerateResult{Text: "test"},
	}

	// Test with nil options - should use defaults
	middleware := AddToolInputExamplesMiddleware(nil)

	opts := &provider.GenerateOptions{
		Tools: []types.Tool{tool},
	}

	transformedOpts, err := middleware.TransformParams(context.Background(), "generate", opts, mockModel)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	desc := transformedOpts.Tools[0].Description
	if !strings.Contains(desc, "Input Examples:") {
		t.Error("should use default prefix")
	}

	if len(transformedOpts.Tools[0].InputExamples) > 0 {
		t.Error("should remove examples by default")
	}
}
