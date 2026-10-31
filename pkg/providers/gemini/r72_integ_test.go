package gemini

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// r72 prov CR-4: TS google codeExecutionInputSchema {language: string,
// code: string}, both required.
func TestR72Integ_CodeExecutionInputSchema(t *testing.T) {
	v := schema.NewJSONSchema(CodeExecutionTool().Parameters.(map[string]interface{}))
	if err := v.Validate(map[string]interface{}{"language": "PYTHON", "code": "print(1)"}); err != nil {
		t.Errorf("valid call rejected: %v", err)
	}
	if err := v.Validate(map[string]interface{}{"language": "PYTHON"}); err == nil {
		t.Errorf("call without code accepted")
	}
}
