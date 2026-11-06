package bedrock

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS validates an exception payload as an object, so a number fails schema
// validation as an error part (it is not dropped as if it were empty).
func TestR55BedrockStreamNonObjectExceptionPayloadIsValidationError(t *testing.T) {
	chunks, err := r55CollectBedrockStream(t, [][2]string{{"internalServerException", `5`}})
	if err != nil {
		t.Fatalf("stream error = %v, want none", err)
	}
	var errChunk *provider.StreamChunk
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeError {
			errChunk = c
		}
	}
	if errChunk == nil {
		t.Fatalf("chunks %v: no error part", chunks)
	}
}
