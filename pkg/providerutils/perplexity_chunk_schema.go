package providerutils

import (
	"github.com/digitallysavvy/go-ai/pkg/internal/pplxschema"
	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
)

// CheckPerplexityAgentChunk validates a Perplexity agent stream frame against
// TS perplexityAgentChunkSchema and returns the error-part message and typed
// error on failure ("", nil when accepted).
func CheckPerplexityAgentChunk(data string) (string, error) {
	value, err := parseJSValue([]byte(data))
	if err != nil {
		return ChunkParseFailure(data, nil)
	}
	if bad := zodlite.Validate(pplxschema.Chunk, []byte(data), value); bad != nil {
		return ChunkParseFailure(data, bad)
	}
	return "", nil
}
