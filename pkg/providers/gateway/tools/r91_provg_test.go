package tools

import "testing"

// TS 838dd53c25: export_pricing.free_rows is declared again as
// z.optional(z.number()): a number passes, a string fails.
func TestR91provgTakoFreeRowsNumber(t *testing.T) {
	set := func(v interface{}) func(map[string]interface{}) {
		return func(m map[string]interface{}) {
			cards := m["cards"].([]interface{})
			cards[0].(map[string]interface{})["content"].(map[string]interface{})["export_pricing"].(map[string]interface{})["free_rows"] = v
		}
	}
	if err := r90provgTakoValidate(t, set(0)); err != nil {
		t.Fatalf("free_rows 0 rejected: %v", err)
	}
	if err := r90provgTakoValidate(t, set("x")); err == nil {
		t.Fatal("free_rows string accepted")
	}
}
