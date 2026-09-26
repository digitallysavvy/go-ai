package bridge

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// TokenQueryParam is the query parameter the bridge authenticates host
// connections with. A connection with a missing or wrong token is closed with
// code 1008 ("unauthorized").
const TokenQueryParam = "agent_bridge_token"

// CreateBridgeToken returns a random 32-byte hexadecimal bridge channel
// token. Mirrors TS `createBridgeToken` (371e954).
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
// `withBridgeToken` (WHATWG `URLSearchParams.set` semantics and encoding).
func WithBridgeToken(endpoint harness.PortEndpoint, token string) (harness.PortEndpoint, error) {
	u, err := url.Parse(endpoint.URL)
	if err != nil {
		return harness.PortEndpoint{}, fmt.Errorf("harness bridge: invalid endpoint URL: %w", err)
	}

	type pair struct{ key, value string }
	var pairs []pair
	if u.RawQuery != "" {
		for _, segment := range strings.Split(u.RawQuery, "&") {
			if segment == "" {
				continue
			}
			key, value, _ := strings.Cut(segment, "=")
			pairs = append(pairs, pair{formDecode(key), formDecode(value)})
		}
	}
	// URLSearchParams.set: replace the first match in place, drop the rest,
	// append when absent.
	replaced := false
	kept := pairs[:0]
	for _, p := range pairs {
		if p.key == TokenQueryParam {
			if replaced {
				continue
			}
			p.value, replaced = token, true
		}
		kept = append(kept, p)
	}
	if !replaced {
		kept = append(kept, pair{TokenQueryParam, token})
	}

	encoded := make([]string, len(kept))
	for i, p := range kept {
		encoded[i] = formEncode(p.key) + "=" + formEncode(p.value)
	}
	u.RawQuery = strings.Join(encoded, "&")
	u.ForceQuery = false

	out := harness.PortEndpoint{URL: u.String()}
	if endpoint.Headers != nil {
		out.Headers = make(map[string]string, len(endpoint.Headers))
		for k, v := range endpoint.Headers {
			out.Headers[k] = v
		}
	}
	return out, nil
}

// formEncode implements the application/x-www-form-urlencoded byte
// serializer used by URLSearchParams: ASCII alphanumerics and `*-._` stay as
// is, space becomes `+`, everything else is percent-encoded.
func formEncode(s string) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '*' || c == '-' || c == '.' || c == '_':
			b.WriteByte(c)
		case c == ' ':
			b.WriteByte('+')
		default:
			b.WriteByte('%')
			b.WriteByte(hexDigits[c>>4])
			b.WriteByte(hexDigits[c&0x0f])
		}
	}
	return b.String()
}

func formDecode(s string) string {
	if v, err := url.QueryUnescape(s); err == nil {
		return v
	}
	return s
}
