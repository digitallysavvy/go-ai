package google

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// End-to-end scenarios over generateContent / streamGenerateContent using the
// TS SDK's recorded fixtures (packages/google/src/__fixtures__, copied to
// ../gemini/testdata/ts).

func geminiFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "gemini", "testdata", "ts", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fixtureHandler(t *testing.T, name string, gotKey *string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if gotKey != nil {
			*gotKey = r.Header.Get("x-goog-api-key")
		}
		if strings.Contains(r.URL.Path, ":streamGenerateContent") {
			w.Header().Set("Content-Type", "text/event-stream")
			for _, l := range strings.Split(string(geminiFixture(t, name+".chunks.txt")), "\n") {
				if strings.TrimSpace(l) != "" {
					_, _ = fmt.Fprintf(w, "data: %s\n\n", l)
				}
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(geminiFixture(t, name+".json"))
	}
}

func e2eModel(t *testing.T, h http.HandlerFunc, id string) (*LanguageModel, func()) {
	t.Helper()
	srv := httptest.NewServer(h)
	return NewLanguageModel(New(Config{APIKey: "test-key", BaseURL: srv.URL}), id), srv.Close
}

func drainAll(t *testing.T, s provider.TextStream) []*provider.StreamChunk {
	t.Helper()
	defer s.Close() //nolint:errcheck
	var out []*provider.StreamChunk
	for {
		c, err := s.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		out = append(out, c)
	}
}

func chunkKinds(chunks []*provider.StreamChunk) string {
	var kinds []string
	for _, c := range chunks {
		k := string(c.Type)
		if n := len(kinds); n > 0 && kinds[n-1] == k && (c.Type == provider.ChunkTypeText || c.Type == provider.ChunkTypeReasoning || c.Type == provider.ChunkTypeToolInputDelta) {
			continue
		}
		kinds = append(kinds, k)
	}
	return strings.Join(kinds, ",")
}

func TestFixtureTextGenerateAndStream(t *testing.T) {
	var key string
	m, closeFn := e2eModel(t, fixtureHandler(t, "google-text", &key), "gemini-3-pro-preview")
	defer closeFn()

	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if key != "test-key" {
		t.Fatalf("x-goog-api-key = %q", key)
	}
	// TS snapshot "doGenerate > text": 9 in, 28 text + 244 thought tokens out.
	if res.FinishReason != types.FinishReasonStop || res.RawFinishReason != "STOP" ||
		*res.Usage.InputTokens != 9 || *res.Usage.OutputTokens != 272 || *res.Usage.OutputDetails.ReasoningTokens != 244 {
		t.Fatalf("generate: finish=%v usage=%+v", res.FinishReason, res.Usage)
	}
	if res.ResponseMetadata == nil || res.ResponseMetadata.ID != "Un6LacrVMcjUxs0PmJfWoQc" {
		t.Fatalf("response metadata = %+v", res.ResponseMetadata)
	}

	s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	chunks := drainAll(t, s)
	if got := chunkKinds(chunks); got != "stream-start,response-metadata,text-start,text,text-end,finish" {
		t.Fatalf("sequence = %s", got)
	}
	if chunks[1].ResponseMetadata.ID != "bH6LaZW8Fp_3nsEPqtaSwQ4" {
		t.Fatalf("response id = %q", chunks[1].ResponseMetadata.ID)
	}
	fin := chunks[len(chunks)-1]
	if *fin.Usage.OutputTokens != 208 || fin.RawFinishReason != "STOP" {
		t.Fatalf("finish = %+v", fin)
	}
}

// Thought summaries stream as reasoning-start / reasoning-delta / reasoning-end
// ahead of the answer, with the thought signature on the closing delta.
func TestReasoningStreamWithThoughtParts(t *testing.T) {
	m, closeFn := e2eModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, l := range []string{
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"Let me ","thought":true}]},"index":0}],"responseId":"r1"}`,
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"think.","thought":true,"thoughtSignature":"SIG"}]},"index":0}]}`,
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"Answer."}]},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"thoughtsTokenCount":5,"totalTokenCount":10}}`,
		} {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", l)
		}
	}, "gemini-2.5-pro")
	defer closeFn()
	s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	chunks := drainAll(t, s)
	if got := chunkKinds(chunks); got != "stream-start,response-metadata,reasoning-start,reasoning,reasoning-end,text-start,text,text-end,finish" {
		t.Fatalf("sequence = %s", got)
	}
	var thought strings.Builder
	sig := ""
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeReasoning {
			thought.WriteString(c.Reasoning)
			if strings.Contains(string(c.ProviderMetadata), "SIG") {
				sig = string(c.ProviderMetadata)
			}
		}
	}
	if thought.String() != "Let me think." || !strings.Contains(sig, `"google"`) {
		t.Fatalf("thought=%q sig=%q", thought.String(), sig)
	}
	fin := chunks[len(chunks)-1]
	if *fin.Usage.OutputTokens != 7 || *fin.Usage.OutputDetails.ReasoningTokens != 5 {
		t.Fatalf("usage = %+v", fin.Usage)
	}
}

func TestFixtureToolCallGenerateAndStream(t *testing.T) {
	m, closeFn := e2eModel(t, fixtureHandler(t, "google-tool-call-gemini3", nil), "gemini-3-pro-preview")
	defer closeFn()
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.FinishReason != types.FinishReasonToolCalls || len(res.ToolCalls) != 1 || res.ToolCalls[0].ToolName != "weather" ||
		res.ToolCalls[0].Arguments["location"] != "San Francisco" || res.ToolCalls[0].ThoughtSignature == "" {
		t.Fatalf("generate: %+v", res)
	}
	s, _ := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if got := chunkKinds(drainAll(t, s)); got != "stream-start,response-metadata,tool-input-start,tool-input-delta,tool-input-end,tool-call,finish" {
		t.Fatalf("sequence = %s", got)
	}
}

// Streamed partial function-call arguments (parallel calls) assemble into the
// JSON TS produces, one id per call.
func TestFixtureStreamingToolCallArguments(t *testing.T) {
	m, closeFn := e2eModel(t, fixtureHandler(t, "google-stream-tool-call-arguments", nil), "gemini-3-pro-preview")
	defer closeFn()
	s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	var inputs []string
	ids := map[string]bool{}
	deltas := map[string]string{}
	for _, c := range drainAll(t, s) {
		switch c.Type {
		case provider.ChunkTypeToolInputDelta:
			deltas[c.ID] += c.Text
		case provider.ChunkTypeToolCall:
			ids[c.ToolCall.ID] = true
			inputs = append(inputs, fmt.Sprint(c.ToolCall.Arguments["location"]))
		}
	}
	if len(ids) != 2 || strings.Join(inputs, "|") != "Boston|San Francisco" {
		t.Fatalf("ids=%v inputs=%v", ids, inputs)
	}
	for id := range ids {
		if deltas[id] != `{"location":"Boston"}` && deltas[id] != `{"location":"San Francisco"}` {
			t.Fatalf("deltas[%s] = %q", id, deltas[id])
		}
	}
}

// Without function-call ids in the response, parallel calls to the same
// function must still get distinct ids (TS: functionCall.id || generateId()),
// and Content keeps thought, text and calls in response order.
func TestGenerateParallelCallsDistinctIDsAndOrder(t *testing.T) {
	m, closeFn := e2eModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[
			{"text":"thinking","thought":true},
			{"text":"Checking both."},
			{"functionCall":{"name":"weather","args":{"city":"SF"}}},
			{"functionCall":{"name":"weather","args":{"city":"NYC"}}}
		]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`))
	}, "gemini-2.5-flash")
	defer closeFn()
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ToolCalls) != 2 || res.ToolCalls[0].ID == "" || res.ToolCalls[0].ID == res.ToolCalls[1].ID {
		t.Fatalf("tool calls = %+v", res.ToolCalls)
	}
	if len(res.Content) != 4 {
		t.Fatalf("content = %#v", res.Content)
	}
	if _, ok := res.Content[0].(types.ReasoningContent); !ok {
		t.Fatalf("content[0] = %#v", res.Content[0])
	}
	if _, ok := res.Content[1].(types.TextContent); !ok {
		t.Fatalf("content[1] = %#v", res.Content[1])
	}
	for i := 2; i < 4; i++ {
		c, ok := res.Content[i].(types.ToolCallContent)
		if !ok || c.ToolCallID != res.ToolCalls[i-2].ID {
			t.Fatalf("content[%d] = %#v", i, res.Content[i])
		}
	}
}

