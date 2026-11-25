package huggingface

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR99provbStreamErrorNumericCodeRawFinishReason: TS sets the finish
// reason's raw value to String(streamError.code), the JS number text. A
// numeric code of 1000000 is "1000000" (Go's %v wrote "1e+06") and 0.00001
// is "0.00001" (Go "1e-05").
func TestR99provbStreamErrorNumericCodeRawFinishReason(t *testing.T) {
	for code, want := range map[string]string{"1000000": "1000000", "0.00001": "0.00001", "429": "429"} {
		sse := `data: {"type":"error","error":{"message":"boom","code":` + code + `}}` + "\n\n"
		s := newHFStream(io.NopCloser(strings.NewReader(sse)), nil, "huggingface.responses")
		var finish *provider.StreamChunk
		for {
			c, err := s.Next()
			if err != nil {
				if err != io.EOF {
					t.Fatalf("code %s: err = %v", code, err)
				}
				break
			}
			if c.Type == provider.ChunkTypeFinish {
				finish = c
			}
		}
		if finish == nil {
			t.Fatalf("code %s: no finish part", code)
		}
		if finish.FinishReason != types.FinishReasonError || finish.RawFinishReason != want {
			t.Errorf("code %s: finish = %q / raw %q, want error / %q", code, finish.FinishReason, finish.RawFinishReason, want)
		}
	}
}
