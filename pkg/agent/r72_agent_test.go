package agent

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Round 72 ports of TS packages/ai/src/agent/tool-loop-agent.test.ts.

func r72agentTextModel(text string) *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         text,
				Content:      []types.ContentPart{types.TextContent{Text: text}},
				FinishReason: types.FinishReasonStop,
			}, nil
		},
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: text},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
}

type r72agentRecorder struct {
	mu     sync.Mutex
	events []string
}

func (r *r72agentRecorder) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, s)
}

func (r *r72agentRecorder) get() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

// TS 2275 "should rethrow the generate error when onError throws": a panicking
// ErrorHandler (TS notify swallows a throwing callback) must not replace the
// generate error.
func TestR72agentGenerateErrorHandlerPanicKeepsError(t *testing.T) {
	providerErr := errors.New("provider failure")
	a := NewToolLoopAgent(AgentConfig{
		Model: &testutil.MockLanguageModel{
			DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				return nil, providerErr
			},
		},
		MaxRetries:   r72agentIntPtr(0),
		ErrorHandler: func(ctx context.Context, err error) { panic("callback failure") },
	})
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Generate panicked: %v", r)
			}
		}()
		_, err = a.Generate(context.Background(), AgentGenerateOptions{Prompt: "Hello, world!"})
	}()
	if !errors.Is(err, providerErr) {
		t.Fatalf("err = %v, want the provider error", err)
	}
}

// TS 2254 "should call onError and rethrow when generate fails".
func TestR72agentGenerateCallsErrorHandlerOnceAndReturnsError(t *testing.T) {
	providerErr := errors.New("provider failure")
	var got []error
	a := NewToolLoopAgent(AgentConfig{
		Model: &testutil.MockLanguageModel{
			DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				return nil, providerErr
			},
		},
		MaxRetries:   r72agentIntPtr(0),
		ErrorHandler: func(ctx context.Context, err error) { got = append(got, err) },
	})
	_, err := a.Generate(context.Background(), AgentGenerateOptions{Prompt: "Hello, world!"})
	if !errors.Is(err, providerErr) {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 1 || !errors.Is(got[0], providerErr) {
		t.Fatalf("ErrorHandler calls = %v", got)
	}
}

// TS 2294 "should not call onError for a timeout in generate()".
func TestR72agentGenerateTimeoutDoesNotCallErrorHandler(t *testing.T) {
	called := false
	a := NewToolLoopAgent(AgentConfig{
		Model: &testutil.MockLanguageModel{
			DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
		},
		MaxRetries:   r72agentIntPtr(0),
		ErrorHandler: func(ctx context.Context, err error) { called = true },
	})
	total := 10 * time.Millisecond
	if _, err := a.Generate(context.Background(), AgentGenerateOptions{
		Prompt:  "Hello, world!",
		Timeout: &ai.TimeoutConfig{Total: &total},
	}); err == nil {
		t.Fatal("expected an error")
	}
	if called {
		t.Fatal("ErrorHandler called for a timeout")
	}
}

// TS 2335 "should not call onError when a provider wraps a timeout error".
func TestR72agentGenerateWrappedTimeoutDoesNotCallErrorHandler(t *testing.T) {
	called := false
	a := NewToolLoopAgent(AgentConfig{
		Model: &testutil.MockLanguageModel{
			DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				return nil, fmt.Errorf("Gateway request failed: %w", context.DeadlineExceeded)
			},
		},
		MaxRetries:   r72agentIntPtr(0),
		ErrorHandler: func(ctx context.Context, err error) { called = true },
	})
	_, err := a.Generate(context.Background(), AgentGenerateOptions{Prompt: "Hello, world!"})
	if err == nil || !strings.Contains(err.Error(), "Gateway request failed") {
		t.Fatalf("err = %v", err)
	}
	if called {
		t.Fatal("ErrorHandler called for a wrapped timeout")
	}
}

// TS 2409 "should not call onError when generate() is aborted with a custom reason".
func TestR72agentGenerateCustomAbortCauseDoesNotCallErrorHandler(t *testing.T) {
	called := false
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	userLeft := errors.New("user left")
	a := NewToolLoopAgent(AgentConfig{
		Model: &testutil.MockLanguageModel{
			DoGenerateFunc: func(c context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				cancel(userLeft)
				return nil, context.Cause(c)
			},
		},
		MaxRetries:   r72agentIntPtr(0),
		ErrorHandler: func(ctx context.Context, err error) { called = true },
	})
	_, err := a.Generate(ctx, AgentGenerateOptions{Prompt: "Hello, world!"})
	if err == nil || !strings.Contains(err.Error(), "user left") {
		t.Fatalf("err = %v", err)
	}
	if called {
		t.Fatal("ErrorHandler called for an abort")
	}
}

