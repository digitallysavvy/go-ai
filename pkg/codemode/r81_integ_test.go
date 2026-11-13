package codemode

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

func r81integEchoTool(params interface{}) types.Tool {
	return types.Tool{
		Name:       "x",
		Parameters: params,
		Execute: func(_ context.Context, input map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"ok": true}, nil
		},
	}
}

// A bare schema.Validator as Parameters is used to validate the input (TS
// code mode validates with asSchema(tool.inputSchema) for every schema).
func TestR81integCodeModeBareValidatorParameters(t *testing.T) {
	v := schema.NewJSONSchema(map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}},
		"required":   []interface{}{"q"},
	})
	codeMode := CreateCodeModeTool(ToolSet{"x": r81integEchoTool(v)}, Options{})
	_, err := codeMode.Execute(context.Background(),
		map[string]interface{}{"js": "return await tools.x({});"},
		types.ToolExecutionOptions{ToolCallID: "code"})
	if err == nil || !strings.Contains(err.Error(), `Invalid input for tool "x"`) {
		t.Fatalf("missing q: err = %v, want an invalid input error", err)
	}
	if _, err := codeMode.Execute(context.Background(),
		map[string]interface{}{"js": `return await tools.x({ q: "a" });`},
		types.ToolExecutionOptions{ToolCallID: "code"}); err != nil {
		t.Fatalf("valid input: %v", err)
	}
}

// A typed nil schema is no schema: no panic in Validator(), input accepted.
func TestR81integCodeModeTypedNilSchema(t *testing.T) {
	for name, params := range map[string]interface{}{
		"nil struct schema": (*schema.SimpleStructSchema)(nil),
		"nil validator":     (*schema.JSONSchemaValidator)(nil),
	} {
		t.Run(name, func(t *testing.T) {
			if v := toolInputValidator(r81integEchoTool(params)); v != nil {
				t.Fatalf("validator = %#v, want nil", v)
			}
		})
	}
}
