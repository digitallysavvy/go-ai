package ai

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// GenerateObject/StreamObject MaxRetries follows TS prepareRetries: nil is
// the default of 2, an explicit 0 disables retries, a negative value is an
// InvalidArgumentError.
func TestObjectMaxRetriesPointerSemantics(t *testing.T) {
	t.Parallel()

	newModel := func(calls *int32) *testutil.MockLanguageModel {
		return &testutil.MockLanguageModel{
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				atomic.AddInt32(calls, 1)
				return nil, fastRetryErr(503)
			},
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				atomic.AddInt32(calls, 1)
				return nil, fastRetryErr(503)
			},
		}
	}
	zero, neg := 0, -1

	cases := []struct {
		name       string
		maxRetries *int
		wantCalls  int32
	}{
		{"nil uses default 2", nil, 3},
		{"zero disables retries", &zero, 1},
	}
	for _, tc := range cases {
		t.Run("generate/"+tc.name, func(t *testing.T) {
			var calls int32
			_, err := GenerateObject(context.Background(), GenerateObjectOptions{
				Model: newModel(&calls), Prompt: "hi", OutputMode: ObjectModeNoSchema, MaxRetries: tc.maxRetries,
			})
			if err == nil {
				t.Fatal("expected error")
			}
			if got := atomic.LoadInt32(&calls); got != tc.wantCalls {
				t.Fatalf("DoGenerate calls = %d, want %d", got, tc.wantCalls)
			}
		})
		t.Run("stream/"+tc.name, func(t *testing.T) {
			var calls int32
			_, err := StreamObject(context.Background(), StreamObjectOptions{
				Model: newModel(&calls), Prompt: "hi", OutputMode: ObjectModeNoSchema, MaxRetries: tc.maxRetries,
			})
			if err == nil {
				t.Fatal("expected error")
			}
			if got := atomic.LoadInt32(&calls); got != tc.wantCalls {
				t.Fatalf("DoStream calls = %d, want %d", got, tc.wantCalls)
			}
		})
	}

	t.Run("negative is InvalidArgumentError", func(t *testing.T) {
		var calls int32
		var iae *providererrors.InvalidArgumentError
		_, err := GenerateObject(context.Background(), GenerateObjectOptions{
			Model: newModel(&calls), Prompt: "hi", OutputMode: ObjectModeNoSchema, MaxRetries: &neg,
		})
		if !errors.As(err, &iae) {
			t.Fatalf("GenerateObject error = %v, want *InvalidArgumentError", err)
		}
		_, err = StreamObject(context.Background(), StreamObjectOptions{
			Model: newModel(&calls), Prompt: "hi", OutputMode: ObjectModeNoSchema, MaxRetries: &neg,
		})
		if !errors.As(err, &iae) {
			t.Fatalf("StreamObject error = %v, want *InvalidArgumentError", err)
		}
		if calls != 0 {
			t.Fatalf("model called %d times, want 0", calls)
		}
	})
}
