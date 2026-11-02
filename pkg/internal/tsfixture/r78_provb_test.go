package tsfixture

import (
	"reflect"
	"testing"
)

// A TS literal's `key: undefined` in providerOptions is an absent key (zod
// optional): BuildPromptMessages drops the extractor's undefined marker at
// any depth.
func TestR78provbPromptProviderOptionsDropUndefined(t *testing.T) {
	undef := map[string]interface{}{"__undef": true}
	msgs, why := BuildPromptMessages([]interface{}{map[string]interface{}{
		"role":    "system",
		"content": "",
		"providerOptions": map[string]interface{}{"moonshotai": map[string]interface{}{
			"name":  undef,
			"tools": []interface{}{map[string]interface{}{"type": "function", "name": "f", "inputSchema": undef}},
		}},
	}})
	if why != "" {
		t.Fatal(why)
	}
	want := map[string]interface{}{"moonshotai": map[string]interface{}{
		"tools": []interface{}{map[string]interface{}{"type": "function", "name": "f"}},
	}}
	if !reflect.DeepEqual(msgs[0].ProviderOptions, want) {
		t.Fatalf("providerOptions = %#v, want %#v", msgs[0].ProviderOptions, want)
	}
}

// `.at(n)` reads Array.prototype.at, counting a negative n from the end.
func TestR78provbEvalPathAt(t *testing.T) {
	roots := map[string]interface{}{"result": map[string]interface{}{"messages": []interface{}{"a", "b", "c"}}}
	for expr, want := range map[string]interface{}{
		"result.messages.at(-1)": "c",
		"result.messages.at(0)":  "a",
		"result.messages.at(5)":  nil,
		"result.messages.at(-4)": nil,
	} {
		got, ok := evalPath(roots, nil, expr)
		if !ok || got != want {
			t.Errorf("%s = %v (ok %v), want %v", expr, got, ok, want)
		}
	}
}
