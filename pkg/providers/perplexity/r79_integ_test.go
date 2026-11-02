package perplexity

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Survivor of the r79 mutation check (prova fix 6): stream ids are JS
// String(number), so 123456789012 stays in fixed notation and 1e-7 has no
// zero-padded exponent (Go's %v prints 1.23456789012e+11 and 1e-07).
func TestR79integStreamIDsJSNumberFormat(t *testing.T) {
	body := `data: {"type":"response.reasoning.started","sequence_number":1e-7,"thought":"t"}` + "\n\n" +
		`data: {"type":"response.output_text.delta","output_index":123456789012,"content_index":0,"delta":"x"}` + "\n\n"
	s := newPerplexityAgentStream(io.NopCloser(strings.NewReader(body)), nil, false, nil)
	defer func() { _ = s.Close() }()
	var got []string
	for i := 0; i < 100; i++ {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeReasoningStart || c.Type == provider.ChunkTypeTextStart {
			got = append(got, string(c.Type)+"="+c.ID)
		}
	}
	if want := "reasoning-start=reasoning-1e-7,text-start=123456789012"; strings.Join(got, ",") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
}
