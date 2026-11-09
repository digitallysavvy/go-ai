package cerebras

import (
	"context"
	"errors"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// errNoPrepareRequest is returned when the wrapped chat model is not the
// OpenAI-compatible chat model (never for a model from Provider).
var errNoPrepareRequest = errors.New("cerebras: the base chat model does not support PrepareRequest")

// PrepareRequest returns the Chat Completions body this model sends for
// opts and the call warnings, without a network call (TS c9c6402cbd
// prepareRequest, inherited from OpenAICompatibleChatLanguageModel). The
// config transformRequestBody (transformCerebrasRequestBody, including the
// per-call providerOptions.cerebras fields) applies; the doGenerate
// override does not (no queue_threshold header, no structured-output
// tool-call fix), as in TS.
func (m *LanguageModel) PrepareRequest(ctx context.Context, opts *provider.GenerateOptions) (map[string]interface{}, []types.Warning, error) {
	base, ok := m.base.(*openai.LanguageModel)
	if !ok {
		return nil, nil, errNoPrepareRequest
	}
	ctx, _ = m.withCerebrasOptions(ctx, opts)
	body, warnings, err := base.PrepareRequest(ctx, opts)
	if err != nil {
		return nil, nil, err
	}
	if extras, ok := ctx.Value(cerebrasOptionsContextKey{}).(cerebrasRequestExtras); ok {
		applyCerebrasRequestExtras(body, extras)
	}
	return body, warnings, nil
}

// ConvertResponse converts a Chat Completions response body into a
// generate result without a network call (TS c9c6402cbd convertResponse,
// inherited unchanged: no Cerebras finish-reason normalization).
func (m *LanguageModel) ConvertResponse(ctx context.Context, body []byte, providerOptions map[string]interface{}) (*types.GenerateResult, error) {
	base, ok := m.base.(*openai.LanguageModel)
	if !ok {
		return nil, errNoPrepareRequest
	}
	return base.ConvertResponse(ctx, body, providerOptions)
}
