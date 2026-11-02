package gateway

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS gateway doStream (createEventSourceResponseHandler(z.any())) passes an
// error part through whatever its error object holds. A field of an
// unexpected type must not fail the chunk decode and end the stream.
func TestR77prova_GatewayStreamErrorObjectMistypedFields(t *testing.T) {
	stream := &gatewayTextStream{
		parser: streamingParser(`data: {"type":"error","error":{"message":"Overloaded","type":"overloaded_error","statusCode":"529","isRetryable":"yes","code":7}}` + "\n\n" +
			`data: {"type":"error","error":{"message":123,"type":"api_error","statusCode":500}}` + "\n\n" +
			`data: {"type":"finish","finishReason":{"unified":"error"}}` + "\n\n"),
		body: io.NopCloser(strings.NewReader("")),
	}
	chunk, err := stream.Next()
	if err != nil || chunk.Type != provider.ChunkTypeError || chunk.Text != "Overloaded" {
		t.Fatalf("chunk = %+v, err = %v", chunk, err)
	}
	var streamErr *providererrors.StreamProviderError
	if !errors.As(chunk.Err, &streamErr) || streamErr.Type != "overloaded_error" || streamErr.Code != float64(7) {
		t.Fatalf("chunk.Err = %#v", chunk.Err)
	}

	chunk, err = stream.Next()
	if err != nil || chunk.Type != provider.ChunkTypeError || chunk.Text != "" {
		t.Fatalf("chunk = %+v, err = %v", chunk, err)
	}
	if !errors.As(chunk.Err, &streamErr) || streamErr.Type != "api_error" || streamErr.StatusCode == nil || *streamErr.StatusCode != 500 {
		t.Fatalf("chunk.Err = %#v", chunk.Err)
	}

	chunk, err = stream.Next()
	if err != nil || chunk.Type != provider.ChunkTypeFinish {
		t.Fatalf("chunk = %+v, err = %v; want finish", chunk, err)
	}
}
