package ai

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type typedStreamErr struct{ Code int }

func (e *typedStreamErr) Error() string { return "typed stream failure" }

// TS rejects the result promises with the original error object of the stream
// error part; errors.As must reach the typed error through Err().
func TestStreamText_ErrKeepsTypedErrorFromErrorChunk(t *testing.T) {
	t.Parallel()
	want := &typedStreamErr{Code: 7}
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "hi"},
				{Type: provider.ChunkTypeError, Err: want, Text: want.Error()},
			}), nil
		},
	}
	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	got := result.Err()
	if got == nil {
		t.Fatal("Err() = nil, want the stream error")
	}
	var typed *typedStreamErr
	if !errors.As(got, &typed) || typed != want {
		t.Fatalf("Err() = %#v, want errors.As to find the original typed error", got)
	}
}

func TestStreamText_NextKeepsTypedErrorFromErrorChunk(t *testing.T) {
	t.Parallel()
	want := &typedStreamErr{Code: 9}
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeError, Err: want, Text: want.Error()},
			}), nil
		},
	}
	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := result.ReadAll()
	var typed *typedStreamErr
	if !errors.As(readErr, &typed) {
		t.Fatalf("ReadAll error = %#v, want typed error", readErr)
	}
}

func TestExternalStream_ErrKeepsTypedErrorFromErrorChunk(t *testing.T) {
	t.Parallel()
	want := &typedStreamErr{Code: 3}
	src := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "partial"},
		{Type: provider.ChunkTypeError, Err: want, Text: want.Error()},
	})
	result := NewStreamTextResultFromParts(context.Background(), src, ExternalStreamOptions{})
	var typed *typedStreamErr
	if err := result.Err(); !errors.As(err, &typed) || typed != want {
		t.Fatalf("Err() = %#v, want the original typed error", err)
	}
	if err := result.Err(); err.Error() != "typed stream failure" {
		t.Fatalf("message = %q", err.Error())
	}
}

// The full stream carries only TS stream parts; the Go lifecycle markers stay
// on OnChunk.
func TestStreamText_FullStreamOmitsInternalMarkers(t *testing.T) {
	t.Parallel()
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeStreamStart},
				{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "r1"}},
				{Type: provider.ChunkTypeText, Text: "hi"},
				{Type: provider.ChunkTypeFinish, FinishReason: "stop"},
			}), nil
		},
	}
	var viaOnChunk []provider.ChunkType
	var mu sync.Mutex
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "x",
		OnChunk: func(c provider.StreamChunk) { mu.Lock(); viaOnChunk = append(viaOnChunk, c.Type); mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	s := result.Stream()
	for {
		c, err := s.Next()
		if err != nil || c == nil {
			break
		}
		if isInternalStreamMarker(c.Type) {
			t.Fatalf("fullStream carried internal marker %q", c.Type)
		}
	}
	_ = result.Err()
	mu.Lock()
	defer mu.Unlock()
	var sawResponseMetadata bool
	for _, ty := range viaOnChunk {
		if ty == provider.ChunkTypeResponseMetadata {
			sawResponseMetadata = true
		}
	}
	if !sawResponseMetadata {
		t.Fatalf("OnChunk lost the response-metadata marker: %v", viaOnChunk)
	}
}

// Chunk-validation failures from providers are *JSONParseError /
// *ValidationError; OnError, Err() and the UI stream's OnError must all see
// the typed error (errors.As), not a flattened string.
func TestStreamText_ChunkValidationErrorsStayTypedEverywhere(t *testing.T) {
	t.Parallel()
	_, jsonErr := providerutils.ChunkParseFailure("{not json", nil)
	_, valErr := providerutils.ChunkParseFailure(`{"a":1}`, errors.New("bad"))
	for name, want := range map[string]error{"json": jsonErr, "validation": valErr} {
		want := want
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			model := &testutil.MockLanguageModel{
				DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
					return testutil.NewMockTextStream([]provider.StreamChunk{
						{Type: provider.ChunkTypeError, Err: want, Text: want.Error()},
					}), nil
				},
			}
			var mu sync.Mutex
			var onErr error
			result, err := StreamText(context.Background(), StreamTextOptions{
				Model: model, Prompt: "x",
				OnError: func(_ context.Context, e error) { mu.Lock(); onErr = e; mu.Unlock() },
			})
			if err != nil {
				t.Fatal(err)
			}
			var uiErr error
			chunks, _ := result.ToUIMessageStream(context.Background(), UIMessageStreamResultOptions{
				// Like TS, the callback state replays the error part to the same
				// handler as new Error(errorText); the first call is the typed one.
				OnError: func(e error) string {
					if uiErr == nil {
						uiErr = e
					}
					return "x"
				},
			})
			for range chunks {
			}
			check := func(where string, got error) {
				var j *providererrors.JSONParseError
				var v *providererrors.ValidationError
				if !errors.As(got, &j) && !errors.As(got, &v) || got != want && !errors.Is(got, want) {
					t.Errorf("%s = %#v, want the original %T", where, got, want)
				}
			}
			check("Err()", result.Err())
			mu.Lock()
			check("OnError", onErr)
			mu.Unlock()
			check("UI OnError", uiErr)
		})
	}
}
