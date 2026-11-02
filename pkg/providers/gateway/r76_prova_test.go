package gateway

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS gateway doStream passes every stream part through as is, whatever its
// `error` holds. A string (or other non-object) error must surface as an
// error part and keep the stream going, not fail the whole chunk decode.
func TestR76prova_GatewayStreamNonObjectError(t *testing.T) {
	stream := &gatewayTextStream{
		parser: streamingParser(`data: {"type":"error","error":"Internal error"}` + "\n\n" +
			`data: {"type":"error","error":42}` + "\n\n" +
			`data: {"type":"finish","finishReason":{"unified":"error"}}` + "\n\n"),
		body: io.NopCloser(strings.NewReader("")),
	}
	for _, want := range []string{"Internal error", "42"} {
		chunk, err := stream.Next()
		if err != nil || chunk == nil || chunk.Type != provider.ChunkTypeError || chunk.Text != want {
			t.Fatalf("chunk = %+v, err = %v; want error part %q", chunk, err, want)
		}
		var streamErr *providererrors.StreamProviderError
		if !errors.As(chunk.Err, &streamErr) || streamErr.Message != want {
			t.Fatalf("chunk.Err = %#v", chunk.Err)
		}
	}
	chunk, err := stream.Next()
	if err != nil || chunk.Type != provider.ChunkTypeFinish {
		t.Fatalf("chunk = %+v, err = %v; want finish", chunk, err)
	}
}
