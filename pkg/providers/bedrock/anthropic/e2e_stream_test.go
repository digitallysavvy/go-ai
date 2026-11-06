package anthropic

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func bedrockChunk(t *testing.T, anthropicEvent string) []byte {
	payload := `{"bytes":"` + base64.StdEncoding.EncodeToString([]byte(anthropicEvent)) + `"}`
	return encodeEventStreamMessage(t, map[string]string{":message-type": "event", ":event-type": "chunk"}, []byte(payload))
}

// The Bedrock Anthropic transport turns the AWS event stream into data-only
// SSE (no `event:` line); the shared Anthropic stream must still decode it.
func TestEndToEndStreamThroughEventStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		for _, ev := range []string{
			`{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":3}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
			`{"type":"message_stop"}`,
		} {
			_, _ = w.Write(bedrockChunk(t, ev))
		}
		_, _ = w.Write(encodeEventStreamMessage(t, map[string]string{":message-type": "event", ":event-type": "messageStop"}, []byte(`{}`)))
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, BearerToken: "t", HTTPClient: srv.Client()})
	model, err := p.LanguageModel("anthropic.claude-sonnet-4-5-20250929-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, IncludeRawChunks: true})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	text := ""
	raws, finish := 0, false
	for {
		c, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch c.Type {
		case provider.ChunkTypeText:
			text += c.Text
		case provider.ChunkTypeRaw:
			raws++
		case provider.ChunkTypeFinish:
			finish = true
		}
	}
	if text != "Hello" || !finish {
		t.Fatalf("text=%q finish=%v raws=%d", text, finish, raws)
	}
	if raws != 6 {
		t.Fatalf("raws = %d, want 6", raws)
	}
}

// A modeled exception as the very first event fails DoStream with a retryable
// provider error carrying the Bedrock status code (TS doStream turns a first
// error part into an APICallError; getAmazonBedrockStreamErrorMetadata
// supplies the status/retryability).
func TestEndToEndStreamExceptionFirstEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		_, _ = w.Write(encodeEventStreamMessage(t, map[string]string{":message-type": "exception", ":exception-type": "throttlingException"}, []byte(`{"message":"slow down"}`)))
	}))
	defer srv.Close()
	p := New(Config{BaseURL: srv.URL, BearerToken: "t", HTTPClient: srv.Client()})
	model, _ := p.LanguageModel("anthropic.claude-sonnet-4-5-20250929-v1:0")
	_, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.StatusCode != 429 || !pe.IsRetryable() {
		t.Fatalf("err = %#v", err)
	}
}

// After content has started, an exception reaches the stream as an error
// chunk carrying the Bedrock status code and retryability.
func TestEndToEndStreamExceptionMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		_, _ = w.Write(bedrockChunk(t, `{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":3}}}`))
		_, _ = w.Write(encodeEventStreamMessage(t, map[string]string{":message-type": "exception", ":exception-type": "throttlingException"}, []byte(`{"message":"slow down"}`)))
	}))
	defer srv.Close()
	p := New(Config{BaseURL: srv.URL, BearerToken: "t", HTTPClient: srv.Client()})
	model, _ := p.LanguageModel("anthropic.claude-sonnet-4-5-20250929-v1:0")
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	for {
		c, err := stream.Next()
		if err == io.EOF {
			t.Fatal("no error chunk")
		}
		if err != nil {
			t.Fatal(err)
		}
		if c.Type == provider.ChunkTypeError {
			var spe *providererrors.StreamProviderError
			if !errors.As(c.Err, &spe) || spe.StatusCode == nil || *spe.StatusCode != 429 || !spe.IsRetryable {
				t.Fatalf("error = %#v", c.Err)
			}
			return
		}
	}
}
