package topaz

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

const videoRequestID = "req-abc-123"

func inputVideoFile() provider.VideoModelV3File {
	return provider.VideoModelV3File{Type: "file", MediaType: "video/mp4", Data: []byte{1, 2, 3, 4, 5, 6, 7, 8}}
}

// videoSourceOptions mirrors the TS sourceOptions fixture: Topaz needs
// source metadata up front, so the happy-path options carry the pieces
// that cannot be derived from the request.
func videoSourceOptions() map[string]interface{} {
	return map[string]interface{}{
		"source": map[string]interface{}{
			"width": 1920, "height": 1080, "duration": 10, "frameRate": 30, "frameCount": 300,
		},
	}
}

// videoTestServer is a configurable httptest server backing the Topaz
// video express/status/cancel/upload endpoints. All mutable fields are
// guarded by mu because the HTTP server invokes handlers from its own
// goroutines while the test's main goroutine reads the captured state.
type videoTestServer struct {
	mu sync.Mutex

	expressCode int
	expressBody string // empty uses the default success body
	statusBody  string
	statusCode  int
	uploadCode  int

	expressCalls   int
	lastExpressReq map[string]interface{}
	expressHeaders http.Header
	cancelCalled   bool
	cancelHeaders  http.Header
	uploadHeaders  http.Header
	uploadBody     []byte

	server *httptest.Server
}

func newVideoTestServer(t *testing.T) *videoTestServer {
	t.Helper()
	ts := &videoTestServer{
		expressCode: http.StatusOK,
		statusCode:  http.StatusOK,
		uploadCode:  http.StatusOK,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/video/express", func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		ts.expressCalls++
		ts.expressHeaders = r.Header.Clone()
		code := ts.expressCode
		body := ts.expressBody
		ts.mu.Unlock()

		var req map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&req)
		ts.mu.Lock()
		ts.lastExpressReq = req
		ts.mu.Unlock()

		w.WriteHeader(code)
		if body != "" {
			_, _ = w.Write([]byte(body))
			return
		}
		_, _ = fmt.Fprintf(w,
			`{"requestId":%q,"uploadId":"upload-express","uploadUrls":[%q]}`,
			videoRequestID, ts.server.URL+"/upload/part-1")
	})
	mux.HandleFunc("/upload/part-1", func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		ts.uploadHeaders = r.Header.Clone()
		code := ts.uploadCode
		ts.mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		ts.mu.Lock()
		ts.uploadBody = body
		ts.mu.Unlock()
		w.WriteHeader(code)
	})
	mux.HandleFunc("/video/"+videoRequestID+"/status", func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		code := ts.statusCode
		body := ts.statusBody
		ts.mu.Unlock()
		if body == "" {
			body = fmt.Sprintf(`{"status":"complete","outputSize":"12345","estimates":{"cost":[14,18],"time":[60,90]},"download":{"url":%q,"expiresAt":1767229200000}}`, ts.server.URL+"/dl/out.mp4")
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/dl/out.mp4", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("video-bytes"))
	})
	mux.HandleFunc("/video/"+videoRequestID, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.NotFound(w, r)
			return
		}
		ts.mu.Lock()
		ts.cancelCalled = true
		ts.cancelHeaders = r.Header.Clone()
		ts.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	ts.server = httptest.NewServer(mux)
	return ts
}

func (ts *videoTestServer) model() *VideoModel {
	prov := New(Config{
		APIKey:                  "test-key",
		BaseURL:                 ts.server.URL,
		VideoPollIntervalMillis: 1,
		VideoPollTimeoutMillis:  200,
	})
	return NewVideoModel(prov, VideoModelStarlightPrecise26)
}

func (ts *videoTestServer) modelWithID(modelID string) *VideoModel {
	prov := New(Config{APIKey: "test-key", BaseURL: ts.server.URL})
	return NewVideoModel(prov, modelID)
}

func (ts *videoTestServer) wasCancelCalled() bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.cancelCalled
}

func (ts *videoTestServer) lastRequest() map[string]interface{} {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.lastExpressReq
}

func defaultStartOptions() *provider.VideoModelV3StartOptions {
	return &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
			N:               1,
			InputReferences: []provider.VideoModelV3File{inputVideoFile()},
			ProviderOptions: map[string]interface{}{"topaz": videoSourceOptions()},
		},
	}
}

