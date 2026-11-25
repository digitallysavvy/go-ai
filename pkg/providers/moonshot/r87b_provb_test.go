package moonshot

import (
	"errors"
	"io"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// V061 36: TS 3fab50aa9b moonshotai-chat-language-model.ts
// createMoonshotAIStreamError passes `code: error.code ?? undefined`, so a
// present empty code stays "" while null or absent is no code.
func TestR87bprovbStreamErrorEmptyCodeKept(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		want       interface{}
	}{
		{"empty", `,"code":""`, ""},
		{"null", `,"code":null`, nil},
		{"absent", ``, nil},
		{"value", `,"code":"upstream_failure"`, "upstream_failure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sse := `data: {"error":{"message":"m","type":"server_error"` + tc.code + "}}\n\ndata: [DONE]\n\n"
			stream := newMoonshotStream(io.NopCloser(strings.NewReader(sse)), false)
			defer stream.Close() //nolint:errcheck
			chunk, err := stream.Next()
			if err != nil {
				t.Fatal(err)
			}
			var spe *providererrors.StreamProviderError
			if !errors.As(chunk.Err, &spe) {
				t.Fatalf("chunk.Err = %T", chunk.Err)
			}
			if spe.Code != tc.want {
				t.Fatalf("code = %#v, want %#v", spe.Code, tc.want)
			}
		})
	}
}
