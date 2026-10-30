package ai

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS streamText pipes every run through createOutputTransformStream(output ??
// text(), retention.replay || output != null): without an output spec the
// partial output is the step's text so far (text().parsePartialOutput), and a
// low-memory run without a spec publishes none.
func TestR70StreamTextPartialOutputWithoutSpecIsText(t *testing.T) {
	chunks := func() []provider.StreamChunk {
		return []provider.StreamChunk{
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "Hel"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "lo"},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}
	}
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream(chunks()), nil
	}}

	t.Run("default retention", func(t *testing.T) {
		r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.ConsumeStream(); err != nil {
			t.Fatal(err)
		}
		if got := r.PartialOutput(); got != "Hello" {
			t.Fatalf("PartialOutput() = %#v, want %q", got, "Hello")
		}
	})

	t.Run("low memory", func(t *testing.T) {
		r, err := StreamTextLowMemory(context.Background(), StreamTextOptions{Model: model, Prompt: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.ConsumeStream(); err != nil {
			t.Fatal(err)
		}
		if got := r.PartialOutput(); got != nil {
			t.Fatalf("low-memory PartialOutput() = %#v, want nil", got)
		}
	})
}

// TS streamText skips onChunk/onError downstream only for the error value the
// model stream reader already reported (errorsHandledForStreamRetry holds
// value.error): an error part a transform replaces with a different error
// reaches onChunk and onError again. Without an Err the part's error value is
// its text.
func TestR70StreamTextTransformedErrorPartReachesOnChunkAgain(t *testing.T) {
	errA, errB := errors.New("a"), errors.New("b")
	cases := []struct {
		name    string
		in, out provider.StreamChunk
		wantLog string
	}{
		{
			name:    "text only",
			in:      provider.StreamChunk{Type: provider.ChunkTypeError, Text: "a"},
			out:     provider.StreamChunk{Type: provider.ChunkTypeError, Text: "b"},
			wantLog: "chunk:error:a onError:a chunk:error:b onError:b",
		},
		{
			name:    "error values",
			in:      provider.StreamChunk{Type: provider.ChunkTypeError, Text: "a", Err: errA},
			out:     provider.StreamChunk{Type: provider.ChunkTypeError, Text: "b", Err: errB},
			wantLog: "chunk:error:a onError:a chunk:error:b onError:b",
		},
		{
			name:    "same text passes through",
			in:      provider.StreamChunk{Type: provider.ChunkTypeError, Text: "a"},
			out:     provider.StreamChunk{Type: provider.ChunkTypeError, Text: "a"},
			wantLog: "chunk:error:a onError:a",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var log r69EventLog
			opts := log.options(StreamTextOptions{
				Model: r69Model(r69Stream(tc.in, provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonError})),
				ExperimentalTransform: []StreamTransformFunc{func(_ context.Context, c provider.StreamChunk) []provider.StreamChunk {
					if c.Type == provider.ChunkTypeError {
						return []provider.StreamChunk{tc.out}
					}
					return []provider.StreamChunk{c}
				}},
			})
			r, err := StreamText(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			_ = r.ConsumeStream()
			got := strings.TrimPrefix(log.String(), "chunk:start ")
			if got != tc.wantLog {
				t.Fatalf("events = %q, want %q", got, tc.wantLog)
			}
		})
	}
}
