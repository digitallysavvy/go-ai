package tool

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// r72 prov CR-4: TS codeInterpreterInputSchema {code: z.string().nullish(),
// containerId: z.string()}.
func TestR72Integ_CodeInterpreterInputSchema(t *testing.T) {
	v := schema.NewJSONSchema(CodeInterpreter(CodeInterpreterConfig{}).Parameters.(map[string]interface{}))
	for _, ok := range []map[string]interface{}{
		{"containerId": "c1", "code": "print(1)"},
		{"containerId": "c1", "code": nil},
		{"containerId": "c1"},
	} {
		if err := v.Validate(ok); err != nil {
			t.Errorf("%v rejected: %v", ok, err)
		}
	}
	for _, bad := range []map[string]interface{}{
		{"code": "print(1)"},
		{"containerId": float64(1)},
	} {
		if err := v.Validate(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}
