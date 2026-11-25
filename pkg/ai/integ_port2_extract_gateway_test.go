package ai_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
	"github.com/digitallysavvy/go-ai/pkg/registry"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// Cross-area twins added by the port wave 2 integrator: ExperimentalExtract
// (gen) through the gateway extraction model (provg).

const integPort2GatewayChildEnv = "GO_AI_INTEG_PORT2_GATEWAY_CHILD"

// TestIntegPort2ExtractThroughGatewayWithoutDefaultProvider is the twin of
// TS registry/extraction-model.test.ts 201 "extracts through Gateway using a
// model %s when no default provider is configured" (string / alias).
//
// The default gateway (gateway.Gateway()) is a process-wide singleton that
// clones http.DefaultTransport when it is first created, so each case runs
// in a fresh test process where the transport is redirected to a local TLS
// server before anything creates the gateway. No request leaves the machine.
func TestIntegPort2ExtractThroughGatewayWithoutDefaultProvider(t *testing.T) {
	if os.Getenv(integPort2GatewayChildEnv) != "" {
		t.Skip("parent test; running as a child process")
	}
	for _, resolution := range []string{"string", "alias"} {
		resolution := resolution
		t.Run(resolution, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestIntegPort2ExtractThroughGatewayChild$", "-test.v", "-test.count=1")
			cmd.Env = append(os.Environ(), integPort2GatewayChildEnv+"="+resolution)
			cmd.Stdin = nil
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("child process failed: %v\n%s", err, out)
			}
			if !strings.Contains(string(out), "--- PASS: TestIntegPort2ExtractThroughGatewayChild") {
				t.Fatalf("child process did not run the case:\n%s", out)
			}
		})
	}
}