// TS 2356 / 2381: a provider wrapping an abort or a timeout in stream() does
// not reach onError.
func TestR72agentStreamWrappedAbortAndTimeoutDoNotCallOnError(t *testing.T) {
	t.Run("abort", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		called := false
		a := NewToolLoopAgent(AgentConfig{
			Model: &testutil.MockLanguageModel{
				DoStreamFunc: func(c context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
					cancel()
					return nil, fmt.Errorf("Gateway request failed: %w", context.Canceled)
				},
			},
			MaxRetries:   r72agentIntPtr(0),
			ErrorHandler: func(ctx context.Context, err error) { called = true },
		})
		result, err := a.Stream(ctx, AgentStreamOptions{AgentGenerateOptions: AgentGenerateOptions{Prompt: "Hello, world!"}})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = result.ReadAll()
		if called {
			t.Fatal("OnError called for a wrapped abort")
		}
	})
	t.Run("timeout", func(t *testing.T) {
		called := false
		a := NewToolLoopAgent(AgentConfig{
			Model: &testutil.MockLanguageModel{
				DoStreamFunc: func(c context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
					<-c.Done()
					return nil, fmt.Errorf("Gateway request failed: %w", context.Cause(c))
				},
			},
			MaxRetries:   r72agentIntPtr(0),
			ErrorHandler: func(ctx context.Context, err error) { called = true },
		})
		total := 10 * time.Millisecond
		result, err := a.Stream(context.Background(), AgentStreamOptions{AgentGenerateOptions: AgentGenerateOptions{
			Prompt:  "Hello, world!",
			Timeout: &ai.TimeoutConfig{Total: &total},
		}})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = result.ReadAll()
		if called {
			t.Fatal("OnError called for a wrapped timeout")
		}
	})
}

// TS 2231 "should call onError when a stream step fails": a stream with only
// stream-start reports NoOutputGeneratedError once.
func TestR72agentStreamNoOutputCallsOnErrorOnce(t *testing.T) {
	var mu sync.Mutex
	var got []error
	a := NewToolLoopAgent(AgentConfig{
		Model: &testutil.MockLanguageModel{
			DoStreamFunc: func(c context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
				return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeStreamStart}}), nil
			},
		},
		ErrorHandler: func(ctx context.Context, err error) {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, err)
		},
	})
	result, err := a.Stream(context.Background(), AgentStreamOptions{AgentGenerateOptions: AgentGenerateOptions{Prompt: "Hello, world!"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := result.ReadAll(); err == nil {
		t.Fatal("expected the text to fail")
	}
	mu.Lock()
	defer mu.Unlock()
	var noOutput *ai.NoOutputGeneratedError
	if len(got) != 1 || !errors.As(got[0], &noOutput) {
		t.Fatalf("OnError calls = %v", got)
	}
}

// TS 2171 / 2190 / 2207: onAbort fires for an aborted or timed-out stream(),
// not for an aborted generate().
func TestR72agentOnAbort(t *testing.T) {
	blockingStream := func(c context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
		<-c.Done()
		return nil, c.Err()
	}
	t.Run("stream abort", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		var calls int
		var mu sync.Mutex
		a := NewToolLoopAgent(AgentConfig{
			Model: &testutil.MockLanguageModel{DoStreamFunc: func(c context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
				cancel()
				return blockingStream(c, opts)
			}},
			AbortHandler: func(ctx context.Context, steps []types.StepResult) {
				mu.Lock()
				defer mu.Unlock()
				calls++
				if len(steps) != 0 {
					t.Errorf("steps = %d, want 0", len(steps))
				}
			},
		})
		result, err := a.Stream(ctx, AgentStreamOptions{AgentGenerateOptions: AgentGenerateOptions{Prompt: "Hello, world!"}})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = result.ReadAll()
		mu.Lock()
		defer mu.Unlock()
		if calls != 1 {
			t.Fatalf("AbortHandler calls = %d, want 1", calls)
		}
	})
	t.Run("stream timeout", func(t *testing.T) {
		var calls int
		var mu sync.Mutex
		a := NewToolLoopAgent(AgentConfig{
			Model: &testutil.MockLanguageModel{DoStreamFunc: blockingStream},
			AbortHandler: func(ctx context.Context, steps []types.StepResult) {
				mu.Lock()
				defer mu.Unlock()
				calls++
			},
		})
		total := 10 * time.Millisecond
		result, err := a.Stream(context.Background(), AgentStreamOptions{AgentGenerateOptions: AgentGenerateOptions{
			Prompt: "Hello, world!", Timeout: &ai.TimeoutConfig{Total: &total},
		}})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = result.ReadAll()
		mu.Lock()
		defer mu.Unlock()
		if calls != 1 {
			t.Fatalf("AbortHandler calls = %d, want 1", calls)
		}
	})
	t.Run("generate abort", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		called := false
		a := NewToolLoopAgent(AgentConfig{
			Model: &testutil.MockLanguageModel{DoGenerateFunc: func(c context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				cancel()
				return nil, c.Err()
			}},
			AbortHandler: func(ctx context.Context, steps []types.StepResult) { called = true },
		})
		if _, err := a.Generate(ctx, AgentGenerateOptions{Prompt: "Hello, world!"}); err == nil {
			t.Fatal("expected an error")
		}
		if called {
			t.Fatal("AbortHandler called for generate()")
		}
	})
}

