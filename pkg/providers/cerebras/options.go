package cerebras

import (
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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
