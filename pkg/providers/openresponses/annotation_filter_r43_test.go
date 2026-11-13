package openresponses

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func openResponsesAnnotationsFrom(t *testing.T, meta json.RawMessage) []interface{} {
	t.Helper()
	var payload map[string]map[string]interface{}
	if err := json.Unmarshal(meta, &payload); err != nil {
		t.Fatal(err)
	}
	annotations, _ := payload["open-responses"]["annotations"].([]interface{})
	return annotations
}

// TS getOutputTextAnnotations keeps a content part's annotations only when
// every one is a well-formed url_citation; otherwise the part carries none.
func TestOutputTextAnnotationsAreAllOrNothing(t *testing.T) {
	good := `{"type":"url_citation","start_index":1,"end_index":4,"url":"https://a.test","title":"A"}`
	cases := []struct {
		name        string
		annotations string
		want        int
	}{
		{"valid", `[` + good + `]`, 1},
		{"other annotation type drops the part", `[` + good + `,{"type":"file_citation","file_id":"f","filename":"x"}]`, 0},
		{"missing title drops the part", `[{"type":"url_citation","start_index":0,"end_index":2,"url":"https://a.test"}]`, 0},
		{"missing start_index drops the part", `[{"type":"url_citation","end_index":2,"url":"https://a.test","title":"t"}]`, 0},
		{"zero offsets are valid", `[{"type":"url_citation","start_index":0,"end_index":0,"url":"https://a.test","title":""}]`, 1},
	}
	for _, tc := range cases {
		item := `{"type":"message","id":"msg_1","content":[{"type":"output_text","text":"hi","annotations":` + tc.annotations + `}]}`

		var generated OpenResponsesResponse
		if err := json.Unmarshal([]byte(`{"id":"r","output":[`+item+`]}`), &generated); err != nil {
			t.Fatal(err)
		}
		result, err := (&LanguageModel{}).convertResponse(generated)
		if err != nil {
			t.Fatal(err)
		}
		text := result.Content[0].(types.TextContent)
		if got := len(openResponsesAnnotationsFrom(t, text.ProviderMetadata)); got != tc.want {
			t.Errorf("%s: generate annotations = %d, want %d", tc.name, got, tc.want)
		}

		sse := `data: {"type":"response.output_item.done","output_index":0,"item":` + item + `}` + "\n\n"
		s := newOpenResponsesStream(io.NopCloser(strings.NewReader(sse)), nil)
		for {
			chunk, err := s.Next()
			if err != nil {
				t.Fatalf("%s: no text-end chunk", tc.name)
			}
			if chunk.Type == provider.ChunkTypeTextEnd {
				if got := len(openResponsesAnnotationsFrom(t, chunk.ProviderMetadata)); got != tc.want {
					t.Errorf("%s: stream annotations = %d, want %d", tc.name, got, tc.want)
				}
				break
			}
		}
	}
}
