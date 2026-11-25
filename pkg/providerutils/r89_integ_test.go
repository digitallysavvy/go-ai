package providerutils

import (
	"strings"
	"testing"
)

type r89integTypedOptions struct {
	Dimensions *int   `json:"dimensions,omitempty"`
	User       string `json:"user,omitempty"`
}

// A provider's exported options struct given as the providerOptions entry is
// read like the TS options object (its json tags are the TS keys); before,
// every ParseProviderOptions caller rejected it with "expected object"
// (provg r89 integrator item).
func TestR89integParseProviderOptionsAcceptsTypedStruct(t *testing.T) {
	schema := OptionSchema{"dimensions": OptNumber, "user": OptString}
	dims := 256
	for name, v := range map[string]interface{}{
		"struct":  r89integTypedOptions{Dimensions: &dims, User: "u"},
		"pointer": &r89integTypedOptions{Dimensions: &dims, User: "u"},
	} {
		obj, err := ParseProviderOptions("acme", map[string]interface{}{"acme": v}, schema)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if obj["dimensions"] != float64(256) || obj["user"] != "u" {
			t.Fatalf("%s: obj = %v", name, obj)
		}
	}
	// Validation still applies to the struct's values.
	type bad struct {
		Dimensions string `json:"dimensions"`
	}
	_, err := ParseProviderOptions("acme", map[string]interface{}{"acme": bad{Dimensions: "x"}}, schema)
	if err == nil || !strings.Contains(err.Error(), "invalid acme provider options") {
		t.Fatalf("err = %v", err)
	}
	// Non-object values are still rejected.
	if _, err := ParseProviderOptions("acme", map[string]interface{}{"acme": 3}, schema); err == nil {
		t.Fatal("number accepted as options object")
	}
}
