package google

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports the Gemini 4 long decode tests (TS #23261).

type scriptedReply struct {
	status int
	body   string
	sse    bool
}

// scriptedServer replays replies in order and records request bodies.
func scriptedServer(t *testing.T, replies ...scriptedReply) (*httptest.Server, func() []map[string]interface{}) {
	t.Helper()
	var mu sync.Mutex
	var bodies []map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&b)
		mu.Lock()
		idx := len(bodies)
		bodies = append(bodies, b)
		mu.Unlock()
		if idx >= len(replies) {
			t.Errorf("unexpected request #%d", idx+1)
			w.WriteHeader(500)
			return
		}
		rep := replies[idx]
		if rep.sse {
			w.Header().Set("Content-Type", "text/event-stream")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		if rep.status != 0 {
			w.WriteHeader(rep.status)
		}
		_, _ = io.WriteString(w, rep.body)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []map[string]interface{} {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]interface{}{}, bodies...)
	}
}

func sseEvents(events ...string) string {
	out := ""
	for _, e := range events {
		out += "data: " + e + "\n\n"
	}
	return out
}

func longDecodeModel(srv *httptest.Server) provider.LanguageModel {
	return NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), ModelGemini4Argon)
}

func googleMeta(t *testing.T, raw json.RawMessage) map[string]interface{} {
	t.Helper()
	var m map[string]map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("metadata: %v (%s)", err, raw)
	}
	return m["google"]
}

const continuationJSON = `{"candidates":[{"content":{"parts":[{"text":"Hello"}],"role":"model"},"finishReason":"CONTINUATION","index":0,"continuationToken":"token-1"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"thoughtsTokenCount":20,"totalTokenCount":35}}`

func TestLongDecode_ContinuationTokenOption(t *testing.T) {
	srv, bodies := scriptedServer(t,
		scriptedReply{body: `{"candidates":[{"content":{"parts":[{"text":"x"}],"role":"model"},"finishReason":"STOP"}]}`},
		scriptedReply{body: `{"candidates":[{"content":{"parts":[{"text":"x"}],"role":"model"},"finishReason":"STOP"}]}`},
	)
	m := longDecodeModel(srv)
	if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"continuationToken": "abc"}},
	}); err != nil {
		t.Fatal(err)
	}
	b := bodies()
	if _, ok := b[0]["continuationToken"]; ok {
		t.Errorf("continuationToken must be omitted when unset")
	}
	if b[1]["continuationToken"] != "abc" {
		t.Errorf("continuationToken = %v, want abc", b[1]["continuationToken"])
	}
}

func TestLongDecode_MaxTokensExposesToken(t *testing.T) {
	srv, _ := scriptedServer(t,
		scriptedReply{body: `{"candidates":[{"content":{"parts":[{"text":"Partial"}],"role":"model"},"finishReason":"MAX_TOKENS","continuationToken":"resume-1"}]}`},
		scriptedReply{body: `{"candidates":[{"content":{"parts":[{"text":"Done"}],"role":"model"},"finishReason":"STOP"}]}`},
	)
	m := longDecodeModel(srv)
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.FinishReason != types.FinishReasonLength {
		t.Errorf("finish = %v", res.FinishReason)
	}
	if got := googleMeta(t, mustJSON(t, res.ProviderMetadata))["continuationToken"]; got != "resume-1" {
		t.Errorf("continuationToken = %v", got)
	}
	res, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := googleMeta(t, mustJSON(t, res.ProviderMetadata))["continuationToken"]; ok {
		t.Errorf("continuationToken must be absent")
	}
}

