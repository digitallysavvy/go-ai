package fx

import (
	"net/url"
	"testing"
)

// V061 item 7 (cr-r86b-prova-to-integrator 1): TS fx-transport.ts 63
// url.pathname.endsWith('/ai/language-model') on the WHATWG URL (Node 24:
// language%2Dmodel false, x/../language-model true).
func TestR86bintegGatewayModelRequestWHATWGPathname(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://gw.example/v4/ai/language%2Dmodel":        false,
		"https://gw.example/v4/ai/x/../language-model":     true,
		"https://gw.example/v4/ai/language-model":          true,
		"https://gw.example/v4/ai/language-model?x=1":      true,
		"https://gw.example/v4/ai/language-model/embeddin": false,
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := (gatewayProtocol{}).isModelRequest(u, true); got != want {
			t.Errorf("isModelRequest(%s) = %v, want %v", raw, got, want)
		}
	}
	u, _ := url.Parse("https://gw.example/v4/ai/language-model")
	if (gatewayProtocol{}).isModelRequest(u, false) {
		t.Error("non-gateway request counted as a model request")
	}
}
