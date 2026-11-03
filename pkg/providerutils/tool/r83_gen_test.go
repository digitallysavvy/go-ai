package tool

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// TS anthropic sanitize-json-schema.ts: schema.items != null, so a JSON null
// items is left out (prova CR r83 item 1).
func TestR83genSanitizeAnthropicNullItems(t *testing.T) {
	out, ok := SanitizeAnthropicSchema(map[string]interface{}{"type": "array", "items": nil}).(map[string]interface{})
	if !ok {
		t.Fatal("not a map")
	}
	if _, has := out["items"]; has {
		t.Fatalf("items kept: %v", out)
	}
}

type r83genToolArgs struct {
	Zeta  string `json:"zeta" validate:"required"`
	Alpha string `json:"alpha" validate:"required"`
}

// A struct schema passed straight to the converters keeps its field order
// (as through ResolveToolSchemas), and a typed nil *schema.OrderedSchema is no
// schema rather than a nil dereference.
func TestR83genConvertersStructOrderAndNilOrdered(t *testing.T) {
	st := schema.NewSimpleStructSchema(reflect.TypeOf(r83genToolArgs{}))
	b, _ := json.Marshal(ToJSONSchema(types.Tool{Name: "t", Parameters: st}))
	if !strings.Contains(string(b), `"properties":{"zeta":{"type":"string"},"alpha":{"type":"string"}}`) {
		t.Fatalf("ToJSONSchema order: %s", b)
	}
	b, _ = json.Marshal(SanitizeAnthropicSchema(st))
	if !strings.Contains(string(b), `"properties":{"zeta":{"type":"string"},"alpha":{"type":"string"}}`) {
		t.Fatalf("SanitizeAnthropicSchema order: %s", b)
	}
	var nilOrdered *schema.OrderedSchema
	b, _ = json.Marshal(ToJSONSchema(types.Tool{Name: "t", Parameters: nilOrdered}))
	if !strings.Contains(string(b), `"parameters":{"properties":{},"type":"object"}`) {
		t.Fatalf("nil ordered parameters: %s", b)
	}
	if got := SanitizeAnthropicSchema(nilOrdered); got != nil {
		t.Fatalf("nil ordered sanitize: %v", got)
	}
}
