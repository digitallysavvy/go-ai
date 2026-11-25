package bedrock

import (
	"errors"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS createAmazonBedrockStreamError gets `data: chunk.rawValue` (the payload
// wrapped under its type) and keeps any string message, even an empty one.
func TestR55BedrockStreamExceptionErrorDataIsWrappedAndEmptyMessageKept(t *testing.T) {
	chunks, err := r55CollectBedrockStream(t, [][2]string{
		{"throttlingException", `{"message":"","name":"ThrottlingException"}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range chunks {
		if c.Type != provider.ChunkTypeError {
			continue
		}
		var spe *providererrors.StreamProviderError
		if !errors.As(c.Err, &spe) {
			t.Fatalf("error = %T, want StreamProviderError", c.Err)
		}
		want := map[string]interface{}{"throttlingException": map[string]interface{}{"message": "", "name": "ThrottlingException"}}
		if !reflect.DeepEqual(spe.Data, want) {
			t.Fatalf("data = %#v, want %#v", spe.Data, want)
		}
		if spe.Message != "" {
			t.Fatalf("message = %q, want empty", spe.Message)
		}
		return
	}
	t.Fatal("no error chunk")
}
