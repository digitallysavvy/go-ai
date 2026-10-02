package topaz

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

const processID = "proc-123"

var outputBytes = []byte{0x89, 0x50, 0x4e, 0x47, 0x01, 0x02}

func inputImageFile() provider.ImageFile {
	return provider.ImageFile{Type: "file", MediaType: "image/png", Data: []byte{1, 2, 3, 4}}
}

// imageTestServer is a configurable httptest server backing the Topaz
// image enhance/status/download/cancel endpoints. statusFn, when set, is
// called on every status poll instead of the fixed statusBody; it is
// guarded by mu because the HTTP server invokes it from its own goroutine
// while the test's main goroutine may also read/write the surrounding
// captured fields.
type imageTestServer struct {
	mu            sync.Mutex
	statusFn      func(count int) (int, string)
	statusBody    string
	statusCode    int
	downloadBody  string
	downloadCode  int
	submitCode    int
	submitBody    string
	cancelCalled  bool
	statusCalls   int
	multipartForm map[string][]string
	requestHeader http.Header
	dlHeader      http.Header

	server *httptest.Server
}

func newImageTestServer(t *testing.T) *imageTestServer {
	t.Helper()
	ts := &imageTestServer{
		statusBody:   `{"status":"Completed","credits":2,"output_width":4000,"output_height":3000,"output_format":"png"}`,
		statusCode:   http.StatusOK,
		downloadBody: "",
		downloadCode: http.StatusOK,
		submitCode:   http.StatusOK,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/image/v1/enhance-gen/async", func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		ts.requestHeader = r.Header.Clone()
		code := ts.submitCode
		body := ts.submitBody
		ts.mu.Unlock()

		if err := r.ParseMultipartForm(10 << 20); err == nil {
			form := map[string][]string{}
			for k, v := range r.MultipartForm.Value {
				form[k] = v
			}
			if _, hasFile := r.MultipartForm.File["image"]; hasFile {
				form["image"] = []string{"<file>"}
			}
			ts.mu.Lock()
			ts.multipartForm = form
			ts.mu.Unlock()
		}

		w.WriteHeader(code)
		if body != "" {
			_, _ = w.Write([]byte(body))
			return
		}
		_, _ = fmt.Fprintf(w, `{"process_id":%q}`, processID)
	})
	mux.HandleFunc("/image/v1/status/"+processID, func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		ts.statusCalls++
		count := ts.statusCalls
		fn := ts.statusFn
		code := ts.statusCode
		body := ts.statusBody
		ts.mu.Unlock()

		if fn != nil {
			code, body = fn(count)
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/image/v1/download/"+processID, func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		code := ts.downloadCode
		body := ts.downloadBody
		ts.mu.Unlock()
		w.WriteHeader(code)
		if body != "" {
			_, _ = w.Write([]byte(body))
			return
		}
		_, _ = fmt.Fprintf(w, `{"download_url":%q}`, ts.server.URL+"/dl/out.png")
	})
	mux.HandleFunc("/dl/out.png", func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		ts.dlHeader = r.Header.Clone()
		ts.mu.Unlock()
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(outputBytes)
	})
	mux.HandleFunc("/image/v1/cancel/"+processID, func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		ts.cancelCalled = true
		ts.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	ts.server = httptest.NewServer(mux)
	return ts
}

func (ts *imageTestServer) wasCancelCalled() bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.cancelCalled
}

func (ts *imageTestServer) getMultipartForm() map[string][]string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.multipartForm
}

func (ts *imageTestServer) model() *ImageModel {
	prov := New(Config{APIKey: "test-key", BaseURL: ts.server.URL})
	return NewImageModel(prov, ImageModelWonder35)
}

func defaultImageOptions(ts *imageTestServer) *provider.ImageGenerateOptions {
	return &provider.ImageGenerateOptions{
		Files: []provider.ImageFile{inputImageFile()},
		ProviderOptions: map[string]interface{}{
			"topaz": map[string]interface{}{"pollIntervalMillis": 1},
		},
	}
}

