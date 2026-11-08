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

// Ports the google-realtime-model.test.ts server connection tests (TS
// 892af4dab7): the unconstrained v1beta Bidi endpoint with the resolved
// x-goog-api-key header, and conversation intent only.
func TestR88provgRealtimeServerConnection(t *testing.T) {
	m := NewRealtimeModel(New(Config{APIKey: "test-key", BaseURL: "https://generativelanguage.googleapis.com/v1beta"}), "gemini-2.0-flash-live-001")
	cfg, err := m.GetServerConnection(context.Background(), provider.RealtimeServerConnectionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.URL != "wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent" {
		t.Fatalf("url = %s", cfg.URL)
	}
	if cfg.Headers["x-goog-api-key"] != "test-key" {
		t.Fatalf("headers = %v", cfg.Headers)
	}
	for k, v := range cfg.Headers {
		if v == "" {
			t.Fatalf("empty header %q sent", k)
		}
	}
	if _, err := m.GetServerConnection(context.Background(), provider.RealtimeServerConnectionOptions{Intent: provider.RealtimeSessionIntentConversation}); err != nil {
		t.Fatal(err)
	}
	const want = "Google realtime does not support the 'transcription' session intent."
	if _, err := m.GetServerConnection(context.Background(), provider.RealtimeServerConnectionOptions{Intent: provider.RealtimeSessionIntentTranscription}); err == nil || err.Error() != want {
		t.Fatalf("err = %v", err)
	}
	if _, err := m.SerializeClientEventForIntent(provider.RealtimeClientEvent{Type: "response-create"}, provider.RealtimeSessionIntentTranscription); err == nil || err.Error() != want {
		t.Fatalf("serialize err = %v", err)
	}
	if _, err := m.BuildSessionConfigForIntent(provider.RealtimeSessionConfig{}, provider.RealtimeSessionIntentTranscription); err == nil || err.Error() != want {
		t.Fatalf("build err = %v", err)
	}
	if _, err := m.BuildSessionConfigForIntent(provider.RealtimeSessionConfig{}, ""); err != nil {
		t.Fatal(err)
	}
}

// TS "isolates usage and turn state between server connections"
// (892af4dab7): each NewServerEventParser has its own mapper, and the
// model-level parser is safe for concurrent use (go test -race).
func TestR88provgRealtimeParserIsolation(t *testing.T) {
	m := NewRealtimeModel(New(Config{APIKey: "k"}), "gemini-3.8-live")
	first, second := m.NewServerEventParser(), m.NewServerEventParser()
	parse := func(p func(json.RawMessage) ([]provider.RealtimeServerEvent, error), raw string) []provider.RealtimeServerEvent {
		evs, err := p(json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		return evs
	}
	firstUsage := parse(first, `{"usageMetadata":{"promptTokensDetails":[{"modality":"TEXT","tokenCount":12}]}}`)
	if len(firstUsage) != 1 || firstUsage[0].Type != "custom" || firstUsage[0].RawType != "usageMetadata" || firstUsage[0].Usage == nil || *firstUsage[0].Usage.InputTextTokens != 12 {
		t.Fatalf("first usage = %+v", firstUsage)
	}
	b, _ := json.Marshal(firstUsage[0].Usage)
	if string(b) != `{"inputTextTokens":12}` {
		t.Fatalf("usage json = %s", b)
	}
	secondDone := parse(second, `{"serverContent":{"turnComplete":true}}`)
	found := false
	for _, ev := range secondDone {
		if ev.Type == "response-done" && ev.ResponseID == "google-resp-0" {
			found = true
		}
		if ev.Usage != nil {
			t.Fatalf("usage leaked into the second connection: %+v", ev)
		}
	}
	if !found {
		t.Fatalf("second done = %+v", secondDone)
	}
	parse(first, `{"serverContent":{"turnComplete":true}}`)
	parse(first, `{"serverContent":{"modelTurn":{"parts":[{"text":"next"}]},"turnComplete":true}}`)
	parse(first, `{"serverContent":{"modelTurn":{"parts":[{"text":"third"}]}}}`)
	secondTurn := parse(second, `{"serverContent":{"modelTurn":{"parts":[{"text":"independent"}]}}}`)
	found = false
	for _, ev := range secondTurn {
		if ev.Type == "text-delta" && ev.ResponseID == "google-resp-1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("second turn = %+v", secondTurn)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_, _ = m.ParseServerEvent(json.RawMessage(`{"serverContent":{"modelTurn":{"parts":[{"text":"x"}]},"turnComplete":true}}`))
				_, _ = m.SerializeClientEvent(provider.RealtimeClientEvent{Type: "response-create"})
			}
		}()
	}
	wg.Wait()
}
