package gateway

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR96provgV4ToolErrorTextIsJSONStringify: an isError tool result whose
// value is not a string is JSON.stringify of the value (TS), so HTML
// characters stay literal (cr-r96-stream-to-provg 1).
func TestR96provgV4ToolErrorTextIsJSONStringify(t *testing.T) {
	var p v4Part
	if err := json.Unmarshal([]byte(`{"type":"tool-result","toolCallId":"c","toolName":"t","isError":true,"result":{"message":"a<b&c>"}}`), &p); err != nil {
		t.Fatal(err)
	}
	const want = `{"message":"a<b&c>"}`
	if tr := p.toolResult(); tr.Error == nil || tr.Error.Error() != want {
		t.Errorf("stream tool error = %v, want %s", tr.Error, want)
	}
	part, _ := decodeV4Content(p)
	if tr, ok := part.(types.ToolResultContent); !ok || tr.Error != want {
		t.Errorf("generate tool error = %#v, want %s", part, want)
	}
}

// TestR96provgTranscriptionPartRejectsPrototypeKeys: TS
// parseTranscriptionStreamPart parses with secureJsonParse, which throws on a
// __proto__ key or a constructor.prototype, so the frame is ignored.
func TestR96provgTranscriptionPartRejectsPrototypeKeys(t *testing.T) {
	for _, frame := range []string{
		`{"type":"transcript-delta","delta":"x","__proto__":{}}`,
		`{"type":"transcript-delta","delta":"x","providerMetadata":{"constructor":{"prototype":{}}}}`,
	} {
		if part, ok := parseGatewayTranscriptionStreamPart(frame); ok {
			t.Errorf("%s: part = %+v, want ignored", frame, part)
		}
	}
	if part, ok := parseGatewayTranscriptionStreamPart(`{"type":"transcript-delta","delta":"x","constructor":{}}`); !ok || part.Delta != "x" {
		t.Errorf("constructor without prototype: ok=%v part=%+v, want the delta", ok, part)
	}
}
