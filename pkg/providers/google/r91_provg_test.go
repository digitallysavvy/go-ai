package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func r91provgStreamChunks(t *testing.T, header http.Header, sse string) []*provider.StreamChunk {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range header {
			w.Header()[k] = v
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse)
	}))
	defer server.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: server.URL}).LanguageModel("gemini-2.5-flash")
	st, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close() //nolint:errcheck
	var out []*provider.StreamChunk
	for {
		c, err := st.Next()
		if err != nil {
			return out
		}
		out = append(out, c)
	}
}

// TS 540509c068: File Search and gs:// RAG chunks stream as document sources,
// each emitted once across chunks.
func TestR91provgStreamDocumentSources(t *testing.T) {
	cand := `{"candidates":[{"content":{"parts":[{"text":"The roof"}],"role":"model"},"index":0,"groundingMetadata":{"groundingChunks":[{"retrievedContext":{"fileSearchStore":"fileSearchStores/roof-reports","title":"roof-report.pdf","text":"x"}},{"retrievedContext":{"uri":"gs://bucket/reports/annual.pdf","title":"Annual report","text":"y"}},{"web":{"uri":"https://a.example","title":"A"}}]}}]}`
	last := `{"candidates":[{"content":{"parts":[{"text":" area"}],"role":"model"},"finishReason":"STOP","index":0,"groundingMetadata":{"groundingChunks":[{"retrievedContext":{"fileSearchStore":"fileSearchStores/roof-reports","title":"roof-report.pdf"}},{"retrievedContext":{"uri":"gs://bucket/reports/annual.pdf","title":"Annual report"}}]}}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`
	chunks := r91provgStreamChunks(t, nil, "data: "+cand+"\n\ndata: "+last+"\n\n")
	var got []string
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeSource {
			got = append(got, c.SourceContent.SourceType+":"+c.SourceContent.Title+":"+c.SourceContent.Filename)
		}
	}
	want := []string{"document:roof-report.pdf:roof-reports", "document:Annual report:annual.pdf", "url:A:"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("sources = %v, want %v", got, want)
	}
}

// TS 45337f0e8d: the x-gemini-service-tier header fills in serviceTier when
// usageMetadata has none; the body field wins.
func TestR91provgServiceTierFromHeader(t *testing.T) {
	tierOf := func(meta map[string]interface{}) string {
		g, _ := meta["google"].(map[string]json.RawMessage)
		return string(g["serviceTier"])
	}
	for _, tc := range []struct{ usage, want string }{
		{`{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}`, `"priority"`},
		{`{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2,"serviceTier":"flex"}`, `"flex"`},
	} {
		body := `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":` + tc.usage + `}`
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("x-gemini-service-tier", "priority")
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, body)
		}))
		m, _ := New(Config{APIKey: "k", BaseURL: server.URL}).LanguageModel("gemini-2.5-flash")
		res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		server.Close()
		if err != nil {
			t.Fatal(err)
		}
		if got := tierOf(res.ProviderMetadata); got != tc.want {
			t.Fatalf("generate serviceTier = %s, want %s", got, tc.want)
		}
		chunks := r91provgStreamChunks(t, http.Header{"X-Gemini-Service-Tier": {"priority"}}, "data: "+body+"\n\n")
		for _, c := range chunks {
			if c.Type == provider.ChunkTypeFinish {
				var fm struct {
					Google struct {
						ServiceTier json.RawMessage `json:"serviceTier"`
					} `json:"google"`
				}
				_ = json.Unmarshal(c.ProviderMetadata, &fm)
				if got := string(fm.Google.ServiceTier); got != tc.want {
					t.Fatalf("stream serviceTier = %s, want %s", got, tc.want)
				}
			}
		}
	}
}

// --- f70336158e: native Gemini Decisions API (google-decision.test.ts) ---

