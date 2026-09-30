package bedrock

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestBedrockToolResultContent_ErrorJSONIsJSONEncoded is a regression test
// for a bug found during parity review: error-json tool results were grouped
// with the raw-text ('text'/'error-text') branch instead of the JSON-encoded
// ('json'/'error-json') branch. Ports TS convertToolResultOutput's switch
// (convert-to-amazon-bedrock-chat-messages.ts:682-690), which groups
// 'text'/'error-text' for raw value passthrough and 'json'/'error-json'/
// default for JSON.stringify(output.value).
func TestBedrockToolResultContent_ErrorJSONIsJSONEncoded(t *testing.T) {
	part := types.ToolResultContent{
		ToolCallID: "call_1",
		ToolName:   "lookup",
		Output: &types.ToolResultOutput{
			Type:  types.ToolResultOutputErrorJSON,
			Value: map[string]interface{}{"code": float64(500), "message": "boom"},
		},
	}
	got, err := bedrockToolResultContent(part, func(string) string { return "document" })
	if err != nil {
		t.Fatalf("bedrockToolResultContent error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d content blocks, want 1: %#v", len(got), got)
	}
	text, ok := got[0]["text"].(string)
	if !ok {
		t.Fatalf("content block = %#v, want a text block", got[0])
	}
	// Must be valid JSON (order of keys may vary), not Go's fmt.Sprint map
	// stringification (e.g. "map[code:500 message:boom]").
	if text != `{"code":500,"message":"boom"}` && text != `{"message":"boom","code":500}` {
		t.Fatalf("text = %q, want JSON-encoded value", text)
	}
}

// TestBedrockToolResultContent_TextAndErrorTextArePassthrough verifies the
// 'text'/'error-text' branch still returns the raw value uninterpreted (no
// JSON encoding), per the same TS switch.
func TestBedrockToolResultContent_TextAndErrorTextArePassthrough(t *testing.T) {
	for _, outputType := range []types.ToolResultOutputType{types.ToolResultOutputText, types.ToolResultOutputErrorText} {
		part := types.ToolResultContent{
			Output: &types.ToolResultOutput{Type: outputType, Value: "plain text, not JSON"},
		}
		got, err := bedrockToolResultContent(part, func(string) string { return "document" })
		if err != nil {
			t.Fatalf("bedrockToolResultContent(%v) error = %v", outputType, err)
		}
		if len(got) != 1 || got[0]["text"] != "plain text, not JSON" {
			t.Fatalf("bedrockToolResultContent(%v) = %#v, want raw text passthrough", outputType, got)
		}
	}
}

// TestBedrockToolResultContent_JSONIsJSONEncoded verifies the plain 'json'
// case is still JSON-encoded (unaffected by the error-json fix).
func TestBedrockToolResultContent_JSONIsJSONEncoded(t *testing.T) {
	part := types.ToolResultContent{
		Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: []interface{}{float64(1), float64(2)}},
	}
	got, err := bedrockToolResultContent(part, func(string) string { return "document" })
	if err != nil {
		t.Fatalf("bedrockToolResultContent error = %v", err)
	}
	if len(got) != 1 || got[0]["text"] != "[1,2]" {
		t.Fatalf("bedrockToolResultContent = %#v, want JSON-encoded array", got)
	}
}

// TestSanitizeToolName ports TS sanitizeToolName test cases
// (normalize-tool-call-id.ts's sibling sanitizeToolName in
// convert-to-amazon-bedrock-chat-messages.ts).
func TestSanitizeToolName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"$READFILE", "READFILE"},
		{"valid_tool-Name123", "valid_tool-Name123"},
		{"$", "_"},
		{"", "_"},
	}
	for _, tt := range tests {
		if got := sanitizeToolName(tt.name); got != tt.want {
			t.Errorf("sanitizeToolName(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestToBedrockToolInput_WrapsInvalidInput ports TS toBedrockToolInput's
// "wrap non-object (invalid) tool call input" test: a JSON value that is not
// an object (or unparseable) is wrapped as {"rawInvalidInput": ...} instead
// of silently becoming {}.
func TestToBedrockToolInput_WrapsInvalidInput(t *testing.T) {
	// Malformed JSON string: wrapped as the raw string.
	got := toBedrockToolInput(`{not valid json`, nil)
	if got["rawInvalidInput"] != `{not valid json` {
		t.Fatalf("got = %#v, want rawInvalidInput to hold the raw string", got)
	}

	// Valid JSON but not an object (an array): wrapped as the decoded value.
	got = toBedrockToolInput(`[1,2,3]`, nil)
	arr, ok := got["rawInvalidInput"].([]interface{})
	if !ok || len(arr) != 3 {
		t.Fatalf("got = %#v, want rawInvalidInput to hold the decoded array", got)
	}

	// A valid JSON object (as a raw string) is used as-is (no wrapping).
	got = toBedrockToolInput(`{"a":1}`, nil)
	if _, wrapped := got["rawInvalidInput"]; wrapped {
		t.Fatalf("got = %#v, valid object input should not be wrapped", got)
	}
	if got["a"] != float64(1) {
		t.Fatalf("got = %#v, want a=1", got)
	}

	// A pre-parsed arguments map always wins over the raw input string.
	got = toBedrockToolInput(`{not valid json`, map[string]interface{}{"a": 1})
	if got["a"] != 1 {
		t.Fatalf("got = %#v, want the pre-parsed arguments map used as-is", got)
	}
}

// TestSanitizeDocumentName ports TS sanitizeDocumentName test cases
// (convert-to-amazon-bedrock-chat-messages.ts:77-84).
func TestSanitizeDocumentName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"report.pdf", "report"},
		{"my\tfile.txt", "my file"},
		{"$$$.txt", ""},
	}
	for _, tt := range tests {
		if got := sanitizeDocumentName(tt.name); got != tt.want {
			t.Errorf("sanitizeDocumentName(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}
