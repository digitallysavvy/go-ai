package perplexity

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// This file ports the TS Agent API golden-fixture tests in
// perplexity-language-model.test.ts ("parses a captured Agent API web search
// response" and "parses captured Agent API web search events") against the
// same fixtures used upstream (packages/perplexity/src/__fixtures__/), copied
// verbatim into testdata/. See testdata's copy of
// packages/perplexity/src/__fixtures__/README.md for provenance: captured
// from the live Agent API on 2026-09-25 with the "fast" preset for the
// prompt "Find the official TypeScript website and describe TypeScript in
// one sentence with a citation."

func loadPerplexityFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", name, err)
	}
	return data
}

// TS: "parses a captured Agent API web search response"
func TestPerplexityDoGenerate_FixtureWebSearchResponse(t *testing.T) {
	fixture := loadPerplexityFixture(t, "agent-web-search.json")

	var fixtureMap map[string]interface{}
	if err := json.Unmarshal(fixture, &fixtureMap); err != nil {
		t.Fatalf("failed to parse fixture JSON: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-token"})
	model := NewLanguageModel(prov, "low")

	result, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}

	var sawSource bool
	for _, part := range result.Content {
		if s, ok := part.(types.SourceContent); ok && s.SourceType == "url" && s.URL == "https://www.typescriptlang.org/" {
			sawSource = true
		}
	}
	if !sawSource {
		t.Fatalf("expected a source content part for https://www.typescriptlang.org/, got %+v", result.Content)
	}

	var sawTypeScriptText bool
	for _, part := range result.Content {
		if text, ok := part.(types.TextContent); ok && strings.Contains(text.Text, "TypeScript") {
			sawTypeScriptText = true
		}
	}
	if !sawTypeScriptText {
		t.Fatalf("expected text content containing 'TypeScript', got %+v", result.Content)
	}

	wantUsageRaw, ok := fixtureMap["usage"].(map[string]interface{})
	if !ok {
		t.Fatalf("fixture usage missing or malformed: %v", fixtureMap["usage"])
	}
	if result.Usage.Raw == nil {
		t.Fatal("result.Usage.Raw is nil")
	}
	assertJSONEqual(t, "usage.raw", result.Usage.Raw, wantUsageRaw)

	if result.FinishReason != types.FinishReasonStop {
		t.Fatalf("FinishReason = %v, want stop", result.FinishReason)
	}
}

// TS: "parses captured Agent API web search events"
func TestPerplexityDoStream_FixtureWebSearchEvents(t *testing.T) {
	raw := loadPerplexityFixture(t, "agent-web-search.chunks.txt")

	var events []map[string]interface{}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("failed to parse fixture line as JSON: %v", err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("fixture produced no events")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range events {
			b, _ := json.Marshal(event)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-token"})
	model := NewLanguageModel(prov, "low")

	stream, err := model.DoStream(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer func() { _ = stream.Close() }()
	chunks := collectChunks(t, stream)

	for _, c := range chunks {
		if c.Type == provider.ChunkTypeError {
			t.Fatalf("unexpected error chunk: %+v", c)
		}
	}

	var gotText strings.Builder
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeText {
			gotText.WriteString(c.Text)
		}
	}

	// Mirrors TS: find the response.completed event and reconstruct the
	// expected text from its response.output[].content[] output_text parts.
	var completedResponse map[string]interface{}
	for _, event := range events {
		if event["type"] == "response.completed" {
			completedResponse, _ = event["response"].(map[string]interface{})
			break
		}
	}
	if completedResponse == nil {
		t.Fatal("fixture has no response.completed event")
	}
	var wantText strings.Builder
	for _, item := range completedResponse["output"].([]interface{}) {
		itemMap, ok := item.(map[string]interface{})
		if !ok || itemMap["type"] != "message" {
			continue
		}
		content, ok := itemMap["content"].([]interface{})
		if !ok {
			continue
		}
		for _, part := range content {
			partMap, ok := part.(map[string]interface{})
			if !ok || partMap["type"] != "output_text" {
				continue
			}
			if text, ok := partMap["text"].(string); ok {
				wantText.WriteString(text)
			}
		}
	}
	if gotText.String() != wantText.String() {
		t.Fatalf("streamed text = %q, want %q", gotText.String(), wantText.String())
	}

	var sourceURLs []string
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeSource && c.SourceContent != nil && c.SourceContent.SourceType == "url" {
			sourceURLs = append(sourceURLs, c.SourceContent.URL)
		}
	}
	if len(sourceURLs) == 0 {
		t.Fatal("expected at least one source chunk")
	}
	seen := map[string]bool{}
	for _, url := range sourceURLs {
		if seen[url] {
			t.Fatalf("source URL %q emitted more than once (want deduped)", url)
		}
		seen[url] = true
	}

	var finish *provider.StreamChunk
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if finish == nil {
		t.Fatal("expected a finish chunk")
	}
	if finish.FinishReason != types.FinishReasonStop || finish.RawFinishReason != "completed" {
		t.Fatalf("finish = %+v, want stop/completed", finish)
	}
}

// assertJSONEqual compares two JSON-decoded values (maps/slices/scalars) via
// round-tripping through json.Marshal, avoiding false negatives from map key
// ordering while still catching real differences.
func assertJSONEqual(t *testing.T, label string, got, want interface{}) {
	t.Helper()
	gotBytes, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("%s: marshal got: %v", label, err)
	}
	wantBytes, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("%s: marshal want: %v", label, err)
	}
	var gotNorm, wantNorm interface{}
	_ = json.Unmarshal(gotBytes, &gotNorm)
	_ = json.Unmarshal(wantBytes, &wantNorm)
	gotCanon, _ := json.Marshal(gotNorm)
	wantCanon, _ := json.Marshal(wantNorm)
	if string(gotCanon) != string(wantCanon) {
		t.Fatalf("%s mismatch:\n got:  %s\n want: %s", label, gotCanon, wantCanon)
	}
}
