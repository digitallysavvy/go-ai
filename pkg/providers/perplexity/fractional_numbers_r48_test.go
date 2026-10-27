package perplexity

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS zod number() accepts fractional counts (12.5, 12.0); the Go decoder must
// not reject them. Counts truncate to the integral Go usage type.
func TestR48_FractionalUsageCountsAreAccepted(t *testing.T) {
	body := `{"id":"r","created_at":1,"model":"m","object":"response","status":"completed","output":[],` +
		`"usage":{"input_tokens":12.5,"output_tokens":12.0,"total_tokens":24.5,` +
		`"input_tokens_details":{"cached_tokens":2.5},"output_tokens_details":{"reasoning_tokens":3.5},` +
		`"tool_calls_details":{"web_search":{"invocation":2.5}}}}`
	_, model := newPerplexityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	})
	res, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err != nil {
		t.Fatal(err)
	}
	u := res.Usage
	if *u.InputTokens != 12 || *u.OutputTokens != 12 || *u.InputDetails.CacheReadTokens != 2 || *u.OutputDetails.ReasoningTokens != 3 {
		t.Fatalf("usage = %+v", u)
	}
	meta := res.ProviderMetadata["perplexity"]
	if meta == nil {
		t.Fatalf("metadata = %#v", res.ProviderMetadata)
	}
}

func TestR48_FractionalStreamIndexesAndUsage(t *testing.T) {
	body := `data: {"type":"response.reasoning.started","sequence_number":12.5}` + "\n\n" +
		`data: {"type":"response.output_text.delta","item_id":null,"output_index":1.5,"content_index":2.0,"delta":"hi"}` + "\n\n" +
		`data: {"type":"response.completed","response":{"id":"r","created_at":1,"model":"m","object":"response","status":"completed","output":[],"usage":{"input_tokens":12.5,"output_tokens":12.0,"total_tokens":24.5}}}` + "\n\n"
	s := newPerplexityAgentStream(io.NopCloser(strings.NewReader(body)), nil, false, nil)
	var ids []string
	var finish *provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeError {
			t.Fatalf("error chunk: %v", c.Err)
		}
		if c.ID != "" {
			ids = append(ids, c.ID)
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	joined := strings.Join(ids, ",")
	if !strings.Contains(joined, "reasoning-12.5") || !strings.Contains(joined, "1.5:2") {
		t.Fatalf("ids = %s", joined)
	}
	if finish == nil || finish.Usage == nil || *finish.Usage.InputTokens != 12 || *finish.Usage.OutputTokens != 12 {
		t.Fatalf("finish = %+v", finish)
	}
}

func TestR48_DecisionFractionalUsage(t *testing.T) {
	for _, in := range []interface{}{12.5, 12.0} {
		fixture := fixtureBody(t)
		fixture["usage"] = map[string]interface{}{"input_tokens": in, "output_tokens": in}
		s := newDecisionServer(t, fixture, 200)
		result, err := s.model(t, "pplx-decider-latest").DoDecide(t.Context(), decisionOptions())
		if err != nil {
			t.Fatalf("%v: %v", in, err)
		}
		if *result.Usage.InputTokens != 12 || *result.Usage.OutputTokens != 12 {
			t.Fatalf("%v: usage = %+v", in, result.Usage)
		}
	}
}
