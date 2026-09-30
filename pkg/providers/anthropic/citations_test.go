package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// citationEnabledFilePart builds a citation-enabled input file part, matching
// TS's `{type: 'file', ..., providerOptions: {anthropic: {citations: {enabled:
// true}}}}`.
func citationEnabledFilePart(mediaType, filename string) types.FileContent {
	return types.FileContent{
		MediaType: mediaType,
		Filename:  filename,
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{
				"citations": map[string]interface{}{"enabled": true},
			},
		},
	}
}

// TestDoGenerate_PDFCitations ports TS "should process PDF citation
// responses": a page_location citation against a citation-enabled PDF file
// part resolves to a document SourceContent part, with no providerMetadata on
// the text part itself (page_location isn't a web-search citation).
func TestDoGenerate_PDFCitations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_017TfcQ4AgGxKyBduUpqYPZn",
			"type": "message",
			"role": "assistant",
			"model": "claude-3-haiku-20240307",
			"content": [
				{
					"type": "text",
					"text": "Based on the document, the results show positive growth.",
					"citations": [
						{
							"type": "page_location",
							"cited_text": "Revenue increased by 25% year over year",
							"document_index": 0,
							"document_title": "Financial Report 2023",
							"start_page_number": 5,
							"end_page_number": 6
						}
					]
				}
			],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 4, "output_tokens": 30}
		}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	model, err := p.LanguageModel(ClaudeSonnet4_5)
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					citationEnabledFilePart("application/pdf", "financial-report.pdf"),
					types.TextContent{Text: "What do the results show?"},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}

	if len(result.Content) != 2 {
		t.Fatalf("result.Content = %#v, want 2 parts (text, source)", result.Content)
	}
	textPart, ok := result.Content[0].(types.TextContent)
	if !ok {
		t.Fatalf("Content[0] = %#v, want TextContent", result.Content[0])
	}
	if textPart.Text != "Based on the document, the results show positive growth." {
		t.Errorf("text = %q", textPart.Text)
	}
	if textPart.ProviderMetadata != nil {
		t.Errorf("text providerMetadata = %s, want nil (page_location isn't a web-search citation)", textPart.ProviderMetadata)
	}

	src, ok := result.Content[1].(types.SourceContent)
	if !ok {
		t.Fatalf("Content[1] = %#v, want SourceContent", result.Content[1])
	}
	if src.SourceType != "document" {
		t.Errorf("SourceType = %q, want document", src.SourceType)
	}
	if src.MediaType != "application/pdf" {
		t.Errorf("MediaType = %q, want application/pdf", src.MediaType)
	}
	if src.Title != "Financial Report 2023" {
		t.Errorf("Title = %q, want Financial Report 2023", src.Title)
	}
	if src.Filename != "financial-report.pdf" {
		t.Errorf("Filename = %q, want financial-report.pdf", src.Filename)
	}
	if src.ID == "" {
		t.Error("ID should be generated")
	}
	var meta map[string]map[string]interface{}
	if err := json.Unmarshal(src.ProviderMetadata, &meta); err != nil {
		t.Fatalf("decode source providerMetadata: %v", err)
	}
	if meta["anthropic"]["citedText"] != "Revenue increased by 25% year over year" {
		t.Errorf("citedText = %v", meta["anthropic"]["citedText"])
	}
	if meta["anthropic"]["startPageNumber"] != float64(5) {
		t.Errorf("startPageNumber = %v, want 5", meta["anthropic"]["startPageNumber"])
	}
	if meta["anthropic"]["endPageNumber"] != float64(6) {
		t.Errorf("endPageNumber = %v, want 6", meta["anthropic"]["endPageNumber"])
	}
}

// TestDoGenerate_TextCitations ports TS "should process text citation
// responses": a char_location citation against a citation-enabled text/plain
// file part resolves to a document SourceContent part.
func TestDoGenerate_TextCitations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_017TfcQ4AgGxKyBduUpqYPZn",
			"type": "message",
			"role": "assistant",
			"model": "claude-3-haiku-20240307",
			"content": [
				{
					"type": "text",
					"text": "The text shows important information.",
					"citations": [
						{
							"type": "char_location",
							"cited_text": "important information",
							"document_index": 0,
							"document_title": "Test Document",
							"start_char_index": 15,
							"end_char_index": 35
						}
					]
				}
			],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 4, "output_tokens": 30}
		}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	model, err := p.LanguageModel(ClaudeSonnet4_5)
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					citationEnabledFilePart("text/plain", "test.txt"),
					types.TextContent{Text: "What does this say?"},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}

	if len(result.Content) != 2 {
		t.Fatalf("result.Content = %#v, want 2 parts (text, source)", result.Content)
	}
	src, ok := result.Content[1].(types.SourceContent)
	if !ok {
		t.Fatalf("Content[1] = %#v, want SourceContent", result.Content[1])
	}
	if src.Title != "Test Document" {
		t.Errorf("Title = %q, want Test Document", src.Title)
	}
	if src.MediaType != "text/plain" {
		t.Errorf("MediaType = %q, want text/plain", src.MediaType)
	}
	var meta map[string]map[string]interface{}
	if err := json.Unmarshal(src.ProviderMetadata, &meta); err != nil {
		t.Fatalf("decode source providerMetadata: %v", err)
	}
	if meta["anthropic"]["startCharIndex"] != float64(15) {
		t.Errorf("startCharIndex = %v, want 15", meta["anthropic"]["startCharIndex"])
	}
	if meta["anthropic"]["endCharIndex"] != float64(35) {
		t.Errorf("endCharIndex = %v, want 35", meta["anthropic"]["endCharIndex"])
	}
}

