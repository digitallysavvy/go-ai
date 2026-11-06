package openai

import "context"

// MetadataExtractor adds provider-specific metadata to the results of an
// OpenAI-compatible chat model (Config.OpenAICompatible), mirroring the TS
// openai-compatible `metadataExtractor` setting (MetadataExtractor in
// openai-compatible-metadata-extractor.ts). The returned metadata is keyed
// by provider name and merged over the result's own provider metadata.
type MetadataExtractor interface {
	// ExtractMetadata reads the parsed JSON body of a complete,
	// non-streaming response. It returns nil when there is no metadata; an
	// error fails the call (TS rejects doGenerate).
	ExtractMetadata(ctx context.Context, parsedBody interface{}) (map[string]interface{}, error)
	// CreateStreamExtractor returns a fresh extractor for one streamed
	// response.
	CreateStreamExtractor() StreamMetadataExtractor
}

// StreamMetadataExtractor accumulates metadata across a streamed response
// (TS createStreamExtractor()).
type StreamMetadataExtractor interface {
	// ProcessChunk is called with the parsed JSON value of every stream
	// event that passes the chunk schema, error events included.
	ProcessChunk(parsedChunk interface{})
	// BuildMetadata is called once when the stream ends, for the finish
	// part's provider metadata; nil means none.
	BuildMetadata() map[string]interface{}
}

// mergeExtractedMetadata applies TS `{[key]: {}, ...extracted}` followed by
// the accepted/rejected prediction token fields set on `[key]`: extracted
// entries replace the result's own, and the token counts the result
// reported under key are kept on the extracted entry for key.
func mergeExtractedMetadata(meta map[string]interface{}, key string, extracted map[string]interface{}) map[string]interface{} {
	if len(extracted) == 0 {
		return meta
	}
	if meta == nil {
		meta = map[string]interface{}{}
	}
	own, _ := meta[key].(map[string]interface{})
	for k, v := range extracted {
		if vm, ok := v.(map[string]interface{}); ok && k == key {
			merged := make(map[string]interface{}, len(vm)+2)
			for f, x := range vm {
				merged[f] = x
			}
			for _, f := range []string{"acceptedPredictionTokens", "rejectedPredictionTokens"} {
				if x, ok := own[f]; ok {
					merged[f] = x
				}
			}
			meta[k] = merged
			continue
		}
		meta[k] = v
	}
	return meta
}
