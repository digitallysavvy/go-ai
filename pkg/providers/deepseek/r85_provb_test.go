package deepseek

import (
	"context"
	"errors"
	"fmt"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS 4ae6986cd8 serializeDeepSeekToolCallArguments: valid JSON input text
// is sent as is, other text as its JSON string literal.
func TestR85provbDeepSeekToolCallArguments(t *testing.T) {
	for raw, want := range map[string]string{
		`{"query":"deepseek","limit":3}`: `{"query":"deepseek","limit":3}`,
		`not json`:                       `"not json"`,
		`[1]`:                            `[1]`,
	} {
		got := deepseekToolCallsWire([]types.ToolCall{{ID: "c", ToolName: "t", RawArguments: raw}})[0]["function"].(map[string]interface{})["arguments"]
		if got != want {
			t.Errorf("%q: got %v, want %s", raw, got, want)
		}
	}
}

// TestR85provbAPIKeyFunc: TS 7716e189ee async credentials. The APIKeyFunc is
// called for every request and its key is sent; the environment is not
// read; its error fails the request; APIKey plus APIKeyFunc is an
// InvalidArgumentError.
func TestR85provbAPIKeyFunc(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "env-key")
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"stop"}}`))
	}))
	defer srv.Close()

	calls := 0
	keyFunc := func(ctx context.Context) (string, error) {
		calls++
		return fmt.Sprintf("k%d", calls), nil
	}
	call := func(cfg Config) error {
		model, err := New(cfg).LanguageModel("deepseek-chat")
		if err != nil {
			return err
		}
		_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		return err
	}
	cfg := Config{BaseURL: srv.URL}
	cfg.APIKeyFunc = keyFunc
	_ = call(cfg)
	_ = call(cfg)
	mu.Lock()
	got := append([]string(nil), seen...)
	mu.Unlock()
	if len(got) != 2 || got[0] != "Bearer k1" || got[1] != "Bearer k2" {
		t.Fatalf("credential headers = %q, want Bearer k1, Bearer k2", got)
	}

	sentinel := errors.New("vault unavailable")
	cfg.APIKeyFunc = func(context.Context) (string, error) { return "", sentinel }
	if err := call(cfg); !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the APIKeyFunc error", err)
	}

	cfg.APIKeyFunc = keyFunc
	cfg.APIKey = "static"
	err := call(cfg)
	var invalid *providererrors.InvalidArgumentError
	if !errors.As(err, &invalid) {
		t.Fatalf("both set: err = %v, want InvalidArgumentError", err)
	}
}
