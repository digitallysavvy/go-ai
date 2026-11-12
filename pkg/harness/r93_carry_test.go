package harness

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r93carryDecodeOutput(t *testing.T, raw string) types.ToolResultOutput {
	t.Helper()
	var out types.ToolResultOutput
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TS normalize-harness-tool-model-output.test.ts "normalizes legacy
// image-data / file-data" and "rejects unsupported image-url /
// image-file-id": a legacy inline part becomes a file part with the detected
// media type; any other legacy part fails, named by its own type with no
// media type / data type (`Unsupported content '${part.type}'.`). A
// non-image legacy inline part reports its own type too.
func TestR93carryNormalizeLegacyToolModelOutputParts(t *testing.T) {
	const png = "iVBORw0KGgo="
	for _, typ := range []string{"image-data", "file-data"} {
		out, err := NormalizeToolModelOutput(r93carryDecodeOutput(t, `{"type":"content","value":[{"type":"`+typ+`","mediaType":"image/*","data":"`+png+`"}]}`))
		if err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		b, _ := json.Marshal(out)
		if !strings.Contains(string(b), `"type":"file"`) || !strings.Contains(string(b), `"mediaType":"image/png"`) {
			t.Fatalf("%s normalized = %s", typ, b)
		}
	}
	for raw, want := range map[string]string{
		`{"type":"image-url","url":"https://example.com/image.png"}`:                                      "Unsupported content 'image-url'.",
		`{"type":"image-file-id","fileId":"image-id"}`:                                                    "Unsupported content 'image-file-id'.",
		`{"type":"file-data","mediaType":"application/pdf","data":"` + png + `"}`:                         "Unsupported content 'file-data' (application/pdf, data).",
		`{"type":"file","mediaType":"image/png","data":{"type":"url","url":"https://example.com/i.png"}}`: "Unsupported content 'file' (image/png, url).",
	} {
		_, err := NormalizeToolModelOutput(r93carryDecodeOutput(t, `{"type":"content","value":[`+raw+`]}`))
		if err == nil || !IsCapabilityUnsupportedError(err) || !strings.Contains(err.Error(), "tool model output. "+want) {
			t.Errorf("%s: err = %v, want %q", raw, err, want)
		}
	}
}

// TS convertHarnessToolModelOutput accepts only a normalized `file` part; a
// legacy part is reported by its own type.
func TestR93carryConvertLegacyToolModelOutputPart(t *testing.T) {
	_, _, err := ConvertToolModelOutput(r93carryDecodeOutput(t, `{"type":"content","value":[{"type":"image-data","mediaType":"image/png","data":"iVBORw0KGgo="}]}`))
	if err == nil || !strings.Contains(err.Error(), "unsupported content 'image-data'.") {
		t.Fatalf("err = %v", err)
	}
}
