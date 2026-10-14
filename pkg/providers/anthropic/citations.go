package anthropic

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// citationDocument is the reduced shape extracted from citation-enabled file
// parts in the request prompt (TS extractCitationDocuments' return type).
type citationDocument struct {
	Title     string
	Filename  string
	MediaType string
}

// extractCitationDocuments scans the user messages of a prompt for file parts
// that opted into citations (providerOptions.anthropic.citations.enabled) and
// have a citable media type (application/pdf or text/plain), returning them in
// prompt order. Mirrors TS anthropic-language-model.ts
// AnthropicMessagesLanguageModel#extractCitationDocuments.
func extractCitationDocuments(messages []types.Message) []citationDocument {
	var docs []citationDocument
	for _, msg := range messages {
		if msg.Role != types.RoleUser {
			continue
		}
		for _, part := range msg.Content {
			file, ok := asFileContent(part)
			if !ok {
				continue
			}
			if file.MediaType != "application/pdf" && file.MediaType != "text/plain" {
				continue
			}
			if !fileCitationsEnabled(file.ProviderOptions) {
				continue
			}
			title := file.Filename
			if title == "" {
				title = "Untitled Document"
			}
			docs = append(docs, citationDocument{
				Title:     title,
				Filename:  file.Filename,
				MediaType: file.MediaType,
			})
		}
	}
	return docs
}

// asFileContent normalizes a content part to (types.FileContent, true) when it
// is a file part, matching either value or pointer content-part storage.
func asFileContent(part types.ContentPart) (types.FileContent, bool) {
	switch p := part.(type) {
	case types.FileContent:
		return p, true
	case *types.FileContent:
		if p == nil {
			return types.FileContent{}, false
		}
		return *p, true
	default:
		return types.FileContent{}, false
	}
}

// fileCitationsEnabled reports whether providerOptions.anthropic.citations.enabled
// is explicitly true.
func fileCitationsEnabled(providerOptions map[string]interface{}) bool {
	anthropicOpts, ok := providerOptions["anthropic"].(map[string]interface{})
	if !ok {
		return false
	}
	citationsOpts, ok := anthropicOpts["citations"].(map[string]interface{})
	if !ok {
		return false
	}
	enabled, _ := citationsOpts["enabled"].(bool)
	return enabled
}

// citationsProviderMetadata wraps citations as {"anthropic":{"citations":[...]}},
// or returns nil when citations is empty (TS's `...(citations.length > 0 &&
// {providerMetadata: {anthropic: {citations}}})`).
func citationsProviderMetadata(citations []map[string]interface{}) json.RawMessage {
	if len(citations) == 0 {
		return nil
	}
	meta, err := json.Marshal(map[string]interface{}{
		"anthropic": map[string]interface{}{"citations": citations},
	})
	if err != nil {
		return nil
	}
	return meta
}

func citationType(c map[string]interface{}) string {
	t, _ := c["type"].(string)
	return t
}

func citationString(c map[string]interface{}, key string) string {
	v, _ := c[key].(string)
	return v
}

// createCitationSource builds a LanguageModelV4Source-equivalent SourceContent
// for a single raw citation, mirroring TS createCitationSource.
//
//   - web_search_result_location -> sourceType "url", built directly from the
//     citation (no document lookup needed).
//   - page_location / char_location -> sourceType "document", resolved
//     against citationDocuments by document_index; missing/out-of-range
//     documents produce no source (matches TS `documentInfo == null`).
//   - any other type (content_block_location, search_result_location, ...) ->
//     no source, matching TS's exhaustive type guard.
func createCitationSource(citation map[string]interface{}, citationDocuments []citationDocument, generateID func() string) (types.SourceContent, bool) {
	switch citationType(citation) {
	case "web_search_result_location":
		meta, _ := json.Marshal(map[string]interface{}{
			"anthropic": map[string]interface{}{
				"citedText":      citationString(citation, "cited_text"),
				"encryptedIndex": citationString(citation, "encrypted_index"),
			},
		})
		return types.SourceContent{
			SourceType:       "url",
			ID:               generateID(),
			URL:              citationString(citation, "url"),
			Title:            citationString(citation, "title"),
			ProviderMetadata: meta,
		}, true

	case "page_location", "char_location":
		docIndexF, ok := citation["document_index"].(float64)
		if !ok {
			return types.SourceContent{}, false
		}
		docIndex := int(docIndexF)
		if docIndex < 0 || docIndex >= len(citationDocuments) {
			return types.SourceContent{}, false
		}
		doc := citationDocuments[docIndex]

		title := doc.Title
		if dtRaw, exists := citation["document_title"]; exists && dtRaw != nil {
			if dt, ok := dtRaw.(string); ok {
				title = dt
			}
		}

		var anthMeta map[string]interface{}
		if citationType(citation) == "page_location" {
			anthMeta = map[string]interface{}{
				"citedText":       citationString(citation, "cited_text"),
				"startPageNumber": citation["start_page_number"],
				"endPageNumber":   citation["end_page_number"],
			}
		} else {
			anthMeta = map[string]interface{}{
				"citedText":      citationString(citation, "cited_text"),
				"startCharIndex": citation["start_char_index"],
				"endCharIndex":   citation["end_char_index"],
			}
		}
		meta, _ := json.Marshal(map[string]interface{}{"anthropic": anthMeta})

		return types.SourceContent{
			SourceType:       "document",
			ID:               generateID(),
			MediaType:        doc.MediaType,
			Title:            title,
			Filename:         doc.Filename,
			ProviderMetadata: meta,
		}, true

	default:
		return types.SourceContent{}, false
	}
}

