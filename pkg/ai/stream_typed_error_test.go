package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
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
