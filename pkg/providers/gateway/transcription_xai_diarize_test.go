package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from TS #22048: warn when xAI speaker diarization is requested but
// the Gateway response carries no speaker metadata.
func gatewayTranscribeWith(t *testing.T, modelID string, body map[string]interface{}, providerOptions map[string]interface{}) *types.TranscriptionResult {
	t.Helper()
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer closeServer()
	p, err := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewTranscriptionModel(p, modelID).DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte("abc"), MimeType: "audio/wav", ProviderOptions: providerOptions,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestGatewayTranscriptionWarnsWhenXaiDiarizationUnavailable(t *testing.T) {
	for _, ns := range []string{"xai", "spacexai"} {
		t.Run(ns, func(t *testing.T) {
			result := gatewayTranscribeWith(t, "spacexai/grok-stt", map[string]interface{}{"text": "Hello"},
				map[string]interface{}{ns: map[string]interface{}{"diarize": true}})
			want := types.Warning{
				Type:    "unsupported",
				Feature: "providerOptions.xai.diarize",
				Details: "AI Gateway does not currently expose xAI speaker diarization metadata.",
			}
			if len(result.Warnings) != 1 || result.Warnings[0] != want {
				t.Errorf("warnings = %#v", result.Warnings)
			}
		})
	}
}

func TestGatewayTranscriptionNoXaiDiarizationWarningWhenSpeakerPresent(t *testing.T) {
	result := gatewayTranscribeWith(t, "spacexai/grok-stt", map[string]interface{}{
		"text": "Hello world",
		"providerMetadata": map[string]interface{}{"xai": map[string]interface{}{
			"words": []interface{}{map[string]interface{}{"text": "Hello", "start": 0, "end": 0.5, "speaker": 0}},
		}},
	}, map[string]interface{}{"xai": map[string]interface{}{"diarize": true}})
	if len(result.Warnings) != 0 {
		t.Errorf("warnings = %#v, want none", result.Warnings)
	}
	if _, ok := result.ProviderMetadata["xai"]; !ok {
		t.Errorf("providerMetadata = %#v", result.ProviderMetadata)
	}
}

func TestGatewayTranscriptionNoXaiDiarizationWarningForOtherModelsOrOptions(t *testing.T) {
	cases := []struct {
		model string
		opts  map[string]interface{}
	}{
		{"openai/whisper", map[string]interface{}{"xai": map[string]interface{}{"diarize": true}}},
		{"xai/grok-stt", map[string]interface{}{"xai": map[string]interface{}{"diarize": false}}},
		{"xai/grok-stt", nil},
	}
	for _, tc := range cases {
		result := gatewayTranscribeWith(t, tc.model, map[string]interface{}{"text": "Hello"}, tc.opts)
		if !reflect.DeepEqual(result.Warnings, []types.Warning{}) {
			t.Errorf("%s %v: warnings = %#v", tc.model, tc.opts, result.Warnings)
		}
	}
}
