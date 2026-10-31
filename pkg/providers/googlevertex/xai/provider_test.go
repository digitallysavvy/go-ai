package xai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestNew_ValidationErrors(t *testing.T) {
	t.Setenv("GOOGLE_VERTEX_PROJECT", "")
	prov, err := New(Config{})
	require.NoError(t, err, "TS loads the project when the first model is created, not up front")
	_, err = prov.LanguageModel("grok-4")
	require.Error(t, err)
	assert.Equal(t, "Google Vertex project setting is missing. Pass it using the 'project' parameter or the GOOGLE_VERTEX_PROJECT environment variable.", err.Error())
}

func TestNew_Name(t *testing.T) {
	prov, err := New(Config{Project: "test-project", AccessToken: "test-token"})
	require.NoError(t, err)
	assert.Equal(t, "googleVertex.xai", prov.Name())
}

// TestNew_BaseURLConstruction mirrors TS "should create a provider with
// correct base URL for global location" / "...for regional location" /
// "...when baseURL is an empty string".
func TestNew_BaseURLConstruction(t *testing.T) {
	tests := []struct {
		name     string
		location string
		baseURL  string
		want     string
	}{
		{
			name:     "global location (explicit)",
			location: "global",
			want:     "https://aiplatform.googleapis.com/v1/projects/test-project/locations/global/endpoints/openapi",
		},
		{
			name:     "location defaults to global when empty",
			location: "",
			want:     "https://aiplatform.googleapis.com/v1/projects/test-project/locations/global/endpoints/openapi",
		},
		{
			name:     "regional location",
			location: "us-central1",
			want:     "https://aiplatform.googleapis.com/v1/projects/test-project/locations/us-central1/endpoints/openapi",
		},
		{
			name:     "empty baseURL override falls back to constructed URL",
			location: "global",
			baseURL:  "",
			want:     "https://aiplatform.googleapis.com/v1/projects/test-project/locations/global/endpoints/openapi",
		},
		{
			name:     "custom baseURL override",
			location: "global",
			baseURL:  "https://custom.example.com",
			want:     "https://custom.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov, err := New(Config{
				Project:     "test-project",
				Location:    tt.location,
				AccessToken: "test-token",
				BaseURL:     tt.baseURL,
			})
			require.NoError(t, err)
			require.NotNil(t, prov)
			cfg, _, err := prov.resolve()
			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.BaseURL)
		})
	}
}

// TestProvider_EmbeddingAndImageModel_NoSuchModelError mirrors TS "should
// throw NoSuchModelError for embedding and image models".
func TestProvider_EmbeddingAndImageModel_NoSuchModelError(t *testing.T) {
	prov, err := New(Config{Project: "test-project", AccessToken: "test-token"})
	require.NoError(t, err)

	_, err = prov.EmbeddingModel("invalid-model-id")
	require.Error(t, err)
	assert.True(t, providererrors.IsNoSuchModelError(err))

	_, err = prov.ImageModel("invalid-model-id")
	require.Error(t, err)
	assert.True(t, providererrors.IsNoSuchModelError(err))
}

func TestProvider_LanguageModel_EmptyModelID(t *testing.T) {
	prov, err := New(Config{Project: "test-project", AccessToken: "test-token"})
	require.NoError(t, err)

	_, err = prov.LanguageModel("")
	assert.Error(t, err)
}

func TestProvider_LanguageModel_And_ChatModel(t *testing.T) {
	prov, err := New(Config{Project: "test-project", AccessToken: "test-token"})
	require.NoError(t, err)

	for _, modelID := range []string{
		ModelGrok420Reasoning,
		ModelGrok420NonReasoning,
		ModelGrok41FastReasoning,
		ModelGrok41FastNonReasoning,
	} {
		lm, err := prov.LanguageModel(modelID)
		require.NoError(t, err)
		assert.Equal(t, modelID, lm.ModelID())
		assert.Equal(t, "googleVertex.xai", lm.Provider())

		cm, err := prov.ChatModel(modelID)
		require.NoError(t, err)
		assert.Equal(t, modelID, cm.ModelID())
		assert.Equal(t, "googleVertex.xai", cm.Provider())
	}
}

func TestProvider_UnsupportedModelTypes(t *testing.T) {
	prov, err := New(Config{Project: "test-project", AccessToken: "test-token"})
	require.NoError(t, err)

	_, err = prov.SpeechModel("m")
	assert.Error(t, err)
	_, err = prov.TranscriptionModel("m")
	assert.Error(t, err)
	_, err = prov.RerankingModel("m")
	assert.Error(t, err)
	_, err = prov.VideoModel("m")
	assert.Error(t, err)
}

func TestNew_EnvAndTrailingSlash(t *testing.T) {
	t.Setenv("GOOGLE_VERTEX_PROJECT", "env-project")
	t.Setenv("GOOGLE_VERTEX_LOCATION", "us-east5")
	t.Setenv("GOOGLE_VERTEX_PROJECT", "")
	t.Setenv("GOOGLE_VERTEX_LOCATION", "")
	prov, err := New(Config{AccessToken: "t"})
	require.NoError(t, err)
	// Environment variables are read when the first model is created.
	t.Setenv("GOOGLE_VERTEX_PROJECT", "env-project")
	t.Setenv("GOOGLE_VERTEX_LOCATION", "us-east5")
	_, err = prov.LanguageModel("grok-4")
	require.NoError(t, err)
	cfg, _, err := prov.resolve()
	require.NoError(t, err)
	assert.Equal(t, "https://aiplatform.googleapis.com/v1/projects/env-project/locations/us-east5/endpoints/openapi", cfg.BaseURL)

	// An explicit baseURL needs neither project nor location (TS loadBaseURL).
	t.Setenv("GOOGLE_VERTEX_PROJECT", "")
	prov, err = New(Config{AccessToken: "t", BaseURL: "https://example.com/v1/"})
	require.NoError(t, err)
	_, err = prov.LanguageModel("grok-4")
	require.NoError(t, err)
	cfg, _, err = prov.resolve()
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/v1", cfg.BaseURL)
}
