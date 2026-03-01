package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config with API key",
			config: Config{
				APIKey: "test-api-key",
			},
			wantErr: false,
		},
		{
			name:    "missing API key",
			config:  Config{},
			wantErr: true,
		},
		{
			name: "zero data retention enabled",
			config: Config{
				APIKey:            "test-api-key",
				ZeroDataRetention: true,
			},
			wantErr: false,
		},
		{
			name: "custom base URL",
			config: Config{
				APIKey:  "test-api-key",
				BaseURL: "https://custom.gateway.example.com",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := New(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && provider == nil {
				t.Error("New() returned nil provider")
			}
		})
	}
}

func TestProvider_Name(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	if got := provider.Name(); got != "gateway" {
		t.Errorf("Name() = %v, want %v", got, "gateway")
	}
}

func TestProvider_LanguageModel(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	tests := []struct {
		name    string
		modelID string
		wantErr bool
	}{
		{
			name:    "valid model ID",
			modelID: "openai/gpt-4",
			wantErr: false,
		},
		{
			name:    "empty model ID",
			modelID: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, err := provider.LanguageModel(tt.modelID)
			if (err != nil) != tt.wantErr {
				t.Errorf("LanguageModel() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && model == nil {
				t.Error("LanguageModel() returned nil model")
			}
			if !tt.wantErr && model.ModelID() != tt.modelID {
				t.Errorf("LanguageModel().ModelID() = %v, want %v", model.ModelID(), tt.modelID)
			}
		})
	}
}

func TestProvider_GetAvailableModels(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metadata" {
			t.Errorf("Expected path /metadata, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"providers": [
				{
					"id": "openai",
					"name": "OpenAI",
					"models": [
						{
							"id": "gpt-4",
							"name": "GPT-4",
							"capabilities": ["text", "chat"]
						}
					]
				}
			]
		}`))
	}))
	defer server.Close()

	provider, err := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	metadata, err := provider.GetAvailableModels(context.Background())
	if err != nil {
		t.Fatalf("GetAvailableModels() error = %v", err)
	}

	if len(metadata.Providers) != 1 {
		t.Errorf("Expected 1 provider, got %d", len(metadata.Providers))
	}

	if metadata.Providers[0].Name != "OpenAI" {
		t.Errorf("Expected provider name OpenAI, got %s", metadata.Providers[0].Name)
	}

	if len(metadata.Providers[0].Models) != 1 {
		t.Errorf("Expected 1 model, got %d", len(metadata.Providers[0].Models))
	}
}

func TestProvider_GetCredits(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/credits" {
			t.Errorf("Expected path /credits, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"available": 1000,
			"used": 250
		}`))
	}))
	defer server.Close()

	provider, err := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	credits, err := provider.GetCredits(context.Background())
	if err != nil {
		t.Fatalf("GetCredits() error = %v", err)
	}

	if credits.Available != 1000 {
		t.Errorf("Expected available credits 1000, got %d", credits.Available)
	}

	if credits.Used != 250 {
		t.Errorf("Expected used credits 250, got %d", credits.Used)
	}
}

