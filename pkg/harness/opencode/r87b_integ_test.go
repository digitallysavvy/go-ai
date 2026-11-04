package opencode

import "testing"

// Isolated test for tools 4ce90c2e (TS 3fab50aa9b opencode-harness.ts
// 537-557, `subscriptionAccessToken == null`): a forwarded empty subscription
// access token still writes OPENCODE_AUTH_CONTENT, and the token variable
// itself is never forwarded. Expected content from bun running the TS
// createOpenCodeSubscriptionAuthContent with accessToken "".
func TestR87bintegBridgeAuthEnvironmentEmptyTokenWritesAuthContent(t *testing.T) {
	sub := &Subscription{ProviderID: SubscriptionOpenAI, AccessToken: "host", AccountID: "acc"}
	for _, tc := range []struct {
		name      string
		forwarded map[string]string
		sub       *Subscription
		brokered  bool
		want      string // "" = no OPENCODE_AUTH_CONTENT
	}{
		{"empty token", map[string]string{SubscriptionAccessTokenEnvironmentVariable: "", "K": "v"}, sub, false,
			`{"openai":{"type":"oauth","access":"","refresh":"host-managed","expires":9007199254740991,"accountId":"acc"}}`},
		{"opencode-go empty token", map[string]string{SubscriptionAccessTokenEnvironmentVariable: ""}, &Subscription{ProviderID: SubscriptionOpenCodeGo, AccessToken: "host"}, false,
			`{"opencode-go":{"type":"api","key":""}}`},
		{"absent token", map[string]string{"K": "v"}, sub, false, ""},
		{"no subscription", map[string]string{SubscriptionAccessTokenEnvironmentVariable: "t"}, nil, false, ""},
		{"gitlab brokered", map[string]string{SubscriptionAccessTokenEnvironmentVariable: "t"}, sub, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, err := openCodeBridgeAuthEnvironment(tc.forwarded, tc.sub, tc.brokered)
			if err != nil {
				t.Fatal(err)
			}
			if _, leaked := env[SubscriptionAccessTokenEnvironmentVariable]; leaked {
				t.Fatalf("access token variable forwarded: %v", env)
			}
			got, ok := env["OPENCODE_AUTH_CONTENT"]
			if tc.want == "" {
				if ok {
					t.Fatalf("OPENCODE_AUTH_CONTENT = %s, want none", got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("OPENCODE_AUTH_CONTENT = %s, want %s", got, tc.want)
			}
		})
	}
}
