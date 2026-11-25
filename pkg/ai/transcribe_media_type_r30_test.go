package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// A downloaded audio media type wins over magic-byte detection and the
// audio/wav fallback, lower-cased (TS 5d18351e1e getAudioMediaType).
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
	if got != "audio/l16" {
		t.Fatalf("media type = %q, want audio/l16", got)
	}
}

// TS passes empty audio through to the model (default media type audio/wav).
func TestTranscribeAllowsEmptyAudio(t *testing.T) {
	var called bool
	var got string
	model := &testutil.MockTranscriptionModel{
		DoTranscribeFunc: func(_ context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
			called = true
			got = opts.MimeType
			return &types.TranscriptionResult{Text: "", Response: &types.ResponseMetadata{ModelID: "m"}}, nil
		},
	}
	if _, err := Transcribe(context.Background(), TranscribeOptions{Model: model, Audio: []byte{}}); err != nil {
		t.Fatal(err)
	}
	if !called || got != "audio/wav" {
		t.Fatalf("called=%v mediaType=%q", called, got)
	}
	if _, err := Transcribe(context.Background(), TranscribeOptions{Model: model}); err == nil {
		t.Fatal("expected error when no audio source is given")
	}
}
