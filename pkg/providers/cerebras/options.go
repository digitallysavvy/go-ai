package cerebras

import (
	"context"
	"reflect"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// cerebrasRequestExtras carries the wire-format fields derived from
// providerOptions.cerebras (CerebrasLanguageModelChatOptions in TS) that the
// shared OpenAI-compatible chat model has no namespace for. LanguageModel
// resolves these before delegating to the OpenAI-compatible base model,
// which has no per-call hook to receive them (openai.Config.TransformRequestBody
// is a static, provider-construction-time function with no access to a
// specific call's opts), so cerebrasTransformTransport merges them into the
// serialized request body at the HTTP transport layer instead — the same
// net effect as TS's transformRequestBody hook, which destructures
// args.parallelToolCalls etc. directly since TS's shared getArgs() forwards
// unrecognized providerOptions[namespace] keys into args as passthrough.
// The structural half of TS's transformRequestBody (renaming max_tokens and
// reasoning_content, which need no per-call data) runs earlier, as
// openai.Config.TransformRequestBody (see transformCerebrasRequestBody in
// provider.go) — since the base model only ever reads
// providerOptions["openai"].
type cerebrasRequestExtras map[string]interface{}

// resolveCerebrasOptions builds cerebrasRequestExtras from
// opts.ProviderOptions["cerebras"], mirroring TS transformCerebrasRequestBody
// / cerebrasLanguageModelChatOptions.
func resolveCerebrasOptions(opts *provider.GenerateOptions) (cerebrasRequestExtras, []types.Warning) {
	if opts == nil {
		return nil, nil
	}
	cerebrasOpts, warnings := providerutils.ResolveOpenAICompatibleProviderOptions("cerebras", opts.ProviderOptions)
	if len(cerebrasOpts) == 0 {
		return nil, warnings
	}

	extras := cerebrasRequestExtras{}

	if user, ok := providerutils.OpenAICompatibleStringOption(cerebrasOpts, "user"); ok {
		extras["user"] = user
	}
	// strictJsonSchema is intentionally not handled here: the shared
	// OpenAI-compatible chat model's strictJSONSchema() already reads
	// providerOptions[m.provider.Name()] (i.e. "cerebras") when building
	// response_format, so re-applying it via cerebrasTransformTransport would
	// just duplicate work the base model already does correctly.
	if parallelToolCalls, ok := providerutils.OpenAICompatibleBoolOption(cerebrasOpts, "parallelToolCalls"); ok {
		extras["parallel_tool_calls"] = parallelToolCalls
	}
	if logprobs, ok := providerutils.OpenAICompatibleBoolOption(cerebrasOpts, "logprobs"); ok {
		extras["logprobs"] = logprobs
	}
	if topLogprobs, ok := providerutils.OpenAICompatibleIntOption(cerebrasOpts, "topLogprobs"); ok {
		extras["top_logprobs"] = topLogprobs
	}
	if logitBias, ok := cerebrasOpts["logitBias"]; ok && logitBias != nil {
		extras["logit_bias"] = logitBias
	}
	if serviceTier, ok := providerutils.OpenAICompatibleStringOption(cerebrasOpts, "serviceTier"); ok {
		extras["service_tier"] = serviceTier
	}
	if reasoningEffort, ok := providerutils.OpenAICompatibleStringOption(cerebrasOpts, "reasoningEffort"); ok {
		extras["reasoning_effort"] = reasoningEffort
	} else if v, present := cerebrasOpts["reasoningEffort"]; present && v == nil {
		// TS 5006d83c4e: an explicit null is sent as reasoning_effort: null
		// and beats the top-level reasoning setting.
		extras["reasoning_effort"] = nil
	}
	if reasoningFormat, ok := providerutils.OpenAICompatibleStringOption(cerebrasOpts, "reasoningFormat"); ok {
		extras["reasoning_format"] = reasoningFormat
	}
	if prediction, ok := cerebrasOpts["prediction"]; ok && prediction != nil {
		extras["prediction"] = prediction
	}
	if promptCacheKey, ok := providerutils.OpenAICompatibleStringOption(cerebrasOpts, "promptCacheKey"); ok {
		extras["prompt_cache_key"] = promptCacheKey
	}

	if len(extras) == 0 {
		return nil, warnings
	}
	return extras, warnings
}

var cerebrasQueueThresholdMin, cerebrasQueueThresholdMax = 50, 20000

// applyQueueThresholdHeader mirrors TS CerebrasChatLanguageModel
// applyQueueThresholdHeader (4a4e854fa6): providerOptions.cerebras
// queueThreshold (an integer in [50, 20000], the maximum queue time in
// milliseconds for flex / auto requests) is validated and sent as the
// queue_threshold header, over the call's own headers. An invalid value
// fails the call with the parseProviderOptions error.
func applyQueueThresholdHeader(opts *provider.GenerateOptions) (*provider.GenerateOptions, error) {
	if opts == nil {
		return opts, nil
	}
	parsed, err := providerutils.ParseProviderOptions("cerebras", opts.ProviderOptions, providerutils.OptionSchema{
		"queueThreshold": providerutils.OptInt(&cerebrasQueueThresholdMin, &cerebrasQueueThresholdMax),
	})
	if err != nil {
		return nil, err
	}
	threshold, ok := parsed["queueThreshold"]
	if !ok || threshold == nil {
		return opts, nil
	}
	n := cerebrasNumber(threshold)
	headers := make(map[string]string, len(opts.Headers)+1)
	for k, v := range opts.Headers {
		// TS normalizeHeaders lower-cases the call headers, so the
		// threshold replaces any spelling of the same header.
		if strings.EqualFold(k, "queue_threshold") {
			continue
		}
		headers[k] = v
	}
	headers["queue_threshold"] = providerutils.JSNumberString(n)
	withHeader := *opts
	withHeader.Headers = headers
	return &withHeader, nil
}

// cerebrasMetadataExtractor is TS cerebrasMetadataExtractor: the response's
// service_tier (a string, even ""; null, absent or a value of another type
// gives no metadata) as {cerebras: {serviceTier}}; streams keep the last
// non-null tier across chunks.
type cerebrasMetadataExtractor struct{}

func (cerebrasMetadataExtractor) ExtractMetadata(_ context.Context, parsedBody interface{}) (map[string]interface{}, error) {
	tier, ok := cerebrasServiceTier(parsedBody)
	return cerebrasServiceTierMetadata(tier, ok), nil
}

func (cerebrasMetadataExtractor) CreateStreamExtractor() openai.StreamMetadataExtractor {
	return &cerebrasStreamMetadataExtractor{}
}

type cerebrasStreamMetadataExtractor struct {
	serviceTier string
	present     bool
}

func (e *cerebrasStreamMetadataExtractor) ProcessChunk(parsedChunk interface{}) {
	if tier, ok := cerebrasServiceTier(parsedChunk); ok {
		e.serviceTier, e.present = tier, true
	}
}

func (e *cerebrasStreamMetadataExtractor) BuildMetadata() map[string]interface{} {
	return cerebrasServiceTierMetadata(e.serviceTier, e.present)
}

// cerebrasServiceTier reads z.looseObject({service_tier: z.string().nullish()}):
// ok is false for a non-object, a schema failure, null or absence.
func cerebrasServiceTier(value interface{}) (string, bool) {
	obj, isObj := value.(map[string]interface{})
	if !isObj {
		return "", false
	}
	tier, ok := obj["service_tier"].(string)
	return tier, ok
}

func cerebrasServiceTierMetadata(tier string, ok bool) map[string]interface{} {
	if !ok {
		return nil
	}
	return map[string]interface{}{"cerebras": map[string]interface{}{"serviceTier": tier}}
}

// cerebrasNumber reads a validated provider-option number.
func cerebrasNumber(v interface{}) float64 {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Float32, reflect.Float64:
		return rv.Float()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint())
	}
	return 0
}
