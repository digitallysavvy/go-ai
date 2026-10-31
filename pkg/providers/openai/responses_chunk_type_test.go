package openai

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS openaiResponsesChunkSchema ends in {type: z.string()}.loose(): unknown
// typed frames are accepted, but a frame without a string `type` fails every
// union member and becomes an error part.
func TestResponsesStream_FrameWithoutTypeIsAnError(t *testing.T) {
	for frame, wantErr := range map[string]bool{
		`{"foo":1}`:                       true,
		`{}`:                              true,
		`{"type":5}`:                      true,
		`{"type":"response.brand_new"}`:   false,
		`{"type":"response.queued"}`:      false,
		`{"type":"response.in_progress"}`: true,
	} {
		chunks := drainResponsesStream(t, "data: "+frame+"\n\ndata: [DONE]\n\n")
		sawError := false
		for _, c := range chunks {
			if c.Type == provider.ChunkTypeError {
				sawError = true
			}
		}
		if sawError != wantErr {
			t.Errorf("%s: error chunk = %v, want %v", frame, sawError, wantErr)
		}
	}
}

// A malformed modeled frame is an error part carrying a typed ValidationError
// whose text is the error text; invalid JSON carries a JSONParseError.
func TestResponsesStream_ParseFailuresCarryTypedErrors(t *testing.T) {
	for frame, want := range map[string]string{
		`{"type":"response.output_text.delta","item_id":"m","delta":5}`: "validation",
		`{"type":"response.output_text.delta"`:                          "json",
	} {
		chunks := drainResponsesStream(t, "data: "+frame+"\n\ndata: [DONE]\n\n")
		var got *provider.StreamChunk
		for _, c := range chunks {
			if c.Type == provider.ChunkTypeError {
				got = c
			}
		}
		if got == nil || got.Err == nil || got.Text != got.Err.Error() {
			t.Fatalf("%s: error chunk = %+v", frame, got)
		}
		var ve *providererrors.ValidationError
		var je *providererrors.JSONParseError
		if (want == "validation") != errors.As(got.Err, &ve) || (want == "json") != errors.As(got.Err, &je) {
			t.Errorf("%s: err type %T, want %s", frame, got.Err, want)
		}
	}
}