// Several text parts in one candidate all belong to result.Text (TS joins
// them); only the first used to survive.
func TestGenerateJoinsAllTextParts(t *testing.T) {
	m, closeFn := e2eModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"Hello, "},{"text":"world."}]},"finishReason":"STOP"}]}`))
	}, "gemini-2.5-flash")
	defer closeFn()
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil || res.Text != "Hello, world." {
		t.Fatalf("text = %q err=%v", res.Text, err)
	}
}

// TS "preserves google.rpc.RetryInfo in APICallError.data", through the whole
// request path, on both generate and stream.
func TestFixture429RetryInfo(t *testing.T) {
	body := geminiFixture(t, "google-429-retry-info.json")
	m, closeFn := e2eModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write(body)
	}, "gemini-2.5-flash")
	defer closeFn()
	check := func(name string, err error) {
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.StatusCode != 429 || !pe.IsRetryable() ||
			pe.Message != "You exceeded your current quota, please check your plan." ||
			!strings.Contains(pe.ResponseBody, "google.rpc.RetryInfo") || pe.Data == nil {
			t.Errorf("%s: err = %#v", name, err)
		}
	}
	_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	check("generate", err)
	_, err = m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	check("stream", err)
}

// A blocked prompt is a content-filter finish, not an error.
func TestGeneratePromptBlocked(t *testing.T) {
	m, closeFn := e2eModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"promptFeedback":{"blockReason":"SAFETY"},"usageMetadata":{"promptTokenCount":4,"totalTokenCount":4}}`))
	}, "gemini-2.5-flash")
	defer closeFn()
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil || res.FinishReason != types.FinishReasonContentFilter {
		t.Fatalf("finish=%v err=%v", res.FinishReason, err)
	}
}
