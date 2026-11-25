package bedrock

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Stream-chunk parse and validation failures carry the typed error in
// StreamChunk.Err so callers can errors.As it, like TS error parts.
func TestStreamChunkErrorsCarryTypedErrors(t *testing.T) {
	cases := map[string]struct {
		payload string
		check   func(error) bool
	}{
		"unparsable json": {`{not json`, func(err error) bool {
			var e *providererrors.JSONParseError
			return errors.As(err, &e) && e.Text == `{not json`
		}},
		"schema mismatch": {`{"usage":{"inputTokens":1,"outputTokens":1,"totalTokens":"2"}}`, func(err error) bool {
			var e *providererrors.ValidationError
			return errors.As(err, &e)
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write(buildBedrockEventStreamBody([][2]string{{"metadata", tc.payload}}))
			}))
			defer server.Close()
			model := newHTTPTestBedrockModel(t, server, "anthropic.claude-3-haiku-20240307-v1:0")
			stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close() //nolint:errcheck
			for {
				chunk, err := stream.Next()
				if err != nil {
					t.Fatal("no error chunk seen")
				}
				if chunk.Type == provider.ChunkTypeError {
					if !tc.check(chunk.Err) {
						t.Fatalf("Err = %#v", chunk.Err)
					}
					return
				}
			}
		})
	}
}
