package codemode

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestToolInputValidator_HintedPointerForm(t *testing.T) {
	m := map[string]interface{}{"type": "object", "required": []interface{}{"a"}}
	for name, p := range map[string]interface{}{
		"value":   jsonorder.Hinted{V: m},
		"pointer": &jsonorder.Hinted{V: m},
	} {
		if toolInputValidator(types.Tool{Name: "t", Parameters: p}) == nil {
			t.Errorf("%s: validator is nil", name)
		}
	}
}
