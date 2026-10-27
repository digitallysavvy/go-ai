package google

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Port of TS google-batch.test.ts "uses a resumable file upload when the
// creation body reaches 20 MB": the creation body (wrapper, displayName,
// webhookConfig and both inlined requests) is padded to exactly 20_000_000
// bytes, which goes to a file upload; one byte less stays inline. TS's test
// injects generateId 'test-id'; Go's displayName suffix always has the 16
// characters of the TS default generateId, so the expected body uses a
// 16-character placeholder of the same length.
func TestR48_BatchCreationBodyOf20MBUsesFileUploadLikeTS(t *testing.T) {
	const model = "gemini-2.5-flash"
	const webhook = "https://example.com/google-batch-webhook"
	inlinedRequest := func(id, prompt string) map[string]interface{} {
		return map[string]interface{}{
			"request": map[string]interface{}{
				"generationConfig": map[string]interface{}{},
				"contents":         []interface{}{map[string]interface{}{"role": "user", "parts": []interface{}{map[string]interface{}{"text": prompt}}}},
			},
			"metadata": map[string]interface{}{"key": id},
		}
	}
	inlineBody := func(largePrompt string) map[string]interface{} {
		return map[string]interface{}{"batch": map[string]interface{}{
			"displayName":   "ai-sdk-batch-0123456789abcdef",
			"webhookConfig": map[string]interface{}{"uris": []string{webhook}},
			"inputConfig": map[string]interface{}{"requests": map[string]interface{}{"requests": []interface{}{
				inlinedRequest("small-request", "small"), inlinedRequest("large-request", largePrompt),
			}}},
		}}
	}
	stringifyLen := func(v interface{}) int {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return len(b)
	}

	// The request Go prepares is the one the TS test inlines.
	p := New(Config{APIKey: "k"})
	req := googleTextBatchRequest("small-request", model, "small")
	got, _, err := NewLanguageModel(p, model).PrepareBatchRequestBody(&req.Text.Options)
	if err != nil {
		t.Fatal(err)
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(inlinedRequest("x", "small")["request"])
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("prepared request = %s, want %s", gotJSON, wantJSON)
	}

	emptyBodyBytes := stringifyLen(inlineBody(""))
	for _, tc := range []struct {
		size     int
		wantFile bool
	}{
		{20_000_000, true},
		{20_000_000 - 1, false},
	} {
		prompt := strings.Repeat("a", tc.size-emptyBodyBytes)
		if n := stringifyLen(inlineBody(prompt)); n != tc.size {
			t.Fatalf("padded body = %d bytes, want %d", n, tc.size)
		}
		var mu sync.Mutex
		var paths []string
		var createBody map[string]interface{}
		mux := http.NewServeMux()
		srv := httptest.NewServer(mux)
		record := func(r *http.Request) {
			mu.Lock()
			paths = append(paths, r.URL.Path)
			mu.Unlock()
		}
		mux.HandleFunc("/upload/v1beta/files", func(w http.ResponseWriter, r *http.Request) {
			record(r)
			w.Header().Set("x-goog-upload-url", srv.URL+"/upload/v1beta/files/session-123")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		})
		mux.HandleFunc("/upload/v1beta/files/session-123", func(w http.ResponseWriter, r *http.Request) {
			record(r)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"file":{"name":"files/batch-input","expirationTime":"2026-08-27T12:00:00Z"}}`))
		})
		mux.HandleFunc("/v1beta/models/"+model+":batchGenerateContent", func(w http.ResponseWriter, r *http.Request) {
			record(r)
			mu.Lock()
			_ = json.NewDecoder(r.Body).Decode(&createBody)
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(googleBatchOperationJSON))
		})
		b := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"}).ExperimentalBatch()
		_, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
			Requests: []provider.BatchV4Request{
				googleTextBatchRequest("small-request", model, "small"),
				googleTextBatchRequest("large-request", model, prompt),
			},
			WebhookURL: webhook,
		})
		srv.Close()
		if err != nil {
			t.Fatalf("size %d: %v", tc.size, err)
		}
		mu.Lock()
		create := "/v1beta/models/" + model + ":batchGenerateContent"
		wantPaths := []string{create}
		if tc.wantFile {
			wantPaths = []string{"/upload/v1beta/files", "/upload/v1beta/files/session-123", create}
		}
		if strings.Join(paths, " ") != strings.Join(wantPaths, " ") {
			t.Fatalf("size %d: requests = %v, want %v", tc.size, paths, wantPaths)
		}
		batch, _ := createBody["batch"].(map[string]interface{})
		inputConfig, _ := batch["inputConfig"].(map[string]interface{})
		webhookConfig, _ := batch["webhookConfig"].(map[string]interface{})
		uris, _ := webhookConfig["uris"].([]interface{})
		if len(uris) != 1 || uris[0] != webhook {
			t.Fatalf("size %d: webhookConfig = %v", tc.size, batch["webhookConfig"])
		}
		if tc.wantFile && (inputConfig["fileName"] != "files/batch-input" || len(inputConfig) != 1) {
			t.Fatalf("size %d: inputConfig = %v", tc.size, inputConfig)
		}
		if name, _ := batch["displayName"].(string); len(name) != len("ai-sdk-batch-0123456789abcdef") {
			t.Fatalf("size %d: displayName = %q", tc.size, name)
		}
		mu.Unlock()
	}
}

// TS `metadata.output.responsesFile ?? response.responsesFile`: without a
// metadata output, an empty response.responsesFile is still a value.
func TestR48_BatchEmptyResponseResponsesFileIsAValue(t *testing.T) {
	empty := ""
	op := googleBatchOperationWire{Response: &googleBatchOutputWire{ResponsesFile: &empty}}
	if got, ok := op.responsesFile(); !ok || got != "" {
		t.Fatalf("responsesFile = %q, %v; want empty and present", got, ok)
	}
	if _, ok := (googleBatchOperationWire{Response: &googleBatchOutputWire{}}).responsesFile(); ok {
		t.Fatal("absent responsesFile reported present")
	}
}
