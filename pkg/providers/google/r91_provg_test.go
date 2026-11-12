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

// r91provgClosedURL is a base URL with nothing listening.
func r91provgClosedURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	u := srv.URL
	srv.Close()
	return u
}

func r91provgWantConnectError(t *testing.T, what string, err error) {
	t.Helper()
	var r interface{ IsRetryable() bool }
	if err == nil || !strings.HasPrefix(err.Error(), "Cannot connect to API: ") || !errors.As(err, &r) || !r.IsRetryable() {
		t.Fatalf("%s: error = %T %v, want the retryable Cannot connect to API error", what, err, err)
	}
}

// provg1-02: the realtime client-secret transport error must not carry the
// API key from ?key=.
func TestR91provgRealtimeClientSecretRedactsKey(t *testing.T) {
	p := New(Config{APIKey: "SECRET-KEY-123", BaseURL: r91provgClosedURL(t) + "/v1beta"})
	m := &GoogleRealtimeModel{provider: p, modelID: "gemini-live-2.5-flash"}
	_, err := m.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{})
	if err == nil || strings.Contains(err.Error(), "SECRET-KEY-123") {
		t.Fatalf("error = %v", err)
	}
	r91provgWantConnectError(t, "realtime", err)
}

// provg1-07: raw Do sites return the TS handleFetchError APICallError.
func TestR91provgRawDoTransportErrors(t *testing.T) {
	base := r91provgClosedURL(t)
	p := New(Config{APIKey: "k", BaseURL: base + "/v1beta"})
	b := &Batch{provider: p}
	_, err := b.uploadBatchInputFile(context.Background(), []byte("{}\n"), "d", nil)
	r91provgWantConnectError(t, "batch upload", err)
	// Results download: the operation names a responses file and the
	// download connection is dropped.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/download/") {
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close()
			return
		}
		_, _ = io.WriteString(w, `{"name":"batches/x","done":true,"metadata":{"name":"batches/x","state":"BATCH_STATE_SUCCEEDED","output":{"responsesFile":"files/out"}}}`)
	}))
	defer srv.Close()
	b2 := &Batch{provider: New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"})}
	_, err = b2.DoGetBatchResults(context.Background(), provider.BatchV4OperationOptions{BatchID: "batches/x"})
	r91provgWantConnectError(t, "batch download", err)
	_, err = p.Files().UploadFile(context.Background(), types.UploadFileOptions{Data: types.FileData{Type: types.FileDataTypeData, Data: []byte{1}}, MediaType: "application/pdf"})
	r91provgWantConnectError(t, "files upload", err)
}

// provg1-06: the batch JSONL input file is JSON.stringify'd per line, with no
// HTML escaping.
func TestR91provgBatchJSONLNoHTMLEscape(t *testing.T) {
	var uploaded []byte
	var mu sync.Mutex
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/upload/v1beta/files", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-goog-upload-url", srv.URL+"/upload/v1beta/files/s")
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/upload/v1beta/files/s", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		uploaded = b
		mu.Unlock()
		_, _ = w.Write([]byte(`{"file":{"name":"files/in"}}`))
	})
	mux.HandleFunc("/v1beta/models/gemini-2.5-flash:batchGenerateContent", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(googleBatchOperationJSON))
	})
	b := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"}).ExperimentalBatch()
	prompt := "<b>& " + strings.Repeat("a", 20_000_000)
	if _, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{Requests: []provider.BatchV4Request{googleTextBatchRequest("r1", "gemini-2.5-flash", prompt)}}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(string(uploaded[:200]), "<b>& ") || uploaded[len(uploaded)-1] != '\n' || strings.Count(string(uploaded), "\n") != 1 {
		t.Fatalf("uploaded JSONL head = %q", uploaded[:200])
	}
}

// provg1-05: TS `call.id || generateId()` runs the generator only for a
// missing id (parse-google-interactions-outputs.ts:209).
func TestR91provgInteractionsLazyGenerateID(t *testing.T) {
	n := 0
	gen := func() string { id := "id-" + string(rune('0'+n)); n++; return id }
	m := &InteractionsLanguageModel{cfg: InteractionsConfig{GenerateID: gen}}
	var steps []interactionsContentBlock
	if err := json.Unmarshal([]byte(`[{"type":"processing_call","id":"p1"},{"type":"url_context_result","call_id":"c1","result":[{"url":"https://a.example"}]}]`), &steps); err != nil {
		t.Fatal(err)
	}
	content, _, _ := m.parseOutputs(steps, "i1")
	for _, part := range content {
		if src, ok := part.(types.SourceContent); ok {
			if src.ID != "id-0" {
				t.Fatalf("source id = %q, want id-0", src.ID)
			}
			return
		}
	}
	t.Fatalf("no source in %#v", content)
}

// provg1-10: TS `url ?? document_uri ?? file_name` keeps a present empty
// url, so no source is emitted (extract-google-interactions-sources.ts:69,220).
func TestR91provgInteractionsNullishCitationURL(t *testing.T) {
	var anns []interactionsAnnotation
	_ = json.Unmarshal([]byte(`[{"type":"file_citation","url":"","document_uri":"doc.pdf"},{"type":"file_citation","url":null,"document_uri":"doc.pdf","file_name":""}]`), &anns)
	srcs := annotationsToSources(anns, func() string { return "s" })
	if len(srcs) != 1 {
		t.Fatalf("sources = %#v", srcs)
	}
	// file_name "" is present: filename and title are "" (TS ??).
	if src := srcs[0].(types.SourceContent); src.SourceType != "document" || src.Filename != "" || src.Title != "" {
		t.Fatalf("source = %#v", src)
	}
}

// provc-06 / provg1-13: video response headers join repeated values.
func TestR91provgVideoHeadersJoinValues(t *testing.T) {
	if got := convertGoogleHeaders(http.Header{"X-A": {"1", "2"}}); got["X-A"] != "1, 2" {
		t.Fatalf("headers = %v", got)
	}
}

// core2-12: TS parseToolCallInput sends `args: null` for a JSON null input
// (convert-to-google-messages.test.ts:3007 'retains ... parsed null'); a call
// with no input keeps {}.
func TestR91provgNullToolInputArgs(t *testing.T) {
	for in, want := range map[string]string{`null`: `"args":null`, `"null"`: `"args":null`, ``: `"args":{}`, `{"city":`: `"args":"{\"city\":"`} {
		msgs := []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "call1", ToolName: "weather", Input: in}}},
		}
		out, err := prompt.ConvertToGoogleMessages(msgs, prompt.GoogleMessagesOptions{ProviderOptionsNames: []string{"google"}})
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		b, _ := json.Marshal(out.Contents)
		if !strings.Contains(string(b), want) {
			t.Fatalf("%q: contents = %s, want %s", in, b, want)
		}
	}
}

// core2-13: a reasoning file given as a base64 DataString is sent as is.
func TestR91provgReasoningFileDataString(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.ReasoningFileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AAEC"}}}},
	}
	out, err := prompt.ConvertToGoogleMessages(msgs, prompt.GoogleMessagesOptions{ProviderOptionsNames: []string{"google"}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out.Contents)
	if !strings.Contains(string(b), `"data":"AAEC"`) {
		t.Fatalf("contents = %s", b)
	}
}

// TS c160b74576: gemini-4-carbon model ID.
func TestR91provgGemini4CarbonID(t *testing.T) {
	if ModelGemini4Carbon != "gemini-4-carbon" {
		t.Fatal(ModelGemini4Carbon)
	}
}
