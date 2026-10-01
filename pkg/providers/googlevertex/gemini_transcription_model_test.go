package googlevertex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ported from
// ai/packages/google-vertex/src/gemini-transcription/google-vertex-gemini-transcription-model.test.ts
// `describe('doGenerate', ...)`.

func newVertexTestProvider(t *testing.T, baseURL string) *Provider {
	t.Helper()
	prov, err := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-oauth-token",
		BaseURL:     baseURL,
	})
	require.NoError(t, err)
	return prov
}

func TestProvider_TranscriptionModel_RoutesGeminiToGenerateContent(t *testing.T) {
	prov := newVertexTestProvider(t, "http://example.invalid")

	tests := []struct {
		modelID  string
		wantType string
	}{
		{"chirp_2", "chirp"},
		{"chirp_3", "chirp"},
		{"telephony", "chirp"},
		{"latest_long", "chirp"},
		{ModelGemini35Transcribe, "gemini"},
		{ModelGemini35TranscribeLive, "gemini"},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			model, err := prov.TranscriptionModel(tt.modelID)
			require.NoError(t, err)
			require.NotNil(t, model)

			switch tt.wantType {
			case "gemini":
				_, ok := model.(*GeminiTranscriptionModel)
				assert.True(t, ok, "expected GeminiTranscriptionModel for %s", tt.modelID)
			default:
				_, ok := model.(*TranscriptionModel)
				assert.True(t, ok, "expected TranscriptionModel (Chirp) for %s", tt.modelID)
			}
			assert.Equal(t, tt.modelID, model.ModelID())
		})
	}
}

func TestGeminiTranscriptionModel_Metadata(t *testing.T) {
	model := NewGeminiTranscriptionModel(nil, ModelGemini35Transcribe)
	assert.Equal(t, "v4", model.SpecificationVersion())
	assert.Equal(t, "google.vertex.transcription", model.Provider())
	assert.Equal(t, ModelGemini35Transcribe, model.ModelID())
}

func TestIsLiveGeminiTranscriptionModelID(t *testing.T) {
	assert.False(t, isLiveGeminiTranscriptionModelID(ModelGemini35Transcribe))
	assert.True(t, isLiveGeminiTranscriptionModelID(ModelGemini35TranscribeLive))
}

// TestGeminiTranscriptionModel_RequestShape mirrors TS "transcribes audio
// via Vertex generateContent with audioTranscriptionConfig".
func TestGeminiTranscriptionModel_RequestShape(t *testing.T) {
	var capturedPath string
	var capturedAuth string
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&capturedBody))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"candidates": []interface{}{
				map[string]interface{}{
					"content": map[string]interface{}{
						"parts": []interface{}{
							map[string]interface{}{"text": "Hello "},
							map[string]interface{}{"text": "world."},
						},
					},
				},
			},
			"usageMetadata": map[string]interface{}{
				"promptTokenCount":     10,
				"candidatesTokenCount": 4,
			},
		})
	}))
	defer server.Close()

	prov := newVertexTestProvider(t, server.URL)
	model := NewGeminiTranscriptionModel(prov, ModelGemini35Transcribe)

	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte{1, 2, 3, 4},
		MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{
			"googleVertex": map[string]interface{}{
				"customVocabulary": []string{"Gemini", "Kubernetes"},
				"languageCodes":    []string{"es-ES"},
				"mode":             "SMART",
			},
		},
	})
	require.NoError(t, err)

	assert.Contains(t, capturedPath, "gemini-3.5-transcribe:generateContent")
	assert.Equal(t, "Bearer test-oauth-token", capturedAuth)
	assert.Equal(t, "Hello world.", result.Text)
	assert.Equal(t, []types.Warning{}, result.Warnings)
	assert.Equal(t, map[string]interface{}{
		"google": map[string]interface{}{
			"usageMetadata": map[string]interface{}{
				"promptTokenCount":     float64(10),
				"candidatesTokenCount": float64(4),
			},
		},
	}, result.ProviderMetadata)

	contents := capturedBody["contents"].([]interface{})
	require.Len(t, contents, 1)
	first := contents[0].(map[string]interface{})
	assert.Equal(t, "user", first["role"])
	parts := first["parts"].([]interface{})
	require.Len(t, parts, 1)
	inlineData := parts[0].(map[string]interface{})["inlineData"].(map[string]interface{})
	assert.Equal(t, "audio/wav", inlineData["mimeType"])
	assert.Equal(t, "AQIDBA==", inlineData["data"])

	genConfig := capturedBody["generationConfig"].(map[string]interface{})
	audioCfg := genConfig["audioTranscriptionConfig"].(map[string]interface{})
	assert.Equal(t, []interface{}{"es-ES"}, audioCfg["languageCodes"])
	assert.Equal(t, []interface{}{"Gemini", "Kubernetes"}, audioCfg["customVocabulary"])
	assert.Equal(t, "SMART", audioCfg["mode"])
}