func TestIntegPort2ExtractThroughGatewayChild(t *testing.T) {
	resolution := os.Getenv(integPort2GatewayChildEnv)
	if resolution == "" {
		t.Skip("runs only as a child of TestIntegPort2ExtractThroughGatewayWithoutDefaultProvider")
	}

	type call struct {
		host, path string
		headers    http.Header
		body       []byte
	}
	var mu sync.Mutex
	var calls []call
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, call{host: r.Host, path: r.URL.Path, headers: r.Header.Clone(), body: body})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"answers":{"vendor":{"type":"text","value":"Acme"}},"usage":{"inputTokens":10,"outputTokens":2},"warnings":[]}`))
	}))
	defer srv.Close()

	addr := srv.Listener.Addr().String()
	http.DefaultTransport = &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", addr)
		},
		// The local httptest certificate does not name ai-gateway.vercel.sh.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test-only redirect to a local httptest server
	}
	t.Setenv("AI_GATEWAY_API_KEY", "test-api-key")
	t.Setenv("VERCEL_OIDC_TOKEN", "")
	ai.SetDefaultProvider(nil)

	const modelID = "scaledown/extract-1"
	var model interface{} = modelID
	if resolution == "alias" {
		custom, ok := registry.NewCustomProvider(registry.CustomProviderOptions{
			ExtractionModels: map[string]interface{}{"invoices": modelID},
		}).(provider.ExtractionModelProvider)
		if !ok {
			t.Fatal("custom provider does not implement provider.ExtractionModelProvider")
		}
		m, err := custom.ExtractionModel("invoices")
		if err != nil {
			t.Fatal(err)
		}
		model = m
	}
	fields := map[string]provider.ExtractionField{"vendor": {Type: provider.ExtractionFieldText}}
	result, err := ai.ExperimentalExtract(context.Background(), ai.ExtractOptions{
		Model:  model,
		State:  "Invoice from Acme.",
		Fields: fields,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Answers["vendor"].Value; got != "Acme" {
		t.Fatalf("answers.vendor.value = %v, want Acme", got)
	}
	if result.Usage.TotalTokens == nil || *result.Usage.TotalTokens != 12 {
		t.Fatalf("usage.totalTokens = %v, want 12", result.Usage.TotalTokens)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	c := calls[0]
	if c.host != "ai-gateway.vercel.sh" || c.path != "/v4/ai/extraction-model" {
		t.Fatalf("request to %s%s, want ai-gateway.vercel.sh/v4/ai/extraction-model", c.host, c.path)
	}
	for k, want := range map[string]string{
		"Authorization": "Bearer test-api-key",
		"Ai-Model-Id":   modelID,
		"Ai-Extraction-Model-Specification-Version": "4",
	} {
		if got := c.headers.Get(k); got != want {
			t.Errorf("header %s = %q, want %q", k, got, want)
		}
	}
	var gotBody, wantBody interface{}
	if err := json.Unmarshal(c.body, &gotBody); err != nil {
		t.Fatalf("request body %s: %v", c.body, err)
	}
	_ = json.Unmarshal([]byte(`{"stateParts":[{"type":"text","text":"Invoice from Acme."}],"fields":{"vendor":{"type":"text"}},"providerOptions":{}}`), &wantBody)
	if !reflect.DeepEqual(gotBody, wantBody) {
		t.Fatalf("request body = %s", c.body)
	}
}

type integPort2ErrorIntegration struct {
	telemetry.NoopTelemetryIntegration
	mu     sync.Mutex
	errors []telemetry.TelemetryErrorEvent
}

func (i *integPort2ErrorIntegration) OnError(_ context.Context, e telemetry.TelemetryErrorEvent) {
	i.mu.Lock()
	i.errors = append(i.errors, e)
	i.mu.Unlock()
}

// TestIntegPort2ExtractKeepsGatewayBYOKCredentialsOutOfTelemetryErrors is the
// twin of TS telemetry/gateway-byok-telemetry.test.ts 43 "keeps BYOK
// credentials out of telemetry integration errors on extraction" (SECURITY,
// TS 83026e8e0f).
func TestIntegPort2ExtractKeepsGatewayBYOKCredentialsOutOfTelemetryErrors(t *testing.T) {
	const secret = "sk-byok-telemetry-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer srv.Close()
	gw, err := gateway.New(gateway.Config{APIKey: "test-gateway-key", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, err := gw.Extraction("openai/gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	onError := &integPort2ErrorIntegration{}
	_, err = ai.ExperimentalExtract(context.Background(), ai.ExtractOptions{
		Model:  model,
		State:  "Invoice INV-2041",
		Fields: map[string]provider.ExtractionField{"invoiceNumber": {Type: provider.ExtractionFieldText}},
		ProviderOptions: map[string]interface{}{"gateway": map[string]interface{}{
			"byok": map[string]interface{}{"openai": []interface{}{map[string]interface{}{"apiKey": secret}}},
		}},
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{onError}},
	})
	if err == nil {
		t.Fatal("ExperimentalExtract succeeded, want the gateway 400 error")
	}
	onError.mu.Lock()
	defer onError.mu.Unlock()
	if len(onError.errors) != 1 {
		t.Fatalf("onError calls = %d, want 1", len(onError.errors))
	}
	event := onError.errors[0]
	// Settings holds funcs (TS JSON.stringify drops them); marshal the rest
	// and check the whole event's printed form as well.
	marshalled := event
	marshalled.Settings = nil
	eventJSON, err := json.Marshal(marshalled)
	if err != nil {
		t.Fatal(err)
	}
	if printed := fmt.Sprintf("%+v", event); strings.Contains(printed, secret) {
		t.Fatalf("telemetry error event carries the BYOK secret: %s", printed)
	}
	if strings.Contains(string(eventJSON), secret) {
		t.Fatalf("telemetry error event carries the BYOK secret: %s", eventJSON)
	}
	errorJSON, _ := json.Marshal(event.Error)
	if strings.Contains(string(errorJSON), secret) {
		t.Fatalf("telemetry error carries the BYOK secret: %s", errorJSON)
	}
	if event.Error != nil && strings.Contains(event.Error.Error(), secret) {
		t.Fatalf("telemetry error text carries the BYOK secret: %s", event.Error.Error())
	}
}

// TestIntegPort2GatewayDecideAndExtractSendEmptyProviderOptions: TS decide and
// extract destructure `providerOptions = {}`, and the gateway models send a
// present providerOptions object, so a call without provider options sends
// `"providerOptions":{}` (found by the registry/extraction-model.test.ts 201
// twin above).
func TestIntegPort2GatewayDecideAndExtractSendEmptyProviderOptions(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer srv.Close()
	gw, err := gateway.New(gateway.Config{APIKey: "test-gateway-key", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := gw.DecisionModel("openai/gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	extraction, err := gw.ExtractionModel("openai/gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = ai.ExperimentalDecide(context.Background(), ai.DecideOptions{
		Model: decision, State: "message",
		Questions: map[string]provider.DecisionQuestion{
			"route": {Type: "choice", Instructions: "Team?", Criteria: map[string]interface{}{"billing": nil, "support": nil}},
		},
	})
	_, _ = ai.ExperimentalExtract(context.Background(), ai.ExtractOptions{
		Model: extraction, State: "Invoice INV-2041",
		Fields: map[string]provider.ExtractionField{"invoiceNumber": {Type: provider.ExtractionFieldText}},
	})
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("requests = %d, want 2", len(bodies))
	}
	for i, name := range []string{"decide", "extract"} {
		got, ok := bodies[i]["providerOptions"]
		if !ok || !reflect.DeepEqual(got, map[string]interface{}{}) {
			t.Errorf("%s request providerOptions = %v (present %v), want {}", name, got, ok)
		}
	}
}
