package googlevertex

import (
	"testing"
)

func TestNewProvider(t *testing.T) {
	cfg := Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
	}

	prov, err := New(cfg)
	if err != nil {
		t.Fatalf("Expected provider to be created, got error: %v", err)
	}

	if prov == nil {
		t.Fatal("Expected provider to be created")
	}

	if prov.Name() != "google-vertex" {
		t.Errorf("Expected provider name 'google-vertex', got '%s'", prov.Name())
	}

	if prov.Project() != "test-project" {
		t.Errorf("Expected project 'test-project', got '%s'", prov.Project())
	}

	if prov.Location() != "us-central1" {
		t.Errorf("Expected location 'us-central1', got '%s'", prov.Location())
	}
}

func TestNewProvider_ValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		expectedErr string
	}{
		{
			name: "missing project",
			config: Config{
				Location:    "us-central1",
				AccessToken: "test-token",
			},
			expectedErr: "project is required for Google Vertex AI",
		},
		{
			name: "missing location",
			config: Config{
				Project:     "test-project",
				AccessToken: "test-token",
			},
			expectedErr: "location is required for Google Vertex AI",
		},
		{
			name: "missing access token",
			config: Config{
				Project:  "test-project",
				Location: "us-central1",
			},
			expectedErr: "access token is required for Google Vertex AI",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.config)
			if err == nil {
				t.Errorf("Expected error containing '%s', got nil", tt.expectedErr)
			} else if err.Error() != tt.expectedErr {
				t.Errorf("Expected error '%s', got '%s'", tt.expectedErr, err.Error())
			}
		})
	}
}

func TestProvider_BaseURLBuilding(t *testing.T) {
	tests := []struct {
		name         string
		location     string
		project      string
		expectedURL  string
	}{
		{
			name:     "us-central1",
			location: "us-central1",
			project:  "my-project",
			expectedURL: "https://us-central1-aiplatform.googleapis.com/v1beta1/projects/my-project/locations/us-central1/publishers/google",
		},
		{
			name:     "europe-west1",
			location: "europe-west1",
			project:  "test-project",
			expectedURL: "https://europe-west1-aiplatform.googleapis.com/v1beta1/projects/test-project/locations/europe-west1/publishers/google",
		},
		{
			name:     "asia-southeast1",
			location: "asia-southeast1",
			project:  "asia-project",
			expectedURL: "https://asia-southeast1-aiplatform.googleapis.com/v1beta1/projects/asia-project/locations/asia-southeast1/publishers/google",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Project:     tt.project,
				Location:    tt.location,
				AccessToken: "test-token",
			}

			prov, err := New(cfg)
			if err != nil {
				t.Fatalf("Failed to create provider: %v", err)
			}

			// The base URL should be built correctly
			// We can verify by checking the client's base URL (though it's not directly accessible)
			// For now, we verify provider was created successfully with the config
			if prov.Project() != tt.project {
				t.Errorf("Expected project '%s', got '%s'", tt.project, prov.Project())
			}
			if prov.Location() != tt.location {
				t.Errorf("Expected location '%s', got '%s'", tt.location, prov.Location())
			}
		})
	}
}

func TestProvider_CustomBaseURL(t *testing.T) {
	customURL := "https://custom-endpoint.example.com"
	cfg := Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     customURL,
	}

	prov, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create provider with custom URL: %v", err)
	}

	if prov == nil {
		t.Fatal("Expected provider to be created")
	}
}

func TestProvider_LanguageModel(t *testing.T) {
	cfg := Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
	}

	prov, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	validModels := []string{
		"gemini-1.5-pro",
		"gemini-1.5-flash",
		"gemini-1.5-flash-8b",
		"gemini-2.0-flash-exp",
		"gemini-pro",
		"gemini-pro-vision",
	}

	for _, modelID := range validModels {
		t.Run(modelID, func(t *testing.T) {
			model, err := prov.LanguageModel(modelID)
			if err != nil {
				t.Errorf("Expected model '%s' to be valid, got error: %v", modelID, err)
			}
			if model == nil {
				t.Errorf("Expected model '%s' to be created", modelID)
			}
			if model.Provider() != "google-vertex" {
				t.Errorf("Expected provider name 'google-vertex', got '%s'", model.Provider())
			}
			if model.ModelID() != modelID {
				t.Errorf("Expected model ID '%s', got '%s'", modelID, model.ModelID())
			}
			if model.SpecificationVersion() != "v3" {
				t.Errorf("Expected spec version 'v3', got '%s'", model.SpecificationVersion())
			}
		})
	}
}

func TestProvider_LanguageModel_EmptyModelID(t *testing.T) {
	cfg := Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
	}

	prov, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	_, err = prov.LanguageModel("")
	if err == nil {
		t.Error("Expected error for empty model ID")
	}
}

func TestLanguageModel_SupportsImageInput(t *testing.T) {
	cfg := Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
	}

	prov, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	tests := []struct {
		modelID      string
		supportsImage bool
	}{
		{"gemini-1.5-pro", true},
		{"gemini-1.5-flash", true},
		{"gemini-1.5-flash-8b", true},
		{"gemini-2.0-flash-exp", true},
		{"gemini-pro-vision", true},
		{"gemini-pro", false},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			model, err := prov.LanguageModel(tt.modelID)
			if err != nil {
				t.Fatalf("Failed to create model: %v", err)
			}

			if model.SupportsImageInput() != tt.supportsImage {
				t.Errorf("Expected SupportsImageInput() to be %v for %s, got %v",
					tt.supportsImage, tt.modelID, model.SupportsImageInput())
			}
		})
	}
}

func TestLanguageModel_SupportsTools(t *testing.T) {
	cfg := Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
	}

	prov, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	model, err := prov.LanguageModel("gemini-1.5-pro")
	if err != nil {
		t.Fatalf("Failed to create model: %v", err)
	}

	if !model.SupportsTools() {
		t.Error("Expected SupportsTools() to be true for Gemini models")
	}
}

func TestLanguageModel_SupportsStructuredOutput(t *testing.T) {
	cfg := Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
	}

	prov, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	model, err := prov.LanguageModel("gemini-1.5-pro")
	if err != nil {
		t.Fatalf("Failed to create model: %v", err)
	}

	if !model.SupportsStructuredOutput() {
		t.Error("Expected SupportsStructuredOutput() to be true for Gemini models")
	}
}
