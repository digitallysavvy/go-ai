package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// r90carryRecorder records every telemetry event a custom integration
// receives, as JSON at delivery time.
type r90carryRecorder struct {
	telemetry.NoopTelemetryIntegration
	mu       sync.Mutex
	recorded []string
}

func (r *r90carryRecorder) add(v interface{}) {
	r.mu.Lock()
	r.recorded = append(r.recorded, fmt.Sprintf("%+v", v))
	r.mu.Unlock()
}

func (r *r90carryRecorder) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	r.add(e)
	return ctx
}

func (r *r90carryRecorder) OnStepStart(ctx context.Context, e telemetry.TelemetryStepStartEvent) context.Context {
	r.add(e)
	return ctx
}

func (r *r90carryRecorder) OnLanguageModelCallStart(ctx context.Context, e telemetry.LanguageModelCallStartEvent) context.Context {
	r.add(e)
	return ctx
}

func (r *r90carryRecorder) OnStepEnd(_ context.Context, e telemetry.TelemetryStepEndEvent) { r.add(e) }
func (r *r90carryRecorder) OnEnd(_ context.Context, e telemetry.TelemetryFinishEvent)      { r.add(e) }
func (r *r90carryRecorder) OnError(_ context.Context, e telemetry.TelemetryErrorEvent)     { r.add(e) }

func (r *r90carryRecorder) joined() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.recorded, "\n")
}

// TS tracing-channel.test.ts "applies OpenAI redaction through middleware"
// (404ce3b14d / 9f37b45262): the code interpreter domain secret reaches the
// provider and the application result, never a telemetry integration or the
// diagnostic channel.
func TestR90carryOpenAIToolCredentialsRedactedInTelemetry(t *testing.T) {
	const secret = "test-domain-secret"
	for _, api := range []string{"generateText", "streamText"} {
		t.Run(api, func(t *testing.T) {
			var mu sync.Mutex
			var sent []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				mu.Lock()
				sent = append(sent, string(raw))
				mu.Unlock()
				var body map[string]interface{}
				_ = json.Unmarshal(raw, &body)
				response := map[string]interface{}{
					"id": "response-1", "model": "gpt-4o", "created_at": 0, "output": []interface{}{},
					"tools": body["tools"],
					"usage": map[string]interface{}{"input_tokens": 1, "output_tokens": 0, "total_tokens": 1},
				}
				if body["stream"] == true {
					w.Header().Set("Content-Type", "text/event-stream")
					b, _ := json.Marshal(map[string]interface{}{"type": "response.completed", "response": response})
					_, _ = io.WriteString(w, "data: "+string(b)+"\n\n")
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer srv.Close()

			model, err := openai.New(openai.Config{APIKey: "test-key", BaseURL: srv.URL}).ResponsesModel("gpt-4o")
			if err != nil {
				t.Fatal(err)
			}
			domainSecrets := []interface{}{map[string]interface{}{"domain": "example.com", "name": "KEY", "value": secret}}
			python := openaitool.CodeInterpreter(openaitool.CodeInterpreterConfig{Container: map[string]interface{}{
				"networkPolicy": map[string]interface{}{"type": "allowlist", "allowedDomains": []interface{}{"example.com"}, "domainSecrets": domainSecrets},
			}})
			python.Name = "python"

			var diagMu sync.Mutex
			var diag []string
			unsub := telemetry.SubscribeDiagnostic(func(_ context.Context, m telemetry.DiagnosticMessage) error {
				diagMu.Lock()
				diag = append(diag, string(m.Type)+" "+fmt.Sprintf("%+v", m.Event))
				diagMu.Unlock()
				return nil
			})
			defer unsub()
			rec := &r90carryRecorder{}
			enabled := true
			settings := &TelemetrySettings{IsEnabled: &enabled, Integrations: []telemetry.TelemetryIntegration{rec}}

			include := &IncludeOptions{RequestBody: true, ResponseBody: true}
			var step types.StepResult
			if api == "generateText" {
				res, err := GenerateText(context.Background(), GenerateTextOptions{
					Model: model, Prompt: "Hello", Tools: []types.Tool{python}, ExperimentalTelemetry: settings, Include: include,
				})
				if err != nil {
					t.Fatal(err)
				}
				step = res.Steps[len(res.Steps)-1]
			} else {
				res, err := StreamText(context.Background(), StreamTextOptions{
					Model: model, Prompt: "Hello", Tools: []types.Tool{python}, ExperimentalTelemetry: settings, Include: include,
				})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := res.ReadAll(); err != nil {
					t.Fatal(err)
				}
				steps := res.Steps()
				if len(steps) == 0 {
					t.Fatal("no steps")
				}
				step = steps[len(steps)-1]
			}

			if b, _ := json.Marshal(step.Request.Body); !strings.Contains(string(b), secret) {
				t.Errorf("application request body lost the secret: %s", b)
			}
			mu.Lock()
			if len(sent) != 1 || !strings.Contains(sent[0], secret) {
				t.Errorf("provider request did not carry the secret: %v", sent)
			}
			mu.Unlock()
			got := rec.joined()
			if !strings.Contains(got, "[redacted]") || strings.Contains(got, secret) {
				t.Errorf("integration events not redacted:\n%s", got)
			}
			diagMu.Lock()
			all := strings.Join(diag, "\n")
			diagMu.Unlock()
			if !strings.Contains(all, "[redacted]") || strings.Contains(all, secret) {
				t.Errorf("diagnostic events not redacted:\n%s", all)
			}
			if v := domainSecrets[0].(map[string]interface{})["value"]; v != secret {
				t.Errorf("caller's tool args mutated: %v", v)
			}
		})
	}
}

// Object callback events on the diagnostic channel (notify.go) carry the
// request / response bodies; their tool credentials are redacted too.
func TestR90carryObjectDiagnosticBodyRedacted(t *testing.T) {
	const secret = "obj-secret"
	body := map[string]interface{}{"tools": []interface{}{map[string]interface{}{"type": "mcp", "server_label": "s", "authorization": secret}}}
	var mu sync.Mutex
	var diag []string
	unsub := telemetry.SubscribeDiagnostic(func(_ context.Context, m telemetry.DiagnosticMessage) error {
		mu.Lock()
		diag = append(diag, fmt.Sprintf("%+v", m.Event))
		mu.Unlock()
		return nil
	})
	defer unsub()
	ev := ObjectOnStepFinishEvent{}
	ev.Request.Body = body
	ev.Response.Body = body
	var seen ObjectOnStepFinishEvent
	Notify(context.Background(), ev, func(_ context.Context, e ObjectOnStepFinishEvent) { seen = e })
	mu.Lock()
	all := strings.Join(diag, "\n")
	mu.Unlock()
	if !strings.Contains(all, "[redacted]") || strings.Contains(all, secret) {
		t.Errorf("diagnostic object event not redacted: %s", all)
	}
	if b, _ := json.Marshal(seen.Request.Body); !strings.Contains(string(b), secret) {
		t.Errorf("listener lost the real body: %s", b)
	}
	if body["tools"].([]interface{})[0].(map[string]interface{})["authorization"] != secret {
		t.Error("caller body mutated")
	}
}