// TS 553-632 "LanguageModelCallOptions forwarding" and 634 "RequestOptions
// forwarding" (headers), for generate and stream: settings reach the model
// call unchanged.
func TestR72agentLanguageModelCallOptionsForwarding(t *testing.T) {
	temp, topP, pres, freq := 0.5, 0.9, 0.3, 0.4
	maxTokens, topK, seed := 100, 40, 42
	stop := []string{"STOP", "END"}
	for _, mode := range []string{"generate", "stream"} {
		t.Run(mode, func(t *testing.T) {
			var got *provider.GenerateOptions
			model := r72agentTextModel("ok")
			gen, str := model.DoGenerateFunc, model.DoStreamFunc
			model.DoGenerateFunc = func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				got = opts
				return gen(ctx, opts)
			}
			model.DoStreamFunc = func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
				got = opts
				return str(ctx, opts)
			}
			a := NewToolLoopAgent(AgentConfig{
				Model:            model,
				Temperature:      &temp,
				MaxTokens:        &maxTokens,
				TopP:             &topP,
				TopK:             &topK,
				PresencePenalty:  &pres,
				FrequencyPenalty: &freq,
				StopSequences:    stop,
				Seed:             &seed,
				Headers:          map[string]string{"x-custom": "value"},
			})
			if mode == "generate" {
				if _, err := a.Generate(context.Background(), AgentGenerateOptions{Prompt: "Hello"}); err != nil {
					t.Fatal(err)
				}
			} else {
				r, err := a.Stream(context.Background(), AgentStreamOptions{AgentGenerateOptions: AgentGenerateOptions{Prompt: "Hello"}})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := r.ReadAll(); err != nil {
					t.Fatal(err)
				}
			}
			if got == nil {
				t.Fatal("model not called")
			}
			if got.Temperature == nil || *got.Temperature != temp ||
				got.MaxTokens == nil || *got.MaxTokens != maxTokens ||
				got.TopP == nil || *got.TopP != topP ||
				got.TopK == nil || *got.TopK != topK ||
				got.PresencePenalty == nil || *got.PresencePenalty != pres ||
				got.FrequencyPenalty == nil || *got.FrequencyPenalty != freq ||
				got.Seed == nil || *got.Seed != seed ||
				!reflect.DeepEqual(got.StopSequences, stop) {
				t.Fatalf("call options not forwarded: %+v", got)
			}
			if got.Headers["x-custom"] != "value" {
				t.Fatalf("headers = %v", got.Headers)
			}
		})
	}
}

func r72agentIntPtr(v int) *int { return &v }

// r72agentToolModel calls tool "t" once, then answers with text.
func r72agentToolModel() *testutil.MockLanguageModel {
	var mu sync.Mutex
	calls := 0
	next := func() int {
		mu.Lock()
		defer mu.Unlock()
		calls++
		return calls
	}
	call := types.ToolCall{ID: "call-1", ToolName: "t", Arguments: map[string]interface{}{"value": "x"}}
	return &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			if next() == 1 {
				return &types.GenerateResult{
					ToolCalls:    []types.ToolCall{call},
					Content:      []types.ContentPart{types.ToolCallContent{ToolCallID: call.ID, ToolName: call.ToolName, Input: `{"value":"x"}`}},
					FinishReason: types.FinishReasonToolCalls,
				}, nil
			}
			return &types.GenerateResult{Text: "done", Content: []types.ContentPart{types.TextContent{Text: "done"}}, FinishReason: types.FinishReasonStop}, nil
		},
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			if next() == 1 {
				c := call
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &c},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
}

