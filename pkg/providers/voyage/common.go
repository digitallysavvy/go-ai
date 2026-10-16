package voyage

import (
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// handleError wraps a transport/API error as a ProviderError, leaving the
// errors TS throws unwrapped (LoadAPIKeyError, the "Invalid JSON response"
// APICallError) untouched.
func handleError(err error) error {
	if providererrors.IsLoadAPIKeyError(err) || providerutils.IsInvalidJSONResponse(err) {
		return err
	}
	return providererrors.NewProviderError("voyage", 0, "", err.Error(), err)
}

func optsHeaders(opts *provider.EmbedModelOptions) map[string]string {
	if opts == nil {
		return nil
	}
	return opts.Headers
}