func mustJSON(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLongDecode_GenerateContinuesOnContinuation(t *testing.T) {
	srv, bodies := scriptedServer(t,
		scriptedReply{body: continuationJSON},
		scriptedReply{body: `{"candidates":[{"content":{"parts":[{"text":", world"}],"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":35,"candidatesTokenCount":7,"totalTokenCount":42}}`},
	)
	m := longDecodeModel(srv)
	mt := 100000
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, MaxTokens: &mt})
	if err != nil {
		t.Fatal(err)
	}
	b := bodies()
	if len(b) != 2 {
		t.Fatalf("requests = %d", len(b))
	}
	if _, ok := b[0]["continuationToken"]; ok {
		t.Errorf("first request must not carry a token")
	}
	want := map[string]interface{}{}
	for k, v := range b[0] {
		want[k] = v
	}
	want["continuationToken"] = "token-1"
	if !reflect.DeepEqual(b[1], want) {
		t.Errorf("second body = %v, want %v", b[1], want)
	}
	gc, _ := b[1]["generationConfig"].(map[string]interface{})
	if gc["maxOutputTokens"] != float64(100000) {
		t.Errorf("maxOutputTokens = %v", gc["maxOutputTokens"])
	}

	var texts []string
	for _, c := range res.Content {
		if tc, ok := c.(types.TextContent); ok {
			texts = append(texts, tc.Text)
		}
	}
	if !reflect.DeepEqual(texts, []string{"Hello", ", world"}) {
		t.Errorf("content texts = %v", texts)
	}
	if res.Text != "Hello, world" {
		t.Errorf("Text = %q", res.Text)
	}
	if res.FinishReason != types.FinishReasonStop || res.RawFinishReason != "STOP" {
		t.Errorf("finish = %v/%v", res.FinishReason, res.RawFinishReason)
	}
	if *res.Usage.InputTokens != 45 || *res.Usage.OutputTokens != 32 || *res.Usage.OutputDetails.TextTokens != 12 || *res.Usage.OutputDetails.ReasoningTokens != 20 {
		t.Errorf("usage = in %d out %d text %d reasoning %d", *res.Usage.InputTokens, *res.Usage.OutputTokens, *res.Usage.OutputDetails.TextTokens, *res.Usage.OutputDetails.ReasoningTokens)
	}
	meta := googleMeta(t, mustJSON(t, res.ProviderMetadata))
	if _, ok := meta["continuationToken"]; ok {
		t.Errorf("continuationToken must be absent")
	}
	if meta["initialPromptTokenCount"] != float64(10) {
		t.Errorf("initialPromptTokenCount = %v", meta["initialPromptTokenCount"])
	}
}

func TestLongDecode_GenerateReturnsTokenWhenBudgetUsedAfterContinuing(t *testing.T) {
	srv, bodies := scriptedServer(t,
		scriptedReply{body: continuationJSON},
		scriptedReply{body: `{"candidates":[{"content":{"parts":[{"text":", wor"}],"role":"model"},"finishReason":"MAX_TOKENS","index":0,"continuationToken":"token-2"}]}`},
	)
	res, err := longDecodeModel(srv).DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(bodies()) != 2 || res.FinishReason != types.FinishReasonLength || res.RawFinishReason != "MAX_TOKENS" {
		t.Fatalf("requests=%d finish=%v/%v", len(bodies()), res.FinishReason, res.RawFinishReason)
	}
	if got := googleMeta(t, mustJSON(t, res.ProviderMetadata))["continuationToken"]; got != "token-2" {
		t.Errorf("continuationToken = %v", got)
	}
}

func drain(t *testing.T, s provider.TextStream) []*provider.StreamChunk {
	t.Helper()
	var out []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatalf("stream error: %v", err)
			}
			return out
		}
		out = append(out, c)
	}
}

func finishOf(t *testing.T, chunks []*provider.StreamChunk) *provider.StreamChunk {
	t.Helper()
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeFinish {
			return c
		}
	}
	t.Fatal("no finish chunk")
	return nil
}

func TestLongDecode_StreamContinuesOnContinuation(t *testing.T) {
	srv, bodies := scriptedServer(t,
		scriptedReply{sse: true, body: sseEvents(
			`{"candidates":[{"content":{"parts":[{"text":"Hello"}],"role":"model"},"index":0}]}`,
			`{"candidates":[{"content":{"parts":[{"text":""}],"role":"model"},"index":0,"continuationToken":"checkpoint-1"}]}`,
			`{"candidates":[{"content":{"parts":[{"text":""}],"role":"model"},"finishReason":"CONTINUATION","index":0,"continuationToken":"token-1"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"thoughtsTokenCount":20,"totalTokenCount":35}}`,
		)},
		scriptedReply{sse: true, body: sseEvents(
			`{"candidates":[{"content":{"parts":[{"text":", world"}],"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":35,"candidatesTokenCount":7,"totalTokenCount":42}}`,
		)},
	)
	mt := 100000
	s, err := longDecodeModel(srv).DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, MaxTokens: &mt})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close() //nolint:errcheck
	chunks := drain(t, s)

	b := bodies()
	if len(b) != 2 || b[1]["continuationToken"] != "token-1" {
		t.Fatalf("bodies = %v", b)
	}
	var deltas []string
	starts := 0
	for _, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeText:
			if c.Text != "" {
				deltas = append(deltas, c.Text)
			}
		case provider.ChunkTypeTextStart:
			starts++
		}
	}
	if !reflect.DeepEqual(deltas, []string{"Hello", ", world"}) || starts != 1 {
		t.Errorf("deltas=%v starts=%d", deltas, starts)
	}
	fin := finishOf(t, chunks)
	if fin.FinishReason != types.FinishReasonStop || fin.RawFinishReason != "STOP" {
		t.Errorf("finish = %v/%v", fin.FinishReason, fin.RawFinishReason)
	}
	if *fin.Usage.InputTokens != 45 || *fin.Usage.OutputTokens != 32 {
		t.Errorf("usage in=%d out=%d", *fin.Usage.InputTokens, *fin.Usage.OutputTokens)
	}
	meta := googleMeta(t, fin.ProviderMetadata)
	if meta["initialPromptTokenCount"] != float64(10) {
		t.Errorf("initialPromptTokenCount = %v", meta["initialPromptTokenCount"])
	}
	if _, ok := meta["continuationToken"]; ok {
		t.Errorf("continuationToken must be absent")
	}
}