func r72agentTool() types.Tool {
	return types.Tool{Name: "t", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "result", nil
	}}
}

// TS "should call both constructor and method <callback> in correct order"
// (onStart 1403/1646, onStepStart 1797/2009, onToolExecutionStart 2904/3110,
// onToolExecutionEnd 3331/3548, onStepFinish 2579/2731, onEnd 3714/3863),
// plus TS mergeCallbacks (Promise.allSettled): a throwing constructor
// callback does not stop the method callback.
func TestR72agentMergedCallbacksOrderAndIsolation(t *testing.T) {
	for _, mode := range []string{"generate", "stream"} {
		for _, settingsPanics := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/panic=%v", mode, settingsPanics), func(t *testing.T) {
				rec := &r72agentRecorder{}
				settings := func(name string) {
					rec.add("settings:" + name)
					if settingsPanics {
						panic("settings " + name)
					}
				}
				a := NewToolLoopAgent(AgentConfig{
					Model:                r72agentToolModel(),
					Tools:                []types.Tool{r72agentTool()},
					OnStart:              func(context.Context, ai.OnStartEvent) { settings("start") },
					OnStepStartEvent:     func(context.Context, ai.OnStepStartEvent) { settings("stepStart") },
					OnToolExecutionStart: func(context.Context, ai.OnToolCallStartEvent) { settings("toolStart") },
					OnToolExecutionEnd:   func(context.Context, ai.OnToolCallFinishEvent) { settings("toolEnd") },
					OnStepEndEvent:       func(context.Context, ai.OnStepFinishEvent) { settings("stepEnd") },
					OnEndEvent:           func(context.Context, ai.OnFinishEvent) { settings("end") },
				})
				opts := AgentGenerateOptions{
					Prompt:               "Hello",
					OnStart:              func(context.Context, ai.OnStartEvent) { rec.add("method:start") },
					OnStepStart:          func(context.Context, ai.OnStepStartEvent) { rec.add("method:stepStart") },
					OnToolExecutionStart: func(context.Context, ai.OnToolCallStartEvent) { rec.add("method:toolStart") },
					OnToolExecutionEnd:   func(context.Context, ai.OnToolCallFinishEvent) { rec.add("method:toolEnd") },
					OnStepEnd:            func(context.Context, ai.OnStepFinishEvent) { rec.add("method:stepEnd") },
					OnEnd:                func(context.Context, ai.OnFinishEvent) { rec.add("method:end") },
				}
				if mode == "generate" {
					res, err := a.Generate(context.Background(), opts)
					if err != nil {
						t.Fatal(err)
					}
					if res.Text != "done" {
						t.Fatalf("text = %q", res.Text)
					}
				} else {
					r, err := a.Stream(context.Background(), AgentStreamOptions{AgentGenerateOptions: opts})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := r.ReadAll(); err != nil {
						t.Fatal(err)
					}
				}
				want := []string{
					"settings:start", "method:start",
					"settings:stepStart", "method:stepStart",
					"settings:toolStart", "method:toolStart",
					"settings:toolEnd", "method:toolEnd",
					"settings:stepEnd", "method:stepEnd",
					"settings:stepStart", "method:stepStart",
					"settings:stepEnd", "method:stepEnd",
					"settings:end", "method:end",
				}
				if got := rec.get(); !reflect.DeepEqual(got, want) {
					t.Fatalf("events =\n%v\nwant\n%v", got, want)
				}
			})
		}
	}
}

// TS 2433 counterpart for the merged stream error handlers: a panicking
// config ErrorHandler still lets the per-call OnError run.
func TestR72agentStreamMergedOnErrorIsolation(t *testing.T) {
	rec := &r72agentRecorder{}
	a := NewToolLoopAgent(AgentConfig{
		Model: &testutil.MockLanguageModel{
			DoStreamFunc: func(c context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
				return nil, errors.New("boom")
			},
		},
		MaxRetries: r72agentIntPtr(0),
		ErrorHandler: func(ctx context.Context, err error) {
			rec.add("config")
			panic("config handler")
		},
	})
	r, err := a.Stream(context.Background(), AgentStreamOptions{
		AgentGenerateOptions: AgentGenerateOptions{Prompt: "hi"},
		OnError:              func(ctx context.Context, err error) { rec.add("call") },
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = r.ReadAll()
	if got := rec.get(); !reflect.DeepEqual(got, []string{"config", "call"}) {
		t.Fatalf("events = %v", got)
	}
}
