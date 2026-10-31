package gateway

import (
	"net/url"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
)

// gatewayWebSocketURL is TS `new URL(`${baseURL.replace(/^http/, 'ws')}${path}`)`
// followed by `searchParams.set('ai-model-id', modelId)`: only a leading
// "http" is upgraded, and the query uses URLSearchParams escaping ("*" literal,
// "~" percent-encoded), which differs from Go's url.Values.Encode.
func gatewayWebSocketURL(baseURL, path, modelID string) string {
	raw := baseURL
	if strings.HasPrefix(raw, "http") {
		raw = "ws" + strings.TrimPrefix(raw, "http")
	}
	u, err := url.Parse(raw + path)
	if err != nil {
		return ""
	}
	u.RawQuery = internalhttp.EncodeQuery(map[string]string{"ai-model-id": modelID})
	return u.String()
}
