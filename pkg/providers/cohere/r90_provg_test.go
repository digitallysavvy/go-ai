package cohere

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const r90provgCitation = `{"start":0,"end":5,"text":"sunny","type":"TEXT_CONTENT","sources":[{"type":"document","id":"doc:0","document":{"id":"doc:0","text":"It is sunny.","title":"weather.txt"}}]}`

// TestR90provgCitationSourceIDsUseGenerateID: TS createCohere passes
// options.generateId to the chat model, and mapCohereCitationToSource ids
// come from it (cohere-chat-language-model.ts doGenerate and the
// citation-start stream event).
func TestR90provgCitationSourceIDsUseGenerateID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if bytes.Contains(body, []byte(`"stream":true`)) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"type\":\"citation-start\",\"index\":0,\"delta\":{\"message\":{\"citations\":"+r90provgCitation+"}}}\n\n")
			_, _ = io.WriteString(w, "data: {\"type\":\"message-end\",\"delta\":{\"finish_reason\":\"COMPLETE\",\"usage\":{}}}\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"r1","finish_reason":"COMPLETE","message":{"role":"assistant","content":[{"type":"text","text":"sunny"}],"citations":[`+r90provgCitation+`]}}`)
	}))
	defer server.Close()

	n := 0
	p := New(Config{APIKey: "k", BaseURL: server.URL + "/v2", GenerateID: func() string { n++; return "src-" + string(rune('0'+n)) }})
	model, _ := p.LanguageModel("command-a-03-2025")
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "weather?"}}

	res, err := model.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, c := range res.Content {
		if s, ok := c.(types.SourceContent); ok {
			id = s.ID
		}
	}
	if id != "src-1" {
		t.Fatalf("generate source id = %q, want src-1", id)
	}

	stream, err := model.DoStream(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	id = ""
	for {
		chunk, err := stream.Next()
		if err != nil {
			break
		}
		if chunk.Type == provider.ChunkTypeSource && chunk.SourceContent != nil {
			id = chunk.SourceContent.ID
		}
	}
	if id != "src-2" {
		t.Fatalf("stream source id = %q, want src-2", id)
	}
}

// TestR90provgEmbedFractionalTokens: TS cohereTextEmbeddingResponseSchema
// meta.billed_units.input_tokens is z.number(), so a fractional count is a
// valid response and usage.tokens is that number.
func TestR90provgEmbedFractionalTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"embeddings":{"float":[[0.1,0.2]]},"meta":{"billed_units":{"input_tokens":3.5}}}`)
	}))
	defer server.Close()
	p := New(Config{APIKey: "k", BaseURL: server.URL + "/v2"})
	model, _ := p.EmbeddingModel("embed-v4.0")
	res, err := model.DoEmbedMany(context.Background(), []string{"a"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage.Tokens != 3.5 || res.Usage.InputTokens != 3 {
		t.Fatalf("usage = %+v, want tokens 3.5", res.Usage)
	}
}

// TestR90provgRerankObjectDocuments: TS sends object documents as
// JSON.stringify(value): HTML characters stay literal, raw JSON keeps its key
// order, and every slice of objects is converted (cohere-reranking-model.ts).
func TestR90provgRerankObjectDocuments(t *testing.T) {
	m := NewRerankingModel(New(Config{APIKey: "k"}), "rerank-v3.5")
	type doc struct {
		Title string `json:"title"`
	}
	cases := []struct {
		docs interface{}
		want []string
	}{
		{[]map[string]interface{}{{"t": "a<b&c"}}, []string{`{"t":"a<b&c"}`}},
		{[]json.RawMessage{json.RawMessage(`{"z":1,"a":"<x>"}`)}, []string{`{"z":1,"a":"<x>"}`}},
		{[]doc{{Title: "x"}}, []string{`{"title":"x"}`}},
	}
	for _, tc := range cases {
		body, warnings := m.buildRequestBodyWithWarnings(&provider.RerankOptions{Query: "q", Documents: tc.docs})
		got, _ := body["documents"].([]string)
		if len(got) != len(tc.want) || got[0] != tc.want[0] {
			t.Errorf("documents %T = %v, want %v", tc.docs, got, tc.want)
		}
		if len(warnings) != 1 {
			t.Errorf("documents %T: warnings = %v, want the object documents warning", tc.docs, warnings)
		}
	}
}
