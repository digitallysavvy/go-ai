package cohere

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports TS cohere-citations.test.ts (3aad18a417) mapCohereCitationToSource.
func TestR88provgCohereCitationToSource(t *testing.T) {
	src := mapCohereCitationToSource(json.RawMessage(`{"start":1,"end":5,"text":"sunny","type":"TEXT_CONTENT","sources":[{"type":"document","id":"doc:0","document":{"id":"doc:0","text":"It is sunny.","title":"weather.txt"}}]}`))
	if src == nil {
		t.Fatal("nil source")
	}
	if src.SourceType != "document" || src.MediaType != "text/plain" || src.Title != "weather.txt" || src.ID == "" {
		t.Fatalf("source = %+v", src)
	}
	want := `{"cohere":{"start":1,"end":5,"text":"sunny","sources":[{"type":"document","id":"doc:0","document":{"id":"doc:0","text":"It is sunny.","title":"weather.txt"}}],"citationType":"TEXT_CONTENT"}}`
	if string(src.ProviderMetadata) != want {
		t.Fatalf("metadata = %s\nwant %s", src.ProviderMetadata, want)
	}

	// The first source with a document provides the title.
	src = mapCohereCitationToSource(json.RawMessage(`{"sources":[{"type":"tool","id":"getWeather:0","tool_output":{}},{"type":"document","document":{"title":"second.txt"}},{"type":"document","document":{"title":"third.txt"}}]}`))
	if src == nil || src.Title != "second.txt" {
		t.Fatalf("title = %+v", src)
	}

	// An empty, null or absent title falls back to "Document".
	for _, doc := range []string{`{}`, `{"title":""}`, `{"title":null}`} {
		src = mapCohereCitationToSource(json.RawMessage(`{"sources":[{"type":"document","document":` + doc + `}]}`))
		if src == nil || src.Title != "Document" {
			t.Fatalf("%s: title = %+v", doc, src)
		}
	}

	// Absent citation fields are omitted from the metadata.
	src = mapCohereCitationToSource(json.RawMessage(`{"sources":[{"document":{"text":"It is sunny."}}]}`))
	if src == nil || string(src.ProviderMetadata) != `{"cohere":{"sources":[{"document":{"text":"It is sunny."}}]}}` {
		t.Fatalf("metadata = %+v", src)
	}

	// Tool-only citations and citations without sources map to no source.
	for _, c := range []string{
		`{"start":1,"end":5,"text":"sunny","sources":[{"type":"tool","id":"getWeather:0","tool_output":{"sunny":true}}],"type":"TEXT_CONTENT"}`,
		`{}`,
		`{"sources":null}`,
	} {
		if src := mapCohereCitationToSource(json.RawMessage(c)); src != nil {
			t.Fatalf("%s: source = %+v, want none", c, src)
		}
	}

	// Loose document keys survive (known keys first), unknown source keys are
	// stripped, tool_output is kept.
	src = mapCohereCitationToSource(json.RawMessage(`{"sources":[{"extra":1,"document":{"url":"u","title":"t","snippet":"s"},"type":"document"},{"type":"tool","tool_output":{"b":1,"a":2}}]}`))
	want = `{"cohere":{"sources":[{"type":"document","document":{"title":"t","url":"u","snippet":"s"}},{"type":"tool","tool_output":{"b":1,"a":2}}]}}`
	if src == nil || string(src.ProviderMetadata) != want {
		t.Fatalf("metadata = %+v\nwant %s", src, want)
	}
}

func r88provgCohereServer(t *testing.T, contentType, body string) *LanguageModel {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return NewLanguageModel(New(Config{BaseURL: srv.URL, APIKey: "k"}), "command-r-plus")
}

// TS 3aad18a417: untitled-document and tool citations, and a usage without
// tokens, no longer fail the response; counts are unset, tool-only
// citations are not sources.
func TestR88provgCohereGeneratePartialCitationsAndUsage(t *testing.T) {
	body := `{"id":"resp-1","message":{"role":"assistant","content":[{"type":"text","text":"It is sunny."}],"citations":[` +
		`{"start":6,"end":11,"text":"sunny","sources":[{"type":"document","id":"doc:0","document":{"id":"doc:0","text":"It is sunny."}}]},` +
		`{"start":0,"end":5,"text":"It is","sources":[{"type":"tool","id":"getWeather:0","tool_output":{"sunny":true}}],"type":"TEXT_CONTENT"}` +
		`]},"finish_reason":"COMPLETE","usage":{"billed_units":{"input_tokens":3,"output_tokens":4}}}`
	m := r88provgCohereServer(t, "application/json", body)
	res, err := m.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	var sources []types.SourceContent
	for _, c := range res.Content {
		if s, ok := c.(types.SourceContent); ok {
			sources = append(sources, s)
		}
	}
	if len(sources) != 1 || sources[0].Title != "Document" {
		t.Fatalf("sources = %+v, want one untitled document source", sources)
	}
	if res.Usage.InputTokens != nil || res.Usage.OutputTokens != nil || res.Usage.TotalTokens != nil {
		t.Fatalf("usage = %+v, want unset counts", res.Usage)
	}
	if res.Usage.Raw["billed_units"] == nil {
		t.Fatalf("raw usage = %v, want billed_units kept", res.Usage.Raw)
	}

	// No usage at all, and partial tokens.
	m = r88provgCohereServer(t, "application/json", `{"message":{"role":"assistant","content":[]},"finish_reason":"COMPLETE","usage":{"tokens":{"input_tokens":10},"cached_tokens":4}}`)
	res, err = m.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	u := res.Usage
	if u.InputTokens == nil || *u.InputTokens != 10 || u.OutputTokens != nil || *u.TotalTokens != 10 ||
		*u.InputDetails.NoCacheTokens != 6 || *u.InputDetails.CacheReadTokens != 4 {
		t.Fatalf("usage = %+v / %+v", u, u.InputDetails)
	}
}

