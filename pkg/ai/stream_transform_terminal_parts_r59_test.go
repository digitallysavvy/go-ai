package ai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS streamText: the `error` parts a step enqueues (an incomplete model
// stream, a later step that fails to open or prepare) and the outer stream's
// `abort` part all pass through experimental_transform, which sits after the
// outer stream. Go sent them straight to the result streams.
func TestR59StreamTextTerminalPartsPassThroughTransforms(t *testing.T) {
	tool := []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "r", nil
	}}}
	toolStep := func() provider.TextStream {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "1", ToolName: "t", Arguments: map[string]interface{}{}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		})
	}
	cases := []struct {
		name  string
		build func(cancel context.CancelFunc) StreamTextOptions
		want  provider.ChunkType
	}{
		{
			name: "incomplete model stream",
			build: func(context.CancelFunc) StreamTextOptions {
				return StreamTextOptions{Prompt: "a", Model: &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
					return testutil.NewMockTextStream(nil), nil
				}}}
			},
			want: provider.ChunkTypeError,
		},
		{
			name: "next model call fails to open",
			build: func(context.CancelFunc) StreamTextOptions {
				calls := 0
				return StreamTextOptions{Prompt: "a", Tools: tool, StopWhen: []StopCondition{IsStepCount(3)}, Model: &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
					calls++
					if calls == 1 {
						return toolStep(), nil
					}
					return nil, errors.New("boom")
				}}}
			},
			want: provider.ChunkTypeError,
		},
		{
			name: "next step preparation fails",
			build: func(context.CancelFunc) StreamTextOptions {
				return StreamTextOptions{Prompt: "a", Tools: tool, StopWhen: []StopCondition{IsStepCount(3)},
					Model: &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
						return toolStep(), nil
					}},
					PrepareStep: func(_ context.Context, o PrepareStepOptions) PrepareStepOptions {
						if o.StepNumber == 1 {
							zero := 0
							return PrepareStepOptions{MaxOutputTokens: &zero}
						}
						return PrepareStepOptions{}
					}}
			},
			want: provider.ChunkTypeError,
		},
		{
			name: "first model call fails to open",
			build: func(context.CancelFunc) StreamTextOptions {
				return StreamTextOptions{Prompt: "a", Model: &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
					return nil, errors.New("boom")
				}}}
			},
			want: provider.ChunkTypeError,
		},
		{
			name: "first step preparation fails",
			build: func(context.CancelFunc) StreamTextOptions {
				return StreamTextOptions{Prompt: "a", Model: &testutil.MockLanguageModel{}, PrepareStep: func(context.Context, PrepareStepOptions) PrepareStepOptions {
					zero := 0
					return PrepareStepOptions{MaxOutputTokens: &zero}
				}}
			},
			want: provider.ChunkTypeError,
		},
		{
			name: "abort before the first stream",
			build: func(cancel context.CancelFunc) StreamTextOptions {
				return StreamTextOptions{Prompt: "a", Model: &testutil.MockLanguageModel{DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
					cancel()
					return nil, ctx.Err()
				}}}
			},
			want: provider.ChunkTypeAbort,
		},
		{
			name: "abort while opening the next step",
			build: func(cancel context.CancelFunc) StreamTextOptions {
				calls := 0
				return StreamTextOptions{Prompt: "a", Tools: tool, StopWhen: []StopCondition{IsStepCount(3)}, Model: &testutil.MockLanguageModel{DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
					calls++
					if calls == 1 {
						return toolStep(), nil
					}
					cancel()
					return nil, ctx.Err()
				}}}
			},
			want: provider.ChunkTypeAbort,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			opts := tc.build(cancel)
			var mu sync.Mutex
			var seen []string
			opts.ExperimentalTransform = []StreamTransformFunc{func(_ context.Context, c provider.StreamChunk) []provider.StreamChunk {
				mu.Lock()
				seen = append(seen, string(c.Type))
				mu.Unlock()
				return []provider.StreamChunk{c}
			}}
			res, err := StreamText(ctx, opts)
			if err != nil {
				t.Fatal(err)
			}
			parts, _ := r58FullStreamParts(t, res)
			if !strings.Contains(parts, string(tc.want)) {
				t.Fatalf("full stream = %q, want a %s part", parts, tc.want)
			}
			mu.Lock()
			defer mu.Unlock()
			if !strings.Contains(strings.Join(seen, " "), string(tc.want)) {
				t.Fatalf("transform saw %q, want the %s part", seen, tc.want)
			}
		})
	}
}
