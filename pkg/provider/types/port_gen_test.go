package types

import (
	"encoding/json"
	"strings"
	"testing"
)

// TS d834c5bb35: the mediaType deprecation warning of a legacy "file-url"
// tool result part no longer names the URL. Warnings reach step results and
// telemetry, and a URL can be a signed private link.
func TestPortgenFileURLWarningLeavesTheURLOut(t *testing.T) {
	const secret = "signature=secret-signature"
	var out ToolResultOutput
	input := `{"type":"content","value":[` +
		`{"type":"file-url","url":"https://files.example.com/report.pdf?` + secret + `"},` +
		`{"type":"file-url","url":"https://files.example.com/noext?` + secret + `"}]}`
	if err := json.Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	wants := []string{
		`A "file-url" tool result content part is missing a "mediaType". Inferred media type 'application/pdf' from URL. `,
		`A "file-url" tool result content part is missing a "mediaType". Unable to infer media type from URL. Defaulting to 'application/octet-stream'. `,
	}
	for i, want := range wants {
		warning, ok := out.Content[i].(FileContentBlock).DeprecationWarning()
		if !ok {
			t.Fatalf("block %d has no warning", i)
		}
		if !strings.HasPrefix(warning.Message, want) || strings.Contains(warning.Message, "secret-signature") || strings.Contains(warning.Message, "files.example.com") {
			t.Errorf("block %d warning = %q", i, warning.Message)
		}
	}
}
