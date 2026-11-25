package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// The TS case suites of this package evaluate the extended assertion forms.
func init() { tsfixture.EnableExtendedAsserts() }

// A Responses function tool whose Parameters is a schema.Schema is sent as
// its JSON Schema and normalized like one (TS normalizeOpenAIJsonSchema runs
// on every tool.inputSchema): the lookaround pattern is removed with the
// compatibility warning, as for a map schema and as on Chat Completions.
func TestR84provbResponsesSchemaValueParametersNormalized(t *testing.T) {
	params := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"code": map[string]interface{}{"type": "string", "pattern": "^(?=a).*$"},
		},
	})
	model := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "gpt-5")
	body, _, warnings, err := model.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools:  []types.Tool{{Name: "lookup", Parameters: params}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(body["tools"])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "pattern") || !strings.Contains(string(b), `"code":{"type":"string"}`) {
		t.Fatalf("tools = %s, want the lookaround pattern removed", b)
	}
	found := false
	for _, w := range warnings {
		if w.Feature == "JSON Schema pattern with regex lookaround" {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %#v, want the lookaround compatibility warning", warnings)
	}
}

// TS doCreateClientSecret builds the WebSocket URL with
// encodeURIComponent(modelId): a space is %20 and ! ' ( ) * stay literal
// (url.QueryEscape wrote + and %21...).
func TestR84provbRealtimeURLUsesEncodeURIComponent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"value": "secret"})
	}))
	defer srv.Close()
	model := NewRealtimeModel(New(Config{APIKey: "k", BaseURL: srv.URL + "/v1"}), "my model(1)!")
	secret, err := model.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if want := "wss://" + strings.TrimPrefix(srv.URL, "http://") + "/v1/realtime?model=my%20model(1)!"; secret.URL != want {
		t.Fatalf("url = %q, want %q", secret.URL, want)
	}
}

// TS getFileId: fileId.trim() === ” uses JavaScript trim, which strips a
// BOM (U+FEFF) but not NEL (U+0085); strings.TrimSpace did the opposite.
func TestR84provbFileIDUsesJSTrim(t *testing.T) {
	if _, err := openAIFileID(types.ProviderReference{"openai": "\ufeff"}); err == nil {
		t.Error("BOM-only id accepted")
	}
	if id, err := openAIFileID(types.ProviderReference{"openai": "\u0085"}); err != nil || id != "\u0085" {
		t.Errorf("NEL id = %q, %v", id, err)
	}
}
