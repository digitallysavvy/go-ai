package deepseek

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// TestR97provbLogprobsNullMember: TS providerMetadata.deepseek.logprobs is
// choice.logprobs as parsed (content / reasoning_content are nullish), so a
// null member is kept as null; Go dropped it.
func TestR97provbLogprobsNullMember(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"logprobs":{"content":null,"reasoning_content":[]},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	res, err := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "deepseek-chat").DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	lp := res.ProviderMetadata["deepseek"].(map[string]interface{})["logprobs"].(map[string]interface{})
	want := map[string]interface{}{"content": nil, "reasoning_content": []deepseekLogprobEntry{}}
	if !reflect.DeepEqual(lp, want) {
		t.Fatalf("logprobs = %#v, want %#v", lp, want)
	}
}

// TestR97provbPromptCacheTokensNull: TS doGenerate writes
// promptCacheHitTokens: responseBody.usage?.prompt_cache_hit_tokens without
// `?? undefined`, so a null count is kept as null (the stream drops it).
func TestR97provbPromptCacheTokensNull(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"prompt_cache_hit_tokens":null,"prompt_cache_miss_tokens":1}}`))
	}))
	defer srv.Close()
	res, err := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "deepseek-chat").DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	meta := res.ProviderMetadata["deepseek"].(map[string]interface{})
	if v, ok := meta["promptCacheHitTokens"]; !ok || v != nil {
		t.Fatalf("promptCacheHitTokens = %#v (present %v), want null", v, ok)
	}
	if meta["promptCacheMissTokens"] != 1 {
		t.Fatalf("promptCacheMissTokens = %#v", meta["promptCacheMissTokens"])
	}
}

// TestR97provbEmptySystemFingerprint: TS keeps system_fingerprint when it
// is != null, so a present "" is reported in generate and stream metadata.
func TestR97provbEmptySystemFingerprint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","system_fingerprint":"","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	res, err := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "deepseek-chat").DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := res.ProviderMetadata["deepseek"].(map[string]interface{})["systemFingerprint"]; !ok || v != "" {
		t.Fatalf("generate systemFingerprint = %#v (present %v)", v, ok)
	}
	s := newDeepseekStream(io.NopCloser(strings.NewReader("data: {\"id\":\"a\",\"system_fingerprint\":\"\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\n")))
	for {
		c, err := s.Next()
		if err != nil {
			t.Fatal("no finish chunk")
		}
		if c.Type == provider.ChunkTypeFinish {
			if !strings.Contains(string(c.ProviderMetadata), `"systemFingerprint":""`) {
				t.Fatalf("stream metadata = %s", c.ProviderMetadata)
			}
			return
		}
	}
}

// TestR97provbResponsesStreamMissingMemberTypeError: a frame without its
// response / item matches the loose {type} member of TS's chunk schema,
// and transform then reads a property of undefined or null; the TypeError
// errors the stream (no finish part). Go skipped the frame.
func TestR97provbResponsesStreamMissingMemberTypeError(t *testing.T) {
	for frame, want := range map[string]string{
		`{"type":"response.created"}`:                     "Cannot read properties of undefined (reading 'id')",
		`{"type":"response.completed","response":null}`:   "Cannot read properties of null (reading 'usage')",
		`{"type":"response.output_item.added"}`:           "Cannot use 'in' operator to search for 'id' in undefined",
		`{"type":"response.output_item.done","item":"x"}`: "Cannot use 'in' operator to search for 'id' in x",
	} {
		s := &deepseekResponsesStream{
			reader: io.NopCloser(strings.NewReader("data: " + frame + "\n\n")), finishReason: types.FinishReasonInterrupted,
			openText: map[string]bool{}, openReasoning: map[string]bool{}, functionCalls: map[string]*deepseekResponsesCallState{},
		}
		s.parser = streaming.NewSSEParser(s.reader)
		var err error
		for err == nil {
			var c *provider.StreamChunk
			c, err = s.Next()
			if c != nil && c.Type == provider.ChunkTypeFinish {
				t.Errorf("%s: finish part, want the TypeError", frame)
			}
		}
		if err.Error() != want {
			t.Errorf("%s: err = %v, want %q", frame, err, want)
		}
	}
}
