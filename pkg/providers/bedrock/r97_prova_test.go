package bedrock

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock/eventstream"
)

// TS safeParseJSON: an event payload with a __proto__ key is an error part
// carrying a JSONParseError whose cause is the secureJsonParse SyntaxError
// (not a nested JSONParseError).
func TestR97provaStreamForbiddenPrototypePayload(t *testing.T) {
	body := buildBedrockEventStreamBody([][2]string{
		{"messageStop", `{"stopReason":"end_turn","__proto__":{}}`},
	})
	s := &bedrockConverseStream{
		decoder:       eventstream.NewDecoder(bytes.NewReader(body)),
		body:          io.NopCloser(bytes.NewReader(nil)),
		contentBlocks: map[int]*bedrockStreamContentBlock{},
		finishReason:  types.FinishReasonInterrupted,
	}
	var errPart *provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeError {
			errPart = c
		}
	}
	if errPart == nil {
		t.Fatal("no error part")
	}
	var parseErr *providererrors.JSONParseError
	if !errors.As(errPart.Err, &parseErr) || parseErr.Cause != jsonexact.ErrForbiddenPrototype {
		t.Fatalf("error = %T %v, want a JSONParseError caused by the forbidden prototype SyntaxError", errPart.Err, errPart.Err)
	}
	if strings.Count(errPart.Text, "JSON parsing failed") != 1 {
		t.Fatalf("text = %q, want one JSONParseError message", errPart.Text)
	}
}

// Ruling 4: the rerank request body is JSON.stringify'd, so a non-finite
// number in a JSON document is sent as null instead of failing the call.
func TestR97provaRerankNonFiniteDocument(t *testing.T) {
	var mu sync.Mutex
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		raw = string(b)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"index":0,"relevanceScore":0.5}]}`))
	}))
	defer srv.Close()
	m, err := signedProvider(srv.URL).RerankingModel("amazon.rerank-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.DoRerank(context.Background(), &provider.RerankOptions{
		Query:     "q",
		Documents: []map[string]interface{}{{"score": math.Inf(1), "name": "a"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(raw, `"jsonDocument":{"name":"a","score":null}`) {
		t.Fatalf("request body = %s, want the non-finite score as null", raw)
	}
}
