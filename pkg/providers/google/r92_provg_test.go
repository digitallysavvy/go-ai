package google

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS getHeaders/loadApiKey throws LoadAPIKeyError, which postJsonToApi does
// not wrap: the Interactions model returns it as is (not a retryable
// ProviderError).
func TestR92provgInteractionsMissingKeyIsLoadAPIKeyError(t *testing.T) {
	t.Setenv("GOOGLE_GENERATIVE_AI_API_KEY", "")
	sent := 0
	hc := &http.Client{Transport: missingKeyRT(func(r *http.Request) (*http.Response, error) {
		sent++
		return nil, errors.New("stop")
	})}
	p := New(Config{HTTPClient: hc})
	m, err := p.Interactions("gemini-2.5-flash")
	if err != nil {
		t.Fatal(err)
	}
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}
	_, err = m.DoGenerate(t.Context(), opts)
	if _, direct := err.(*providererrors.LoadAPIKeyError); !direct || sent != 0 {
		t.Fatalf("DoGenerate err = %T %v sent = %d", err, err, sent)
	}
	_, err = m.DoStream(t.Context(), opts)
	if _, direct := err.(*providererrors.LoadAPIKeyError); !direct || sent != 0 {
		t.Fatalf("DoStream err = %T %v sent = %d", err, err, sent)
	}
}

// TS extract-google-interactions-sources.ts basename() is undefined for a
// trailing "/": the document title falls back to the uri (file_name ??
// filename ?? uri), and no filename is set.
func TestR92provgInteractionsDocumentSourceTrailingSlashTitle(t *testing.T) {
	gen := func() string { return "id" }
	ann := annotationsToSources([]interactionsAnnotation{{Type: "file_citation", DocumentURI: "gs://bucket/dir/"}}, gen)
	if len(ann) != 1 {
		t.Fatalf("annotation sources = %#v", ann)
	}
	if src := ann[0].(types.SourceContent); src.Title != "gs://bucket/dir/" || src.Filename != "" || src.SourceType != "document" {
		t.Fatalf("annotation source = %#v", src)
	}
	block := interactionsContentBlock{Type: "file_search_result", Result: []interface{}{
		map[string]interface{}{"document_uri": "gs://bucket/dir/"},
		map[string]interface{}{"document_uri": "gs://bucket/dir/", "file_name": ""},
		map[string]interface{}{"document_uri": "gs://bucket/dir/", "title": "T"},
	}}
	res := builtinToolResultSources(block, gen)
	want := []string{"gs://bucket/dir/", "", "T"}
	if len(res) != len(want) {
		t.Fatalf("tool result sources = %#v", res)
	}
	for i, w := range want {
		if src := res[i].(types.SourceContent); src.Title != w || src.Filename != "" {
			t.Fatalf("source %d = %#v, want title %q", i, src, w)
		}
	}
}

// TS googleDecisionResponseSchema parses usage.input_tokens_by_modality as
// z.array(z.object({modality, tokens})): undeclared keys of an entry are
// stripped from providerMetadata.google.usage.
func TestR92provgDecisionUsageModalityEntriesStripped(t *testing.T) {
	body := strings.Replace(r91provgDecisionFixture, `"modality":"text"`, `"modality":"text","extra":1`, 1)
	if body == r91provgDecisionFixture {
		t.Fatal("fixture shape changed")
	}
	m, _, _ := r91provgDecisionSetup(t, body, 200)
	res, err := m.DoDecide(context.Background(), r91provgDecisionOptions())
	if err != nil {
		t.Fatal(err)
	}
	usage, _ := json.Marshal(res.ProviderMetadata["google"].(map[string]interface{})["usage"])
	if want := `{"input_tokens_by_modality":[{"modality":"text","tokens":193}],"total_input_tokens":193,"total_output_tokens":0,"total_tokens":193}`; string(usage) != want {
		t.Fatalf("usage = %s", usage)
	}
}

// TS inlineInputBytes measures JSON.stringify output, which writes U+2028 /
// U+2029 as one 3-byte character (Go's encoder writes a 6-byte escape).
func TestR92provgBatchInlineInputBytesLineSeparator(t *testing.T) {
	if got := googleBatchInlineInputBytes(map[string]interface{}{"a": " <"}); got != len(`{"a":"`+" "+`<"}`) {
		t.Fatalf("bytes = %d", got)
	}
}

