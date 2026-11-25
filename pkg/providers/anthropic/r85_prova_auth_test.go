package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
)

// anthropic-provider.test.ts refreshable auth tokens (TS 63bf860638): the
// token func runs before each request and is sent as a bearer token, with
// no x-api-key; it cannot be combined with apiKey or authToken.
func TestR85provaAnthropicAuthTokenFunc(t *testing.T) {
	var auth, apiKey []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
		apiKey = append(apiKey, r.Header.Get("x-api-key"))
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	t.Setenv("ANTHROPIC_API_KEY", "env-key")
	tokens := []string{"t1", "t2"}
	p := New(Config{BaseURL: srv.URL, AuthTokenFunc: func(context.Context) (string, error) {
		tok := tokens[0]
		tokens = tokens[1:]
		return tok, nil
	}})
	for i := 0; i < 2; i++ {
		if _, err := p.client.Do(context.Background(), internalhttp.Request{Method: http.MethodGet, Path: "/r85"}); err != nil {
			t.Fatal(err)
		}
	}
	if auth[0] != "Bearer t1" || auth[1] != "Bearer t2" || apiKey[0] != "" || apiKey[1] != "" {
		t.Fatalf("Authorization = %q, x-api-key = %q", auth, apiKey)
	}
	for name, cfg := range map[string]Config{
		"apiKey":    {APIKey: "k", AuthTokenFunc: func(context.Context) (string, error) { return "t", nil }},
		"authToken": {AuthToken: "s", AuthTokenFunc: func(context.Context) (string, error) { return "t", nil }},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s + AuthTokenFunc: want a panic", name)
				}
			}()
			New(cfg)
		}()
	}
}
