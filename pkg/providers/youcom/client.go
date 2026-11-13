package youcom

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/mcp"
)

// defaultMCPURL is the hosted You.com MCP server createYouClient connects to.
const defaultMCPURL = "https://api.you.com/mcp"

// YouClientConfig configures CreateYouClient, matching the TypeScript
// @youdotcom-oss/ai-sdk-plugin (3.x) YouClientConfig.
type YouClientConfig struct {
	// APIKey is sent as `Authorization: Bearer <key>`. Nil falls back to
	// YDC_API_KEY (TS default parameter: only an undefined apiKey does); an
	// empty key, given or from the environment, sends no Authorization
	// header.
	APIKey *string

	// Tools scopes the hosted tool set to these tool ids (sent as
	// `tools=<ids joined by ",">`; TS tools?: string | string[]). Nil requests
	// the default set (you-search, you-contents, you-balance, you-discover).
	// Ignored when Profile is set.
	Tools []string

	// Profile names a hosted MCP profile that resolves the tools (sent as
	// `profile=<name>`); it takes precedence over Tools.
	Profile string

	// URL and HTTPClient are Go-specific testability hooks. Leave empty to
	// connect to https://api.you.com/mcp with the default client.
	URL        string
	HTTPClient *http.Client
}

// CreateYouClient connects to the hosted You.com MCP server and returns the
// initialized MCP client, matching the TypeScript
// @youdotcom-oss/ai-sdk-plugin createYouClient(config?) (3.x). Resolve its
// tools with mcp.GetMCPToolsForAgent (TS `await client.tools()`) and Close it
// when finished. A failed handshake closes the client and returns the error.
func CreateYouClient(ctx context.Context, config ...YouClientConfig) (*mcp.MCPClient, error) {
	var cfg YouClientConfig
	if len(config) > 0 {
		cfg = config[0]
	}
	var apiKey string
	if cfg.APIKey != nil {
		apiKey = *cfg.APIKey
	} else {
		apiKey = os.Getenv("YDC_API_KEY")
	}
	endpoint, err := youClientURL(cfg)
	if err != nil {
		return nil, err
	}
	var headers map[string]string
	if apiKey != "" {
		headers = map[string]string{"Authorization": "Bearer " + apiKey}
	}
	transport := mcp.NewHTTPTransport(mcp.HTTPTransportConfig{
		URL:        endpoint,
		HTTPClient: cfg.HTTPClient,
		Config:     mcp.TransportConfig{Headers: headers},
	})
	client := mcp.NewMCPClient(transport, mcp.MCPClientConfig{})
	if err := client.Connect(ctx); err != nil {
		return nil, err
	}
	return client, nil
}

// youClientURL is TS `new URL('https://api.you.com/mcp')` with
// `searchParams.set('profile', ...)` or, without a profile,
// `searchParams.set('tools', tools.join(','))` (URLSearchParams escaping).
func youClientURL(cfg YouClientConfig) (string, error) {
	base := cfg.URL
	if base == "" {
		base = defaultMCPURL
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if cfg.Profile == "" && cfg.Tools == nil {
		return u.String(), nil
	}
	query := internalhttp.ParseSearchParams(u.RawQuery)
	if cfg.Profile != "" {
		query.Set("profile", cfg.Profile)
	} else {
		// a JS array is truthy even when empty: `tools: []` sends `tools=`
		query.Set("tools", strings.Join(cfg.Tools, ","))
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}
