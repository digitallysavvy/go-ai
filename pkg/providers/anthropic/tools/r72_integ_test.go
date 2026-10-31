package tools

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// r72 prov CR-1: TS code-execution_20250825/_20260120 text editor create has
// file_text: z.string().nullish(), so a provider-executed create call with
// "file_text": null is valid.
func TestR72Integ_CodeExecutionFileTextNullable(t *testing.T) {
	for name, params := range map[string]interface{}{
		"20250825": CodeExecution20250825().Parameters,
		"20260120": CodeExecution20260120().Parameters,
	} {
		v := schema.NewJSONSchema(params.(map[string]interface{}))
		input := map[string]interface{}{"type": "text_editor_code_execution", "command": "create", "path": "/a", "file_text": nil}
		if err := v.Validate(input); err != nil {
			t.Errorf("%s: file_text null rejected: %v", name, err)
		}
		input["file_text"] = float64(1)
		if err := v.Validate(input); err == nil {
			t.Errorf("%s: file_text 1 accepted", name)
		}
	}
}

// r72 prov CR-2: TS memory_20250818 view_range is z.tuple([z.number(),
// z.number()]) and insert_line z.number(), so non-integer numbers are valid.
func TestR72Integ_MemoryNumbersAreNumbers(t *testing.T) {
	v := schema.NewJSONSchema(Memory20250818().Parameters.(map[string]interface{}))
	if err := v.Validate(map[string]interface{}{"command": "view", "path": "/m", "view_range": []interface{}{1.5, float64(2)}}); err != nil {
		t.Errorf("view_range [1.5, 2] rejected: %v", err)
	}
	if err := v.Validate(map[string]interface{}{"command": "insert", "path": "/m", "insert_line": 2.5, "insert_text": "x"}); err != nil {
		t.Errorf("insert_line 2.5 rejected: %v", err)
	}
	if err := v.Validate(map[string]interface{}{"command": "view", "path": "/m", "view_range": []interface{}{"a", "b"}}); err == nil {
		t.Errorf("view_range strings accepted")
	}
}
