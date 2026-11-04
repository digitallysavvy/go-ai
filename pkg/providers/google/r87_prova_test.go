package google

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS ef2dfb5e9f google-video-model.test.ts "should pass seed when it is 0".
func TestR87provaVideoSeedZeroIsSent(t *testing.T) {
	var captured map[string]interface{}
	model, _ := newTestVideoServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, ":predictLongRunning") {
			_ = json.NewDecoder(r.Body).Decode(&captured)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "operations/x", "done": false})
	})
	seed := 0
	if _, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x", N: 1, Seed: &seed},
	}); err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	params, _ := captured["parameters"].(map[string]interface{})
	if v, ok := params["seed"]; !ok || v != float64(0) {
		t.Fatalf("parameters = %#v, want seed 0", params)
	}
}

// r87provaOrderCase is one recorded TS GoogleLanguageModel stream (3fab50aa9b,
// fd7e8a75d7): an SSE body and the stream parts TS emits for it (without
// stream-start, response-metadata and raw; finish reduced to its reason).
type r87provaOrderCase struct {
	Provider string        `json:"provider"`
	Seq      [][]string    `json:"seq"`
	SSE      string        `json:"sse"`
	Events   []interface{} `json:"events"`
}

func r87provaLoadOrderCases(t *testing.T, path, providerName string) []r87provaOrderCase {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var all, out []r87provaOrderCase
	if err := json.NewDecoder(zr).Decode(&all); err != nil {
		t.Fatal(err)
	}
	for _, c := range all {
		if c.Provider == providerName {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		t.Fatalf("no %s cases", providerName)
	}
	return out
}

// r87provaProjectStream renders Go stream chunks as the recorded TS parts.
func r87provaProjectStream(t *testing.T, stream provider.TextStream) []interface{} {
	t.Helper()
	var chunks []*provider.StreamChunk
	for {
		c, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		chunks = append(chunks, c)
	}
	var out []interface{}
	for _, p := range tsfixture.StreamToTS(chunks) {
		m, _ := p.(map[string]interface{})
		switch m["type"] {
		case "stream-start", "response-metadata", "raw":
			continue
		case "finish":
			reason := m["finishReason"]
			if fr, ok := reason.(map[string]interface{}); ok {
				reason = fr["unified"]
			}
			out = append(out, map[string]interface{}{"type": "finish", "finishReason": reason})
			continue
		}
		out = append(out, p)
	}
	return out
}

// TestR87provaStreamOrderMatchesTS replays 26 part sequences (text,
// reasoning, complete / no-args / partial-argument calls, standalone
// signatures, files and code execution, within and across events) through
// the Google model and compares every stream part with TS 3fab50aa9b.
func TestR87provaStreamOrderMatchesTS(t *testing.T) {
	for _, c := range r87provaLoadOrderCases(t, "testdata/r87/stream_order_ts.json.gz", "google.generative-ai") {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, c.SSE)
		}))
		m, err := New(Config{APIKey: "dummy-key", BaseURL: srv.URL}).LanguageModel("gemini-2.5-flash")
		if err != nil {
			t.Fatal(err)
		}
		stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}})
		if err != nil {
			t.Fatal(err)
		}
		got := r87provaProjectStream(t, stream)
		srv.Close()
		r87provaCompareOrder(t, fmt.Sprint(c.Seq), c.Events, got)
	}
}

func r87provaCompareOrder(t *testing.T, name string, want, got []interface{}) {
	t.Helper()
	// TS generateId is the fixed 'generated-call-id'; Go generates its own.
	for i, w := range want {
		wm, _ := w.(map[string]interface{})
		if i < len(got) {
			gm, _ := got[i].(map[string]interface{})
			for _, k := range []string{"id", "toolCallId"} {
				if wm[k] == "generated-call-id" && gm != nil {
					wm[k] = gm[k]
				}
			}
		}
	}
	w, g := tsfixture.Normalize(r87provaJSONCopy(want)), tsfixture.Normalize(r87provaJSONCopy(got))
	if d := tsfixture.Diff("", w, g); d != "" {
		gotJSON, _ := json.Marshal(got)
		t.Errorf("%s: stream differs from TS: %s\n got: %.3000s", name, d, gotJSON)
	}
}

func r87provaJSONCopy(v interface{}) interface{} {
	b, _ := json.Marshal(v)
	var out interface{}
	_ = json.Unmarshal(b, &out)
	return out
}

// TS google-language-model.test.ts "keeps non-streaming text and call
// content order" (fd7e8a75d7).
func TestR87provaGenerateKeepsTextAndCallOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"A"},{"functionCall":{"id":"call-1","name":"lookup","args":{"query":"example"}},"thoughtSignature":"c2ln"},{"text":"B"},{"functionCall":{"id":"call-2","name":"read_theme"}},{"text":"C"}]},"finishReason":"STOP","index":0}]}`)
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("gemini-2.5-flash")
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, part := range res.Content {
		switch p := part.(type) {
		case types.TextContent:
			got = append(got, "text:"+p.Text)
		case types.ToolCallContent:
			got = append(got, "call:"+p.ToolCallID+":"+p.ToolName)
		}
	}
	want := []string{"text:A", "call:call-1:lookup", "text:B", "call:call-2:read_theme", "text:C"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("content order = %v, want %v", got, want)
	}
}
