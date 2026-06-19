package googlevertex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestVertexSpeechModelUsesVertexAuthAndOptions(t *testing.T) {
	var capturedAuth string
	var capturedUserAgent string
	var capturedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedUserAgent = r.Header.Get("User-Agent")
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/L16;rate=24000","data":"` + base64.StdEncoding.EncodeToString([]byte{1, 0}) + `"}}]}}]}`))
	}))
	defer server.Close()

	p, err := New(Config{
		Project:     "p",
		Location:    "us-central1",
		AccessToken: "vertex-token",
		BaseURL:     server.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model, err := p.SpeechModel("gemini-2.5-flash-tts")
	if err != nil {
		t.Fatalf("SpeechModel: %v", err)
	}
	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Joe: Hi",
		ProviderOptions: map[string]interface{}{
			"googleVertex": map[string]interface{}{
				"multiSpeakerVoiceConfig": map[string]interface{}{
					"speakerVoiceConfigs": []interface{}{},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if capturedAuth != "Bearer vertex-token" {
		t.Fatalf("Authorization = %q", capturedAuth)
	}
	if capturedUserAgent != "go-ai/google-vertex/0.5.0" {
		t.Fatalf("User-Agent = %q", capturedUserAgent)
	}
	gen := capturedBody["generationConfig"].(map[string]interface{})
	if gen["speechConfig"].(map[string]interface{})["multiSpeakerVoiceConfig"] == nil {
		t.Fatalf("expected googleVertex provider options in speechConfig: %#v", gen["speechConfig"])
	}
	if model.Provider() != "google.vertex.speech" {
		t.Fatalf("Provider = %q", model.Provider())
	}
	meta := result.ProviderMetadata["google"].(map[string]interface{})
	if meta["sampleRate"] != 24000 {
		t.Fatalf("metadata = %#v", meta)
	}
}

func TestVertexSpeechModelPrefersGoogleVertexOptionsOverVertex(t *testing.T) {
	var capturedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/L16;rate=24000","data":"` + base64.StdEncoding.EncodeToString([]byte{1, 0}) + `"}}]}}]}`))
	}))
	defer server.Close()

	p, err := New(Config{
		Project:     "p",
		Location:    "us-central1",
		AccessToken: "vertex-token",
		BaseURL:     server.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model, err := p.SpeechModel("gemini-2.5-flash-preview-tts")
	if err != nil {
		t.Fatalf("SpeechModel: %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Joe: Hi",
		ProviderOptions: map[string]interface{}{
			"googleVertex": map[string]interface{}{
				"multiSpeakerVoiceConfig": map[string]interface{}{
					"speakerVoiceConfigs": []interface{}{
						map[string]interface{}{
							"speaker": "Joe",
							"voiceConfig": map[string]interface{}{
								"prebuiltVoiceConfig": map[string]interface{}{"voiceName": "Kore"},
							},
						},
					},
				},
			},
			"vertex": map[string]interface{}{
				"multiSpeakerVoiceConfig": map[string]interface{}{
					"speakerVoiceConfigs": []interface{}{
						map[string]interface{}{
							"speaker": "Jane",
							"voiceConfig": map[string]interface{}{
								"prebuiltVoiceConfig": map[string]interface{}{"voiceName": "Kore"},
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	config := capturedBody["generationConfig"].(map[string]interface{})["speechConfig"].(map[string]interface{})["multiSpeakerVoiceConfig"].(map[string]interface{})
	speakers := config["speakerVoiceConfigs"].([]interface{})
	if got := speakers[0].(map[string]interface{})["speaker"]; got != "Joe" {
		t.Fatalf("preferred provider options speaker = %#v", got)
	}
}
