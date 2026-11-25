package providers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
	"github.com/digitallysavvy/go-ai/pkg/providers/bytedance"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/typesafeai"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// r86bintegDeniedPrompt is a prompt whose tool message carries an
// execution-denied output. reasonSet marks an empty reason as given (TS
// `reason: ”`).
func r86bintegDeniedPrompt(reasonSet bool) types.Prompt {
	return types.Prompt{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "run it"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "call_1", ToolName: "weather", Input: `{"city":"Paris"}`}}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{
			ToolCallID: "call_1", ToolName: "weather",
			Output: &types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, Reason: "", ReasonSet: reasonSet},
		}}},
	}}
}

// V061 item 23 (gen 0dee3cf2 + the converter CRs applied by the integrator):
// TS `output.reason ?? '<default>'` keeps a given empty reason, so no
// provider sends its default denial text for it; an absent reason still gets
// the default. Every provider converter that has an execution-denied branch
// is reached through DoGenerate and its request body is inspected.
func TestR86bintegEmptyDenialReasonReachesTheWire(t *testing.T) {
	ctx := context.Background()
	builders := map[string]func(base string, f providerutils.APIKeyFunc) (provider.LanguageModel, error){}
	for name, b := range r85integKeyFuncs {
		builders[name] = b
	}
	builders["anthropic"] = func(b string, _ providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return anthropic.New(anthropic.Config{BaseURL: b, APIKey: "k"}).LanguageModel("claude-sonnet-4-5")
	}
	builders["openai responses"] = func(b string, _ providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return openai.New(openai.Config{BaseURL: b, APIKey: "k"}).ResponsesModel("gpt-4o")
	}
	builders["google interactions"] = func(b string, _ providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return google.New(google.Config{BaseURL: b, APIKey: "k"}).Interactions("gemini-2.5-flash")
	}
	builders["gateway"] = func(b string, _ providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		p, err := gateway.New(gateway.Config{BaseURL: b, APIKey: "k"})
		if err != nil {
			return nil, err
		}
		return p.LanguageModel("openai/gpt-4o")
	}
	builders["bedrock"] = func(b string, _ providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return bedrock.New(bedrock.Config{BaseURL: b, APIKey: "k", Region: "us-east-1"}).LanguageModel("anthropic.claude-3-5-sonnet-20241022-v2:0")
	}
	denialTexts := []string{"execution denied", "denied by user"}
	hasDefault := func(body string) bool {
		lower := strings.ToLower(body)
		for _, d := range denialTexts {
			if strings.Contains(lower, d) {
				return true
			}
		}
		return false
	}
	send := func(t *testing.T, build func(string, providerutils.APIKeyFunc) (provider.LanguageModel, error), reasonSet bool) (string, bool) {
		t.Helper()
		var c r86integCapture
		srv := c.server(t)
		m, err := build(srv.URL, func(context.Context) (string, error) { return "k", nil })
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		_, _ = m.DoGenerate(ctx, &provider.GenerateOptions{Prompt: r86bintegDeniedPrompt(reasonSet), Tools: []types.Tool{{
			Name: "weather", Description: "w",
			Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}},
		}}})
		return c.first()
	}
	for name, build := range builders {
		t.Run(name, func(t *testing.T) {
			absent, ok := send(t, build, false)
			if !ok {
				t.Skip("no request sent (provider rejects the prompt before the wire)")
			}
			if name == "gateway" {
				// The gateway forwards the prompt as is: the reason key is
				// absent, or "" when given.
				given, _ := send(t, build, true)
				if strings.Contains(absent, `"reason"`) || !strings.Contains(given, `"type":"execution-denied","reason":""`) && !strings.Contains(given, `"reason":"","type":"execution-denied"`) {
					t.Errorf("gateway reason: absent %s\ngiven %s", absent, given)
				}
				return
			}
			if !hasDefault(absent) {
				// The provider does not send tool results as text (no
				// execution-denied branch); nothing to compare.
				t.Skipf("absent reason sends no default denial text: %s", absent)
			}
			given, _ := send(t, build, true)
			if hasDefault(given) {
				t.Errorf("a given empty reason was replaced by the default denial text (TS reason ?? default keeps \"\"):\n%s", given)
			}
		})
	}
}

// cr-r86b-provb-to-integrator / cr-r86b-prova-to-integrator 4: TS
// withoutTrailingSlash removes ONE trailing "/" and the request paths are
// appended to that base as is, so a base URL ending in "//" keeps one
// (Node 24: withoutTrailingSlash("https://h/v1//") + "/x" =
// "https://h/v1//x"). Go trimmed all of them, and the poll URLs trimmed the
// normalized base a second time.
func TestR86bintegBaseURLOneTrailingSlash(t *testing.T) {
	ctx := context.Background()
	newServer := func(t *testing.T) (*httptest.Server, func() []string) {
		var mu sync.Mutex
		var paths []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			paths = append(paths, r.Method+" "+r.URL.Path)
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodPost {
				_, _ = w.Write([]byte(`{"id":"t1","status":"starting","code":0,"data":{"task_id":"t1","task_status":"submitted"}}`))
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"stop"}}`))
		}))
		t.Cleanup(srv.Close)
		return srv, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), paths...) }
	}
	cases := []struct {
		name string
		want string
		run  func(base string)
	}{
		{"typesafeai decide", "POST /v1//systemone", func(base string) {
			m, _ := typesafeai.New(typesafeai.Config{APIKey: "k", BaseURL: base + "/v1//"}).DecisionModel("jev-latest")
			_, _ = m.DoDecide(ctx, provider.DecisionCallOptions{State: "s", Questions: map[string]provider.DecisionQuestion{
				"q": {Type: "boolean", Instructions: "i"},
			}})
		}},
		{"bytedance video poll", "GET /v3//contents/generations/tasks/t1", func(base string) {
			p, _ := bytedance.New(bytedance.Config{APIKey: "k", BaseURL: base + "/v3//"})
			m, _ := p.VideoModel(string(bytedance.ModelSeedance20))
			_, _ = m.DoGenerate(ctx, &provider.VideoModelV3CallOptions{Prompt: "p", PromptSet: true})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, paths := newServer(t)
			tc.run(srv.URL)
			got := paths()
			found := false
			for _, p := range got {
				found = found || p == tc.want
			}
			if !found {
				t.Errorf("requests %v, want %q (TS one trailing slash)", got, tc.want)
			}
		})
	}
}