// TS 3aad18a417: a stream message-end whose usage has no tokens no longer
// fails the chunk; the finish usage counts stay unset.
func TestR88provgCohereStreamUsageWithoutTokens(t *testing.T) {
	sse := "data: {\"type\":\"message-end\",\"delta\":{\"finish_reason\":\"COMPLETE\",\"usage\":{\"billed_units\":{\"input_tokens\":1,\"output_tokens\":null}}}}\n\n"
	s := newCohereV2Stream(io.NopCloser(strings.NewReader(sse)))
	var finish *provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeError {
			t.Fatalf("error part: %s", c.Text)
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if finish == nil || finish.FinishReason != types.FinishReasonStop || finish.Usage == nil || finish.Usage.InputTokens != nil {
		t.Fatalf("finish = %+v", finish)
	}
}

// TS 033a980fee: citation-start events become source parts (one citation
// or an array; tool-only citations skipped; null citations tolerated); a
// malformed citation fails the chunk.
func TestR88provgCohereStreamCitationSources(t *testing.T) {
	sse := "data: {\"type\":\"citation-start\",\"index\":0,\"delta\":{\"message\":{\"citations\":{\"start\":49,\"end\":60,\"text\":\"Marrowdale.\",\"sources\":[{\"type\":\"document\",\"id\":\"doc:0\",\"document\":{\"id\":\"doc:0\",\"text\":\"Zephyria\",\"title\":\"zephyria.txt\"}}],\"type\":\"TEXT_CONTENT\"}}}}\n\n" +
		"data: {\"type\":\"citation-end\",\"index\":0}\n\n" +
		"data: {\"type\":\"citation-start\",\"index\":1,\"delta\":{\"message\":{\"citations\":[{\"sources\":[{\"type\":\"document\",\"document\":{\"text\":\"a\"}}]},{\"sources\":[{\"type\":\"tool\",\"tool_output\":{}}]},{\"sources\":[{\"document\":{\"title\":\"b\"}}]}]}}}\n\n" +
		"data: {\"type\":\"citation-start\",\"index\":2}\n\n" +
		"data: {\"type\":\"citation-start\",\"index\":3,\"delta\":{\"message\":{\"citations\":null}}}\n\n" +
		"data: {\"type\":\"citation-start\",\"delta\":{\"message\":{\"citations\":{\"start\":\"x\"}}}}\n\n" +
		"data: {\"type\":\"message-end\",\"delta\":{\"finish_reason\":\"COMPLETE\",\"usage\":{\"tokens\":{\"input_tokens\":1,\"output_tokens\":2}}}}\n\n"
	s := newCohereV2Stream(io.NopCloser(strings.NewReader(sse)))
	var titles []string
	ids := map[string]bool{}
	errors := 0
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		switch c.Type {
		case provider.ChunkTypeSource:
			titles = append(titles, c.SourceContent.Title)
			ids[c.SourceContent.ID] = true
		case provider.ChunkTypeError:
			errors++
		}
	}
	if strings.Join(titles, ",") != "zephyria.txt,Document,b" {
		t.Fatalf("source titles = %v", titles)
	}
	if len(ids) != 3 {
		t.Fatalf("source ids = %v, want 3 distinct", ids)
	}
	if errors != 1 {
		t.Fatalf("error parts = %d, want 1 (malformed citation)", errors)
	}
}

// TS 73400b299d: the response id is `id ?? generation_id`.
func TestR88provgCohereResponseID(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"id":"resp-1","generation_id":"gen-1","message":{"role":"assistant"},"finish_reason":"COMPLETE"}`, "resp-1"},
		{`{"id":"resp-1","message":{"role":"assistant"},"finish_reason":"COMPLETE"}`, "resp-1"},
		{`{"generation_id":"gen-1","message":{"role":"assistant"},"finish_reason":"COMPLETE"}`, "gen-1"},
		{`{"id":null,"generation_id":"gen-1","message":{"role":"assistant"},"finish_reason":"COMPLETE"}`, "gen-1"},
	} {
		m := r88provgCohereServer(t, "application/json", tc.body)
		res, err := m.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		if err != nil {
			t.Fatal(err)
		}
		if res.ResponseMetadata == nil || res.ResponseMetadata.ID != tc.want {
			t.Fatalf("%s: id = %+v, want %q", tc.body, res.ResponseMetadata, tc.want)
		}
	}
	// A non-string id fails the response schema.
	m := r88provgCohereServer(t, "application/json", `{"id":1,"message":{"role":"assistant"},"finish_reason":"COMPLETE"}`)
	if _, err := m.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err == nil {
		t.Fatal("want Invalid JSON response for a numeric id")
	}
}
