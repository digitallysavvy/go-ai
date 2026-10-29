package anthropic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r58: TS doStream throws `new Error('Expected a normalized Anthropic stream
// error')` when the first part is an error that is not a provider stream error
// (an unparsable or schema-invalid first frame); it is not an APICallError.
func TestR58StreamFirstChunkNonProviderError(t *testing.T) {
	for name, frame := range map[string]string{
		"unparsable": `{"type":"message_start",`,
		"invalid":    `{"type":"content_block_delta","index":"x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			model, closeFn := fixtureModel(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
			})
			defer closeFn()
			_, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
			if err == nil || err.Error() != "Expected a normalized Anthropic stream error" {
				t.Fatalf("err = %v, want the TS plain error", err)
			}
			var pe *providererrors.ProviderError
			if errors.As(err, &pe) {
				t.Fatalf("err must not be a ProviderError: %#v", pe)
			}
		})
	}
}