// TestGeminiTranscriptionModel_GoogleNamespaceFallback mirrors TS "accepts
// options under the google namespace as a fallback".
func TestGeminiTranscriptionModel_GoogleNamespaceFallback(t *testing.T) {
	var capturedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&capturedBody))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"candidates": []interface{}{}})
	}))
	defer server.Close()

	prov := newVertexTestProvider(t, server.URL)
	model := NewGeminiTranscriptionModel(prov, ModelGemini35Transcribe)

	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte{1, 2, 3, 4},
		MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{"mode": "SMART"},
		},
	})
	require.NoError(t, err)

	genConfig := capturedBody["generationConfig"].(map[string]interface{})
	audioCfg := genConfig["audioTranscriptionConfig"].(map[string]interface{})
	assert.Equal(t, "SMART", audioCfg["mode"])
	_, hasLanguageCodes := audioCfg["languageCodes"]
	assert.False(t, hasLanguageCodes)
}

// TestGeminiTranscriptionModel_ExtractsWordSegments mirrors TS "extracts
// text, language, and word segments from the audioTranscription part".
func TestGeminiTranscriptionModel_ExtractsWordSegments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"candidates": []interface{}{
				map[string]interface{}{
					"content": map[string]interface{}{
						"parts": []interface{}{
							map[string]interface{}{
								"audioTranscription": map[string]interface{}{
									"text":         "The quick brown fox.",
									"languageCode": "en-US",
									"speakerLabel": "spk:0",
									"words": []interface{}{
										map[string]interface{}{"word": "The", "startOffset": "0.100s", "endOffset": "0.100s"},
										map[string]interface{}{"word": "quick", "startOffset": "0.100s", "endOffset": "0.400s"},
										map[string]interface{}{"word": "brown", "startOffset": "0.400s", "endOffset": "0.700s"},
										map[string]interface{}{"word": "fox.", "startOffset": "0.700s", "endOffset": "1s"},
									},
								},
							},
						},
					},
				},
			},
			"usageMetadata": map[string]interface{}{"promptTokenCount": 64},
		})
	}))
	defer server.Close()

	prov := newVertexTestProvider(t, server.URL)
	model := NewGeminiTranscriptionModel(prov, ModelGemini35Transcribe)

	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte{1, 2, 3, 4},
		MimeType: "audio/wav",
	})
	require.NoError(t, err)

	assert.Equal(t, "The quick brown fox.", result.Text)
	assert.Equal(t, "en-US", result.Language)
	require.Len(t, result.Segments, 4)
	assert.Equal(t, "The", result.Segments[0].Text)
	assert.Equal(t, 0.1, result.Segments[0].Start)
	assert.Equal(t, 0.1, result.Segments[0].End)
	assert.Equal(t, "fox.", result.Segments[3].Text)
	assert.Equal(t, 0.7, result.Segments[3].Start)
	assert.Equal(t, 1.0, result.Segments[3].End)
}

// TestGeminiTranscriptionModel_LiveModelRejected mirrors TS "rejects unary
// transcription on live model ids".
func TestGeminiTranscriptionModel_LiveModelRejected(t *testing.T) {
	prov := newVertexTestProvider(t, "http://example.invalid")
	model := NewGeminiTranscriptionModel(prov, ModelGemini35TranscribeLive)

	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte{1},
		MimeType: "audio/wav",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only supports streaming transcription")
}

func TestGeminiTranscriptionModel_HeadersForwarded(t *testing.T) {
	var capturedHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("Custom-Request-Header")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"candidates": []interface{}{}})
	}))
	defer server.Close()

	prov := newVertexTestProvider(t, server.URL)
	model := NewGeminiTranscriptionModel(prov, ModelGemini35Transcribe)

	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte{1, 2, 3, 4},
		MimeType: "audio/wav",
		Headers:  map[string]string{"Custom-Request-Header": "request-header-value"},
	})
	require.NoError(t, err)
	assert.Equal(t, "request-header-value", capturedHeader)
}
