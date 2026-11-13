package cerebras

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
)

func r96toolsCaptureBody(t *testing.T, providerOptions map[string]interface{}) string {
	t.Helper()
	var mu sync.Mutex
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = string(b)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	model, err := New(Config{APIKey: "k", BaseURL: server.URL}).LanguageModel("gpt-oss-120b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "a <b> & c"},
		ProviderOptions: providerOptions,
	}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	return body
}

// TS sends JSON.stringify(transformRequestBody(args)): with per-call
// providerOptions.cerebras extras the body is still JSON.stringify output,
// so `<`, `>` and `&` stay literal as they do without extras.
func TestR96toolsCerebrasExtrasBodyKeepsHTMLLiteral(t *testing.T) {
	plain := r96toolsCaptureBody(t, nil)
	withExtras := r96toolsCaptureBody(t, map[string]interface{}{"cerebras": map[string]interface{}{"parallelToolCalls": false}})
	for _, body := range []string{plain, withExtras} {
		if !strings.Contains(body, `a <b> & c`) {
			t.Fatalf("body = %s, want the prompt text literal", body)
		}
	}
	if !strings.Contains(withExtras, `"parallel_tool_calls":false`) {
		t.Fatalf("body = %s, want parallel_tool_calls", withExtras)
	}
}
