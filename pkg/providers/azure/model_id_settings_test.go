package azure

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from azure-model-id.test.ts (TS #23463): the settings modelId is the
// reported identity and drives capability detection; requests keep using the
// deployment ID.
func TestSeparateDeploymentAndModelIDs(t *testing.T) {
	const deployment, modelID = "production-chat", "gpt-5"
	p := mustNewProvider(t, Config{APIKey: "k", BaseURL: "https://example.test/openai"})

	factories := map[string]func(string, *LanguageModelSettings) (provider.LanguageModel, error){
		"default":    p.LanguageModelWithSettings,
		"responses":  p.ResponsesModelWithSettings,
		"chat":       p.ChatModelWithSettings,
		"completion": p.CompletionModelWithSettings,
	}
	for name, create := range factories {
		t.Run("identity "+name, func(t *testing.T) {
			for settings, want := range map[*LanguageModelSettings]string{
				{ModelID: modelID}: modelID,
				{}:                 deployment,
				nil:                deployment,
			} {
				m, err := create(deployment, settings)
				if err != nil {
					t.Fatal(err)
				}
				if m.ModelID() != want {
					t.Fatalf("ModelID = %q, want %q", m.ModelID(), want)
				}
			}
		})
	}

	for _, useDeploymentURLs := range []bool{false, true} {
		for _, api := range []string{"responses", "chat", "completion"} {
			for _, op := range []string{"generate", "stream"} {
				name := api + "/" + op
				if useDeploymentURLs {
					name += "/deployment-urls"
				}
				t.Run(name, func(t *testing.T) {
					var mu sync.Mutex
					var body map[string]interface{}
					var path string
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						raw, _ := io.ReadAll(r.Body)
						mu.Lock()
						path = r.URL.Path
						_ = json.Unmarshal(raw, &body)
						mu.Unlock()
						_, _ = w.Write([]byte(azureModelIDResponse(api, op)))
					}))
					defer srv.Close()
					prov := mustNewProvider(t, Config{APIKey: "k", BaseURL: srv.URL + "/openai", UseDeploymentBasedURLs: useDeploymentURLs})
					create := map[string]func(string, *LanguageModelSettings) (provider.LanguageModel, error){
						"responses": prov.ResponsesModelWithSettings, "chat": prov.ChatModelWithSettings, "completion": prov.CompletionModelWithSettings,
					}[api]
					m, err := create(deployment, &LanguageModelSettings{ModelID: modelID})
					if err != nil {
						t.Fatal(err)
					}
					temp := 0.5
					opts := &provider.GenerateOptions{Prompt: types.Prompt{System: "Be helpful.", Text: "Hello"}, Temperature: &temp}
					if op == "generate" {
						_, err = m.DoGenerate(context.Background(), opts)
					} else {
						var s provider.TextStream
						if s, err = m.DoStream(context.Background(), opts); err == nil {
							for {
								if _, serr := s.Next(); serr != nil {
									break
								}
							}
							_ = s.Close()
						}
					}
					if err != nil {
						t.Fatal(err)
					}
					mu.Lock()
					defer mu.Unlock()
					if body["model"] != deployment {
						t.Fatalf("request model = %v, want deployment %q", body["model"], deployment)
					}
					if useDeploymentURLs && path != "/openai/deployments/"+deployment+map[string]string{"responses": "/responses", "chat": "/chat/completions", "completion": "/completions"}[api] {
						t.Fatalf("path = %q", path)
					}
					if api == "completion" {
						if body["temperature"] != 0.5 {
							t.Fatalf("completion temperature = %v", body["temperature"])
						}
						return
					}
					// gpt-5 capabilities came from the model ID, not "production-chat".
					if body["temperature"] != nil {
						t.Fatalf("temperature should be dropped for reasoning model: %v", body["temperature"])
					}
					key := map[string]string{"responses": "input", "chat": "messages"}[api]
					first, _ := body[key].([]interface{})
					if len(first) == 0 || first[0].(map[string]interface{})["role"] != "developer" {
						t.Fatalf("first message = %v, want developer role", body[key])
					}
				})
			}
		}
	}
}

func azureModelIDResponse(api, op string) string {
	const model = "gpt-5-2025-08-07"
	switch {
	case api == "responses" && op == "generate":
		return `{"id":"resp-test","created_at":1741257730,"model":"` + model + `","output":[],"status":"completed","usage":{"input_tokens":4,"output_tokens":0,"total_tokens":4}}`
	case api == "responses":
		r := `{"id":"resp-test","created_at":1741257730,"model":"` + model + `","output":[],"status":"completed","usage":{"input_tokens":4,"output_tokens":0,"total_tokens":4}}`
		return "data: {\"type\":\"response.created\",\"response\":" + r + "}\n\ndata: {\"type\":\"response.completed\",\"response\":" + r + "}\n\ndata: [DONE]\n\n"
	case op == "generate":
		return `{"id":"c","created":1741257730,"model":"` + model + `","choices":[{"index":0,"message":{"role":"assistant","content":"Hello"},"text":"Hello","finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1,"total_tokens":5}}`
	default:
		return `data: {"id":"c","created":1741257730,"model":"` + model + `","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"text":"Hello","finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
	}
}
