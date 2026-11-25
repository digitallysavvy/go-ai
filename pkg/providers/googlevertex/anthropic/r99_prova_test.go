package anthropic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS createVertexAnthropic: the provider headers (options.headers) are the
// model's config headers, combined before the call headers; postJsonToApi
// spreads them over its default Content-Type and withUserAgentSuffix appends
// the runtime tag to their user agent. So a provider User-Agent is sent
// followed by the runtime tag and a provider Content-Type replaces the
// default, unless the call sets that header (any spelling).
func TestR99provaProviderUserAgentAndContentType(t *testing.T) {
	var mu sync.Mutex
	var uas, cts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		uas = append(uas, r.Header.Get("User-Agent"))
		cts = append(cts, r.Header.Get("Content-Type"))
		mu.Unlock()
		if strings.HasSuffix(r.URL.Path, ":streamRawPredict") {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":2,"output_tokens":3}}`))
	}))
	defer srv.Close()
	rt := providerutils.RuntimeEnvironmentUserAgent()
	configHeaders := map[string]string{"user-agent": "my-app/1.0", "Content-Type": "application/json; charset=utf-8"}

	for _, tc := range []struct {
		name           string
		opts           Options
		call           map[string]string
		wantUA, wantCT string
	}{
		{"static", Options{Headers: configHeaders}, nil, "my-app/1.0 " + rt, "application/json; charset=utf-8"},
		{"resolver", Options{HeadersResolver: StaticHeaders(configHeaders)}, nil, "my-app/1.0 " + rt, "application/json; charset=utf-8"},
		{"call wins", Options{Headers: configHeaders}, map[string]string{"User-Agent": "call/2", "content-type": "application/json"}, "call/2 " + rt, "application/json"},
		{"no provider headers", Options{}, nil, rt, "application/json"},
	} {
		opts := tc.opts
		opts.BaseURL = srv.URL
		opts.AuthToken = func(context.Context) (string, error) { return "t", nil }
		opts.HTTPClient = srv.Client()
		p := NewGoogleVertexAnthropicProvider(opts)
		model, err := p.LanguageModel(string(ClaudeSonnet4_6))
		if err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		uas, cts = nil, nil
		mu.Unlock()
		if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Headers: tc.call}); err != nil {
			t.Fatalf("%s: DoGenerate: %v", tc.name, err)
		}
		s, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Headers: tc.call})
		if err != nil {
			t.Fatalf("%s: DoStream: %v", tc.name, err)
		}
		for {
			if _, err := s.Next(); err != nil {
				break
			}
		}
		_ = s.Close()
		mu.Lock()
		if len(uas) != 2 {
			t.Fatalf("%s: %d requests", tc.name, len(uas))
		}
		for i := range uas {
			if uas[i] != tc.wantUA || cts[i] != tc.wantCT {
				t.Errorf("%s request %d: User-Agent %q Content-Type %q, want %q / %q", tc.name, i, uas[i], cts[i], tc.wantUA, tc.wantCT)
			}
		}
		mu.Unlock()
	}
}