// TestVideoModel_Metadata mirrors TS "exposes provider and model
// information".
func TestVideoModel_Metadata(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	m := NewVideoModel(prov, VideoModelStarlightPrecise26)
	if m.Provider() != "topaz.video" {
		t.Errorf("Provider() = %q", m.Provider())
	}
	if m.ModelID() != VideoModelStarlightPrecise26 {
		t.Errorf("ModelID() = %q", m.ModelID())
	}
	if m.SpecificationVersion() != "v4" {
		t.Errorf("SpecificationVersion() = %q", m.SpecificationVersion())
	}
	if m.MaxVideosPerCall() == nil || *m.MaxVideosPerCall() != 1 {
		t.Errorf("MaxVideosPerCall() = %v, want 1", m.MaxVideosPerCall())
	}
}

// TestVideoModel_DoStart_CreatesExpressRequestAndUploads mirrors TS
// "creates an express request with the source metadata and uploads the
// file".
func TestVideoModel_DoStart_CreatesExpressRequestAndUploads(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	result, err := ts.model().DoStart(context.Background(), defaultStartOptions())
	if err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}

	req := ts.lastRequest()
	source := req["source"].(map[string]interface{})
	if source["container"] != "mp4" || source["duration"] != 10.0 || source["frameCount"] != 300.0 ||
		source["frameRate"] != 30.0 || source["size"] != 8.0 {
		t.Errorf("source = %#v", source)
	}
	resolution := source["resolution"].(map[string]interface{})
	if resolution["width"] != 1920.0 || resolution["height"] != 1080.0 {
		t.Errorf("resolution = %#v", resolution)
	}

	var op topazVideoOperation
	if err := json.Unmarshal(result.Operation, &op); err != nil {
		t.Fatalf("Unmarshal(operation) error: %v", err)
	}
	if op.RequestID != videoRequestID || op.OutputContainer != "mp4" {
		t.Errorf("operation = %#v", op)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("Warnings = %#v, want none", result.Warnings)
	}
}

// TestVideoModel_ReportsInitialCostEstimate mirrors TS "reports the
// initial cost estimate without a billed amount".
func TestVideoModel_ReportsInitialCostEstimate(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()
	ts.expressBody = fmt.Sprintf(`{"requestId":%q,"uploadUrls":[%q],"estimates":{"cost":[12,15],"time":[60,90]}}`, videoRequestID, ts.server.URL+"/upload/part-1")

	result, err := ts.model().DoStart(context.Background(), defaultStartOptions())
	if err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	topazMeta := result.ProviderMetadata["topaz"].(map[string]interface{})
	if topazMeta["requestId"] != videoRequestID {
		t.Errorf("requestId = %#v", topazMeta["requestId"])
	}
	estimated, ok := topazMeta["estimatedCredits"].([]float64)
	if !ok || len(estimated) != 2 || estimated[0] != 12 || estimated[1] != 15 {
		t.Errorf("estimatedCredits = %#v", topazMeta["estimatedCredits"])
	}
}

// TestVideoModel_MapsModelIDOntoFilterModelName mirrors TS "maps the
// model id onto the Topaz filter model name" / "maps proteus onto its
// Topaz model name" / "forwards a raw Topaz model name unchanged".
func TestVideoModel_MapsModelIDOntoFilterModelName(t *testing.T) {
	cases := []struct {
		modelID string
		want    string
	}{
		{VideoModelStarlightPrecise26, "slp-2.6"},
		{VideoModelProteus, "prob-4"},
		{"slp-2.5", "slp-2.5"},
	}
	for _, tc := range cases {
		ts := newVideoTestServer(t)
		_, err := ts.modelWithID(tc.modelID).DoStart(context.Background(), defaultStartOptions())
		if err != nil {
			t.Fatalf("DoStart() error for %s: %v", tc.modelID, err)
		}
		req := ts.lastRequest()
		filters := req["filters"].([]interface{})
		filter := filters[0].(map[string]interface{})
		if filter["model"] != tc.want {
			t.Errorf("modelID %s: filter.model = %#v, want %q", tc.modelID, filter["model"], tc.want)
		}
		ts.server.Close()
	}
}

// TestVideoModel_DerivesFrameCountFromDurationAndFrameRate mirrors TS
// "derives frameCount from duration and frameRate".
func TestVideoModel_DerivesFrameCountFromDurationAndFrameRate(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	opts := defaultStartOptions()
	opts.ProviderOptions = map[string]interface{}{
		"topaz": map[string]interface{}{
			"source": map[string]interface{}{"width": 1280, "height": 720, "duration": 4, "frameRate": 25},
		},
	}
	if _, err := ts.model().DoStart(context.Background(), opts); err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	req := ts.lastRequest()
	source := req["source"].(map[string]interface{})
	if source["frameCount"] != 100.0 {
		t.Errorf("frameCount = %#v, want 100", source["frameCount"])
	}
}

