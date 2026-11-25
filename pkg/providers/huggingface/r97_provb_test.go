package huggingface

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR97provbStreamMissingMemberTypeError: a frame without its response /
// item matches TS's loose {type} fallback member, and transform then reads
// value.response.id / value.item.type of undefined or null; the TypeError
// errors the stream (no finish part). Go skipped the frame.
func TestR97provbStreamMissingMemberTypeError(t *testing.T) {
	for frame, want := range map[string]string{
		`{"type":"response.created"}`:                      "Cannot read properties of undefined (reading 'id')",
		`{"type":"response.completed","response":null}`:    "Cannot read properties of null (reading 'id')",
		`{"type":"response.output_item.added"}`:            "Cannot read properties of undefined (reading 'type')",
		`{"type":"response.output_item.done","item":null}`: "Cannot read properties of null (reading 'type')",
	} {
		s := newHFStream(io.NopCloser(strings.NewReader("data: "+frame+"\n\n")), nil, "huggingface.responses")
		var err error
		for err == nil {
			var c *provider.StreamChunk
			c, err = s.Next()
			if c != nil && c.Type == provider.ChunkTypeFinish {
				t.Errorf("%s: finish part, want the TypeError", frame)
			}
		}
		if err.Error() != want {
			t.Errorf("%s: err = %v, want %q", frame, err, want)
		}
	}
}
