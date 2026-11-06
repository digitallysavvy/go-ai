package errors

import "testing"

// Exact TS GatewayAuthenticationError.createContextualError texts.
func TestContextualAuthenticationErrorMessagesMatchTS(t *testing.T) {
	cases := []struct {
		apiKey, oidc bool
		want         string
	}{
		{true, false, "AI Gateway authentication failed: Invalid API key or token.\n\nCreate a new API key: https://vercel.com/d?to=%2F%5Bteam%5D%2F%7E%2Fai%2Fapi-keys\n\nProvide an API key or Vercel access token via 'apiKey' option or 'AI_GATEWAY_API_KEY' environment variable."},
		{false, true, "AI Gateway authentication failed: Invalid OIDC token.\n\nRun 'npx vercel link' to link your project, then 'vc env pull' to fetch the token.\n\nAlternatively, use an API key: https://vercel.com/d?to=%2F%5Bteam%5D%2F%7E%2Fai%2Fapi-keys\nor pass a Vercel access token via the 'apiKey' option."},
		{false, false, "AI Gateway authentication failed: No authentication provided.\n\nOption 1 - API key:\nCreate an API key: https://vercel.com/d?to=%2F%5Bteam%5D%2F%7E%2Fai%2Fapi-keys\nProvide via 'apiKey' option or 'AI_GATEWAY_API_KEY' environment variable.\n\nOption 2 - Vercel access token:\nPass a Vercel personal access token or Vercel app access token via the 'apiKey' option.\n\nOption 3 - OIDC token:\nRun 'npx vercel link' to link your project, then 'vc env pull' to fetch the token."},
	}
	for _, tc := range cases {
		if got := CreateContextualAuthenticationError(tc.apiKey, tc.oidc, 401, nil, "").Error(); got != tc.want {
			t.Errorf("apiKey=%v oidc=%v:\n got %q\nwant %q", tc.apiKey, tc.oidc, got, tc.want)
		}
	}
}
