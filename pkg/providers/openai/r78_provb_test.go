package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r78provbSSE serves the given SSE frames and then ends the body.
func r78provbSSE(t *testing.T, frames ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range frames {
			_, _ = w.Write([]byte("data: " + f + "\n\n"))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func r78provbDrain(t *testing.T, s provider.TextStream) (string, []*provider.StreamChunk) {
	t.Helper()
	defer s.Close()
	var text strings.Builder
	var finishes []*provider.StreamChunk
	var last *provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		last = c
		switch c.Type {
		case provider.ChunkTypeText:
			text.WriteString(c.Text)
		case provider.ChunkTypeFinish:
			finishes = append(finishes, c)
		case provider.ChunkTypeError:
			t.Fatalf("unexpected error part: %+v", c)
		}
	}
	if len(finishes) != 1 || last != finishes[0] {
		t.Fatalf("want exactly one finish part, last; got %d finishes, last %+v", len(finishes), last)
	}
	return text.String(), finishes
}

// TS parseJsonEventStream drops a `data: [DONE]` frame and keeps reading:
// frames after it still count and flush() (the finish part) runs at the end
// of the body (coordinator ruling r78 a).
func TestR78provbChatReadsPastDone(t *testing.T) {
	srv := r78provbSSE(t,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}`,
		`[DONE]`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":"stop"}]}`,
	)
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).ChatModel("gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	text, fin := r78provbDrain(t, s)
	if text != "Hello world" || fin[0].FinishReason != types.FinishReasonStop {
		t.Fatalf("text %q finish %q, want %q stop", text, fin[0].FinishReason, "Hello world")
	}
}

func TestR78provbCompletionReadsPastDone(t *testing.T) {
	srv := r78provbSSE(t,
		`{"id":"c1","object":"text_completion","created":1,"model":"gpt-3.5-turbo-instruct","choices":[{"index":0,"text":"Hello","finish_reason":null}]}`,
		`[DONE]`,
		`{"id":"c1","object":"text_completion","created":1,"model":"gpt-3.5-turbo-instruct","choices":[{"index":0,"text":" world","finish_reason":"stop"}]}`,
	)
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).CompletionModel("gpt-3.5-turbo-instruct")
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	text, fin := r78provbDrain(t, s)
	if text != "Hello world" || fin[0].FinishReason != types.FinishReasonStop {
		t.Fatalf("text %q finish %q, want %q stop", text, fin[0].FinishReason, "Hello world")
	}
}

func r78provbFinishMeta(t *testing.T, s provider.TextStream, key string) map[string]interface{} {
	t.Helper()
	defer s.Close()
	var meta map[string]map[string]interface{}
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeFinish {
			if err := json.Unmarshal(c.ProviderMetadata, &meta); err != nil {
				t.Fatal(err)
			}
		}
	}
	if meta == nil {
		t.Fatal("no finish part")
	}
	return meta[key]
}

// Ruling r78 b: TS reports serviceTier when `typeof service_tier ===
// 'string'` and reasoningContext when `reasoning.context != null`, so a
// present empty string is reported; null is not.
func TestR78provbResponsesEmptyServiceTierReported(t *testing.T) {
	cases := map[string]struct {
		body     string
		wantTier interface{}
		wantCtx  interface{}
	}{
		"empty": {`"service_tier":"","reasoning":{"context":""}`, "", ""},
		"null":  {`"service_tier":null,"reasoning":{"context":null}`, nil, nil},
	}
	for name, tc := range cases {
		t.Run("generate/"+name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"resp_1","object":"response","created_at":1,"status":"completed","model":"gpt-4o",` + tc.body + `,"output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hi","annotations":[]}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
			}))
			defer srv.Close()
			m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).ResponsesModel("gpt-4o")
			if err != nil {
				t.Fatal(err)
			}
			r, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
			if err != nil {
				t.Fatal(err)
			}
			pm, _ := r.ProviderMetadata["openai"].(map[string]interface{})
			r78provbCheckEchoes(t, pm, tc.wantTier, tc.wantCtx)
		})
		t.Run("stream/"+name, func(t *testing.T) {
			srv := r78provbSSE(t,
				`{"type":"response.created","sequence_number":0,"response":{"id":"resp_1","object":"response","created_at":1,"status":"in_progress","model":"gpt-4o","output":[]}}`,
				`{"type":"response.completed","sequence_number":1,"response":{"id":"resp_1","object":"response","created_at":1,"status":"completed","model":"gpt-4o",`+tc.body+`,"output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
			)
			m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).ResponsesModel("gpt-4o")
			if err != nil {
				t.Fatal(err)
			}
			s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
			if err != nil {
				t.Fatal(err)
			}
			r78provbCheckEchoes(t, r78provbFinishMeta(t, s, "openai"), tc.wantTier, tc.wantCtx)
		})
	}
}

func r78provbCheckEchoes(t *testing.T, pm map[string]interface{}, wantTier, wantCtx interface{}) {
	t.Helper()
	if pm == nil {
		t.Fatal("no openai provider metadata")
	}
	tier, hasTier := pm["serviceTier"]
	ctx, hasCtx := pm["reasoningContext"]
	if hasTier != (wantTier != nil) || (wantTier != nil && tier != wantTier) {
		t.Fatalf("serviceTier = %v (present %v), want %v", tier, hasTier, wantTier)
	}
	if hasCtx != (wantCtx != nil) || (wantCtx != nil && ctx != wantCtx) {
		t.Fatalf("reasoningContext = %v (present %v), want %v", ctx, hasCtx, wantCtx)
	}
}

// TS openai-image-model.ts: images[i] carries `created: response.created ??
// undefined` (and size/quality/background/outputFormat likewise), so a
// present 0 or "" is reported; absent ones are left out.
func TestR78provbImageMetadataPresentZeroValues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":0,"size":"","quality":"","background":"","output_format":"","data":[{"b64_json":"aGVsbG8="}]}`))
	}))
	defer srv.Close()
	m := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gpt-image-1")
	r, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	images := r.ProviderMetadata["openai"].(map[string]interface{})["images"].([]map[string]interface{})
	want := map[string]interface{}{"created": int64(0), "size": "", "quality": "", "background": "", "outputFormat": ""}
	for k, w := range want {
		if got, ok := images[0][k]; !ok || got != w {
			t.Fatalf("images[0][%q] = %#v (present %v), want %#v; %v", k, got, ok, w, images[0])
		}
	}
}

// TS openai-batch.ts convertOpenAIBatchResult: timestamp is `created_at !=
// null ? new Date(created_at * 1000)` (0 is the epoch) and serviceTier /
// reasoningContext follow typeof === 'string' / != null.
func TestR78provbBatchResultCreatedZeroAndEmptyEchoes(t *testing.T) {
	body := json.RawMessage(`{"id":"resp_1","object":"response","created_at":0,"status":"completed","model":"gpt-4o","service_tier":"","reasoning":{"context":""},"output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hi","annotations":[]}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
	r, berr := convertOpenAIBatchResponseBodyBytes(body)
	if berr != nil {
		t.Fatal(berr)
	}
	if r.ResponseMetadata == nil || r.ResponseMetadata.Timestamp.IsZero() || !r.ResponseMetadata.Timestamp.Equal(time.Unix(0, 0)) {
		t.Fatalf("response metadata = %+v, want the epoch timestamp", r.ResponseMetadata)
	}
	pm, _ := r.ProviderMetadata["openai"].(map[string]interface{})
	r78provbCheckEchoes(t, pm, "", "")
}