// TestImageModel_Metadata mirrors TS "exposes provider and model
// information".
func TestImageModel_Metadata(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	m := NewImageModel(prov, ImageModelWonder35)
	if m.Provider() != "topaz.image" {
		t.Errorf("Provider() = %q", m.Provider())
	}
	if m.ModelID() != ImageModelWonder35 {
		t.Errorf("ModelID() = %q", m.ModelID())
	}
	if m.SpecificationVersion() != "v4" {
		t.Errorf("SpecificationVersion() = %q", m.SpecificationVersion())
	}
	if m.MaxImagesPerCall() != 1 {
		t.Errorf("MaxImagesPerCall() = %d, want 1", m.MaxImagesPerCall())
	}
}

// TestImageModel_DoGenerate_SubmitsPollsDownloadsAndReturns mirrors TS
// "submits, polls, downloads and returns the enhanced image".
func TestImageModel_DoGenerate_SubmitsPollsDownloadsAndReturns(t *testing.T) {
	ts := newImageTestServer(t)
	defer ts.server.Close()

	result, err := ts.model().DoGenerate(context.Background(), defaultImageOptions(ts))
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	if len(result.Images) != 1 || string(result.Images[0]) != string(outputBytes) {
		t.Errorf("Images = %#v", result.Images)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("Warnings = %#v, want none", result.Warnings)
	}
	if result.Response == nil || result.Response.ModelID != ImageModelWonder35 {
		t.Fatalf("Response = %#v", result.Response)
	}

	topazMeta, ok := result.ProviderMetadata["topaz"].(map[string]interface{})
	if !ok {
		t.Fatalf("ProviderMetadata = %#v", result.ProviderMetadata)
	}
	images := topazMeta["images"].([]interface{})
	image := images[0].(map[string]interface{})
	if image["processId"] != processID || image["credits"] != 2.0 || image["width"] != 4000.0 || image["height"] != 3000.0 || image["format"] != "png" {
		t.Errorf("image metadata = %#v", image)
	}
}

// TestImageModel_MapsModelIDAndUploadsFile mirrors TS "maps the model id
// onto the Topaz model name and uploads the file".
func TestImageModel_MapsModelIDAndUploadsFile(t *testing.T) {
	ts := newImageTestServer(t)
	defer ts.server.Close()

	if _, err := ts.model().DoGenerate(context.Background(), defaultImageOptions(ts)); err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}

	form := ts.getMultipartForm()
	if got := form["model"]; len(got) != 1 || got[0] != "Wonder 3.5" {
		t.Errorf("model field = %#v, want Wonder 3.5", got)
	}
	if _, ok := form["image"]; !ok {
		t.Error("image file part missing")
	}
}

// TestImageModel_SendsAPIKey mirrors TS "sends the API key on the Topaz
// endpoints".
func TestImageModel_SendsAPIKey(t *testing.T) {
	ts := newImageTestServer(t)
	defer ts.server.Close()

	if _, err := ts.model().DoGenerate(context.Background(), defaultImageOptions(ts)); err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	if got := ts.requestHeader.Get("X-API-Key"); got != "test-key" {
		t.Errorf("X-API-Key = %q, want test-key", got)
	}
}

// TestImageModel_DerivesOutputDimensionsFromSize mirrors TS "derives
// output dimensions from the size option".
func TestImageModel_DerivesOutputDimensionsFromSize(t *testing.T) {
	ts := newImageTestServer(t)
	defer ts.server.Close()

	opts := defaultImageOptions(ts)
	opts.Size = "4000x3000"
	if _, err := ts.model().DoGenerate(context.Background(), opts); err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}

	form := ts.getMultipartForm()
	if form["output_width"][0] != "4000" || form["output_height"][0] != "3000" {
		t.Errorf("output dims = %#v", form)
	}
}

// TestImageModel_PrefersExplicitOutputDimensions mirrors TS "prefers
// explicit output dimensions over the size option".
func TestImageModel_PrefersExplicitOutputDimensions(t *testing.T) {
	ts := newImageTestServer(t)
	defer ts.server.Close()

	opts := defaultImageOptions(ts)
	opts.Size = "4000x3000"
	opts.ProviderOptions = map[string]interface{}{
		"topaz": map[string]interface{}{
			"pollIntervalMillis": 1,
			"outputWidth":        8000,
			"outputHeight":       6000,
		},
	}
	if _, err := ts.model().DoGenerate(context.Background(), opts); err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}

	form := ts.getMultipartForm()
	if form["output_width"][0] != "8000" || form["output_height"][0] != "6000" {
		t.Errorf("output dims = %#v", form)
	}
}