// TestVideoModel_FrameCountRoundsNotTruncates guards against a regression
// where the derived frameCount (duration * frameRate) was computed with a
// plain int() conversion, which truncates toward zero, instead of
// rounding like TS's Math.round. 5 * 29.97 = 149.85, which must round up
// to 150, not truncate down to 149.
func TestVideoModel_FrameCountRoundsNotTruncates(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	opts := defaultStartOptions()
	opts.ProviderOptions = map[string]interface{}{
		"topaz": map[string]interface{}{
			"source": map[string]interface{}{"width": 1280, "height": 720, "duration": 5, "frameRate": 29.97},
		},
	}
	if _, err := ts.model().DoStart(context.Background(), opts); err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	req := ts.lastRequest()
	source := req["source"].(map[string]interface{})
	if source["frameCount"] != 150.0 {
		t.Errorf("frameCount = %#v, want 150", source["frameCount"])
	}
}

// TestVideoModel_MapsResolutionAndFPSOntoOutput mirrors TS "maps the
// resolution and fps call options onto the output".
func TestVideoModel_MapsResolutionAndFPSOntoOutput(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	opts := defaultStartOptions()
	opts.Resolution = "3840x2160"
	fps := 60
	opts.FPS = &fps
	if _, err := ts.model().DoStart(context.Background(), opts); err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	req := ts.lastRequest()
	source := req["source"].(map[string]interface{})
	sourceRes := source["resolution"].(map[string]interface{})
	if sourceRes["width"] != 1920.0 || sourceRes["height"] != 1080.0 {
		t.Errorf("source.resolution = %#v", sourceRes)
	}
	output := req["output"].(map[string]interface{})
	outputRes := output["resolution"].(map[string]interface{})
	if outputRes["width"] != 3840.0 || outputRes["height"] != 2160.0 {
		t.Errorf("output.resolution = %#v", outputRes)
	}
	if output["frameRate"] != 60.0 {
		t.Errorf("output.frameRate = %#v, want 60", output["frameRate"])
	}
}

// TestVideoModel_DefaultsOutputToSourceWithAACCopyAudio mirrors TS
// "defaults the output to the source, with AAC/Copy audio".
func TestVideoModel_DefaultsOutputToSourceWithAACCopyAudio(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	if _, err := ts.model().DoStart(context.Background(), defaultStartOptions()); err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	req := ts.lastRequest()
	output := req["output"].(map[string]interface{})
	res := output["resolution"].(map[string]interface{})
	if res["width"] != 1920.0 || res["height"] != 1080.0 || output["frameRate"] != 30.0 ||
		output["audioCodec"] != "AAC" || output["audioTransfer"] != "Copy" || output["container"] != "mp4" {
		t.Errorf("output = %#v", output)
	}
}

// TestVideoModel_AppliesOutputProviderOptions mirrors TS "applies output
// provider options".
func TestVideoModel_AppliesOutputProviderOptions(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	topazOpts := videoSourceOptions()
	topazOpts["output"] = map[string]interface{}{
		"width": 3840, "height": 2160, "frameRate": 60,
		"audioCodec": "PCM", "audioTransfer": "None", "container": "mov",
	}
	opts := defaultStartOptions()
	opts.ProviderOptions = map[string]interface{}{"topaz": topazOpts}
	if _, err := ts.model().DoStart(context.Background(), opts); err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	req := ts.lastRequest()
	output := req["output"].(map[string]interface{})
	res := output["resolution"].(map[string]interface{})
	if res["width"] != 3840.0 || res["height"] != 2160.0 || output["frameRate"] != 60.0 ||
		output["audioCodec"] != "PCM" || output["audioTransfer"] != "None" || output["container"] != "mov" {
		t.Errorf("output = %#v", output)
	}
}

// TestVideoModel_SendsModelSettingsAsFilterFields mirrors TS "sends model
// settings as filter fields".
func TestVideoModel_SendsModelSettingsAsFilterFields(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	topazOpts := videoSourceOptions()
	topazOpts["sharpness"] = 3.5
	topazOpts["videoCodec"] = "prores"
	topazOpts["watermark"] = false
	opts := defaultStartOptions()
	opts.ProviderOptions = map[string]interface{}{"topaz": topazOpts}
	if _, err := ts.model().DoStart(context.Background(), opts); err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	req := ts.lastRequest()
	filter := req["filters"].([]interface{})[0].(map[string]interface{})
	if filter["model"] != "slp-2.6" || filter["sharpness"] != 3.5 || filter["videoCodec"] != "prores" || filter["watermark"] != false {
		t.Errorf("filter = %#v", filter)
	}
}

