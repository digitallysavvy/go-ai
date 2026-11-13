package bedrock

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock/eventstream"
)

// TS postJsonToApi sets only Content-Type, so a Bedrock JSON request carries
// no Accept header of its own (fetch then adds `*/*`, the documented
// "Accept header" known difference: Go non-streaming requests send none).
func TestR98provaNoAcceptHeader(t *testing.T) {
	var mu sync.Mutex
	accepts := map[string][]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		accepts[r.URL.Path] = append(accepts[r.URL.Path], r.Header.Get("Accept"))
		mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"stop"}`))
	}))
	defer srv.Close()
	p := signedProvider(srv.URL)
	ctx := context.Background()

	lm, err := p.LanguageModel("amazon.nova-lite-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = lm.DoGenerate(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if s, err := lm.DoStream(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err == nil {
		_ = s.Close()
	}
	em, err := p.EmbeddingModel("amazon.titan-embed-text-v2:0")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = em.DoEmbed(ctx, "hi", nil)
	im, err := p.ImageModel("amazon.nova-canvas-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = im.DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "a cat"})
	rm, err := p.RerankingModel("amazon.rerank-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = rm.DoRerank(ctx, &provider.RerankOptions{Query: "q", Documents: []string{"a"}})

	mu.Lock()
	defer mu.Unlock()
	if len(accepts) < 4 {
		t.Fatalf("requests by path = %v, want converse, converse-stream, invoke and rerank", accepts)
	}
	for path, values := range accepts {
		for _, v := range values {
			if v != "" {
				t.Errorf("%s: Accept = %q, want none", path, v)
			}
		}
	}
}

// TS keys the payload by payloadType whenever the :event-type header is
// present (`payloadType == null` skips only a missing header), so an empty
// event type still runs safeParseJSON: an unparsable payload is an error part
// and the stream finishes with `error`. A missing header is skipped.
func TestR98provaStreamEmptyEventTypeHeader(t *testing.T) {
	frame := func(headers map[string]string, payload string) []byte {
		return buildBedrockEventFrame(headers, []byte(payload))
	}
	run := func(body []byte, raw bool) []*provider.StreamChunk {
		s := &bedrockConverseStream{
			decoder:          eventstream.NewDecoder(bytes.NewReader(body)),
			body:             io.NopCloser(bytes.NewReader(nil)),
			contentBlocks:    map[int]*bedrockStreamContentBlock{},
			finishReason:     types.FinishReasonInterrupted,
			includeRawChunks: raw,
		}
		var out []*provider.StreamChunk
		for {
			c, err := s.Next()
			if err != nil {
				break
			}
			out = append(out, c)
		}
		return out
	}

	chunks := run(frame(map[string]string{":message-type": "event", ":event-type": ""}, `not json`), false)
	var sawError bool
	var finish *provider.StreamChunk
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeError {
			sawError = true
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if !sawError || finish == nil || finish.FinishReason != types.FinishReasonError {
		t.Fatalf("empty event type: error part %v, finish %+v; want an error part and finish reason error", sawError, finish)
	}

	// A valid payload under an empty event type passes the schema (zod
	// strips the unknown key): only the raw chunk {"": payload}.
	chunks = run(frame(map[string]string{":message-type": "event", ":event-type": ""}, `{"a":1}`), true)
	var rawValue interface{}
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeRaw {
			rawValue = c.Raw
		}
	}
	m, _ := rawValue.(map[string]interface{})
	if _, ok := m[""]; !ok {
		t.Fatalf("raw chunk = %#v, want {\"\": payload}", rawValue)
	}

	// A missing :event-type header is skipped entirely.
	chunks = run(frame(map[string]string{":message-type": "event"}, `not json`), false)
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeError {
			t.Fatalf("missing event type produced an error part: %+v", c)
		}
	}
}