// TestImageModel_SchemaSnakeCaseModelSettingsCamelCase mirrors TS "sends
// schema fields in snake_case and model settings in camelCase".
func TestImageModel_SchemaSnakeCaseModelSettingsCamelCase(t *testing.T) {
	ts := newImageTestServer(t)
	defer ts.server.Close()

	opts := defaultImageOptions(ts)
	opts.ProviderOptions = map[string]interface{}{
		"topaz": map[string]interface{}{
			"pollIntervalMillis":  1,
			"outputFormat":        "jpeg",
			"cropToFill":          true,
			"enhancementStrength": "medium",
			"grain":               true,
			"grainDensity":        0.25,
			"grainModel":          "gaussian",
			"grainSize":           2,
			"grainStrength":       0.75,
			"inputWidth":          1000,
			"inputHeight":         800,
		},
	}
	if _, err := ts.model().DoGenerate(context.Background(), opts); err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}

	form := ts.getMultipartForm()
	want := map[string]string{
		"output_format":       "jpeg",
		"crop_to_fill":        "true",
		"enhancementStrength": "medium",
		"grain":               "true",
		"grainDensity":        "0.25",
		"grainModel":          "gaussian",
		"grainSize":           "2",
		"grainStrength":       "0.75",
		"inputWidth":          "1000",
		"inputHeight":         "800",
	}
	for k, v := range want {
		if got := form[k]; len(got) != 1 || got[0] != v {
			t.Errorf("%s = %#v, want %q", k, got, v)
		}
	}
}

// TestImageModel_URLInputAsSourceURL mirrors TS "passes a URL input
// through as source_url instead of uploading bytes".
func TestImageModel_URLInputAsSourceURL(t *testing.T) {
	ts := newImageTestServer(t)
	defer ts.server.Close()

	opts := defaultImageOptions(ts)
	opts.Files = []provider.ImageFile{{Type: "url", URL: "https://example.com/input.png"}}
	if _, err := ts.model().DoGenerate(context.Background(), opts); err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}

	form := ts.getMultipartForm()
	if form["source_url"] == nil || form["source_url"][0] != "https://example.com/input.png" {
		t.Errorf("source_url = %#v", form["source_url"])
	}
	if _, ok := form["image"]; ok {
		t.Error("image file part should not be present for a URL input")
	}
}

// TestImageModel_PollsUntilCompletion mirrors TS "polls until the job
// completes".
func TestImageModel_PollsUntilCompletion(t *testing.T) {
	ts := newImageTestServer(t)
	defer ts.server.Close()

	statuses := []string{"Pending", "Processing", "Completed"}
	ts.statusFn = func(count int) (int, string) {
		idx := count - 1
		status := "Completed"
		if idx < len(statuses) {
			status = statuses[idx]
		}
		return http.StatusOK, fmt.Sprintf(`{"status":%q}`, status)
	}

	result, err := ts.model().DoGenerate(context.Background(), defaultImageOptions(ts))
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	if len(result.Images) != 1 {
		t.Errorf("Images = %#v", result.Images)
	}
	ts.mu.Lock()
	calls := ts.statusCalls
	ts.mu.Unlock()
	if calls != 3 {
		t.Errorf("statusCalls = %d, want 3", calls)
	}
}

// TestImageModel_WarnsAboutUnsupportedOptions mirrors TS "warns about
// options Topaz does not support".
func TestImageModel_WarnsAboutUnsupportedOptions(t *testing.T) {
	ts := newImageTestServer(t)
	defer ts.server.Close()

	n := 2
	seed := 42
	mask := inputImageFile()
	opts := defaultImageOptions(ts)
	opts.Prompt = "make it pretty"
	opts.AspectRatio = "16:9"
	opts.Seed = &seed
	opts.N = &n
	opts.Mask = &mask

	result, err := ts.model().DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}

	var features []string
	for _, w := range result.Warnings {
		features = append(features, w.Feature)
	}
	want := []string{"prompt", "aspectRatio", "seed", "mask", "n"}
	if len(features) != len(want) {
		t.Fatalf("warnings = %#v, want %#v", features, want)
	}
	for i, f := range want {
		if features[i] != f {
			t.Errorf("warnings[%d] = %q, want %q (full: %#v)", i, features[i], f, features)
		}
	}
}