// TestVideoModel_FilterEscapeHatchOverridesTypedOptions mirrors TS "lets
// the filter escape hatch override typed options".
func TestVideoModel_FilterEscapeHatchOverridesTypedOptions(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	topazOpts := videoSourceOptions()
	topazOpts["sharpness"] = 3.5
	topazOpts["filter"] = map[string]interface{}{"sharpness": 1, "experimentalSetting": "on"}
	opts := defaultStartOptions()
	opts.ProviderOptions = map[string]interface{}{"topaz": topazOpts}
	if _, err := ts.model().DoStart(context.Background(), opts); err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	req := ts.lastRequest()
	filter := req["filters"].([]interface{})[0].(map[string]interface{})
	if filter["model"] != "slp-2.6" || filter["sharpness"] != 1.0 || filter["experimentalSetting"] != "on" {
		t.Errorf("filter = %#v", filter)
	}
}

// TestVideoModel_AppendsAdditionalFilters mirrors TS "appends additional
// filters".
func TestVideoModel_AppendsAdditionalFilters(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	topazOpts := videoSourceOptions()
	topazOpts["additionalFilters"] = []interface{}{map[string]interface{}{"model": "apo-8", "fps": 60}}
	opts := defaultStartOptions()
	opts.ProviderOptions = map[string]interface{}{"topaz": topazOpts}
	if _, err := ts.model().DoStart(context.Background(), opts); err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	req := ts.lastRequest()
	filters := req["filters"].([]interface{})
	if len(filters) != 2 {
		t.Fatalf("filters = %#v", filters)
	}
	if filters[1].(map[string]interface{})["model"] != "apo-8" {
		t.Errorf("filters[1] = %#v", filters[1])
	}
}

// TestVideoModel_UploadsBytesWithContainerContentType mirrors TS "uploads
// the bytes with the container content type" and "does not send the API
// key to the upload URL".
func TestVideoModel_UploadsBytesWithContainerContentType(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	if _, err := ts.model().DoStart(context.Background(), defaultStartOptions()); err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	ts.mu.Lock()
	uploadHeaders := ts.uploadHeaders
	uploadBody := ts.uploadBody
	ts.mu.Unlock()
	if uploadHeaders.Get("Content-Type") != "video/mp4" {
		t.Errorf("upload Content-Type = %q, want video/mp4", uploadHeaders.Get("Content-Type"))
	}
	if string(uploadBody) != string(inputVideoFile().Data) {
		t.Errorf("upload body = %v, want %v", uploadBody, inputVideoFile().Data)
	}
	if uploadHeaders.Get("X-API-Key") != "" {
		t.Error("upload request should not carry the API key")
	}
}

// TestVideoModel_SendsAPIKeyToTopazEndpoints mirrors TS "sends the API
// key to the Topaz endpoints".
func TestVideoModel_SendsAPIKeyToTopazEndpoints(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	_, err := ts.model().DoStart(context.Background(), defaultStartOptions())
	if err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	ts.mu.Lock()
	gotKey := ts.expressHeaders.Get("X-API-Key")
	ts.mu.Unlock()
	if gotKey != "test-key" {
		t.Errorf("X-API-Key = %q, want test-key", gotKey)
	}
}

// TestVideoModel_DoesNotWarnAboutEmptyPrompt mirrors TS "does not warn
// about an empty prompt".
func TestVideoModel_DoesNotWarnAboutEmptyPrompt(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	opts := defaultStartOptions()
	opts.Prompt = ""
	opts.PromptSet = true
	result, err := ts.model().DoStart(context.Background(), opts)
	if err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("Warnings = %#v, want none", result.Warnings)
	}
}

// TestVideoModel_WarnsAboutUnsupportedOptions mirrors TS "warns about
// options Topaz does not support".
func TestVideoModel_WarnsAboutUnsupportedOptions(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	seed := 7
	duration := 5.0
	generateAudio := true
	opts := defaultStartOptions()
	opts.Prompt = "make it sharp"
	opts.PromptSet = true
	opts.AspectRatio = "16:9"
	opts.Seed = &seed
	opts.GenerateAudio = &generateAudio
	opts.FrameImages = []provider.VideoFrameImage{{
		Image:     provider.VideoModelV3File{Type: "url", URL: "https://example.com/first.png"},
		FrameType: provider.VideoFrameTypeFirstFrame,
	}}
	opts.Duration = &duration
	opts.N = 2

	result, err := ts.model().DoStart(context.Background(), opts)
	if err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	var features []string
	for _, w := range result.Warnings {
		features = append(features, w.Feature)
	}
	want := []string{"prompt", "aspectRatio", "seed", "duration", "generateAudio", "frameImages", "n"}
	if len(features) != len(want) {
		t.Fatalf("warnings = %#v, want %#v", features, want)
	}
	for i, f := range want {
		if features[i] != f {
			t.Errorf("warnings[%d] = %q, want %q (full: %#v)", i, features[i], f, features)
		}
	}
}

