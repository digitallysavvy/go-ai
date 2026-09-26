package azure

import (
	"context"
	"encoding/json"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestGetAzureOpenAIBaseURLInfo ports the branching covered by TS
// azure-openai-provider.test.ts's various baseURL describe blocks for
// getAzureOpenAIBaseURLInfo (993e900, 2f10cdf, 85db433).
func TestGetAzureOpenAIBaseURLInfo(t *testing.T) {
	tests := []struct {
		name            string
		baseURL         string
		wantAzureOpenAI bool
		wantFoundry     bool
		wantVersioned   bool
	}{
		{
			name:            "no baseURL defaults to Azure",
			baseURL:         "",
			wantAzureOpenAI: true,
		},
		{
			name:            "bare resource host",
			baseURL:         "https://test-resource.openai.azure.com/openai",
			wantAzureOpenAI: true,
		},
		{
			name:            "versioned openai.azure.com host",
			baseURL:         "https://test-resource.openai.azure.com/openai/v1",
			wantAzureOpenAI: true,
			wantVersioned:   true,
		},
		{
			name:            "versioned openai.azure.com host with trailing slash is case-insensitive",
			baseURL:         "https://test-resource.openai.azure.com/OpenAI/V1/",
			wantAzureOpenAI: true,
			wantVersioned:   true,
		},
		{
			name:            "services.ai.azure.com host",
			baseURL:         "https://test-resource.services.ai.azure.com/openai",
			wantAzureOpenAI: true,
		},
		{
			name:            "versioned services.ai.azure.com host",
			baseURL:         "https://test-resource.services.ai.azure.com/openai/v1/",
			wantAzureOpenAI: true,
			wantVersioned:   true,
		},
		{
			name:            "cognitiveservices.azure.com host",
			baseURL:         "https://test-resource.cognitiveservices.azure.com/openai",
			wantAzureOpenAI: true,
		},
		{
			name:            "versioned cognitiveservices.azure.com host",
			baseURL:         "https://test-resource.cognitiveservices.azure.com/openai/v1",
			wantAzureOpenAI: true,
			wantVersioned:   true,
		},
		{
			name:            "Foundry project host",
			baseURL:         "https://test-resource.services.ai.azure.com/api/projects/proj1",
			wantAzureOpenAI: true,
			wantFoundry:     true,
		},
		{
			name:            "non-Foundry-path services.ai.azure.com host is not a Foundry project",
			baseURL:         "https://test-resource.services.ai.azure.com/not-a-project",
			wantAzureOpenAI: true,
			wantFoundry:     false,
		},
		{
			name:    "custom non-Azure gateway",
			baseURL: "https://our-gateway.example.com/azure",
		},
		{
			name:            "mixed-case Azure host is still recognized (WHATWG URL lowercases hostname)",
			baseURL:         "https://Test-Resource.OpenAI.Azure.COM/openai",
			wantAzureOpenAI: true,
		},
		{
			name:            "mixed-case versioned Azure host is still recognized as versioned",
			baseURL:         "https://Test-Resource.OpenAI.Azure.COM/OpenAI/V1",
			wantAzureOpenAI: true,
			wantVersioned:   true,
		},
		{
			name:            "mixed-case Foundry host is still recognized as Foundry",
			baseURL:         "https://Test-Resource.Services.AI.Azure.COM/api/projects/proj1",
			wantAzureOpenAI: true,
			wantFoundry:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := getAzureOpenAIBaseURLInfo(tt.baseURL)
			if info.isAzureOpenAI != tt.wantAzureOpenAI {
				t.Errorf("isAzureOpenAI = %v, want %v", info.isAzureOpenAI, tt.wantAzureOpenAI)
			}
			if info.isFoundryProject != tt.wantFoundry {
				t.Errorf("isFoundryProject = %v, want %v", info.isFoundryProject, tt.wantFoundry)
			}
			if info.isVersioned != tt.wantVersioned {
				t.Errorf("isVersioned = %v, want %v", info.isVersioned, tt.wantVersioned)
			}
		})
	}
}

// dialingClient returns an *http.Client whose Transport ignores the
// requested host and always dials addr. This lets tests use realistic Azure
// hostnames (for getAzureOpenAIBaseURLInfo classification) while the actual
// bytes travel to a local httptest.Server.
func dialingClient(addr string) *stdhttp.Client {
	return &stdhttp.Client{
		Transport: &stdhttp.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		},
	}
}