func TestLongDecode_StreamCheckpointTokensNotExposedOnNormalFinish(t *testing.T) {
	srv, _ := scriptedServer(t, scriptedReply{sse: true, body: sseEvents(
		`{"candidates":[{"content":{"parts":[{"text":"Hello"}],"role":"model"},"index":0,"continuationToken":"checkpoint-1"}]}`,
		`{"candidates":[{"content":{"parts":[{"text":", world"}],"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":40000,"totalTokenCount":40010}}`,
	)})
	s, err := longDecodeModel(srv).DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close() //nolint:errcheck
	meta := googleMeta(t, finishOf(t, drain(t, s)).ProviderMetadata)
	if _, ok := meta["continuationToken"]; ok {
		t.Error("continuationToken must be absent")
	}
	if _, ok := meta["initialPromptTokenCount"]; ok {
		t.Error("initialPromptTokenCount must be absent")
	}
}

func TestLongDecode_StreamMaxTokensExposesLastToken(t *testing.T) {
	srv, _ := scriptedServer(t, scriptedReply{sse: true, body: sseEvents(
		`{"candidates":[{"content":{"parts":[{"text":"Partial"}],"role":"model"},"index":0}]}`,
		`{"candidates":[{"content":{"parts":[{"text":""}],"role":"model"},"finishReason":"MAX_TOKENS","index":0,"continuationToken":"resume-1"}]}`,
	)})
	s, err := longDecodeModel(srv).DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close() //nolint:errcheck
	fin := finishOf(t, drain(t, s))
	if fin.FinishReason != types.FinishReasonLength {
		t.Errorf("finish = %v", fin.FinishReason)
	}
	if got := googleMeta(t, fin.ProviderMetadata)["continuationToken"]; got != "resume-1" {
		t.Errorf("continuationToken = %v", got)
	}
}

func TestLongDecode_StreamFinishesWithTokenWhenContinuingFails(t *testing.T) {
	srv, _ := scriptedServer(t,
		scriptedReply{sse: true, body: sseEvents(
			`{"candidates":[{"content":{"parts":[{"text":"Hello"}],"role":"model"},"finishReason":"CONTINUATION","index":0,"continuationToken":"token-1"}]}`,
		)},
		scriptedReply{status: 400, body: `{"error":{"code":400,"message":"Invalid continuation token","status":"INVALID_ARGUMENT"}}`},
	)
	s, err := longDecodeModel(srv).DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close() //nolint:errcheck
	chunks := drain(t, s)
	sawErr := false
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeError {
			sawErr = true
		}
	}
	if !sawErr {
		t.Error("expected an error chunk")
	}
	fin := finishOf(t, chunks)
	if fin.FinishReason != types.FinishReasonLength || fin.RawFinishReason != "CONTINUATION" {
		t.Errorf("finish = %v/%v", fin.FinishReason, fin.RawFinishReason)
	}
	if got := googleMeta(t, fin.ProviderMetadata)["continuationToken"]; got != "token-1" {
		t.Errorf("continuationToken = %v", got)
	}
}

// TS #23261: doGenerate polls background: true model calls (not only agents)
// until they reach a terminal status.
func TestInteractionsPollsBackgroundModelCalls(t *testing.T) {
	var mu sync.Mutex
	var postBody map[string]interface{}
	var gets int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&postBody)
			_, _ = io.WriteString(w, `{"id":"v1_poll-test","status":"in_progress"}`)
			return
		}
		gets++
		_, _ = io.WriteString(w, `{"id":"v1_poll-test","status":"completed","steps":[{"type":"model_output","content":[{"type":"text","text":"long answer"}]}],"usage":{"total_input_tokens":5,"total_output_tokens":100000,"total_tokens":100005}}`)
	}))
	defer srv.Close()

	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).Interactions(ModelGemini4Argon)
	if err != nil {
		t.Fatal(err)
	}
	mt := 200000
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		MaxTokens:       &mt,
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"background": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if postBody["model"] != ModelGemini4Argon || postBody["background"] != true {
		t.Errorf("post body = %v", postBody)
	}
	if gets != 1 {
		t.Errorf("GET polls = %d, want 1", gets)
	}
	if res.FinishReason != types.FinishReasonStop || res.Text != "long answer" {
		t.Errorf("finish=%v text=%q", res.FinishReason, res.Text)
	}
}
