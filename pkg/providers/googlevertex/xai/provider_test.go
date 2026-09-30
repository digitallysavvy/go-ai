package xai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestNew_ValidationErrors(t *testing.T) {
	_, err := New(Config{})
	require.Error(t, err)
	assert.Equal(t, "project is required for Google Vertex xAI", err.Error())
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
			assert.Equal(t, tt.want, prov.config.BaseURL)
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
