package gateway

import (
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/urlnorm"
)

// gatewayWebSocketURL is gatewayWebSocketURLChecked for the exported URL
// helpers, which keep their v0.5.2 string result: "" when the base URL does
// not parse.
func gatewayWebSocketURL(baseURL, path, modelID string) string {
	u, _ := gatewayWebSocketURLChecked(baseURL, path, modelID)
	return u
}

// gatewayWebSocketURLChecked is TS
// `new URL(`${baseURL.replace(/^http/, 'ws')}${path}`)` followed by
// `searchParams.set('ai-model-id', modelId)`: only a leading "http" is
// upgraded, the URL is the WHATWG serialization (host lower-cased, a default
// port left out), the base URL's own query parameters are kept, the query
// uses URLSearchParams escaping ("*" literal, "~" percent-encoded), and a
// URL that does not parse is the TypeError "Invalid URL".
func gatewayWebSocketURLChecked(baseURL, path, modelID string) (string, error) {
	raw := baseURL
	if strings.HasPrefix(raw, "http") {
		raw = "ws" + strings.TrimPrefix(raw, "http")
	}
	u, err := urlnorm.Parse(raw + path)
	if err != nil {
		return "", err
	}
	query := ""
	if u.Query != nil {
		query = *u.Query
	}
	query = internalhttp.SetSearchParam(query, "ai-model-id", modelID)
	u.Query = &query
	return u.String(), nil
}