const r91provgDecisionFixture = `{"answers":{"requestsRefund":{"confidence":0.6375181188885488,"noul":0.36248188111145113,"type":"noul"},"department":{"confidence":0.8624268048164867,"choice":"billing","probabilities":{"billing":0.8624268048164867,"technical":0.13757319518351327},"type":"choice"},"severity":{"confidence":0.5849037405969755,"score":0.8093588003103453,"probabilities":{"0":0.3028687295463396,"1":0.5849037405969755,"2":0.11222752985668494},"legend":{"0":"Low","1":"Medium","2":"High"},"type":"score"}},"usage":{"total_tokens":193,"total_input_tokens":193,"input_tokens_by_modality":[{"modality":"text","tokens":193}],"total_output_tokens":0}}`

type r91provgDecisionServer struct {
	mu      sync.Mutex
	calls   int
	path    string
	headers http.Header
	body    []byte
}

func r91provgDecisionSetup(t *testing.T, body string, status int) (provider.DecisionModel, *r91provgDecisionServer, *Provider) {
	t.Helper()
	rec := &r91provgDecisionServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.calls++
		rec.path, rec.headers, rec.body = r.URL.Path, r.Header.Clone(), b
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-request-id", "req-test")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	p := New(Config{APIKey: "test-key", BaseURL: srv.URL + "/v1beta/", Headers: map[string]string{"x-provider": "configured"}})
	m, err := p.DecisionModel(ModelGeminiDecision1)
	if err != nil {
		t.Fatal(err)
	}
	return m, rec, p
}

func r91provgDecisionOptions() provider.DecisionCallOptions {
	return provider.DecisionCallOptions{
		State: []provider.DecisionStatePart{provider.DecisionJSONPart(map[string]interface{}{"message": "A billing issue with a workaround."})},
		Questions: map[string]provider.DecisionQuestion{
			"department":     {Type: "choice", Instructions: "Pick the team.", Criteria: map[string]interface{}{"technical": "Bugs", "billing": "Charges"}},
			"severity":       {Type: "score", Instructions: []interface{}{"Rate severity."}, Criteria: []interface{}{"Low", "Medium", "High"}},
			"requestsRefund": {Type: "boolean", Instructions: "Is a refund requested?"},
		},
		QuestionOrder: []string{"department", "severity", "requestsRefund"},
		CriteriaOrder: map[string][]string{"department": {"technical", "billing"}},
	}
}

