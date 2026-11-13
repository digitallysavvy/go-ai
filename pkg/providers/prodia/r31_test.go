package prodia

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS getArgs: both parts non-empty and Number() finite; zero, negative and
// fractional values are accepted, anything else is an invalid size.
func TestParseProdiaSizeMatchesTS(t *testing.T) {
	for in, want := range map[string]bool{
		"1024x768": true, "0x0": true, "-5x10": true, "1024.5x768": true, "1e3x2": true, " 10 x 20 ": true,
		"1024": false, "x768": false, "1024x": false, "axb": false, "10x20x30": true, "0x1Fx2": false,
	} {
		if _, _, ok := parseProdiaSize(in); ok != want {
			t.Errorf("parseProdiaSize(%q) ok = %v, want %v", in, ok, want)
		}
	}
}

// TS doStream emits response-metadata (modelId, timestamp) right after
// stream-start.
func TestLanguageDoStreamEmitsResponseMetadata(t *testing.T) {
	body, contentType := buildTestMultipartBody(`{"id":"job-1"}`, []byte("hello"), "text/plain")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	m := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), LanguageModelNanoBananaImgToImgV2)
	stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	var order []provider.ChunkType
	var meta *provider.ResponseMetadata
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		order = append(order, c.Type)
		if c.Type == provider.ChunkTypeResponseMetadata {
			meta = c.ResponseMetadata
		}
	}
	if len(order) < 2 || order[0] != provider.ChunkTypeStreamStart || order[1] != provider.ChunkTypeResponseMetadata {
		t.Fatalf("chunk order = %v", order)
	}
	if meta == nil || meta.ModelID != LanguageModelNanoBananaImgToImgV2 || meta.Timestamp.IsZero() {
		t.Fatalf("response metadata = %+v", meta)
	}
}
