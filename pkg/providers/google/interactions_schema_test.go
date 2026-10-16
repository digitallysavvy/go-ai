package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// ai.GenerateObject passes a schema.Schema as ResponseFormat.Schema and Go
// callers pass one as Tool.Parameters; neither may reach the wire as {}.
func TestInteractionsSchemaSchemaReachesTheWire(t *testing.T) {
	var raw []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&m)
		raw, _ = json.Marshal(m)
		_, _ = fmt.Fprint(w, `{"id":"v1","status":"completed","model":"gemini-2.5-flash","steps":[{"type":"model_output","content":[{"type":"text","text":"{}"}]}],"usage":{}}`)
	}))
	defer server.Close()
	model, err := New(Config{APIKey: "k", BaseURL: server.URL}).Interactions(ModelGemini25Flash)
	if err != nil {
		t.Fatal(err)
	}
	s := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"age": map[string]interface{}{"type": "integer"}},
	})
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "x"},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: s},
		Tools:          []types.Tool{{Name: "t", Type: types.ToolTypeFunction, Parameters: s}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), `"age"`); n != 2 {
		t.Fatalf("schema reached the wire %d times, want 2: %s", n, raw)
	}
}

// TS falls back to the x-gemini-service-tier response header when the body has
// no service_tier, for generate and for streams.
func TestInteractionsServiceTierFromHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-gemini-service-tier", "priority")
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"event_type\":\"interaction.complete\",\"interaction\":{\"id\":\"v1\",\"status\":\"completed\",\"usage\":{}}}\n\n")
			return
		}
		_, _ = fmt.Fprint(w, `{"id":"v1","status":"completed","model":"gemini-2.5-flash","steps":[{"type":"model_output","content":[{"type":"model_output","content":[{"type":"text","text":"hi"}]}]}],"usage":{}}`)
	}))
	defer server.Close()
	model, err := New(Config{APIKey: "k", BaseURL: server.URL}).Interactions(ModelGemini25Flash)
	if err != nil {
		t.Fatal(err)
	}
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}}
	res, err := model.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]map[string]interface{}
	switch pm := interface{}(res.ProviderMetadata).(type) {
	case map[string]interface{}:
		b, _ := json.Marshal(pm)
		_ = json.Unmarshal(b, &meta)
	}
	if meta["google"]["serviceTier"] != "priority" {
		t.Fatalf("generate providerMetadata = %v", res.ProviderMetadata)
	}

	st, err := model.DoStream(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close() //nolint:errcheck
	var finish *provider.StreamChunk
	for {
		c, err := st.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if finish == nil || !strings.Contains(string(finish.ProviderMetadata), `"serviceTier":"priority"`) {
		t.Fatalf("finish chunk = %#v", finish)
	}
}

// TS getArgs: environment maps to {type:"remote", sources?, network?} for
// agents, and is dropped with a warning on model-id calls; background is sent
// only when set.
func TestInteractionsEnvironmentAndBackgroundMatchTS(t *testing.T) {
	p := New(Config{APIKey: "k"})
	agent, _ := p.InteractionsAgent("deep-research-pro-preview-12-2025")
	model, _ := p.Interactions(ModelGemini25Flash)
	env := map[string]interface{}{
		"type": "remote",
		"sources": []interface{}{
			map[string]interface{}{"type": "inline", "content": "c", "target": "/a.txt"},
			map[string]interface{}{"type": "gcs", "source": "gs://b/o"},
		},
		"network": map[string]interface{}{"allowlist": []interface{}{map[string]interface{}{"domain": "example.com"}}},
	}
	opts := &provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "x"},
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"environment": env}},
	}

	body, warnings, _, err := agent.(*InteractionsLanguageModel).buildArgs(opts, false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(body.Environment)
	want := `{"network":{"allowlist":[{"domain":"example.com"}]},"sources":[{"content":"c","target":"/a.txt","type":"inline"},{"source":"gs://b/o","type":"gcs"}],"type":"remote"}`
	if string(b) != want {
		t.Fatalf("environment = %s\nwant %s", b, want)
	}
	if body.Background != nil || len(warnings) != 0 {
		t.Fatalf("background = %v, warnings = %v; want none", body.Background, warnings)
	}

	body, warnings, _, err = model.(*InteractionsLanguageModel).buildArgs(opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if body.Environment != nil || len(warnings) != 1 || !strings.Contains(warnings[0].Message, "environment is only supported when an agent is set") {
		t.Fatalf("environment = %v, warnings = %v", body.Environment, warnings)
	}
	if s := interactionsEnvironment("env-123"); s != "env-123" {
		t.Fatalf("string environment = %v", s)
	}
}

// TS annotationsToSources de-duplicates by URL or document filename/title and
// skips url/place citations without a url.
func TestInteractionsAnnotationsToSourcesDeduplicates(t *testing.T) {
	out := annotationsToSources([]interactionsAnnotation{
		{Type: "url_citation", URL: "https://a.example", Title: "A"},
		{Type: "url_citation", URL: "https://a.example", Title: "A again"},
		{Type: "url_citation"},
		{Type: "place_citation", Name: "no url"},
		{Type: "place_citation", URL: "https://p.example", Name: "P"},
		{Type: "file_citation", FileName: "doc.pdf"},
		{Type: "file_citation", FileName: "doc.pdf"},
		{Type: "file_citation", URL: "https://f.example/x", FileName: "x"},
	}, streaming.GenerateID)
	if len(out) != 4 {
		t.Fatalf("got %d sources, want 4: %#v", len(out), out)
	}
}

// A schema.OrderedSchema keeps its property order in the interactions
// response_format and tool parameters.
func TestInteractionsOrderedSchemaKeepsOrder(t *testing.T) {
	var raw []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		_, _ = fmt.Fprint(w, `{"id":"v1","status":"completed","model":"gemini-2.5-flash","steps":[{"type":"model_output","content":[{"type":"text","text":"{}"}]}],"usage":{}}`)
	}))
	defer server.Close()
	model, err := New(Config{APIKey: "k", BaseURL: server.URL}).Interactions(ModelGemini25Flash)
	if err != nil {
		t.Fatal(err)
	}
	ordered, err := schema.NewOrderedSchema([]byte(`{"type":"object","properties":{"zeta":{"type":"string"},"alpha":{"type":"string"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "x"},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: ordered},
		Tools:          []types.Tool{{Name: "t", Type: types.ToolTypeFunction, Parameters: ordered}},
	}); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), `"properties":{"zeta":{"type":"string"},"alpha":{"type":"string"}}`); n != 2 {
		t.Fatalf("property order kept %d times, want 2: %s", n, raw)
	}
}

// TS parseProviderOptions: googleInteractionsLanguageModelOptions rejects bad
// shapes with InvalidArgumentError, and getArgs only sends agent_config for
// agents, built from the deep-research fields or {type: "dynamic"}.
func TestInteractionsOptionValidationAndAgentConfigMatchTS(t *testing.T) {
	p := New(Config{APIKey: "k"})
	agent, _ := p.InteractionsAgent("deep-research-pro-preview-12-2025")
	model, _ := p.Interactions(ModelGemini25Flash)
	mk := func(g map[string]interface{}) *provider.GenerateOptions {
		return &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}, ProviderOptions: map[string]interface{}{"google": g}}
	}
	bad := map[string]map[string]interface{}{
		"store type":          {"store": "yes"},
		"environment no type": {"environment": map[string]interface{}{"network": "disabled"}},
		"responseFormat type": {"responseFormat": []interface{}{map[string]interface{}{"type": "bogus"}}},
		"agentConfig no type": {"agentConfig": map[string]interface{}{"visualization": "auto"}},
		"polling":             {"pollingTimeoutMs": 0},
		"inline no target":    {"environment": map[string]interface{}{"type": "remote", "sources": []interface{}{map[string]interface{}{"type": "inline", "content": "c"}}}},
	}
	for name, g := range bad {
		_, _, _, err := model.(*InteractionsLanguageModel).buildArgs(mk(g), false)
		var ia *providererrors.InvalidArgumentError
		if !errors.As(err, &ia) {
			t.Errorf("%s: err = %v, want InvalidArgumentError", name, err)
		}
	}

	cfg := map[string]interface{}{"agentConfig": map[string]interface{}{"type": "dynamic", "extra": 1}}
	body, _, _, err := agent.(*InteractionsLanguageModel).buildArgs(mk(cfg), false)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(body.AgentConfig); string(b) != `{"type":"dynamic"}` {
		t.Errorf("agent_config = %s", b)
	}
	body, _, _, err = model.(*InteractionsLanguageModel).buildArgs(mk(cfg), false)
	if err != nil {
		t.Fatal(err)
	}
	if body.AgentConfig != nil {
		t.Errorf("agent_config on a model call = %v, want omitted", body.AgentConfig)
	}
}

// TS getArgs warning order: penalties, then tool warnings; function tools send
// description ?? '' and provider tool args keep empty strings.
func TestInteractionsWarningOrderAndToolShapeMatchTS(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, _ := p.Interactions(ModelGemini25Flash)
	pen := 0.5
	body, warnings, _, err := model.(*InteractionsLanguageModel).buildArgs(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "x"},
		PresencePenalty: &pen,
		Tools: []types.Tool{
			{Type: types.ToolTypeFunction, Name: "f", Parameters: map[string]interface{}{"type": "object"}},
			{Type: types.ToolTypeProviderDefined, ProviderID: "google.nope"},
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 2 || warnings[0].Feature != "presencePenalty" || !strings.Contains(warnings[1].Feature, "google.nope") {
		t.Fatalf("warnings = %#v", warnings)
	}
	if v, ok := body.Tools[0]["description"]; !ok || v != "" {
		t.Fatalf("function tool = %#v, want description \"\"", body.Tools[0])
	}
}