// TS google-files.ts copies displayName / sizeBytes as sent (null kept,
// absent left out), the time and hash fields when != null ("" kept), and
// mediaType is file.mimeType ?? options.mediaType.
func TestR92provgFilesMetadataPresence(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload/v1beta/files":
			w.Header().Set("x-goog-upload-url", srv.URL+"/upload-session")
		case "/upload-session":
			_, _ = w.Write([]byte(`{"file":{"name":"files/a","mimeType":"","uri":"u","state":"ACTIVE","displayName":null,"createTime":"","sha256Hash":null}}`))
		}
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	res, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{
		Data: types.FileData{Type: types.FileDataTypeData, Data: []byte{1}}, MediaType: "application/pdf",
	})
	if err != nil {
		t.Fatal(err)
	}
	meta, _ := json.Marshal(res.ProviderMetadata)
	if want := `{"google":{"createTime":"","displayName":null,"mimeType":"","name":"files/a","state":"ACTIVE","uri":"u"}}`; string(meta) != want {
		t.Fatalf("metadata = %s", meta)
	}
	if res.MediaType != "" {
		t.Fatalf("mediaType = %q", res.MediaType)
	}
}

// TS `uploadedFile.file.expirationTime != null`: an empty expirationTime is
// reported, a null one is not.
func TestR92provgBatchInputFileExpiresAtPresence(t *testing.T) {
	for _, tc := range []struct {
		expiration string
		want       interface{}
		present    bool
	}{{`""`, "", true}, {`null`, nil, false}} {
		mux := http.NewServeMux()
		srv := httptest.NewServer(mux)
		mux.HandleFunc("/upload/v1beta/files", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("x-goog-upload-url", srv.URL+"/upload/v1beta/files/session")
		})
		mux.HandleFunc("/upload/v1beta/files/session", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"file":{"name":"files/in","expirationTime":` + tc.expiration + `}}`))
		})
		mux.HandleFunc("/v1beta/models/gemini-2.5-flash:batchGenerateContent", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(googleBatchOperationJSON))
		})
		p := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"})
		res, err := p.ExperimentalBatch().DoStartBatch(t.Context(), provider.BatchV4StartOptions{
			Requests: []provider.BatchV4Request{googleTextBatchRequest("big", "gemini-2.5-flash", strings.Repeat("a", googleBatchInlineCreationMaxBytes+10))},
		})
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		meta := res.ProviderMetadata["google"].(map[string]interface{})
		if v, ok := meta["inputFileExpiresAt"]; ok != tc.present || v != tc.want {
			t.Fatalf("%s: inputFileExpiresAt = %v (%v)", tc.expiration, v, ok)
		}
	}
}

// Typed-option ruling, part level: a Go-typed providerOptions.google literal
// on a prompt part reads like its JSON form (TS parses JSON values): the
// reasoning signature and the video processing config are sent.
func TestR92provgInteractionsPartOptionsTypedLiterals(t *testing.T) {
	m := NewInteractionsLanguageModel(New(Config{APIKey: "k"}), ModelGemini25Flash)
	steps, _, warnings, err := m.convertPrompt(types.Prompt{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.FileContent{
			FileData:        types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/v.mp4"},
			MediaType:       "video/mp4",
			ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"processing": map[string]interface{}{"type": "static", "fps": uint8(2), "startOffset": int16(3)}}},
		}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.ReasoningContent{
			Text:            "thinking",
			ProviderOptions: map[string]interface{}{"google": map[string]string{"signature": "sig-1"}},
		}}},
	}}, GoogleInteractionsProviderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %+v", warnings)
	}
	b, _ := json.Marshal(steps)
	for _, want := range []string{`"processing":{"fps":2,"start_offset":"3s","type":"static"}`, `"signature":"sig-1"`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("steps = %s, missing %s", b, want)
		}
	}
}

// TS google-speech-model.ts: parseSampleRate(mimeType) ?? 24000 keeps a
// parsed rate=0, and `mimeType == null` (not "") decides the PCM fallback
// and providerMetadata.google.mimeType.
func TestR92provgSpeechSampleRateAndMimeTypePresence(t *testing.T) {
	pcm := base64.StdEncoding.EncodeToString([]byte{1, 0, 2, 0})
	for _, tc := range []struct {
		inline   string
		rate     int
		mimeType interface{}
		wav      bool
	}{
		{`{"mimeType":"audio/L16;rate=0","data":"` + pcm + `"}`, 0, "audio/L16;rate=0", true},
		{`{"mimeType":"","data":"` + pcm + `"}`, 24000, "", false},
		{`{"data":"` + pcm + `"}`, 24000, nil, true},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"inlineData":` + tc.inline + `}]}}]}`))
		}))
		m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).SpeechModel(ModelGemini25FlashTTS)
		if err != nil {
			t.Fatal(err)
		}
		res, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello."})
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		meta := res.ProviderMetadata["google"].(map[string]interface{})
		if meta["sampleRate"] != tc.rate || meta["mimeType"] != tc.mimeType {
			t.Fatalf("%s: metadata = %v", tc.inline, meta)
		}
		if isWAV := len(res.Audio) > 4 && string(res.Audio[:4]) == "RIFF"; isWAV != tc.wav {
			t.Fatalf("%s: wav = %v", tc.inline, isWAV)
		}
	}
}
