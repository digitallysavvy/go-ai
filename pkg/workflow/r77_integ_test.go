package workflow

import "testing"

// Round 77 integrator survivor test (carry fix 1): a hook result that leaves
// CallOptions and CustomData nil keeps the current values, like Tools and
// ExperimentalSandbox (TS prepareCall/prepareStep apply only defined keys).
func TestR77Integ_KeepUnsetHookFieldsCallOptionsAndCustomData(t *testing.T) {
	in := LanguageModelCallOptions{CallOptions: map[string]interface{}{"tier": "pro"}, CustomData: "state-1"}
	out := keepUnsetHookFields(in, LanguageModelCallOptions{})
	if m, ok := out.CallOptions.(map[string]interface{}); !ok || m["tier"] != "pro" {
		t.Errorf("CallOptions = %#v, want the current call options", out.CallOptions)
	}
	if out.CustomData != "state-1" {
		t.Errorf("CustomData = %#v, want the current custom data", out.CustomData)
	}
	out = keepUnsetHookFields(in, LanguageModelCallOptions{CallOptions: "new", CustomData: "state-2"})
	if out.CallOptions != "new" || out.CustomData != "state-2" {
		t.Errorf("set fields = %#v / %#v, want the hook's values", out.CallOptions, out.CustomData)
	}
}
