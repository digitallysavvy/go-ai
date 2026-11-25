package bridge

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/urlnorm"
)

// TokenQueryParam is the query parameter a legacy bridge authenticates host
// connections with. A connection with a missing or wrong token is closed with
// code 1008 ("unauthorized").
//
// Deprecated: bridges authenticate hosts with a per-session Ed25519 key pair
// (CreateBridgeAuthKeyPair; TS c849d37dac).
const TokenQueryParam = "agent_bridge_token"

// CreateBridgeToken returns a random 32-byte hexadecimal bridge channel
// token. Mirrors the removed TS `createBridgeToken` (371e954).
//
// Deprecated: use CreateBridgeAuthKeyPair. A token readable in the sandbox
// lets sandbox code take over the bridge.
func CreateBridgeToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand never fails on supported platforms.
		panic(fmt.Sprintf("harness bridge: crypto/rand: %v", err))
	}
	return hex.EncodeToString(b)
}

// WithBridgeToken returns a copy of endpoint whose URL carries token in the
// `agent_bridge_token` query parameter, replacing any existing value. Other
// query parameters keep their order, and headers are copied. Mirrors TS
// `withBridgeToken`: `new URL(endpoint.url)`, `searchParams.set` and the
// WHATWG serialization (lowercased host, default port dropped, the query
// re-encoded as application/x-www-form-urlencoded).
//
// Deprecated: current bridges authenticate with a public key, and the
// endpoint is used unchanged (BridgeConnectOptions.AuthPrivateKey).
func WithBridgeToken(endpoint harness.PortEndpoint, token string) (harness.PortEndpoint, error) {
	u, err := urlnorm.Parse(endpoint.URL)
	if err != nil {
		return harness.PortEndpoint{}, fmt.Errorf("harness bridge: invalid endpoint URL: %w", err)
	}
	rawQuery := ""
	if u.Query != nil {
		rawQuery = *u.Query
	}
	query := internalhttp.SetSearchParam(rawQuery, TokenQueryParam, token)
	u.Query = &query

	out := harness.PortEndpoint{URL: u.String()}
	if endpoint.Headers != nil {
		out.Headers = make(map[string]string, len(endpoint.Headers))
		for k, v := range endpoint.Headers {
			out.Headers[k] = v
		}
	}
	return out, nil
}
