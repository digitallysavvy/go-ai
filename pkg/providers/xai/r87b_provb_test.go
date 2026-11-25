package xai

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Class sweep of V061 36: TS 3fab50aa9b xai-responses-language-model.ts
// createXaiResponsesStreamError passes `code: code ?? undefined` for both the
// `error` event (event.code) and `response.failed` (response.error.code), so
// a present empty code stays "" and null or absent is no code.
func TestR87bprovbResponsesStreamErrorEmptyCodeKept(t *testing.T) {
	for _, tc := range []struct {
		name, event string
		want        interface{}
	}{
		{"error empty", `{"type":"error","code":"","message":"m"}`, ""},
		{"error null", `{"type":"error","code":null,"message":"m"}`, nil},
		{"error absent", `{"type":"error","message":"m"}`, nil},
		{"failed empty", `{"type":"response.failed","response":{"id":"r","status":"failed","error":{"code":"","message":"m"}}}`, ""},
		{"failed absent", `{"type":"response.failed","response":{"id":"r","status":"failed","error":{"message":"m"}}}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chunks := drainXAIResponses(t, "data: "+tc.event+"\n\n")
			for _, c := range chunks {
				if c.Type != provider.ChunkTypeError {
					continue
				}
				var spe *providererrors.StreamProviderError
				if !errors.As(c.Err, &spe) {
					t.Fatalf("chunk.Err = %T", c.Err)
				}
				if spe.Code != tc.want {
					t.Fatalf("code = %#v, want %#v", spe.Code, tc.want)
				}
				return
			}
			t.Fatal("no error chunk")
		})
	}
}