// TestDoGenerate_WebSearchCitations verifies a web_search_result_location
// citation both (a) produces a url SourceContent part and (b) is kept on the
// text part's providerMetadata.anthropic.citations (TS's webSearchCitations
// filter — unlike page_location/char_location, which are never mirrored onto
// the text part).
func TestDoGenerate_WebSearchCitations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_web_search_citation",
			"type": "message",
			"role": "assistant",
			"model": "claude-3-haiku-20240307",
			"content": [
				{
					"type": "text",
					"text": "Paris is sunny.",
					"citations": [
						{
							"type": "web_search_result_location",
							"cited_text": "Paris is sunny.",
							"url": "https://example.com/weather",
							"title": "Paris weather",
							"encrypted_index": "encrypted-index"
						}
					]
				}
			],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 4, "output_tokens": 10}
		}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	model, err := p.LanguageModel(ClaudeSonnet4_5)
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "What's the weather in Paris?"},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}

	if len(result.Content) != 2 {
		t.Fatalf("result.Content = %#v, want 2 parts (text, source)", result.Content)
	}
	textPart, ok := result.Content[0].(types.TextContent)
	if !ok {
		t.Fatalf("Content[0] = %#v, want TextContent", result.Content[0])
	}
	var textMeta map[string]struct {
		Citations []map[string]interface{} `json:"citations"`
	}
	if err := json.Unmarshal(textPart.ProviderMetadata, &textMeta); err != nil {
		t.Fatalf("decode text providerMetadata: %v", err)
	}
	if len(textMeta["anthropic"].Citations) != 1 {
		t.Fatalf("text citations = %#v, want 1 entry", textMeta["anthropic"].Citations)
	}
	if textMeta["anthropic"].Citations[0]["type"] != "web_search_result_location" {
		t.Errorf("citation type = %v", textMeta["anthropic"].Citations[0]["type"])
	}

	src, ok := result.Content[1].(types.SourceContent)
	if !ok {
		t.Fatalf("Content[1] = %#v, want SourceContent", result.Content[1])
	}
	if src.SourceType != "url" || src.URL != "https://example.com/weather" {
		t.Errorf("source = %+v", src)
	}
}

// TestStream_PDFCitations ports TS "should process PDF citation responses in
// streaming": a citations_delta event (even arriving after the owning text
// block's content_block_stop, as in the TS fixture) still produces a source
// chunk, and the preceding text-start/text-delta/text-end chunks carry no
// citations metadata (page_location isn't a web-search citation).
func TestStream_PDFCitations(t *testing.T) {
	sseData := "" +
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_01KfpJoAEabmH2iHRRFjQMAG\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-3-haiku-20240307\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":17,\"output_tokens\":1}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Based on the document\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\", results show growth.\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"citations_delta\",\"citation\":{\"type\":\"page_location\",\"cited_text\":\"Revenue increased by 25% year over year\",\"document_index\":0,\"document_title\":\"Financial Report 2023\",\"start_page_number\":5,\"end_page_number\":6}}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":227}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)
	stream.citationDocuments = extractCitationDocuments([]types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				citationEnabledFilePart("application/pdf", "financial-report.pdf"),
			},
		},
	})

	var chunks []*provider.StreamChunk
	for {
		c, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next() error: %v", err)
		}
		chunks = append(chunks, c)
	}

	var sawTextStart, sawTextEnd bool
	var src *types.SourceContent
	for _, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeTextStart:
			sawTextStart = true
			if c.ProviderMetadata != nil {
				t.Errorf("text-start providerMetadata = %s, want nil", c.ProviderMetadata)
			}
		case provider.ChunkTypeTextEnd:
			sawTextEnd = true
			if c.ProviderMetadata != nil {
				t.Errorf("text-end providerMetadata = %s, want nil (page_location isn't web-search)", c.ProviderMetadata)
			}
		case provider.ChunkTypeSource:
			src = c.SourceContent
		}
	}
	if !sawTextStart || !sawTextEnd {
		t.Fatalf("expected text-start and text-end chunks, got %#v", chunks)
	}
	if src == nil {
		t.Fatal("expected a source chunk for the page_location citation")
	}
	if src.SourceType != "document" || src.Title != "Financial Report 2023" {
		t.Errorf("source = %+v", src)
	}
	if src.Filename != "financial-report.pdf" {
		t.Errorf("Filename = %q, want financial-report.pdf", src.Filename)
	}
}

