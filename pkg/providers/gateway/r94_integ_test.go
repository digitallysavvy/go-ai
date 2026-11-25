package gateway

import "testing"

// TestR94integURLsAreWHATWG: the Gateway WebSocket URLs are TS
// `new URL(`${baseURL.replace(/^http/, 'ws')}${path}`)` plus
// searchParams.set('ai-model-id', ...) (the base URL's query kept), the
// client-secret endpoint is new URL(path, baseURL) and the metadata calls use
// new URL(baseURL).origin (cr-r94-provb-to-provg). Expected from Node 24.
func TestR94integURLsAreWHATWG(t *testing.T) {
	for base, want := range map[string]string{
		"https://AI-Gateway.Vercel.sh:443/v3/ai?x=1": "wss://ai-gateway.vercel.sh/v3/ai?x=1%2Frealtime-model&ai-model-id=openai%2Fgpt-realtime",
		"HTTPS://h/v3/ai": "https://h/v3/ai/realtime-model?ai-model-id=openai%2Fgpt-realtime",
	} {
		if got := ToGatewayRealtimeURL(base, "openai/gpt-realtime"); got != want {
			t.Errorf("ToGatewayRealtimeURL(%q) = %s, want %s", base, got, want)
		}
	}
	if got := ToGatewayRealtimeURL("not a url", "m"); got != "" {
		t.Errorf("exported helper with an invalid base = %q, want \"\"", got)
	}
	if _, err := gatewayWebSocketURLChecked("not a url", "/speech-model", "m"); err == nil || err.Error() != "Invalid URL" {
		t.Errorf("invalid base: err = %v, want Invalid URL", err)
	}
	got, err := gatewayOriginURL("https://U:P@AI-Gateway.Vercel.sh:443/v3/ai", "/v1/realtime/client-secrets")
	if err != nil || got != "https://U:P@ai-gateway.vercel.sh/v1/realtime/client-secrets" {
		t.Errorf("client secret URL = %q, %v", got, err)
	}
	origin, err := gatewayBaseOrigin("https://U:P@AI-Gateway.Vercel.sh:443/v3/ai")
	if err != nil || origin != "https://ai-gateway.vercel.sh" {
		t.Errorf("origin = %q, %v", origin, err)
	}
	if _, err := gatewayBaseOrigin("foo://h/x"); err == nil || err.Error() != "Invalid URL" {
		t.Errorf("opaque origin: err = %v, want Invalid URL", err)
	}
}
