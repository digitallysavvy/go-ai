package gemini

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

var markdownURIPattern = regexp.MustCompile(`\.(md|markdown)$`)

// groundingChunk mirrors the groundingChunks entries of the Gemini
// groundingMetadata object that carry source information.
type groundingChunk struct {
	Web *struct {
		URI   string  `json:"uri"`
		Title *string `json:"title"`
	} `json:"web"`
	Image *struct {
		SourceURI string  `json:"sourceUri"`
		Title     *string `json:"title"`
	} `json:"image"`
	RetrievedContext *struct {
		URI             *string `json:"uri"`
		Title           *string `json:"title"`
		FileSearchStore *string `json:"fileSearchStore"`
	} `json:"retrievedContext"`
	Maps *struct {
		URI   *string `json:"uri"`
		Title *string `json:"title"`
	} `json:"maps"`
}

func lastPathSegment(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// extractSources converts groundingMetadata.groundingChunks into source parts.
// Mirrors TS extractSources (google-language-model.ts): web, image,
// retrievedContext (http(s) URI -> url source, other URI or fileSearchStore ->
// document source) and maps chunks. Returns nil when there are none.
func extractSources(groundingMetadata json.RawMessage, generateID func() string) []types.SourceContent {
	return extractSourcesDedup(groundingMetadata, generateID, nil)
}

// extractSourcesDedup is TS extractSources with emittedSourceKeys: a source
// whose key (url, document uri, or file-search store + title) was already
// emitted is skipped (its id is still generated, as in TS). A nil emitted
// map keeps every source.
func extractSourcesDedup(groundingMetadata json.RawMessage, generateID func() string, emitted map[string]bool) []types.SourceContent {
	if len(groundingMetadata) == 0 {
		return nil
	}
	var meta struct {
		GroundingChunks []groundingChunk `json:"groundingChunks"`
	}
	if err := json.Unmarshal(groundingMetadata, &meta); err != nil {
		return nil
	}
	var sources []types.SourceContent
	add := func(src types.SourceContent, key ...string) {
		k := strings.Join(key, "\x00")
		if emitted != nil {
			if emitted[k] {
				return
			}
			emitted[k] = true
		}
		sources = append(sources, src)
	}
	for _, chunk := range meta.GroundingChunks {
		switch {
		case chunk.Web != nil:
			add(types.SourceContent{SourceType: "url", ID: generateID(), URL: chunk.Web.URI, Title: deref(chunk.Web.Title)}, "url", chunk.Web.URI)
		case chunk.Image != nil:
			// Google requires attribution to the source URI, not the image URI.
			add(types.SourceContent{SourceType: "url", ID: generateID(), URL: chunk.Image.SourceURI, Title: deref(chunk.Image.Title)}, "url", chunk.Image.SourceURI)
		case chunk.RetrievedContext != nil:
			rc := chunk.RetrievedContext
			uri := deref(rc.URI)
			switch {
			case strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://"):
				add(types.SourceContent{SourceType: "url", ID: generateID(), URL: uri, Title: deref(rc.Title)}, "url", uri)
			case uri != "":
				title := "Unknown Document"
				if rc.Title != nil {
					title = *rc.Title
				}
				mediaType := "application/octet-stream"
				switch {
				case strings.HasSuffix(uri, ".pdf"):
					mediaType = "application/pdf"
				case strings.HasSuffix(uri, ".txt"):
					mediaType = "text/plain"
				case strings.HasSuffix(uri, ".docx"):
					mediaType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
				case strings.HasSuffix(uri, ".doc"):
					mediaType = "application/msword"
				case markdownURIPattern.MatchString(uri):
					mediaType = "text/markdown"
				}
				add(types.SourceContent{SourceType: "document", ID: generateID(), MediaType: mediaType, Title: title, Filename: lastPathSegment(uri)}, "document", uri)
			case deref(rc.FileSearchStore) != "":
				title := "Unknown Document"
				if rc.Title != nil {
					title = *rc.Title
				}
				add(types.SourceContent{SourceType: "document", ID: generateID(), MediaType: "application/octet-stream", Title: title, Filename: lastPathSegment(*rc.FileSearchStore)}, "file-search", *rc.FileSearchStore, title)
			}
		case chunk.Maps != nil:
			if uri := deref(chunk.Maps.URI); uri != "" {
				add(types.SourceContent{SourceType: "url", ID: generateID(), URL: uri, Title: deref(chunk.Maps.Title)}, "url", uri)
			}
		}
	}
	return sources
}