func TestR91provgDecisionNativeAllQuestionTypes(t *testing.T) {
	m, rec, _ := r91provgDecisionSetup(t, r91provgDecisionFixture, 200)
	opts := r91provgDecisionOptions()
	opts.Headers = map[string]string{"x-call": "forwarded"}
	res, err := m.DoDecide(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if m.Provider() != "google.decision" || m.ModelID() != "gemini-decision-1" || m.SpecificationVersion() != "v4" || strings.Join(m.SupportedQuestionTypes(), ",") != "choice,score,boolean" {
		t.Fatalf("model metadata %s %s", m.Provider(), m.ModelID())
	}
	d, s, b := res.Answers["department"], res.Answers["severity"], res.Answers["requestsRefund"]
	if d.Type != "choice" || d.Choice != "billing" || d.Probabilities["billing"] != 0.8624268048164867 {
		t.Fatalf("department = %+v", d)
	}
	if s.Type != "score" || *s.Score != 0.8093588003103453 || s.Probabilities["1"] != 0.5849037405969755 {
		t.Fatalf("severity = %+v", s)
	}
	if b.Type != "boolean" || *b.Probability != 0.36248188111145113 {
		t.Fatalf("requestsRefund = %+v", b)
	}
	if res.Usage == nil || *res.Usage.InputTokens != 193 || *res.Usage.OutputTokens != 0 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
	if res.Response.ModelID != "gemini-decision-1" || res.Response.Headers["X-Request-Id"] != "req-test" {
		t.Fatalf("response = %+v", res.Response)
	}
	meta, _ := json.Marshal(res.ProviderMetadata)
	if want := `{"google":{"confidence":{"department":0.8624268048164867,"requestsRefund":0.6375181188885488,"severity":0.5849037405969755},"legend":{"severity":{"0":"Low","1":"Medium","2":"High"}},"usage":{"input_tokens_by_modality":[{"modality":"text","tokens":193}],"total_input_tokens":193,"total_output_tokens":0,"total_tokens":193}}}`; string(meta) != want {
		t.Fatalf("providerMetadata = %s", meta)
	}
	if rec.calls != 1 || rec.path != "/v1beta/decisions" {
		t.Fatalf("calls=%d path=%s", rec.calls, rec.path)
	}
	if rec.headers.Get("x-goog-api-key") != "test-key" || rec.headers.Get("x-provider") != "configured" || rec.headers.Get("x-call") != "forwarded" || !strings.Contains(rec.headers.Get("User-Agent"), "ai-sdk-google/") {
		t.Fatalf("headers = %v", rec.headers)
	}
	if want := `{"model":"gemini-decision-1","input":[{"type":"text","text":"{\"message\":\"A billing issue with a workaround.\"}"}],"questions":{"department":{"type":"choice","instructions":"Pick the team.","criteria":{"technical":"Bugs","billing":"Charges"}},"severity":{"type":"score","instructions":"[\"Rate severity.\"]","criteria":["Low","Medium","High"]},"requestsRefund":{"type":"noul","instructions":"Is a refund requested?"}}}`; string(rec.body) != want {
		t.Fatalf("body = %s\nwant %s", rec.body, want)
	}
}

func TestR91provgDecisionStructuredInstructionsAndCriteria(t *testing.T) {
	m, rec, _ := r91provgDecisionSetup(t, r91provgDecisionFixture, 200)
	opts := r91provgDecisionOptions()
	opts.Questions = map[string]provider.DecisionQuestion{
		"department":     {Type: "choice", Instructions: map[string]interface{}{"task": "Pick a team"}, Criteria: map[string]interface{}{"billing": map[string]interface{}{"handles": []interface{}{"payments"}}, "technical": nil}},
		"severity":       {Type: "score", Instructions: "Rate severity", Criteria: []interface{}{map[string]interface{}{"level": "low"}, []interface{}{"medium"}, "high"}},
		"requestsRefund": {Type: "boolean", Instructions: "Refund?", Criteria: map[string]interface{}{"true": map[string]interface{}{"requests": "money back"}, "false": []interface{}{"No refund needed"}}},
	}
	opts.CriteriaOrder = map[string][]string{"department": {"billing", "technical"}}
	if _, err := m.DoDecide(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	var body struct {
		Questions json.RawMessage `json:"questions"`
	}
	_ = json.Unmarshal(rec.body, &body)
	if want := `{"department":{"type":"choice","instructions":"{\"task\":\"Pick a team\"}","criteria":{"billing":"{\"handles\":[\"payments\"]}","technical":null}},"severity":{"type":"score","instructions":"Rate severity","criteria":["{\"level\":\"low\"}","[\"medium\"]","high"]},"requestsRefund":{"type":"noul","instructions":"Refund?","criteria":{"true_description":"{\"requests\":\"money back\"}","false_description":"[\"No refund needed\"]"}}}`; string(body.Questions) != want {
		t.Fatalf("questions = %s", body.Questions)
	}
	// 'omits null boolean descriptions'
	opts.Questions["requestsRefund"] = provider.DecisionQuestion{Type: "boolean", Instructions: "Refund?", Criteria: map[string]interface{}{"true": "Money back", "false": nil}}
	if _, err := m.DoDecide(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rec.body), `"requestsRefund":{"type":"noul","instructions":"Refund?","criteria":{"true_description":"Money back"}}`) {
		t.Fatalf("body = %s", rec.body)
	}
}

func TestR91provgDecisionWarnsAboutGenerationSettings(t *testing.T) {
	m, rec, _ := r91provgDecisionSetup(t, r91provgDecisionFixture, 200)
	opts := r91provgDecisionOptions()
	maxTokens := 100
	level := types.ReasoningHigh
	opts.MaxOutputTokens, opts.Reasoning = &maxTokens, &level
	opts.ProviderOptions = map[string]interface{}{"google": map[string]interface{}{"thinkingConfig": map[string]interface{}{"thinkingLevel": "high"}, "cachedContent": "cache-id"}}
	res, err := m.DoDecide(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, w := range res.Warnings {
		got = append(got, w.Type+":"+w.Feature)
	}
	// TS lists provider option keys in insertion order; a Go map has none, so
	// they are sorted.
	if want := "unsupported:maxOutputTokens,unsupported:reasoning,unsupported:providerOptions.google.cachedContent,unsupported:providerOptions.google.thinkingConfig"; strings.Join(got, ",") != want {
		t.Fatalf("warnings = %v", got)
	}
	if !strings.HasPrefix(string(rec.body), `{"model":"gemini-decision-1","input":[`) || strings.Contains(string(rec.body), "thinking") {
		t.Fatalf("body = %s", rec.body)
	}
}

func TestR91provgDecisionStateParts(t *testing.T) {
	m, rec, _ := r91provgDecisionSetup(t, r91provgDecisionFixture, 200)
	opts := r91provgDecisionOptions()
	opts.State = []provider.DecisionStatePart{
		provider.DecisionTextPart("Check these files."),
		provider.DecisionJSONPart(map[string]interface{}{"type": "image", "user": "data"}),
		provider.DecisionFilePart(types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte{1, 2, 3}}}),
		provider.DecisionFilePart(types.FileContent{MediaType: "audio/mp3", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/clip.mp3"}}),
		provider.DecisionFilePart(types.FileContent{MediaType: "video/mp4", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}}),
		provider.DecisionFilePart(types.FileContent{MediaType: "application/pdf", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "BAUG"}}),
	}
	if _, err := m.DoDecide(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	var body struct {
		Input json.RawMessage `json:"input"`
	}
	_ = json.Unmarshal(rec.body, &body)
	if want := `[{"type":"text","text":"Check these files."},{"type":"text","text":"{\"type\":\"image\",\"user\":\"data\"}"},{"type":"image","mime_type":"image/png","data":"AQID"},{"type":"audio","mime_type":"audio/mp3","uri":"https://example.com/clip.mp3"},{"type":"video","mime_type":"video/mp4","data":"AQID"},{"type":"document","mime_type":"application/pdf","data":"BAUG"}]`; string(body.Input) != want {
		t.Fatalf("input = %s", body.Input)
	}
}

func TestR91provgDecisionSafetyBlockedRefusals(t *testing.T) {
	m, _, _ := r91provgDecisionSetup(t, `{"prompt_feedback":{"block_reason":"SAFETY","block_reason_message":"Blocked input."},"usage":{"total_tokens":193,"total_input_tokens":193,"total_output_tokens":0}}`, 200)
	res, err := m.DoDecide(context.Background(), r91provgDecisionOptions())
	if err != nil {
		t.Fatal(err)
	}
	for id, a := range res.Answers {
		if a.Type != "refusal" {
			t.Fatalf("%s = %+v", id, a)
		}
	}
	meta, _ := json.Marshal(res.ProviderMetadata["google"].(map[string]interface{})["promptFeedback"])
	if string(meta) != `{"block_reason":"SAFETY","block_reason_message":"Blocked input."}` || *res.Usage.InputTokens != 193 {
		t.Fatalf("promptFeedback = %s usage = %+v", meta, res.Usage)
	}
}

func TestR91provgDecisionRejectsInvalidAnswers(t *testing.T) {
	var fx map[string]map[string]json.RawMessage
	_ = json.Unmarshal([]byte(r91provgDecisionFixture), &fx)
	with := func(edit func(a map[string]json.RawMessage)) string {
		a := map[string]json.RawMessage{}
		for k, v := range fx["answers"] {
			a[k] = v
		}
		edit(a)
		b, _ := json.Marshal(map[string]interface{}{"answers": a})
		return string(b)
	}
	for label, body := range map[string]string{
		"missing answers":  `{}`,
		"empty answers":    `{"answers":{}}`,
		"missing question": `{"answers":{"department":` + string(fx["answers"]["department"]) + `}}`,
		"extra question":   with(func(a map[string]json.RawMessage) { a["extra"] = json.RawMessage(`{"type":"noul","noul":0.5}`) }),
		"wrong question ID": with(func(a map[string]json.RawMessage) {
			a["wrong"] = a["requestsRefund"]
			delete(a, "requestsRefund")
		}),
		"mismatched type": with(func(a map[string]json.RawMessage) { a["requestsRefund"] = a["department"] }),
		"unknown choice": with(func(a map[string]json.RawMessage) {
			a["department"] = json.RawMessage(strings.Replace(string(a["department"]), `"choice":"billing"`, `"choice":"unknown"`, 1))
		}),
		"out-of-bounds score": with(func(a map[string]json.RawMessage) {
			a["severity"] = json.RawMessage(strings.Replace(string(a["severity"]), `"score":0.8093588003103453`, `"score":3`, 1))
		}),
	} {
		m, _, _ := r91provgDecisionSetup(t, body, 200)
		_, err := m.DoDecide(context.Background(), r91provgDecisionOptions())
		var ire *providererrors.InvalidResponseDataError
		if !errors.As(err, &ire) {
			t.Fatalf("%s: error = %T %v", label, err, err)
		}
	}
	for _, noul := range []string{"-0.1", "1.1"} {
		m, _, _ := r91provgDecisionSetup(t, with(func(a map[string]json.RawMessage) { a["requestsRefund"] = json.RawMessage(`{"type":"noul","noul":` + noul + `}`) }), 200)
		_, err := m.DoDecide(context.Background(), r91provgDecisionOptions())
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != "Invalid JSON response" {
			t.Fatalf("noul %s: error = %T %v", noul, err, err)
		}
	}
}

func TestR91provgDecisionAbsentOptionalMetadata(t *testing.T) {
	m, _, _ := r91provgDecisionSetup(t, `{"answers":{"department":{"type":"choice","choice":"billing","probabilities":{"billing":0.9,"technical":0.1},"confidence":null},"severity":{"type":"score","score":1,"probabilities":{"1":1},"confidence":null,"legend":null},"requestsRefund":{"type":"noul","noul":0.02,"confidence":null}},"usage":null,"prompt_feedback":null}`, 200)
	res, err := m.DoDecide(context.Background(), r91provgDecisionOptions())
	if err != nil {
		t.Fatal(err)
	}
	meta, _ := json.Marshal(res.ProviderMetadata)
	if res.Usage != nil || string(meta) != `{"google":{"confidence":{},"legend":{}}}` {
		t.Fatalf("usage = %+v meta = %s", res.Usage, meta)
	}
}

func TestR91provgDecisionRejectsBeforeRequest(t *testing.T) {
	levels := func(n int, v interface{}) []interface{} {
		out := make([]interface{}, n)
		for i := range out {
			out[i] = v
		}
		return out
	}
	many := map[string]interface{}{}
	for i := 0; i < 256; i++ {
		many[fmt.Sprintf("c%d", i)] = nil
	}
	cases := []provider.DecisionQuestion{
		{Type: "choice", Instructions: "Choose", Criteria: map[string]interface{}{"only": nil}},
		{Type: "choice", Instructions: "Choose", Criteria: many},
		{Type: "score", Instructions: "Rate", Criteria: []interface{}{"One"}},
		{Type: "score", Instructions: "Rate", Criteria: levels(256, "Level")},
		{Type: "score", Instructions: "Rate", Criteria: []interface{}{"Low", nil}},
		{Type: "score", Instructions: "Rate", Criteria: []interface{}{"Low", ""}},
		{Type: "boolean", Criteria: map[string]interface{}{"true": "Yes"}},
	}
	m, rec, _ := r91provgDecisionSetup(t, r91provgDecisionFixture, 200)
	state := r91provgDecisionOptions().State
	for i, q := range cases {
		_, err := m.DoDecide(context.Background(), provider.DecisionCallOptions{State: state, Questions: map[string]provider.DecisionQuestion{"question": q}})
		var ae *providererrors.InvalidArgumentError
		if !errors.As(err, &ae) {
			t.Fatalf("case %d: error = %T %v", i, err, err)
		}
	}
	if _, err := m.DoDecide(context.Background(), provider.DecisionCallOptions{State: state, Questions: map[string]provider.DecisionQuestion{}}); err == nil {
		t.Fatal("empty questions accepted")
	}
	_, err := m.DoDecide(context.Background(), provider.DecisionCallOptions{State: state, Questions: map[string]provider.DecisionQuestion{"box": {Type: "box", Instructions: "Find a box"}}})
	var ue *providererrors.DecisionUnsupportedQuestionTypeError
	if !errors.As(err, &ue) {
		t.Fatalf("box: %T %v", err, err)
	}
	for _, mt := range []string{"text/plain", "application/octet-stream"} {
		opts := r91provgDecisionOptions()
		opts.State = []provider.DecisionStatePart{provider.DecisionFilePart(types.FileContent{MediaType: mt, FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}})}
		_, err := m.DoDecide(context.Background(), opts)
		var uf *providererrors.UnsupportedFunctionalityError
		if !errors.As(err, &uf) {
			t.Fatalf("%s: %T %v", mt, err, err)
		}
	}
	opts := r91provgDecisionOptions()
	opts.State = []provider.DecisionStatePart{provider.DecisionFilePart(types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"google": "files/image"}}})}
	_, err = m.DoDecide(context.Background(), opts)
	var uf *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &uf) {
		t.Fatalf("reference: %T %v", err, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.DoDecide(ctx, r91provgDecisionOptions()); !errors.Is(err, context.Canceled) {
		t.Fatalf("aborted: %v", err)
	}
	if rec.calls != 0 {
		t.Fatalf("requests sent: %d", rec.calls)
	}
}

func TestR91provgDecisionGoogleAPIError(t *testing.T) {
	m, rec, _ := r91provgDecisionSetup(t, `{"error":{"code":403,"message":"EAP access required","status":"PERMISSION_DENIED"}}`, 403)
	_, err := m.DoDecide(context.Background(), r91provgDecisionOptions())
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "EAP access required" || pe.StatusCode != 403 || rec.calls != 1 {
		t.Fatalf("error = %T %v calls=%d", err, err, rec.calls)
	}
}

func TestR91provgDecisionDeprecatedFactoryAndNames(t *testing.T) {
	_, rec, p := r91provgDecisionSetup(t, r91provgDecisionFixture, 200)
	em, err := p.EvaluationModel(ModelGeminiDecision1)
	if err != nil {
		t.Fatal(err)
	}
	res, err := em.DoEvaluate(context.Background(), r91provgDecisionOptions())
	if err != nil || res.Answers["department"].Choice != "billing" || rec.path != "/v1beta/decisions" {
		t.Fatalf("evaluate = %+v, %v", res, err)
	}
	custom := New(Config{APIKey: "k", Name: "custom.generative-ai"})
	cm, _ := custom.DecisionModel("custom-model")
	if cm.Provider() != "custom.decision" || cm.ModelID() != "custom-model" {
		t.Fatalf("custom = %s %s", cm.Provider(), cm.ModelID())
	}
}
