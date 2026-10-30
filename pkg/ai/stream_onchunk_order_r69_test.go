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

// r69EventLog records OnChunk (start, error and abort parts), OnError and
// OnAbort calls in order.
type r69EventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *r69EventLog) add(e string) {
	l.mu.Lock()
	l.events = append(l.events, e)
	l.mu.Unlock()
}

func (l *r69EventLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.events, " ")
}

func (l *r69EventLog) options(opts StreamTextOptions) StreamTextOptions {
	opts.Prompt = "hi"
	opts.OnChunk = func(c provider.StreamChunk) {
		switch c.Type {
		case provider.ChunkTypeStart, provider.ChunkTypeAbort:
			l.add("chunk:" + string(c.Type))
		case provider.ChunkTypeError:
			l.add("chunk:error:" + c.Text)
		}
	}
	opts.OnError = func(_ context.Context, err error) { l.add("onError:" + err.Error()) }
	opts.OnAbortEvent = func(context.Context, GenerateTextAbortEvent) { l.add("onAbort") }
	return opts
}

func r69Stream(chunks ...provider.StreamChunk) func() (provider.TextStream, error) {
	return func() (provider.TextStream, error) { return testutil.NewMockTextStream(chunks), nil }
}

func r69Model(attempts ...func() (provider.TextStream, error)) *testutil.MockLanguageModel {
	var mu sync.Mutex
	calls := 0
	return &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		i := calls
		calls++
		mu.Unlock()
		if i >= len(attempts) {
			i = len(attempts) - 1
		}
		return attempts[i]()
	}}
}

// TS streamText calls onChunk for an error part before onError: the event
// processor for error parts it adds itself, and the model stream reader
// (before the retry decision) for a model call's error parts, also for one
// that a stream retry replaces. Parts of a run that fails before its first
// model stream (start, error or abort) reach onChunk too.
func TestR69StreamTextOnChunkSeesErrorPartsBeforeOnError(t *testing.T) {
	finish := provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}
	text := provider.StreamChunk{Type: provider.ChunkTypeText, Text: "ok"}
	modelErr := provider.StreamChunk{Type: provider.ChunkTypeError, Err: errors.New("model failed"), Text: "model failed"}
	one := 1
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	cases := []struct {
		name  string
		ctx   context.Context
		opts  StreamTextOptions
		model *testutil.MockLanguageModel
		want  string
	}{
		{
			name:  "model error part",
			model: r69Model(r69Stream(text, modelErr, finish)),
			want:  "chunk:start chunk:error:model failed onError:model failed",
		},
		{
			name:  "retried model error part",
			opts:  StreamTextOptions{StreamRetries: &one},
			model: r69Model(r69Stream(modelErr), r69Stream(text, finish)),
			want:  "chunk:start chunk:error:model failed onError:model failed",
		},
		{
			name:  "model call fails to open",
			model: r69Model(func() (provider.TextStream, error) { return nil, errors.New("open failed") }),
			want:  "chunk:start chunk:error:open failed onError:open failed",
		},
		{
			name: "retry fails to open",
			opts: StreamTextOptions{StreamRetries: &one},
			model: r69Model(r69Stream(text, modelErr), func() (provider.TextStream, error) {
				return nil, errors.New("reopen failed")
			}),
			want: "chunk:start chunk:error:model failed onError:model failed chunk:error:reopen failed onError:reopen failed",
		},
		{
			name: "tool choice violation",
			opts: StreamTextOptions{
				Tools:      []types.Tool{{Name: "t", Parameters: map[string]interface{}{"type": "object"}}},
				ToolChoice: types.ToolChoice{Type: types.ToolChoiceRequired},
			},
			model: r69Model(r69Stream(text, finish)),
			want:  "chunk:start chunk:error:VIOLATION onError:VIOLATION",
		},
		{
			name:  "invalid prompt",
			opts:  StreamTextOptions{Messages: []types.Message{{Role: types.RoleUser}}},
			model: r69Model(r69Stream(text, finish)),
			want:  "chunk:start chunk:error:PROMPT onError:PROMPT",
		},
		{
			name:  "aborted before the first stream",
			ctx:   cancelled,
			model: r69Model(r69Stream(text, finish)),
			want:  "chunk:start onAbort chunk:abort",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var log r69EventLog
			opts := log.options(tc.opts)
			opts.Model = tc.model
			ctx := tc.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			res, err := StreamText(ctx, opts)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = res.ReadAll()
			got := log.String()
			if strings.Contains(tc.want, "VIOLATION") || strings.Contains(tc.want, "PROMPT") {
				// The error text is the SDK's; check the order only.
				var errText string
				for _, e := range log.events {
					if strings.HasPrefix(e, "onError:") {
						errText = strings.TrimPrefix(e, "onError:")
					}
				}
				if errText == "" {
					t.Fatalf("events = %q, want an onError call", got)
				}
				got = strings.ReplaceAll(got, errText, "X")
				tc.want = strings.NewReplacer("VIOLATION", "X", "PROMPT", "X").Replace(tc.want)
			}
			if got != tc.want {
				t.Fatalf("events = %q\n want %q", got, tc.want)
			}
		})
	}
}

// TS resets the step's raw finish reason at a stream retry boundary, so a
// retried attempt's finish part without one leaves it undefined.
func TestR69StreamRetryResetsRawFinishReason(t *testing.T) {
	one := 1
	res, err := StreamText(context.Background(), StreamTextOptions{
		Prompt:        "hi",
		StreamRetries: &one,
		Model: r69Model(
			r69Stream(
				provider.StreamChunk{Type: provider.ChunkTypeText, Text: "a"},
				provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "first_attempt"},
				provider.StreamChunk{Type: provider.ChunkTypeError, Text: "late failure"},
			),
			r69Stream(
				provider.StreamChunk{Type: provider.ChunkTypeText, Text: "b"},
				provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			),
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if got := res.RawFinishReason(); got != "" {
		t.Fatalf("RawFinishReason = %q, want empty (the retried attempt reported none)", got)
	}
	if got := res.FinalStep().RawFinishReason; got != "" {
		t.Fatalf("step RawFinishReason = %q, want empty", got)
	}
}
