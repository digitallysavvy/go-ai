package google

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

const r91provgGenerateResponse = `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`

func r91provgRecorder(t *testing.T, resp string) (*httptest.Server, *[]byte) {
	t.Helper()
	var got []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(server.Close)
	return server, &got
}

// provg1-01 (typed-option probe): the documented safetySettings example
// (docs/05-providers/04-google.mdx:891) and other Go-typed literals parse the
// way the TS zod schema parses their JSON form.
func TestR91provgLanguageModelTypedOptionLiterals(t *testing.T) {
	cases := map[string]struct {
		google interface{}
		want   []string
	}{
		"[]map[string]string safetySettings": {
			map[string]interface{}{"safetySettings": []map[string]string{{"category": "HARM_CATEGORY_HATE_SPEECH", "threshold": "BLOCK_LOW_AND_ABOVE"}}},
			[]string{`"safetySettings":[{"category":"HARM_CATEGORY_HATE_SPEECH","threshold":"BLOCK_LOW_AND_ABOVE"}]`},
		},
		"[]map[string]interface{} safetySettings": {
			map[string]interface{}{"safetySettings": []map[string]interface{}{{"category": "HARM_CATEGORY_HARASSMENT", "threshold": "BLOCK_NONE"}}},
			[]string{`"safetySettings":[{"category":"HARM_CATEGORY_HARASSMENT","threshold":"BLOCK_NONE"}]`},
		},
		"int64 thinkingBudget and map[string]float64 latLng": {
			map[string]interface{}{
				"thinkingConfig":  map[string]interface{}{"thinkingBudget": int64(1024)},
				"retrievalConfig": map[string]interface{}{"latLng": map[string]float64{"latitude": 1.5, "longitude": 2}},
			},
			[]string{`"thinkingBudget":1024`, `"latLng":{"latitude":1.5,"longitude":2}`},
		},
		"map[string]string namespace": {
			map[string]string{"cachedContent": "cachedContents/c1"},
			[]string{`"cachedContent":"cachedContents/c1"`},
		},
		"[]string responseModalities and map[string]string labels": {
			map[string]interface{}{"responseModalities": []string{"TEXT"}, "labels": map[string]string{"a": "b"}},
			[]string{`"responseModalities":["TEXT"]`, `"labels":{"a":"b"}`},
		},
	}
	for name, tc := range cases {
		server, got := r91provgRecorder(t, r91provgGenerateResponse)
		m, _ := New(Config{APIKey: "k", BaseURL: server.URL}).LanguageModel("gemini-2.5-flash")
		_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
			Prompt:          types.Prompt{Text: "hi"},
			ProviderOptions: map[string]interface{}{"google": tc.google},
		})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, w := range tc.want {
			if !strings.Contains(string(*got), w) {
				t.Errorf("%s: body %s lacks %s", name, *got, w)
			}
		}
	}
}

// provg1-01: part-level Go-typed options (thoughtSignature in a
// map[string]string) are read like their JSON form.
func TestR91provgPromptPartTypedOptions(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "ok", ProviderOptions: map[string]interface{}{"google": map[string]string{"thoughtSignature": "sig-1"}}}}},
	}
	out, err := prompt.ConvertToGoogleMessages(msgs, prompt.GoogleMessagesOptions{ProviderOptionsNames: []string{"google"}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out.Contents)
	if !strings.Contains(string(b), `"thoughtSignature":"sig-1"`) {
		t.Fatalf("contents = %s", b)
	}
}