// TestVideoModel_ThrowsWhenSourceMetadataPartial mirrors TS "throws and
// names the missing metadata when source metadata is partial".
func TestVideoModel_ThrowsWhenSourceMetadataPartial(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	opts := defaultStartOptions()
	opts.ProviderOptions = map[string]interface{}{
		"topaz": map[string]interface{}{"source": map[string]interface{}{"width": 1920}},
	}
	_, err := ts.model().DoStart(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "must be complete. Missing: source.height, source.duration, source.frameRate.") {
		t.Fatalf("error = %v", err)
	}
	ts.mu.Lock()
	calls := ts.expressCalls
	ts.mu.Unlock()
	if calls != 0 {
		t.Errorf("expressCalls = %d, want 0", calls)
	}
}

// TestVideoModel_ThrowsWhenNoVideoReference mirrors TS "throws when no
// video reference is passed".
func TestVideoModel_ThrowsWhenNoVideoReference(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	opts := defaultStartOptions()
	opts.InputReferences = nil
	_, err := ts.model().DoStart(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "require an input video") {
		t.Fatalf("error = %v", err)
	}
}

// TestVideoModel_ThrowsTargetedErrorForStillImage mirrors TS "throws a
// targeted error when only a still image is passed".
func TestVideoModel_ThrowsTargetedErrorForStillImage(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	opts := defaultStartOptions()
	opts.InputReferences = nil
	opts.Image = &provider.VideoModelV3File{Type: "url", URL: "https://example.com/frame.png"}
	_, err := ts.model().DoStart(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "not a still image") {
		t.Fatalf("error = %v", err)
	}
}

// TestVideoModel_ThrowsForUnsupportedContainer mirrors TS "throws for an
// unsupported container".
func TestVideoModel_ThrowsForUnsupportedContainer(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	opts := defaultStartOptions()
	opts.InputReferences = []provider.VideoModelV3File{{Type: "file", MediaType: "video/ogg", Data: inputVideoFile().Data}}
	_, err := ts.model().DoStart(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), `Could not map the media type "video/ogg"`) {
		t.Fatalf("error = %v", err)
	}
}

