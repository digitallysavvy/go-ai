package openresponses

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR97provbStreamMissingMemberTypeError: TS validates Open Responses
// chunks with z.any() and then reads chunk.item.type, chunk.response.* or
// chunk.error.code; a missing or null member is a TypeError that errors the
// stream (no finish part). Go skipped the frame. A non-object member is
// read without a throw.
func TestR97provbStreamMissingMemberTypeError(t *testing.T) {
	for frame, want := range map[string]string{
		`{"type":"response.output_item.added"}`:            "Cannot read properties of undefined (reading 'type')",
		`{"type":"response.output_item.done","item":null}`: "Cannot read properties of null (reading 'type')",
		`{"type":"response.completed"}`:                    "Cannot read properties of undefined (reading 'incomplete_details')",
		`{"type":"response.failed","response":null}`:       "Cannot read properties of null (reading 'error')",
		`{"type":"error"}`:                                 "Cannot read properties of undefined (reading 'code')",
		`{"type":"response.output_item.added","item":"x"}`: "",
	} {
		s := newOpenResponsesStream(io.NopCloser(strings.NewReader("data: "+frame+"\n\n")), nil)
		var err error
		sawFinish := false
		for err == nil {
			var c *provider.StreamChunk
			c, err = s.Next()
			if c != nil && c.Type == provider.ChunkTypeFinish {
				sawFinish = true
			}
		}
		if want == "" {
			if err != io.EOF || !sawFinish {
				t.Errorf("%s: err = %v, finish %v; want a clean end", frame, err, sawFinish)
			}
			continue
		}
		if sawFinish || err.Error() != want {
			t.Errorf("%s: err = %v, finish %v; want %q and no finish", frame, err, sawFinish, want)
		}
	}
}