// provg1-01: interactions responseFormat as []map[string]string.
func TestR91provgInteractionsTypedOptionLiterals(t *testing.T) {
	out, err := parseInteractionsProviderOptions(map[string]interface{}{"google": map[string]interface{}{
		"responseFormat":   []map[string]string{{"type": "text"}},
		"pollingTimeoutMs": int64(5000),
		"labels":           map[string]string{"a": "b"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.ResponseFormat) != 1 || out.ResponseFormat[0]["type"] != "text" {
		t.Fatalf("responseFormat = %#v", out.ResponseFormat)
	}
	if out.Labels["a"] != "b" {
		t.Fatalf("labels = %#v", out.Labels)
	}
	if _, err := parseInteractionsProviderOptions(map[string]interface{}{"google": map[string]string{"previousInteractionId": "p"}}); err != nil {
		t.Fatalf("map[string]string namespace: %v", err)
	}
}

// provg1-01: speech multiSpeakerVoiceConfig with Go-typed speakers, as a map
// and inside the typed options struct.
func TestR91provgSpeechTypedOptionLiterals(t *testing.T) {
	speakers := map[string]interface{}{"speakerVoiceConfigs": []map[string]interface{}{
		{"speaker": "A", "voiceConfig": map[string]interface{}{"prebuiltVoiceConfig": map[string]string{"voiceName": "Kore"}}},
		{"speaker": "B", "voiceConfig": map[string]interface{}{"prebuiltVoiceConfig": map[string]string{"voiceName": "Puck"}}},
	}}
	for name, raw := range map[string]interface{}{
		"map":    map[string]interface{}{"multiSpeakerVoiceConfig": speakers},
		"struct": GoogleSpeechModelOptions{MultiSpeakerVoiceConfig: speakers},
	} {
		opts, err := parseGoogleSpeechOptions(raw)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b, _ := json.Marshal(opts.MultiSpeakerVoiceConfig)
		if !strings.Contains(string(b), `"voiceName":"Puck"`) {
			t.Fatalf("%s: multiSpeakerVoiceConfig = %s", name, b)
		}
	}
}

// provg1-01: embedding, transcription, speech translation, video poll and
// realtime readers accept Go-typed namespaces.
func TestR91provgOtherModelTypedOptionLiterals(t *testing.T) {
	emb, err := parseEmbeddingProviderOptions(&provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{"google": map[string]string{"taskType": "RETRIEVAL_QUERY"}}})
	if err != nil || emb.TaskType != "RETRIEVAL_QUERY" {
		t.Fatalf("embedding: %+v, %v", emb, err)
	}
	tr, err := parseTranscriptionModelOptionsChecked(map[string]interface{}{"google": map[string][]string{"languageCodes": {"en-US"}, "customVocabulary": {"go"}}})
	if err != nil || len(tr.LanguageCodes) != 1 || len(tr.CustomVocabulary) != 1 {
		t.Fatalf("transcription: %+v, %v", tr, err)
	}
	st, err := parseSpeechTranslationModelOptionsChecked(map[string]interface{}{"google": map[string]bool{"echoTargetLanguage": true}})
	if err != nil || st.EchoTargetLanguage == nil || !*st.EchoTargetLanguage {
		t.Fatalf("speech translation: %+v, %v", st, err)
	}
	vm := &VideoModel{}
	poll := vm.getPollOptions(map[string]interface{}{"google": map[string]int64{"pollIntervalMs": 1234}})
	if poll.PollIntervalMs != 1234 {
		t.Fatalf("video poll = %+v", poll)
	}
	// A thinking live model defaults thinkingLevel only when the explicit
	// thinkingConfig sets neither level nor budget.
	rm := &GoogleRealtimeModel{modelID: "gemini-3.1-live-thinking"}
	setup, _ := json.Marshal(rm.BuildSessionConfig(provider.RealtimeSessionConfig{ProviderOptions: map[string]interface{}{
		"google": map[string]interface{}{"thinkingConfig": map[string]int64{"thinkingBudget": 64}},
	}}))
	if !strings.Contains(string(setup), `"thinkingBudget":64`) || strings.Contains(string(setup), `"thinkingLevel"`) {
		t.Fatalf("realtime setup = %s", setup)
	}
}

// provg1-01: Files API displayName as a map[string]string namespace.
func TestR91provgFilesTypedOptionLiterals(t *testing.T) {
	var start string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload/v1beta/files":
			b, _ := io.ReadAll(r.Body)
			start = string(b)
			w.Header().Set("x-goog-upload-url", srv.URL+"/upload-session")
		case "/upload-session":
			_, _ = io.WriteString(w, `{"file":{"name":"files/a","mimeType":"application/pdf","uri":"u","state":"ACTIVE"}}`)
		}
	}))
	defer srv.Close()
	_, err := New(Config{APIKey: "k", BaseURL: srv.URL}).Files().UploadFile(context.Background(), types.UploadFileOptions{
		Data:            types.FileData{Type: types.FileDataTypeData, Data: []byte{1}},
		MediaType:       "application/pdf",
		ProviderOptions: map[string]interface{}{"google": map[string]string{"displayName": "report"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(start, `"display_name":"report"`) {
		t.Fatalf("start body = %s", start)
	}
}

// provg1-03: a background DoStream returns before the GET; a GET that keeps
// failing is a stream error mapped like the model's (TS
// googleFailedResponseHandler), and core makes exactly one POST.
func TestR91provgInteractionsBackgroundStreamLazyGET(t *testing.T) {
	old := interactionsStreamRetryDelay
	interactionsStreamRetryDelay = time.Millisecond
	t.Cleanup(func() { interactionsStreamRetryDelay = old })
	var mu sync.Mutex
	posts, gets := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			posts++
			_, _ = io.WriteString(w, `{"id":"i1","status":"in_progress"}`)
		case http.MethodGet:
			gets++
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"code":500,"message":"backend exploded","status":"INTERNAL"}}`)
		}
	}))
	defer server.Close()
	model, _ := New(Config{APIKey: "k", BaseURL: server.URL}).Interactions(ModelGemini25Flash)
	st, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "x"},
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"background": true}},
	})
	if err != nil {
		t.Fatalf("DoStream failed on the GET: %v", err)
	}
	defer st.Close() //nolint:errcheck
	mu.Lock()
	if gets != 0 {
		t.Fatalf("GET opened before the first Next: %d", gets)
	}
	mu.Unlock()
	if c, err := st.Next(); err != nil || c.Type != provider.ChunkTypeStreamStart {
		t.Fatalf("first chunk = %+v, %v", c, err)
	}
	_, err = st.Next()
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.StatusCode != 500 || !strings.Contains(err.Error(), "backend exploded") {
		t.Fatalf("stream error = %T %v", err, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if posts != 1 || gets != interactionsStreamMaxRetries {
		t.Fatalf("posts=%d gets=%d", posts, gets)
	}
}

// provg1-11: interaction ids use encodeURIComponent (TS
// poll-google-interactions.ts:84, cancel-google-interaction.ts:42,
// stream-google-interactions.ts:96).
func TestR91provgInteractionsIDEncodeURIComponent(t *testing.T) {
	var paths []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.EscapedPath())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"a:b","status":"completed","steps":[]}`)
	}))
	defer server.Close()
	m := &InteractionsLanguageModel{cfg: InteractionsConfig{Client: New(Config{APIKey: "k", BaseURL: server.URL}).client}}
	if _, _, err := m.pollUntilTerminal(context.Background(), "a:b", nil, 0); err != nil {
		t.Fatal(err)
	}
	_ = m.cancelInteraction(context.Background(), "a:b", nil)
	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 2 || paths[0] != "GET /interactions/a%3Ab" || paths[1] != "POST /interactions/a%3Ab/cancel" {
		t.Fatalf("paths = %v", paths)
	}
}

// provg1-09: TS convertGoogleInteractionsUsage reports no per-modality
// input token details.
func TestR91provgInteractionsUsageNoModalityDetails(t *testing.T) {
	five := int64(5)
	u := convertInteractionsUsage(&interactionsUsage{TotalInputTokens: &five, InputTokensByModality: []interactionsModalityTokens{{Modality: "text", Tokens: &five}, {Modality: "image", Tokens: &five}}})
	if u.InputDetails == nil || u.InputDetails.TextTokens != nil || u.InputDetails.ImageTokens != nil {
		t.Fatalf("input details = %+v", u.InputDetails)
	}
	if u.InputDetails.NoCacheTokens == nil || *u.InputDetails.NoCacheTokens != 5 {
		t.Fatalf("noCache = %v", u.InputDetails.NoCacheTokens)
	}
	if u2 := convertInteractionsUsage(&interactionsUsage{InputTokensByModality: []interactionsModalityTokens{{Modality: "text", Tokens: &five}}}); u2.InputDetails != nil {
		t.Fatalf("modality-only input details = %+v", u2.InputDetails)
	}
}
