package telemetry

import (
	"reflect"
	"testing"
)

// TS 797dc0254e: text parts lose their provider options in the telemetry
// copy as file parts do; a json part (whose value merely contains a
// providerOptions key) is kept as is; the caller's state is untouched.
func TestPort2carryExtractStateStripsTextPartProviderOptions(t *testing.T) {
	opts := map[string]interface{}{"user": map[string]interface{}{"value": "private"}}
	jsonPart := portcarrytelStatePart{Type: "json", Text: "", ProviderOptions: nil}
	in := []portcarrytelStatePart{
		{Type: "text", Text: "Invoice", ProviderOptions: opts},
		{Type: "text", Text: "plain"},
		jsonPart,
	}
	out := redactFileLocations(in).([]portcarrytelStatePart)
	if out[0].ProviderOptions != nil || out[0].Text != "Invoice" {
		t.Errorf("text part = %+v, want options dropped and text kept", out[0])
	}
	if !reflect.DeepEqual(out[1], in[1]) || !reflect.DeepEqual(out[2], in[2]) {
		t.Errorf("parts without options changed: %+v", out)
	}
	if !reflect.DeepEqual(in[0].ProviderOptions, opts) {
		t.Errorf("the caller's state was modified")
	}
	// Nothing to redact: the very same slice.
	plain := []portcarrytelStatePart{{Type: "text", Text: "x"}}
	if got := redactFileLocations(plain).([]portcarrytelStatePart); &got[0] != &plain[0] {
		t.Errorf("a state without options was copied")
	}
}