// TestImageModel_WarnsWhenMoreThanOneFile mirrors TS "warns when more
// than one input file is passed".
func TestImageModel_WarnsWhenMoreThanOneFile(t *testing.T) {
	ts := newImageTestServer(t)
	defer ts.server.Close()

	opts := defaultImageOptions(ts)
	opts.Files = []provider.ImageFile{inputImageFile(), inputImageFile()}
	result, err := ts.model().DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	found := false
	for _, w := range result.Warnings {
		if w.Feature == "files" {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %#v, want a files warning", result.Warnings)
	}
}

// TestImageModel_ThrowsWhenNoInputImage mirrors TS "throws when no input
// image is provided".
func TestImageModel_ThrowsWhenNoInputImage(t *testing.T) {
	ts := newImageTestServer(t)
	defer ts.server.Close()

	opts := defaultImageOptions(ts)
	opts.Files = nil
	_, err := ts.model().DoGenerate(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "enhance an existing image") {
		t.Fatalf("error = %v, want 'enhance an existing image'", err)
	}
}

// TestImageModel_ThrowsWhenJobFails mirrors TS "throws when the job
// fails".
func TestImageModel_ThrowsWhenJobFails(t *testing.T) {
	ts := newImageTestServer(t)
	defer ts.server.Close()
	ts.statusBody = `{"status":"Failed"}`

	_, err := ts.model().DoGenerate(context.Background(), defaultImageOptions(ts))
	if err == nil || !strings.Contains(err.Error(), "failed for process "+processID) {
		t.Fatalf("error = %v, want 'failed for process %s'", err, processID)
	}
}

// TestImageModel_ThrowsWhenJobCancelled mirrors TS "throws when the job
// is cancelled" (and never issues an extra DELETE for an already-terminal
// job).
func TestImageModel_ThrowsWhenJobCancelled(t *testing.T) {
	ts := newImageTestServer(t)
	defer ts.server.Close()
	ts.statusBody = `{"status":"Cancelled"}`

	_, err := ts.model().DoGenerate(context.Background(), defaultImageOptions(ts))
	if err == nil || !strings.Contains(err.Error(), "cancelled for process "+processID) {
		t.Fatalf("error = %v, want 'cancelled for process %s'", err, processID)
	}
	if ts.wasCancelCalled() {
		t.Error("cancel should not be called for an already-terminal (Cancelled) job")
	}
}

// TestImageModel_ThrowsWhenPollingTimesOut mirrors TS "throws when
// polling exceeds the timeout" and verifies the abandoned job is
// canceled.
func TestImageModel_ThrowsWhenPollingTimesOut(t *testing.T) {
	ts := newImageTestServer(t)
	defer ts.server.Close()
	ts.statusBody = `{"status":"Processing"}`

	opts := defaultImageOptions(ts)
	opts.ProviderOptions = map[string]interface{}{
		"topaz": map[string]interface{}{"pollIntervalMillis": 1, "pollTimeoutMillis": 1},
	}

	_, err := ts.model().DoGenerate(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "did not finish within 1ms") {
		t.Fatalf("error = %v, want 'did not finish within 1ms'", err)
	}
	// Topaz charges on completion, so the abandoned job is canceled.
	if !ts.wasCancelCalled() {
		t.Error("expected the timed-out job to be canceled")
	}
}

// TestImageModel_CancelsJobOnCallerAbort mirrors TS "cancels the job when
// the caller aborts while polling".
func TestImageModel_CancelsJobOnCallerAbort(t *testing.T) {
	ts := newImageTestServer(t)
	defer ts.server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	ts.statusFn = func(count int) (int, string) {
		cancel()
		return http.StatusOK, `{"status":"Processing"}`
	}

	_, err := ts.model().DoGenerate(ctx, defaultImageOptions(ts))
	if err == nil {
		t.Fatal("expected an error after the caller aborts")
	}
	// Give the best-effort cancel request (issued with a detached context,
	// since the caller's own context is already canceled) a moment to land.
	deadline := time.Now().Add(time.Second)
	for !ts.wasCancelCalled() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !ts.wasCancelCalled() {
		t.Error("expected the job to be canceled after the caller aborted")
	}
}

// TestImageModel_ThrowsWhenNoDownloadURL mirrors TS "throws when no
// download URL is returned".
func TestImageModel_ThrowsWhenNoDownloadURL(t *testing.T) {
	ts := newImageTestServer(t)
	defer ts.server.Close()
	ts.downloadBody = `{}`

	_, err := ts.model().DoGenerate(context.Background(), defaultImageOptions(ts))
	if err == nil || !strings.Contains(err.Error(), "did not return a download URL") {
		t.Fatalf("error = %v, want 'did not return a download URL'", err)
	}
}

// TestImageModel_SurfacesTopazErrorDetails mirrors TS "surfaces Topaz
// error details".
func TestImageModel_SurfacesTopazErrorDetails(t *testing.T) {
	ts := newImageTestServer(t)
	defer ts.server.Close()
	ts.submitCode = http.StatusUnprocessableEntity
	ts.submitBody = `{"detail":[{"msg":"model is required"}]}`

	_, err := ts.model().DoGenerate(context.Background(), defaultImageOptions(ts))
	if err == nil || !strings.Contains(err.Error(), "model is required") {
		t.Fatalf("error = %v, want 'model is required'", err)
	}
}

// TestImageModel_SurfacesDocumentedImageErrorShape mirrors TS "surfaces
// the documented image error shape".
func TestImageModel_SurfacesDocumentedImageErrorShape(t *testing.T) {
	ts := newImageTestServer(t)
	defer ts.server.Close()
	ts.submitCode = http.StatusPaymentRequired
	ts.submitBody = `{"code":402,"message":"Insufficient credits"}`

	_, err := ts.model().DoGenerate(context.Background(), defaultImageOptions(ts))
	if err == nil || !strings.Contains(err.Error(), "Insufficient credits") {
		t.Fatalf("error = %v, want 'Insufficient credits'", err)
	}
}

// TestImageModel_WorkflowSerializationRoundTrip mirrors TS "supports
// workflow serialization", exercised through the package-level helpers
// rather than TS's bracketed symbol access.
func TestImageModel_WorkflowSerializationRoundTrip(t *testing.T) {
	prov := New(Config{APIKey: "test-key", BaseURL: "https://api.topazlabs.com"})
	model := NewImageModel(prov, ImageModelWonder35)

	serialized := model.Serialize()
	data, err := json.Marshal(serialized)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	var roundTripped provider.SerializedModel
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}
	restored, err := deserializeImageModel(roundTripped)
	if err != nil {
		t.Fatalf("deserializeImageModel() error: %v", err)
	}
	if restored.Provider() != "topaz.image" || restored.ModelID() != ImageModelWonder35 {
		t.Fatalf("restored = %#v", restored)
	}
}

// TestImageModel_SameOriginDownloadForwardsAPIKey guards against a
// regression where the final foreign-download-URL fetch only forwarded
// the caller's per-call headers (opts.Headers) instead of the full header
// set (including the provider's own X-API-Key default), so a download URL
// that happens to stay on the configured base URL would be fetched
// without authentication.
func TestImageModel_SameOriginDownloadForwardsAPIKey(t *testing.T) {
	ts := newImageTestServer(t)
	defer ts.server.Close()

	opts := defaultImageOptions(ts)
	opts.Headers = map[string]string{"X-Custom": "custom-value"}
	if _, err := ts.model().DoGenerate(context.Background(), opts); err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}

	ts.mu.Lock()
	dlHeader := ts.dlHeader
	ts.mu.Unlock()
	if got := dlHeader.Get("X-API-Key"); got != "test-key" {
		t.Errorf("download request X-API-Key = %q, want test-key", got)
	}
	if got := dlHeader.Get("X-Custom"); got != "custom-value" {
		t.Errorf("download request X-Custom = %q, want custom-value", got)
	}
}