// TestVideoModel_WarnsWhenMoreThanOneReference mirrors TS "warns when
// more than one reference is passed".
func TestVideoModel_WarnsWhenMoreThanOneReference(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	opts := defaultStartOptions()
	opts.InputReferences = []provider.VideoModelV3File{inputVideoFile(), inputVideoFile()}
	result, err := ts.model().DoStart(context.Background(), opts)
	if err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	found := false
	for _, w := range result.Warnings {
		if w.Feature == "inputReferences" {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %#v, want an inputReferences warning", result.Warnings)
	}
}

// TestVideoModel_SurfacesTopazErrorDetailsFromCreateCall mirrors TS
// "surfaces Topaz error details from the create call".
func TestVideoModel_SurfacesTopazErrorDetailsFromCreateCall(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()
	ts.expressCode = http.StatusBadRequest
	ts.expressBody = `{"detail":"frameCount must be positive"}`

	_, err := ts.model().DoStart(context.Background(), defaultStartOptions())
	if err == nil || !strings.Contains(err.Error(), "frameCount must be positive") {
		t.Fatalf("error = %v", err)
	}
}

// TestVideoModel_ThrowsWhenNoUploadURL mirrors TS "throws when no upload
// URL is returned".
func TestVideoModel_ThrowsWhenNoUploadURL(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()
	ts.expressBody = fmt.Sprintf(`{"requestId":%q}`, videoRequestID)

	_, err := ts.model().DoStart(context.Background(), defaultStartOptions())
	if err == nil || !strings.Contains(err.Error(), "returned no upload URL") {
		t.Fatalf("error = %v", err)
	}
}

// TestVideoModel_LetsTopazFetchURLInput mirrors TS "lets Topaz fetch a
// URL input when source metadata is set".
func TestVideoModel_LetsTopazFetchURLInput(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()
	ts.expressBody = fmt.Sprintf(`{"requestId":%q}`, videoRequestID)

	const sourceURL = "https://media.example.com/clips/input.mov"
	opts := defaultStartOptions()
	opts.InputReferences = []provider.VideoModelV3File{{Type: "url", URL: sourceURL, MediaType: "video/quicktime"}}
	result, err := ts.model().DoStart(context.Background(), opts)
	if err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	req := ts.lastRequest()
	source := req["source"].(map[string]interface{})
	external := source["external"].(map[string]interface{})
	if source["container"] != "mov" || external["provider"] != "s3" || external["presignedUrl"] != sourceURL {
		t.Errorf("source = %#v", source)
	}
	if _, hasSize := source["size"]; hasSize {
		t.Error("source.size should not be set for a URL input")
	}
	var op topazVideoOperation
	_ = json.Unmarshal(result.Operation, &op)
	if op.RequestID != videoRequestID || op.OutputContainer != "mov" {
		t.Errorf("operation = %#v", op)
	}
}

// TestVideoModel_CancelsRequestWhenUploadFails mirrors TS "cancels the
// request when a step after create fails" / "cancels the request when
// the upload fails".
func TestVideoModel_CancelsRequestWhenUploadFails(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()
	ts.uploadCode = http.StatusServiceUnavailable

	_, err := ts.model().DoStart(context.Background(), defaultStartOptions())
	if err == nil || !strings.Contains(err.Error(), "failed with status 503") {
		t.Fatalf("error = %v", err)
	}
	if !ts.wasCancelCalled() {
		t.Error("expected the request to be canceled after the upload failed")
	}
	ts.mu.Lock()
	cancelHeaders := ts.cancelHeaders
	ts.mu.Unlock()
	if cancelHeaders.Get("X-API-Key") != "test-key" {
		t.Errorf("cancel X-API-Key = %q, want test-key", cancelHeaders.Get("X-API-Key"))
	}
}

// TestVideoModel_IncludesFieldValidationErrors mirrors TS "includes field
// validation errors in API errors".
func TestVideoModel_IncludesFieldValidationErrors(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()
	ts.expressCode = http.StatusBadRequest
	ts.expressBody = `{"message":"Invalid input","errorCode":"INVALID_INPUT","errors":[{"type":"field","msg":"frameCount is required"},{"type":"field","msg":"resolution is required"}]}`

	_, err := ts.model().DoStart(context.Background(), defaultStartOptions())
	want := "Invalid input (INVALID_INPUT): frameCount is required; resolution is required"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestVideoModel_IncludesTopazErrorCode mirrors TS "includes the Topaz
// error code in API errors".
func TestVideoModel_IncludesTopazErrorCode(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()
	ts.expressCode = http.StatusPaymentRequired
	ts.expressBody = `{"message":"Not enough credits","errorCode":"INSUFFICIENT_CREDITS"}`

	_, err := ts.model().DoStart(context.Background(), defaultStartOptions())
	want := "Not enough credits (INSUFFICIENT_CREDITS)"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestVideoModel_ReportsContainerForcedByEncoder mirrors TS "reports the
// container Topaz forces for the chosen encoder".
func TestVideoModel_ReportsContainerForcedByEncoder(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	topazOpts := videoSourceOptions()
	topazOpts["output"] = map[string]interface{}{"videoEncoder": "ProRes", "container": "mp4"}
	opts := defaultStartOptions()
	opts.ProviderOptions = map[string]interface{}{"topaz": topazOpts}
	result, err := ts.model().DoStart(context.Background(), opts)
	if err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	var op topazVideoOperation
	_ = json.Unmarshal(result.Operation, &op)
	if op.OutputContainer != "mov" {
		t.Errorf("outputContainer = %q, want mov", op.OutputContainer)
	}
}

// TestVideoModel_RequiresOutputResolution mirrors TS "requires an output
// resolution".
func TestVideoModel_RequiresOutputResolution(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	opts := defaultStartOptions()
	opts.ProviderOptions = map[string]interface{}{"topaz": map[string]interface{}{"sharpness": 3}}
	_, err := ts.model().DoStart(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "needs the output resolution") {
		t.Fatalf("error = %v", err)
	}
	ts.mu.Lock()
	calls := ts.expressCalls
	ts.mu.Unlock()
	if calls != 0 {
		t.Errorf("expressCalls = %d, want 0", calls)
	}
}

// --- DoStatus ---

func statusOperation(outputContainer string) json.RawMessage {
	op, _ := json.Marshal(topazVideoOperation{RequestID: videoRequestID, OutputContainer: outputContainer})
	return op
}

// TestVideoModel_DoStatus_ReturnsDownloadURLWhenComplete mirrors TS
// "returns the download URL when the request completes".
func TestVideoModel_DoStatus_ReturnsDownloadURLWhenComplete(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	result, err := ts.model().DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: statusOperation("mp4")})
	if err != nil {
		t.Fatalf("DoStatus() error: %v", err)
	}
	if result.Status != provider.VideoOperationStatusCompleted {
		t.Fatalf("Status = %q", result.Status)
	}
	if len(result.Videos) != 1 || result.Videos[0].MediaType != "video/mp4" {
		t.Errorf("Videos = %#v", result.Videos)
	}
}

// TestVideoModel_DoStatus_BillsLowerBoundCost mirrors TS "bills the lower
// bound of the post-upload cost estimate" and "uses the cheaper bound
// regardless of order".
func TestVideoModel_DoStatus_BillsLowerBoundCost(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	result, err := ts.model().DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: statusOperation("mp4")})
	if err != nil {
		t.Fatalf("DoStatus() error: %v", err)
	}
	meta := result.ProviderMetadata["topaz"].(map[string]interface{})
	if meta["credits"] != 14.0 {
		t.Errorf("credits = %#v, want 14", meta["credits"])
	}
	if meta["outputSize"] != "12345" {
		t.Errorf("outputSize = %#v", meta["outputSize"])
	}

	ts.statusBody = fmt.Sprintf(`{"status":"complete","estimates":{"cost":[20,16]},"download":{"url":%q}}`, ts.server.URL+"/dl/out.mp4")
	result2, err := ts.model().DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: statusOperation("mp4")})
	if err != nil {
		t.Fatalf("DoStatus() error: %v", err)
	}
	meta2 := result2.ProviderMetadata["topaz"].(map[string]interface{})
	if meta2["credits"] != 16.0 {
		t.Errorf("credits = %#v, want 16", meta2["credits"])
	}
}

