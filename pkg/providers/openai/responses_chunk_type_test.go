package openai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
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
		`{"type":"response.in_progress"}`: false,
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