func TestProvider_MetadataCache(t *testing.T) {
	callCount := 0

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"providers": []}`))
	}))
	defer server.Close()

	provider, err := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	// First call should hit the server
	_, err = provider.GetAvailableModels(context.Background())
	if err != nil {
		t.Fatalf("GetAvailableModels() error = %v", err)
	}

	// Second call should use cache
	_, err = provider.GetAvailableModels(context.Background())
	if err != nil {
		t.Fatalf("GetAvailableModels() error = %v", err)
	}

	// Should only have called the server once (second call used cache)
	if callCount != 1 {
		t.Errorf("Expected 1 server call (cached second call), got %d", callCount)
	}
}

func TestGetO11yHeaders(t *testing.T) {
	// Set environment variables
	t.Setenv("VERCEL_DEPLOYMENT_ID", "test-deployment")
	t.Setenv("VERCEL_ENV", "production")
	t.Setenv("VERCEL_REGION", "us-east-1")
	t.Setenv("VERCEL_PROJECT_ID", "proj-abc123")

	headers := GetO11yHeaders()

	if headers.DeploymentID != "test-deployment" {
		t.Errorf("Expected DeploymentID test-deployment, got %s", headers.DeploymentID)
	}

	if headers.Environment != "production" {
		t.Errorf("Expected Environment production, got %s", headers.Environment)
	}

	if headers.Region != "us-east-1" {
		t.Errorf("Expected Region us-east-1, got %s", headers.Region)
	}

	if headers.ProjectID != "proj-abc123" {
		t.Errorf("Expected ProjectID proj-abc123, got %s", headers.ProjectID)
	}
}

func TestAddO11yHeaders(t *testing.T) {
	headers := make(map[string]string)
	o11y := O11yHeaders{
		DeploymentID: "test-deployment",
		Environment:  "production",
		Region:       "us-east-1",
		RequestID:    "req-123",
		ProjectID:    "proj-xyz",
	}

	AddO11yHeaders(headers, o11y)

	expected := map[string]string{
		"ai-o11y-deployment-id": "test-deployment",
		"ai-o11y-environment":   "production",
		"ai-o11y-region":        "us-east-1",
		"ai-o11y-request-id":    "req-123",
		"ai-o11y-project-id":    "proj-xyz",
	}

	for k, v := range expected {
		if headers[k] != v {
			t.Errorf("Expected header %s = %s, got %s", k, v, headers[k])
		}
	}
}

// TestNew_WithProjectID verifies that WithProjectID option sets the project ID config field.
func TestNew_WithProjectID(t *testing.T) {
	projectID := "my-project-123"
	p, err := New(Config{APIKey: "test-key"}, WithProjectID(projectID))
	if err != nil {
		t.Fatalf("New() with WithProjectID error: %v", err)
	}
	if p == nil {
		t.Fatal("New() returned nil provider")
	}
	if p.config.ProjectID == nil {
		t.Fatal("Expected ProjectID to be set, got nil")
	}
	if *p.config.ProjectID != projectID {
		t.Errorf("Expected ProjectID %q, got %q", projectID, *p.config.ProjectID)
	}
}

// TestProjectIDHeader_PresentWhenSet verifies that the ai-o11y-project-id header is
// injected on all requests when ProjectID is configured. (GW-T15)
func TestProjectIDHeader_PresentWhenSet(t *testing.T) {
	var capturedHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("ai-o11y-project-id")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"providers":[]}`))
	}))
	defer server.Close()

	projectID := "proj-test-456"
	p, err := New(Config{
		APIKey:    "test-key",
		BaseURL:   server.URL,
		ProjectID: &projectID,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	_, _ = p.GetAvailableModels(context.Background())

	if capturedHeader != projectID {
		t.Errorf("Expected ai-o11y-project-id header %q, got %q", projectID, capturedHeader)
	}
}

// TestProjectIDHeader_PresentWhenSetViaOption verifies that WithProjectID() injects the
// header just as setting Config.ProjectID does. (GW-T15 via functional option)
func TestProjectIDHeader_PresentWhenSetViaOption(t *testing.T) {
	var capturedHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("ai-o11y-project-id")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"providers":[]}`))
	}))
	defer server.Close()

	projectID := "proj-option-789"
	p, err := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	}, WithProjectID(projectID))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	_, _ = p.GetAvailableModels(context.Background())

	if capturedHeader != projectID {
		t.Errorf("Expected ai-o11y-project-id header %q, got %q", projectID, capturedHeader)
	}
}

// TestProjectIDHeader_AbsentWhenNotSet verifies that the ai-o11y-project-id header is
// NOT present when ProjectID is not configured. (GW-T16)
func TestProjectIDHeader_AbsentWhenNotSet(t *testing.T) {
	var capturedHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("ai-o11y-project-id")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"providers":[]}`))
	}))
	defer server.Close()

	// Ensure VERCEL_PROJECT_ID is not set in the environment
	t.Setenv("VERCEL_PROJECT_ID", "")

	p, err := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		// ProjectID intentionally not set
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	_, _ = p.GetAvailableModels(context.Background())

	if capturedHeader != "" {
		t.Errorf("Expected ai-o11y-project-id header to be absent, got %q", capturedHeader)
	}
}
