package codemode

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

type r80integAddInput struct {
	A float64 `json:"a" validate:"required"`
	B float64 `json:"b"`
}

// A code-mode tool with a struct-tag Parameters schema validates the decoded
// input (v0.5.2 always failed: "validator: (nil map[string]interface {})").
func TestR80integCodeModeStructSchemaTool(t *testing.T) {
	tools := ToolSet{"add": {
		Name:       "add",
		Parameters: schema.NewSimpleStructSchema(reflect.TypeOf(r80integAddInput{})),
		Execute: func(_ context.Context, input map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			a, _ := input["a"].(float64)
			b, _ := input["b"].(float64)
			return map[string]interface{}{"sum": a + b}, nil
		},
	}}
	codeMode := CreateCodeModeTool(tools, Options{})
	got, err := codeMode.Execute(context.Background(),
		map[string]interface{}{"js": "return await tools.add({ a: 4, b: 6 });"},
		types.ToolExecutionOptions{ToolCallID: "code"})
	if err != nil {
		t.Fatalf("valid input: %v", err)
	}
	assertDeepEqual(t, got, map[string]interface{}{"sum": float64(10)})

	_, err = codeMode.Execute(context.Background(),
		map[string]interface{}{"js": "return await tools.add({ b: 6 });"},
		types.ToolExecutionOptions{ToolCallID: "code"})
	if err == nil || !strings.Contains(err.Error(), `Invalid input for tool "add"`) {
		t.Fatalf("missing required a: err = %v, want an invalid input error", err)
	}
}