// TestResponsesModelVersionedAzureURLSkipsV1AndAPIVersion covers 993e900:
// a baseURL that already ends in /openai/v1 must not get a second /v1
// segment, and must not receive an api-version query parameter appended by
// Go (the caller owns versioning already).
func TestResponsesModelVersionedAzureURLSkipsV1AndAPIVersion(t *testing.T) {
	var capturedPath, capturedQuery string
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		capturedPath = r.URL.Path
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","model":"dep","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	p, err := New(Config{
		APIKey:     "k",
		BaseURL:    "http://test-resource.openai.azure.com/openai/v1",
		APIVersion: "2025-01-01",
		HTTPClient: dialingClient(addr),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model, err := p.ResponsesModel("dep")
	if err != nil {
		t.Fatalf("ResponsesModel: %v", err)
	}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if capturedPath != "/openai/v1/responses" {
		t.Fatalf("path = %q, want /openai/v1/responses (no double /v1)", capturedPath)
	}
	if capturedQuery != "" {
		t.Fatalf("query = %q, want empty for an already-versioned Azure URL", capturedQuery)
	}
}

// TestResponsesModelFoundryProjectOmitsAPIVersionAndSetsExplicitMessageType
// covers 85db433 + the Foundry half of 993e900: a Foundry project base URL
// must omit api-version and must set explicit "type":"message" on
// system/user input items.
func TestResponsesModelFoundryProjectOmitsAPIVersionAndSetsExplicitMessageType(t *testing.T) {
	var capturedPath, capturedQuery string
	var capturedBody map[string]interface{}
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		capturedPath = r.URL.Path
		capturedQuery = r.URL.RawQuery
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","model":"dep","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	p, err := New(Config{
		APIKey:     "k",
		BaseURL:    "http://test-resource.services.ai.azure.com/api/projects/proj1",
		APIVersion: "2025-01-01",
		HTTPClient: dialingClient(addr),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model, err := p.ResponsesModel("dep")
	if err != nil {
		t.Fatalf("ResponsesModel: %v", err)
	}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			System: "sys",
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
			},
		},
	}); err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if capturedPath != "/api/projects/proj1/v1/responses" {
		t.Fatalf("path = %q, want /api/projects/proj1/v1/responses (Foundry: not versioned, so /v1 is appended)", capturedPath)
	}
	if capturedQuery != "" {
		t.Fatalf("query = %q, want empty for a Foundry project baseURL", capturedQuery)
	}
	input, ok := capturedBody["input"].([]interface{})
	if !ok || len(input) < 2 {
		t.Fatalf("input = %#v", capturedBody["input"])
	}
	sysItem, ok := input[0].(map[string]interface{})
	if !ok || sysItem["type"] != "message" {
		t.Fatalf("system item = %#v, want explicit type:message", input[0])
	}
	userItem, ok := input[1].(map[string]interface{})
	if !ok || userItem["type"] != "message" {
		t.Fatalf("user item = %#v, want explicit type:message", input[1])
	}
}

// TestResponsesModelDefaultAzureHostAppendsV1AndAPIVersion is a sanity check
// that a bare (non-versioned, non-Foundry) Azure OpenAI host still behaves
// like before: /v1 is appended and the api-version query is sent.
func TestResponsesModelDefaultAzureHostAppendsV1AndAPIVersion(t *testing.T) {
	var capturedPath, capturedQuery string
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		capturedPath = r.URL.Path
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","model":"dep","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	p, err := New(Config{
		APIKey:     "k",
		BaseURL:    "http://test-resource.openai.azure.com/openai",
		APIVersion: "2025-01-01",
		HTTPClient: dialingClient(addr),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model, err := p.ResponsesModel("dep")
	if err != nil {
		t.Fatalf("ResponsesModel: %v", err)
	}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if capturedPath != "/openai/v1/responses" {
		t.Fatalf("path = %q, want /openai/v1/responses", capturedPath)
	}
	if capturedQuery != "api-version=2025-01-01" {
		t.Fatalf("query = %q, want api-version=2025-01-01", capturedQuery)
	}
}
