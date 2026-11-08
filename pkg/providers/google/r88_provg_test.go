package google

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// r88provgSpeechBody returns the raw generateContent body a Gemini TTS call
// sends and the warnings it returns.
func r88provgSpeechBody(t *testing.T, modelID string, opts *provider.SpeechGenerateOptions) (string, int) {
	t.Helper()
	var mu sync.Mutex
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = string(b)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/L16;rate=24000","data":"AQACAA=="}}]}}]}`)
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).SpeechModel(modelID)
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	return body, len(res.Warnings)
}

// Ports the google-speech-model.test.ts language blocks (TS 3c6fb4322c):
// language goes to speechConfig.languageCode (single and multi speaker), "auto" and unset send none, and the old
// unsupported-language warning is gone.
func TestR88provgGeminiTTSLanguageCode(t *testing.T) {
	body, warnings := r88provgSpeechBody(t, ModelGemini25FlashTTS, &provider.SpeechGenerateOptions{Text: "hello", Language: "en-US"})
	if !strings.Contains(body, `"languageCode":"en-US"`) || !strings.Contains(body, `"voiceName":"`+defaultGeminiTTSVoice+`"`) || warnings != 0 {
		t.Fatalf("single speaker: body %s warnings %d", body, warnings)
	}
	multi := map[string]interface{}{"google": map[string]interface{}{"multiSpeakerVoiceConfig": map[string]interface{}{"speakerVoiceConfigs": []interface{}{
		map[string]interface{}{"speaker": "A", "voiceConfig": map[string]interface{}{"prebuiltVoiceConfig": map[string]interface{}{"voiceName": "Kore"}}},
		map[string]interface{}{"speaker": "B", "voiceConfig": map[string]interface{}{"prebuiltVoiceConfig": map[string]interface{}{"voiceName": "Puck"}}},
	}}}}
	body, warnings = r88provgSpeechBody(t, ModelGemini25FlashTTS, &provider.SpeechGenerateOptions{Text: "A: hi\nB: hey", Language: "de-DE", ProviderOptions: multi})
	var parsed struct {
		GenerationConfig struct {
			SpeechConfig json.RawMessage `json:"speechConfig"`
		} `json:"generationConfig"`
	}
	_ = json.Unmarshal([]byte(body), &parsed)
	sc := string(parsed.GenerationConfig.SpeechConfig)
	if !strings.Contains(sc, `"multiSpeakerVoiceConfig":`) || !strings.Contains(sc, `"languageCode":"de-DE"`) || warnings != 0 {
		t.Fatalf("multi speaker: speechConfig %s warnings %d", sc, warnings)
	}
	for _, lang := range []string{"", "auto"} {
		for _, po := range []map[string]interface{}{nil, multi} {
			body, warnings = r88provgSpeechBody(t, ModelGemini25FlashTTS, &provider.SpeechGenerateOptions{Text: "A: hi\nB: hey", Language: lang, ProviderOptions: po})
			if strings.Contains(body, "languageCode") || warnings != 0 {
				t.Fatalf("language %q: body %s warnings %d", lang, body, warnings)
			}
		}
	}
}
