package tsfixture_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	"github.com/digitallysavvy/go-ai/pkg/providers/huggingface"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	"github.com/digitallysavvy/go-ai/pkg/providers/ollama"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// r83provbInner and r83provbArgs exercise the struct schema rules: required
// tags, a nullable pointer object, an omitempty slice and root recursion.
type r83provbInner struct {
	Z string `json:"z" validate:"required"`
	A *int   `json:"a"`
}
type r83provbArgs struct {
	Zeta  string      `json:"zeta" validate:"required"`
	Alpha []string    `json:"alpha,omitempty"`
	Inner *r83provbInner `json:"inner"`
	Next  *r83provbArgs  `json:"next,omitempty"`
}

// TestR83provbStructSchemaWireOrder sends a struct schema (reviewer item 2)
// as tool parameters and as the ObjectOutput response schema through every
// provb chat / Responses provider: TS sends the zod JSON Schema object as is
// (moonshot strips the top-level $schema from response_format only; the
// json_object providers have no response schema), so the wire text must be
// the StructValidator JSON in its field order.
func TestR83provbStructSchemaWireOrder(t *testing.T) {
	var mu sync.Mutex
	var last []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		last = b
		mu.Unlock()
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"message":"x"}}`))
	}))
	defer srv.Close()
	sst := schema.NewSimpleStructSchema(reflect.TypeOf(r83provbArgs{}))
	wantJSON, err := json.Marshal(sst)
	if err != nil {
		t.Fatal(err)
	}
	want := string(wantJSON)
	noDollar := `{` + want[strings.Index(want, `"type":"object"`):]
	cases := []struct {
		name     string
		model    func() (provider.LanguageModel, error)
		response string // expected response schema text; "" = json_object
	}{
		{"openai chat", func() (provider.LanguageModel, error) { return openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL}).ChatModel("gpt-4o") }, want},
		{"openai responses", func() (provider.LanguageModel, error) { return openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL}).ResponsesModel("gpt-4o") }, want},
		{"xai", func() (provider.LanguageModel, error) { return xai.New(xai.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("grok-4") }, want},
		{"deepseek", func() (provider.LanguageModel, error) { return deepseek.New(deepseek.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("deepseek-chat") }, ""},
		{"alibaba", func() (provider.LanguageModel, error) { return alibaba.New(alibaba.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("qwen-plus") }, ""},
		{"moonshot", func() (provider.LanguageModel, error) { return moonshot.New(moonshot.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("kimi-k2.5") }, noDollar},
		{"openresponses", func() (provider.LanguageModel, error) { return openresponses.New(openresponses.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m") }, want},
		{"huggingface", func() (provider.LanguageModel, error) { return huggingface.New(huggingface.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m") }, want},
		{"gmicloud", func() (provider.LanguageModel, error) { return gmicloud.New(gmicloud.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m") }, ""},
		{"together", func() (provider.LanguageModel, error) { return together.New(together.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m") }, ""},
		{"fireworks", func() (provider.LanguageModel, error) { return fireworks.New(fireworks.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m") }, want},
		{"zai", func() (provider.LanguageModel, error) { return zai.New(zai.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("glm-4.6") }, ""},
		{"ollama", func() (provider.LanguageModel, error) { return ollama.New(ollama.Config{BaseURL: srv.URL}).LanguageModel("m") }, want},
	}
	for _, c := range cases {
		m, err := c.model()
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		_, _ = ai.GenerateText(context.Background(), ai.GenerateTextOptions{
			Model: m, Prompt: "hi", MaxRetries: new(int),
			Tools: []types.Tool{{Name: "t", Parameters: sst, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return nil, nil
			}}},
			Output: ai.ObjectOutput[r83provbArgs](ai.ObjectOutputOptions{Schema: sst}),
		})
		mu.Lock()
		body := string(last)
		mu.Unlock()
		if !strings.Contains(body, `"parameters":`+want) {
			t.Errorf("%s: tool parameters not sent as the struct schema text:\n%s", c.name, body)
		}
		if c.response != "" && !strings.Contains(body, `"schema":`+c.response) {
			t.Errorf("%s: response schema not sent as %s:\n%s", c.name, c.response, body)
		}
	}
}
