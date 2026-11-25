package providerutils

import (
	"math"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR97integResponseIssueInJSKeyOrder (cr-r97-carry-to-integrator): the
// response validators run zodlite.Validate, so a catchall issue names the
// first bad key on the wire, as zod does.
func TestR97integResponseIssueInJSKeyOrder(t *testing.T) {
	body := []byte(`{"id":"r","created":1,"model":"m","object":"chat.completion","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"z_x":1e400,"b_y":1e400}}`)
	err := ValidateChatResponse(body, ResponseKindMistral)
	if err == nil {
		t.Fatal("want a schema error")
	}
	msg := err.Error()
	z, b := strings.Index(msg, "z_x"), strings.Index(msg, "b_y")
	if z < 0 || (b >= 0 && b < z) {
		t.Fatalf("err = %s, want the usage.z_x issue first", msg)
	}
}

// TestR97integMediaSchemaParserPlain: MediaSchemaParserPlain is the TS
// `await response.json()` then schema.parse twin (AssemblyAI poll): a
// __proto__ key passes, where MediaSchemaParser (secureJsonParse) fails.
func TestR97integMediaSchemaParserPlain(t *testing.T) {
	body := []byte(`{"__proto__":{"a":1},"id":"t","status":"completed"}`)
	if err := MediaSchemaParserPlain(SchemaAssemblyAITranscript)(200, nil, body); err != nil {
		t.Fatalf("plain: %v", err)
	}
	if err := MediaSchemaParser(SchemaAssemblyAITranscript)(200, nil, body); err == nil {
		t.Fatal("secure parser accepted a __proto__ key")
	}
	// The schema still runs on the plain path.
	if err := MediaSchemaParserPlain(SchemaAssemblyAITranscript)(200, nil, []byte(`{"__proto__":{},"status":"completed"}`)); err == nil {
		t.Fatal("plain parser accepted a body without id")
	}
}

// TestR97integToolResultProviderMetadataNonFinite (cr-r97-stream-to-integrator
// item 1): a tool result's provider metadata holding +Inf keeps the object,
// with the value written as null (JSON.stringify); json.Marshal dropped it
// (the response message had no providerOptions).
func TestR97integToolResultProviderMetadataNonFinite(t *testing.T) {
	got := toolResultContentFromResult(types.ToolResult{ToolCallID: "c", ToolName: "t", Result: "ok",
		ProviderMetadata: map[string]interface{}{"p": map[string]interface{}{"k": "v", "x": math.Inf(1)}}})
	// The response message carries it as providerOptions.
	p, _ := got.ProviderOptions["p"].(map[string]interface{})
	if x, ok := p["x"]; p["k"] != "v" || !ok || x != nil {
		t.Fatalf("ProviderOptions = %#v, want {p: {k: v, x: null}}", got.ProviderOptions)
	}
}
