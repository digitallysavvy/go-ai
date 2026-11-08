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
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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

// Ports the Interactions labels tests (TS a210a29f9b,
// google-interactions-language-model.test.ts and the Vertex Interactions
// labels block): labels are sent top-level after input (generate and stream
// share buildArgs), an empty object is still sent, null or absent sends
// none, and a non-string label fails.
func TestR88provgInteractionsLabels(t *testing.T) {
	m, err := New(Config{APIKey: "k"}).Interactions("gemini-2.5-flash")
	if err != nil {
		t.Fatal(err)
	}
	im := m.(*InteractionsLanguageModel)
	body := func(google interface{}) (string, error) {
		req, _, _, err := im.buildArgs(&provider.GenerateOptions{
			Prompt:          types.Prompt{Text: "hi"},
			ProviderOptions: map[string]interface{}{"google": google},
		}, false)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(req)
		return string(b), nil
	}
	got, err := body(map[string]interface{}{"labels": map[string]interface{}{"application": "example", "safety_identifier": "opaque_user_123"}, "store": false})
	if err != nil || !strings.Contains(got, `"type":"user_input"}],"labels":{"application":"example","safety_identifier":"opaque_user_123"},`) {
		t.Fatalf("provided: %s %v", got, err)
	}
	got, _ = body(map[string]interface{}{"labels": map[string]interface{}{}})
	if !strings.Contains(got, `"labels":{}`) {
		t.Fatalf("empty: %s", got)
	}
	for _, opts := range []interface{}{map[string]interface{}{"labels": nil}, map[string]interface{}{}, GoogleInteractionsProviderOptions{}} {
		got, _ = body(opts)
		if strings.Contains(got, "labels") {
			t.Fatalf("absent: %s", got)
		}
	}
	got, _ = body(GoogleInteractionsProviderOptions{Labels: map[string]string{"a": "b"}})
	if !strings.Contains(got, `"labels":{"a":"b"}`) {
		t.Fatalf("struct: %s", got)
	}
	if _, err := body(map[string]interface{}{"labels": map[string]interface{}{"a": 1}}); err == nil {
		t.Fatal("non-string label: want error")
	}
}
