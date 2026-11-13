package providerutils

import (
	"github.com/digitallysavvy/go-ai/pkg/internal/pplxschema"
)

// CheckPerplexityAgentChunk validates a Perplexity agent stream frame against
// TS perplexityAgentChunkSchema and returns the error-part message and typed
// error on failure ("", nil when accepted).
func CheckPerplexityAgentChunk(data string) (string, error) {
	value, err := parseJSValue([]byte(data))
	if err != nil {
		return ChunkParseFailure(data, nil)
	}
	if bad := pplxschema.Chunk(value, true, nil); bad != nil {
		return ChunkParseFailure(data, bad)
	}
	return "", nil
}
