package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// A downloaded media type wins over magic-byte detection and the audio/wav
// fallback (TS: downloadedMediaType ?? detectMediaType(...) ?? 'audio/wav').
func TestTranscribeDownloadedMediaTypeWins(t *testing.T) {
	var got string
	model := &testutil.MockTranscriptionModel{
		DoTranscribeFunc: func(_ context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
			got = opts.MimeType
			return &types.TranscriptionResult{Text: "t", Response: &types.ResponseMetadata{ModelID: "m"}}, nil
		},
	}
	_, err := Transcribe(context.Background(), TranscribeOptions{
		Model:    model,
		AudioURL: "https://example.com/a",
		DownloadWithMetadata: func(context.Context, string) (*DownloadResult, error) {
			return &DownloadResult{Data: []byte("not-a-known-signature"), MediaType: "audio/L16"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "audio/L16" {
		t.Fatalf("media type = %q, want audio/L16", got)
	}
}
