package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// stream-text.test.ts describe('raw chunks forwarding').

func rawChunkModel(captured **provider.GenerateOptions) *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
			if captured != nil {
				*captured = o
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeStreamStart},
				{Type: provider.ChunkTypeRaw, Raw: map[string]interface{}{"type": "raw-data", "content": "should appear"}},
				{Type: provider.ChunkTypeTextStart, ID: "1"},
				{Type: provider.ChunkTypeText, ID: "1", Text: "Hello, world!"},
				{Type: provider.ChunkTypeTextEnd, ID: "1"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: usage(3, 10)},
			}), nil
		},
	}
}

func TestTSReplay_StreamText_RawChunksForwarding(t *testing.T) {
	cases := []struct {
		name         string
		opts         StreamTextOptions
		wantRaw      int
		wantIncluded bool
	}{
		{"include.rawChunks true", StreamTextOptions{Include: &IncludeOptions{RawChunks: boolPtr(true)}}, 1, true},
		{"include.rawChunks false", StreamTextOptions{Include: &IncludeOptions{RawChunks: boolPtr(false)}}, 0, false},
		{"default", StreamTextOptions{}, 0, false},
		{"deprecated IncludeRawChunks fallback", StreamTextOptions{IncludeRawChunks: true}, 1, true},
		{"include.rawChunks false wins over deprecated", StreamTextOptions{Include: &IncludeOptions{RawChunks: boolPtr(false)}, IncludeRawChunks: true}, 0, false},
		{"ExperimentalInclude alias", StreamTextOptions{ExperimentalInclude: &IncludeOptions{RawChunks: boolPtr(true)}}, 1, true},
		{"include wins over ExperimentalInclude", StreamTextOptions{Include: &IncludeOptions{RawChunks: boolPtr(false)}, ExperimentalInclude: &IncludeOptions{RawChunks: boolPtr(true)}}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var captured *provider.GenerateOptions
			var onChunkRaw int
			opts := tc.opts
			opts.Model = rawChunkModel(&captured)
			opts.Prompt = "test prompt"
			opts.OnChunk = func(c provider.StreamChunk) {
				if c.Type == provider.ChunkTypeRaw {
					onChunkRaw++
				}
			}
			result, err := StreamText(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			raw := 0
			s := result.Stream()
			for {
				c, err := s.Next()
				if err != nil {
					break
				}
				if c.Type == provider.ChunkTypeRaw {
					raw++
				}
			}
			if raw != tc.wantRaw || onChunkRaw != tc.wantRaw {
				t.Fatalf("raw chunks: stream=%d onChunk=%d, want %d", raw, onChunkRaw, tc.wantRaw)
			}
			if captured == nil || captured.IncludeRawChunks != tc.wantIncluded {
				t.Fatalf("model IncludeRawChunks = %v, want %v", captured != nil && captured.IncludeRawChunks, tc.wantIncluded)
			}
		})
	}
}
