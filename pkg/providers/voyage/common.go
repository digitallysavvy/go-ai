package voyage

import "github.com/digitallysavvy/go-ai/pkg/provider"

func optsHeaders(opts *provider.EmbedModelOptions) map[string]string {
	if opts == nil {
		return nil
	}
	return opts.Headers
}