// TestVideoModel_DoStatus_OmitsCreditsWhenNoEstimate mirrors TS "omits
// credits when Topaz returns no cost estimate".
func TestVideoModel_DoStatus_OmitsCreditsWhenNoEstimate(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()
	ts.statusBody = fmt.Sprintf(`{"status":"complete","download":{"url":%q}}`, ts.server.URL+"/dl/out.mp4")

	result, err := ts.model().DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: statusOperation("mp4")})
	if err != nil {
		t.Fatalf("DoStatus() error: %v", err)
	}
	meta := result.ProviderMetadata["topaz"].(map[string]interface{})
	if len(meta) != 1 || meta["requestId"] != videoRequestID {
		t.Errorf("metadata = %#v, want only requestId", meta)
	}
}

// TestVideoModel_DoStatus_ReportsOutputContainerMediaType mirrors TS
// "reports the media type of the output container".
func TestVideoModel_DoStatus_ReportsOutputContainerMediaType(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	result, err := ts.model().DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: statusOperation("mov")})
	if err != nil {
		t.Fatalf("DoStatus() error: %v", err)
	}
	if result.Videos[0].MediaType != "video/quicktime" {
		t.Errorf("MediaType = %q, want video/quicktime", result.Videos[0].MediaType)
	}
}

// TestVideoModel_DoStatus_ReportsPendingStatuses mirrors TS
// "reports %s as pending" for each documented in-progress status.
func TestVideoModel_DoStatus_ReportsPendingStatuses(t *testing.T) {
	statuses := []string{
		"requested", "accepted", "initializing", "preprocessing",
		"processing", "postprocessing", "canceling",
	}
	for _, status := range statuses {
		ts := newVideoTestServer(t)
		ts.statusBody = fmt.Sprintf(`{"status":%q}`, status)
		result, err := ts.model().DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: statusOperation("mp4")})
		if err != nil {
			t.Fatalf("DoStatus() error for %s: %v", status, err)
		}
		if result.Status != provider.VideoOperationStatusPending {
			t.Errorf("status %s: Status = %q, want pending", status, result.Status)
		}
		ts.server.Close()
	}
}

// TestVideoModel_DoStatus_ReportsFailedAsError mirrors TS "reports a
// failed request as an error" and "includes the Topaz error code on a
// failed request".
func TestVideoModel_DoStatus_ReportsFailedAsError(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()
	ts.statusBody = `{"status":"failed","message":"out of credits"}`

	result, err := ts.model().DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: statusOperation("mp4")})
	if err != nil {
		t.Fatalf("DoStatus() error: %v", err)
	}
	if result.Status != provider.VideoOperationStatusError || !strings.Contains(result.Error, "out of credits") {
		t.Fatalf("result = %#v", result)
	}
}

