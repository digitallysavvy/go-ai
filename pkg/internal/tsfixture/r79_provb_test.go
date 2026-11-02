package tsfixture

import "testing"

// Call-level providerOptions and headers drop TS `undefined` values like the
// prompt and tool provider options do (TS option schemas read an undefined
// property as absent; combineHeaders drops undefined headers).
func TestR79provbBuildOptionsDropsUndefined(t *testing.T) {
	undef := map[string]interface{}{"__undef": true}
	o, why := BuildOptions(map[string]interface{}{
		"prompt":          []interface{}{},
		"providerOptions": map[string]interface{}{"openai": map[string]interface{}{"a": undef, "b": 1.0}},
		"headers":         map[string]interface{}{"x-a": undef, "x-b": "v"},
	})
	if why != "" {
		t.Fatalf("unsupported: %s", why)
	}
	po, _ := o.ProviderOptions["openai"].(map[string]interface{})
	if _, ok := po["a"]; ok || po["b"] != 1.0 {
		t.Fatalf("providerOptions = %#v", o.ProviderOptions)
	}
	if _, ok := o.Headers["x-a"]; ok || o.Headers["x-b"] != "v" {
		t.Fatalf("headers = %#v", o.Headers)
	}
}
