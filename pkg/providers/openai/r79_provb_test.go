package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r79provbFrac is a fractional unix-seconds `created` and its TS timestamp,
// new Date(created * 1000).
const r79provbFrac = `1700000000.25`

var r79provbFracTime = time.UnixMilli(1700000000250)

func r79provbJSONServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func r79provbStreamMeta(t *testing.T, s provider.TextStream) *provider.ResponseMetadata {
	t.Helper()
	defer func() { _ = s.Close() }()
	var meta *provider.ResponseMetadata
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeError {
			t.Fatalf("unexpected error part: %+v", c)
		}
		if c.Type == provider.ChunkTypeResponseMetadata && c.ResponseMetadata != nil && meta == nil {
			meta = c.ResponseMetadata
		}
	}
	if meta == nil {
		t.Fatal("no response-metadata part")
	}
	return meta
}

func r79provbCheckTime(t *testing.T, got time.Time) {
	t.Helper()
	if !got.Equal(r79provbFracTime) {
		t.Fatalf("timestamp = %v, want %v", got, r79provbFracTime)
	}
}

// TS openai chat/completion schemas declare created: z.number().nullish(), so
// a fractional created is accepted and the timestamp is new Date(created *
// 1000); Go decoded an int64 and failed the whole call.
func TestR79provbChatCompletionFractionalCreated(t *testing.T) {
	t.Run("chat/generate", func(t *testing.T) {
		srv := r79provbJSONServer(t, `{"id":"c1","object":"chat.completion","created":`+r79provbFrac+`,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
		m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).ChatModel("gpt-4o")
		if err != nil {
			t.Fatal(err)
		}
		r, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
		if err != nil {
			t.Fatal(err)
		}
		r79provbCheckTime(t, r.ResponseMetadata.Timestamp)
	})
	t.Run("chat/stream", func(t *testing.T) {
		srv := r78provbSSE(t,
			`{"id":"c1","object":"chat.completion.chunk","created":`+r79provbFrac+`,"model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`,
			"[DONE]")
		m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).ChatModel("gpt-4o")
		if err != nil {
			t.Fatal(err)
		}
		s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
		if err != nil {
			t.Fatal(err)
		}
		r79provbCheckTime(t, r79provbStreamMeta(t, s).Timestamp)
	})
	t.Run("completion/generate", func(t *testing.T) {
		srv := r79provbJSONServer(t, `{"id":"c1","object":"text_completion","created":`+r79provbFrac+`,"model":"gpt-3.5-turbo-instruct","choices":[{"index":0,"text":"hi","finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
		m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).CompletionModel("gpt-3.5-turbo-instruct")
		if err != nil {
			t.Fatal(err)
		}
		r, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
		if err != nil {
			t.Fatal(err)
		}
		r79provbCheckTime(t, r.ResponseMetadata.Timestamp)
	})
	t.Run("completion/stream", func(t *testing.T) {
		srv := r78provbSSE(t,
			`{"id":"c1","object":"text_completion","created":`+r79provbFrac+`,"model":"gpt-3.5-turbo-instruct","choices":[{"index":0,"text":"hi","finish_reason":"stop"}]}`,
			"[DONE]")
		m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).CompletionModel("gpt-3.5-turbo-instruct")
		if err != nil {
			t.Fatal(err)
		}
		s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
		if err != nil {
			t.Fatal(err)
		}
		r79provbCheckTime(t, r79provbStreamMeta(t, s).Timestamp)
	})
}

// TS openai-responses-api.ts: created_at is z.number() on the response and
// on response.created, and the batch result reads the same schema.
func TestR79provbResponsesFractionalCreatedAt(t *testing.T) {
	body := `{"id":"resp_1","object":"response","created_at":` + r79provbFrac + `,"status":"completed","model":"gpt-4o","output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hi","annotations":[]}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
	t.Run("generate", func(t *testing.T) {
		srv := r79provbJSONServer(t, body)
		m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).ResponsesModel("gpt-4o")
		if err != nil {
			t.Fatal(err)
		}
		r, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
		if err != nil {
			t.Fatal(err)
		}
		r79provbCheckTime(t, r.ResponseMetadata.Timestamp)
	})
	t.Run("stream", func(t *testing.T) {
		srv := r78provbSSE(t,
			`{"type":"response.created","sequence_number":0,"response":{"id":"resp_1","object":"response","created_at":`+r79provbFrac+`,"status":"in_progress","model":"gpt-4o","output":[]}}`,
			`{"type":"response.completed","sequence_number":1,"response":{"id":"resp_1","object":"response","created_at":`+r79provbFrac+`,"status":"completed","model":"gpt-4o","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
		)
		m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).ResponsesModel("gpt-4o")
		if err != nil {
			t.Fatal(err)
		}
		s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
		if err != nil {
			t.Fatal(err)
		}
		meta := r79provbStreamMeta(t, s)
		if meta.ID != "resp_1" || meta.ModelID != "gpt-4o" {
			t.Fatalf("metadata = %+v", meta)
		}
		r79provbCheckTime(t, meta.Timestamp)
	})
	t.Run("batch", func(t *testing.T) {
		r, berr := convertOpenAIBatchResponseBodyBytes(json.RawMessage(body))
		if berr != nil {
			t.Fatal(berr)
		}
		r79provbCheckTime(t, r.ResponseMetadata.Timestamp)
	})
}

// TS image providerMetadata created: response.created ?? undefined (a JS
// number); TS files/batch convert created_at / expires_at with new
// Date(x * 1000). Integral values keep the v0.5 int64 metadata type.
func TestR79provbImageFilesBatchFractionalTimes(t *testing.T) {
	t.Run("image", func(t *testing.T) {
		for _, tc := range []struct {
			created string
			want    interface{}
		}{{"1.5", 1.5}, {"7", int64(7)}} {
			srv := r79provbJSONServer(t, `{"created":`+tc.created+`,"data":[{"b64_json":"aGVsbG8="}]}`)
			m := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gpt-image-1")
			r, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
			if err != nil {
				t.Fatal(err)
			}
			images := r.ProviderMetadata["openai"].(map[string]interface{})["images"].([]map[string]interface{})
			if got := images[0]["created"]; got != tc.want {
				t.Fatalf("created = %#v, want %#v", got, tc.want)
			}
		}
	})
	t.Run("files", func(t *testing.T) {
		var r openAIFilesResponse
		if err := json.Unmarshal([]byte(`{"id":"f","created_at":1700000000.25,"expires_at":1700000100}`), &r); err != nil {
			t.Fatal(err)
		}
		meta := r.toMetadata()
		if meta["createdAt"] != 1700000000.25 || meta["expiresAt"] != int64(1700000100) {
			t.Fatalf("meta = %#v", meta)
		}
		if got := unixSecondsToTime(r.CreatedAt); got == nil || !got.Equal(r79provbFracTime) {
			t.Fatalf("createdAt = %v", got)
		}
		huge := 1e300
		if unixSecondsToTime(&huge) != nil {
			t.Fatal("out-of-range seconds must be no time (TS Invalid Date)")
		}
	})
	t.Run("batch-status", func(t *testing.T) {
		var r openAIBatchResponseWire
		if err := json.Unmarshal([]byte(`{"id":"b","status":"in_progress","created_at":1700000000.25}`), &r); err != nil {
			t.Fatal(err)
		}
		if got := r.toStatus().CreatedAt; got != "2023-11-14T22:13:20.250Z" {
			t.Fatalf("createdAt = %q", got)
		}
	})
}

// TS doStream returns response: { headers } for chat and completion streams;
// Go reports them on the response-metadata part, also when no chunk carries
// an id, model or created.
func TestR79provbChatCompletionStreamHeaders(t *testing.T) {
	serve := func(frame string) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("X-Test", "v")
			_, _ = w.Write([]byte("data: " + frame + "\n\ndata: [DONE]\n\n"))
		}))
		t.Cleanup(srv.Close)
		return srv
	}
	headersOf := func(t *testing.T, s provider.TextStream) map[string]string {
		t.Helper()
		defer func() { _ = s.Close() }()
		var h map[string]string
		for {
			c, err := s.Next()
			if err != nil {
				break
			}
			if c.Type == provider.ChunkTypeResponseMetadata && c.ResponseMetadata != nil && len(c.ResponseMetadata.Headers) > 0 && h == nil {
				h = c.ResponseMetadata.Headers
			}
		}
		return h
	}
	for _, tc := range []struct{ name, frame string }{
		{"chat/with-id", `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`},
		{"chat/no-id", `{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`},
		{"completion/with-id", `{"id":"c1","object":"text_completion","created":1,"model":"m","choices":[{"index":0,"text":"hi","finish_reason":"stop"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := serve(tc.frame)
			p := New(Config{APIKey: "k", BaseURL: srv.URL})
			var m provider.LanguageModel
			var err error
			if tc.name[:4] == "chat" {
				m, err = p.ChatModel("gpt-4o")
			} else {
				m, err = p.CompletionModel("gpt-3.5-turbo-instruct")
			}
			if err != nil {
				t.Fatal(err)
			}
			s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
			if err != nil {
				t.Fatal(err)
			}
			if h := headersOf(t, s); h["x-test"] != "v" && h["X-Test"] != "v" {
				t.Fatalf("headers = %v", h)
			}
		})
	}
}