// anthropicGenerateID is the fallback ID generator used for citation source
// IDs (TS's per-provider generateId, defaulted to createIdGenerator()).
func anthropicGenerateID() string {
	return streaming.GenerateID()
}

// extractWebFetchCitationDocument builds the citationDocument that a
// successful web_fetch_tool_result contributes to the running citation
// document list, mirroring TS's inline `citationDocuments.push({title:
// part.content.content.title ?? part.content.url, mediaType:
// part.content.content.source.media_type})` (anthropic-language-model.ts:1463,
// 2228). Only the "web_fetch_result" content variant contributes a document;
// error results (content.type !== "web_fetch_result") are skipped, matching
// TS's `if (part.content.type === 'web_fetch_result')` guard. Note: unlike
// extractCitationDocuments, a web-fetched document has no filename.
func extractWebFetchCitationDocument(raw json.RawMessage) (citationDocument, bool) {
	var wire webFetchResultWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return citationDocument{}, false
	}
	if wire.Type != "web_fetch_result" {
		return citationDocument{}, false
	}
	title := wire.Content.Title
	if title == "" {
		title = wire.URL
	}
	return citationDocument{
		Title:     title,
		MediaType: wire.Content.Source.MediaType,
	}, true
}

// mapAnthropicCitations normalizes raw Anthropic citations onto the shared
// types.Citation shape (TS mapAnthropicCitation), resolving indexed document
// citations against documents. It returns nil for no citations.
func mapAnthropicCitations(citations []map[string]interface{}, documents []citationDocument) []types.Citation {
	if len(citations) == 0 {
		return nil
	}
	out := make([]types.Citation, 0, len(citations))
	for _, citation := range citations {
		out = append(out, mapAnthropicCitation(citation, documents))
	}
	return out
}

// mapAnthropicCitation mirrors TS mapAnthropicCitation: web_search_result_location
// and search_result_location become url sources (the id is the URL); every
// other kind becomes a document source whose id is the file_id or the
// document index. The full raw citation is kept under providerMetadata.anthropic.
func mapAnthropicCitation(citation map[string]interface{}, documents []citationDocument) types.Citation {
	meta, _ := json.Marshal(map[string]interface{}{"anthropic": citation})
	citedText := citationString(citation, "cited_text")

	switch citationType(citation) {
	case "web_search_result_location", "search_result_location":
		url := citationString(citation, "url")
		if citationType(citation) == "search_result_location" {
			url = citationString(citation, "source")
		}
		return types.Citation{
			Source: types.SourceContent{
				SourceType:       "url",
				ID:               url,
				URL:              url,
				Title:            citationString(citation, "title"),
				ProviderMetadata: meta,
			},
			CitedText: citedText,
		}
	}

	docIndex := 0
	if f, ok := citation["document_index"].(float64); ok {
		docIndex = int(f)
	}
	var doc *citationDocument
	if docIndex >= 0 && docIndex < len(documents) {
		doc = &documents[docIndex]
	}
	id := citationString(citation, "file_id")
	if id == "" {
		id = strconv.Itoa(docIndex)
	}
	src := types.SourceContent{
		SourceType:       "document",
		ID:               id,
		MediaType:        "text/plain",
		ProviderMetadata: meta,
	}
	title := fmt.Sprintf("Document %d", docIndex)
	if doc != nil {
		src.MediaType = doc.MediaType
		src.Filename = doc.Filename
		title = doc.Title
	}
	if dt, ok := citation["document_title"].(string); ok {
		title = dt
	}
	src.Title = title
	return types.Citation{Source: src, CitedText: citedText}
}

// anthropicResponseHasWebSearchResults reports whether any web_search_tool_result
// block carries a non-empty array of retrieved results.
func anthropicResponseHasWebSearchResults(content []anthropicContent) bool {
	for _, part := range content {
		if part.Type != "web_search_tool_result" {
			continue
		}
		var results []json.RawMessage
		if json.Unmarshal(part.Content, &results) == nil && len(results) > 0 {
			return true
		}
	}
	return false
}