// webFetchResultContent builds the JSON payload of a successful
// web_fetch_tool_result's "content" field.
func webFetchResultContent(title, mediaType string) string {
	return `{"type":"web_fetch_result","url":"https://example.com/report","retrieved_at":"2026-01-01T00:00:00Z",` +
		`"content":{"type":"document","title":"` + title + `","source":{"type":"base64","media_type":"` + mediaType + `","data":"abc"}}}`
}

// TestDoGenerate_WebFetchCitationDocument verifies that a web_fetch_tool_result
// block contributes a citation document *in response-content order*, so a
// later page_location/char_location citation in the same response can resolve
// against a document the model just fetched (not just documents from the
// original prompt). Mirrors TS's inline `citationDocuments.push(...)` inside
// the single ordered `for (const part of response.content)` loop
// (anthropic-language-model.ts:1463); there is no TS unit test for this exact
// combination, so this locks in behavior derived directly from the TS source.
func TestDoGenerate_WebFetchCitationDocument(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_1",
			"type": "message",
			"role": "assistant",
			"model": "claude-3-haiku-20240307",
			"content": [
				{
					"type": "web_fetch_tool_result",
					"tool_use_id": "toolu_1",
					"content": ` + webFetchResultContent("Fetched Report", "application/pdf") + `
				},
				{
					"type": "text",
					"text": "The report shows growth.",
					"citations": [
						{
							"type": "page_location",
							"cited_text": "growth",
							"document_index": 0,
							"start_page_number": 1,
							"end_page_number": 2
						}
					]
				}
			],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 4, "output_tokens": 10}
		}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	model, err := p.LanguageModel(ClaudeSonnet4_5)
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Fetch and cite the report"}}},
		}},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}

	var src *types.SourceContent
	for _, c := range result.Content {
		if s, ok := c.(types.SourceContent); ok {
			src = &s
			break
		}
	}
	if src == nil {
		t.Fatalf("no SourceContent in result.Content = %#v", result.Content)
	}
	if src.SourceType != "document" {
		t.Errorf("SourceType = %q, want document", src.SourceType)
	}
	if src.Title != "Fetched Report" {
		t.Errorf("Title = %q, want Fetched Report (from the web-fetched document, not the prompt)", src.Title)
	}
	if src.MediaType != "application/pdf" {
		t.Errorf("MediaType = %q, want application/pdf", src.MediaType)
	}
}

// TestStream_WebFetchCitationDocument is the streaming counterpart of
// TestDoGenerate_WebFetchCitationDocument: the web_fetch_tool_result arrives
// pre-populated in message_start (as it does after a compaction/resume), and
// the citing text block streams live afterward. The citation document list
// must grow from the pre-populated tool result before the live text block's
// citations_delta is processed, mirroring TS's single ordered content loop.
func TestStream_WebFetchCitationDocument(t *testing.T) {
	sseData := "" +
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"web_fetch_tool_result\",\"tool_use_id\":\"toolu_1\",\"content\":" + webFetchResultContent("Fetched Report", "application/pdf") + "}],\"model\":\"claude-3-haiku-20240307\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":17,\"output_tokens\":1}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"The report shows growth.\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"citations_delta\",\"citation\":{\"type\":\"page_location\",\"cited_text\":\"growth\",\"document_index\":0,\"start_page_number\":1,\"end_page_number\":2}}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":30}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)
	// No citation-enabled prompt documents: the cited document (index 0) comes
	// solely from the pre-populated web_fetch_tool_result.
	stream.citationDocuments = nil

	var src *types.SourceContent
	for {
		c, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next() error: %v", err)
		}
		if c.Type == provider.ChunkTypeSource {
			src = c.SourceContent
		}
	}
	if src == nil {
		t.Fatal("expected a source chunk for the page_location citation against the web-fetched document")
	}
	if src.SourceType != "document" {
		t.Errorf("SourceType = %q, want document", src.SourceType)
	}
	if src.Title != "Fetched Report" {
		t.Errorf("Title = %q, want Fetched Report", src.Title)
	}
	if src.MediaType != "application/pdf" {
		t.Errorf("MediaType = %q, want application/pdf", src.MediaType)
	}
}
