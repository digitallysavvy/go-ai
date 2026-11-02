package perplexity

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS builds ids with String(number): getTextId's String(outputIndex),
// `reasoning-${sequence_number}` and a search result's String(result.id).
// JS prints 1e21 as "1e+21" and -0 as "0".
func TestR79prova_StreamIDsUseJSNumberText(t *testing.T) {
	body := `data: {"type":"response.reasoning.started","sequence_number":1e21,"thought":"t"}` + "\n\n" +
		`data: {"type":"response.reasoning.search_results","results":[{"id":-0,"title":"T","url":"https://a.example"}]}` + "\n\n" +
		`data: {"type":"response.output_text.delta","output_index":1e21,"content_index":-0,"delta":"x"}` + "\n\n"
	s := newPerplexityAgentStream(io.NopCloser(strings.NewReader(body)), nil, false, nil)
	defer func() { _ = s.Close() }()
	var got []string
	for i := 0; i < 100; i++ {
		c, err := s.Next()
		if err != nil {
			break
		}
		switch c.Type {
		case provider.ChunkTypeReasoningStart, provider.ChunkTypeTextStart:
			got = append(got, string(c.Type)+"="+c.ID)
		case provider.ChunkTypeSource:
			got = append(got, "source="+c.SourceContent.ID)
		case provider.ChunkTypeError:
			t.Fatalf("error part: %s", c.Text)
		}
	}
	want := "reasoning-start=reasoning-1e+21,source=0,text-start=1e+21"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
}