func TestVideoModel_DoStatus_IncludesErrorCodeOnFailedRequest(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()
	ts.statusBody = `{"status":"failed","errorCode":"CREDIT_DIFFERENCE","message":"Estimate changed after upload"}`

	result, err := ts.model().DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: statusOperation("mp4")})
	if err != nil {
		t.Fatalf("DoStatus() error: %v", err)
	}
	wantErr := fmt.Sprintf("Topaz video request %s failed (CREDIT_DIFFERENCE): Estimate changed after upload", videoRequestID)
	if result.Status != provider.VideoOperationStatusError || result.Error != wantErr {
		t.Fatalf("result.Error = %q, want %q", result.Error, wantErr)
	}
	meta := result.ProviderMetadata["topaz"].(map[string]interface{})
	if meta["errorCode"] != "CREDIT_DIFFERENCE" {
		t.Errorf("errorCode = %#v", meta["errorCode"])
	}
}

// TestVideoModel_DoStatus_ReportsCanceledAsError mirrors TS "reports a
// canceled request as an error".
func TestVideoModel_DoStatus_ReportsCanceledAsError(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()
	ts.statusBody = `{"status":"canceled"}`

	result, err := ts.model().DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: statusOperation("mp4")})
	if err != nil {
		t.Fatalf("DoStatus() error: %v", err)
	}
	if result.Status != provider.VideoOperationStatusError {
		t.Errorf("Status = %q, want error", result.Status)
	}
}

// TestVideoModel_DoStatus_ThrowsWhenCompletedWithNoDownloadURL mirrors TS
// "throws when a completed request has no download URL".
func TestVideoModel_DoStatus_ThrowsWhenCompletedWithNoDownloadURL(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()
	ts.statusBody = `{"status":"complete"}`

	_, err := ts.model().DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: statusOperation("mp4")})
	if err == nil || !strings.Contains(err.Error(), "returned no download URL") {
		t.Fatalf("error = %v", err)
	}
}

// TestVideoModel_DoStatus_KeepsPollingOnUnrecognizedStatus mirrors TS
// "keeps polling on an unrecognized status".
func TestVideoModel_DoStatus_KeepsPollingOnUnrecognizedStatus(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()
	ts.statusBody = `{"status":"sideways"}`

	result, err := ts.model().DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: statusOperation("mp4")})
	if err != nil {
		t.Fatalf("DoStatus() error: %v", err)
	}
	if result.Status != provider.VideoOperationStatusPending {
		t.Errorf("Status = %q, want pending", result.Status)
	}
}

// TestVideoModel_DoGenerate_SynchronousWrapper is a Go-only test (Topaz's
// TS provider has no doGenerate; the TS SDK core polls doStart/doStatus
// itself). It exercises the DoGenerate synchronous start+poll wrapper Go's
// provider.VideoModelV3 interface requires.
func TestVideoModel_DoGenerate_SynchronousWrapper(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	opts := defaultStartOptions().VideoModelV3CallOptions
	result, err := ts.model().DoGenerate(context.Background(), &opts)
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	if len(result.Videos) != 1 || result.Videos[0].URL == "" {
		t.Errorf("Videos = %#v", result.Videos)
	}
}

// TestVideoModel_DoGenerate_PropagatesErrorStatus is a Go-only test
// verifying DoGenerate surfaces an "error" DoStatus result as an error.
func TestVideoModel_DoGenerate_PropagatesErrorStatus(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()
	ts.statusBody = `{"status":"failed","message":"boom"}`

	opts := defaultStartOptions().VideoModelV3CallOptions
	_, err := ts.model().DoGenerate(context.Background(), &opts)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v, want to contain boom", err)
	}
}

// TestVideoModel_DoGenerate_RespectsContextCancellation is a Go-only test
// verifying DoGenerate's poll loop returns promptly when ctx is canceled,
// instead of only noticing cancellation once an in-flight request
// completes.
func TestVideoModel_DoGenerate_RespectsContextCancellation(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()
	ts.statusBody = `{"status":"processing"}`

	ctx, cancel := context.WithCancel(context.Background())
	opts := defaultStartOptions().VideoModelV3CallOptions

	startResult, err := ts.model().DoStart(ctx, &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: opts})
	if err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	_ = startResult
	cancel()

	_, err = ts.model().DoGenerate(ctx, &opts)
	if err == nil {
		t.Fatal("expected an error after ctx is canceled before DoGenerate starts")
	}
}
