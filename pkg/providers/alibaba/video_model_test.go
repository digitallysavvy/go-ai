package alibaba

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

func TestVideoModelDetectMode(t *testing.T) {
	tests := []struct {
		modelID string
		want    string
	}{
		{"wan2.5-t2v", "t2v"},
		{"wan2.6-t2v", "t2v"},
		{"wan2.6-i2v", "i2v"},
		{"wan2.6-i2v-flash", "i2v"},
		{"wan2.6-r2v", "r2v"},
		{"wan2.6-r2v-flash", "r2v"},
		{"unknown-model", "t2v"}, // defaults to t2v
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			model := NewVideoModel(New(Config{APIKey: "test-key"}), tt.modelID)
			if got := model.detectMode(); got != tt.want {
				t.Errorf("detectMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectAlibabaVideoProtocol(t *testing.T) {
	tests := []struct {
		modelID string
		want    alibabaVideoProtocol
	}{
		{"wan2.6-t2v", alibabaProtocolLegacy},
		{"wan2.6-i2v-flash", alibabaProtocolLegacy},
		{"wan2.7-t2v", alibabaProtocolWan27},
		{"wan2.7-r2v-2026-06-12", alibabaProtocolWan27},
		{"wan3.0-video", alibabaProtocolWan3},
	}
	for _, tt := range tests {
		if got := detectAlibabaVideoProtocol(tt.modelID); got != tt.want {
			t.Errorf("detectAlibabaVideoProtocol(%q) = %q, want %q", tt.modelID, got, tt.want)
		}
	}
}

func buildOpts(modelID string) *VideoModel {
	return NewVideoModel(New(Config{APIKey: "test-key"}), modelID)
}

// TestBuildRequest_T2V mirrors "doStart > text-to-video > should send correct request body for T2V".
func TestBuildRequest_T2V(t *testing.T) {
	model := buildOpts("wan2.6-t2v")
	req := model.buildRequest(&provider.VideoModelV3CallOptions{Prompt: "A cat walking", PromptSet: true})

	if req.Input["prompt"] != "A cat walking" {
		t.Errorf("input.prompt = %v", req.Input["prompt"])
	}
	if _, exists := req.Input["img_url"]; exists {
		t.Error("T2V should not set img_url")
	}
}

// TestBuildRequest_T2V_SizeConversion mirrors "should send size parameter for T2V resolution (x converted to *)".
func TestBuildRequest_T2V_SizeConversion(t *testing.T) {
	model := buildOpts("wan2.6-t2v")
	req := model.buildRequest(&provider.VideoModelV3CallOptions{Prompt: "x", Resolution: "1280x720"})

	if req.Parameters["size"] != "1280*720" {
		t.Errorf("parameters.size = %v, want 1280*720", req.Parameters["size"])
	}
}

// TestBuildRequest_I2V_URLImage mirrors "image-to-video > should send img_url from URL-based image for I2V model".
func TestBuildRequest_I2V_URLImage(t *testing.T) {
	model := buildOpts("wan2.6-i2v")
	req := model.buildRequest(&provider.VideoModelV3CallOptions{
		Prompt: "The cat turns its head",
		Image:  &provider.VideoModelV3File{Type: "url", URL: "https://example.com/cat.jpg"},
	})

	if req.Input["img_url"] != "https://example.com/cat.jpg" {
		t.Errorf("img_url = %v", req.Input["img_url"])
	}
}

// TestBuildRequest_I2V_FileImage mirrors "should send img_url as base64 from file data".
func TestBuildRequest_I2V_FileImage(t *testing.T) {
	model := buildOpts("wan2.6-i2v")
	req := model.buildRequest(&provider.VideoModelV3CallOptions{
		Image: &provider.VideoModelV3File{Type: "file", Data: []byte{0x89, 0x50, 0x4E, 0x47}, MediaType: "image/png"},
	})

	imgURL, ok := req.Input["img_url"].(string)
	if !ok || imgURL != "iVBORw==" {
		t.Errorf("img_url = %v, want raw base64 iVBORw==", req.Input["img_url"])
	}
}

// TestBuildRequest_I2V_ResolutionTier mirrors "should map resolution to I2V format (WxH -> 720P/1080P)".
func TestBuildRequest_I2V_ResolutionTier(t *testing.T) {
	model := buildOpts("wan2.6-i2v")
	req := model.buildRequest(&provider.VideoModelV3CallOptions{Resolution: "1920x1080"})
	if req.Parameters["resolution"] != "1080P" {
		t.Errorf("resolution = %v, want 1080P", req.Parameters["resolution"])
	}
}

// TestBuildRequest_R2V_ReferenceURLs mirrors "reference-to-video > should send reference_urls for R2V model".
func TestBuildRequest_R2V_ReferenceURLs(t *testing.T) {
	model := buildOpts("wan2.6-r2v")
	req := model.buildRequest(&provider.VideoModelV3CallOptions{
		Prompt:          "A character walking",
		InputReferences: []provider.VideoModelV3File{{Type: "url", URL: "https://example.com/reference.jpg"}},
	})

	urls, ok := req.Input["reference_urls"].([]string)
	if !ok || len(urls) != 1 || urls[0] != "https://example.com/reference.jpg" {
		t.Errorf("reference_urls = %v", req.Input["reference_urls"])
	}
}

// TestBuildRequest_R2V_NotSentForNonR2V mirrors "should not send reference_urls for non-R2V model".
func TestBuildRequest_R2V_NotSentForNonR2V(t *testing.T) {
	model := buildOpts("wan2.6-t2v")
	req := model.buildRequest(&provider.VideoModelV3CallOptions{
		Prompt:          "x",
		InputReferences: []provider.VideoModelV3File{{Type: "url", URL: "https://example.com/reference.jpg"}},
	})
	if _, exists := req.Input["reference_urls"]; exists {
		t.Error("reference_urls should not be set for non-R2V model")
	}
	if len(req.Warnings) == 0 {
		t.Error("expected an unsupported inputReferences warning")
	}
}

// --- wan3 (all-in-one) ---

// TestBuildRequest_Wan3_TextOnly mirrors "wan3 > should send a bare prompt with no media for text-to-video".
func TestBuildRequest_Wan3_TextOnly(t *testing.T) {
	model := buildOpts("wan3.0-video")
	req := model.buildRequest(&provider.VideoModelV3CallOptions{Prompt: "A cat walking", PromptSet: true})

	if req.Input["prompt"] != "A cat walking" {
		t.Errorf("input.prompt = %v", req.Input["prompt"])
	}
	if _, exists := req.Input["media"]; exists {
		t.Error("expected no media for a bare text-to-video wan3 call")
	}
}

// TestBuildRequest_Wan3_StartImageInMedia mirrors "should carry the start image in input.media instead of img_url".
func TestBuildRequest_Wan3_StartImageInMedia(t *testing.T) {
	model := buildOpts("wan3.0-video")
	req := model.buildRequest(&provider.VideoModelV3CallOptions{
		Image: &provider.VideoModelV3File{Type: "url", URL: "https://example.com/start.jpg"},
	})

	if _, exists := req.Input["img_url"]; exists {
		t.Error("wan3 should never set img_url")
	}
	media, ok := req.Input["media"].([]map[string]interface{})
	if !ok || len(media) != 1 || media[0]["type"] != "first_frame" || media[0]["url"] != "https://example.com/start.jpg" {
		t.Errorf("media = %v", req.Input["media"])
	}
}

// TestBuildRequest_Wan3_FrameImages mirrors "should send both frame images without warning" and
// "should send last_frame alone without warning".
func TestBuildRequest_Wan3_FrameImages(t *testing.T) {
	model := buildOpts("wan3.0-video")
	req := model.buildRequest(&provider.VideoModelV3CallOptions{
		FrameImages: []provider.VideoFrameImage{
			{FrameType: provider.VideoFrameTypeFirstFrame, Image: provider.VideoModelV3File{Type: "url", URL: "https://example.com/first.jpg"}},
			{FrameType: provider.VideoFrameTypeLastFrame, Image: provider.VideoModelV3File{Type: "url", URL: "https://example.com/last.jpg"}},
		},
	})

	media, ok := req.Input["media"].([]map[string]interface{})
	if !ok || len(media) != 2 {
		t.Fatalf("media = %v", req.Input["media"])
	}
	if media[0]["type"] != "first_frame" || media[1]["type"] != "last_frame" {
		t.Errorf("media types = %v, %v", media[0]["type"], media[1]["type"])
	}
	for _, w := range req.Warnings {
		if w.Feature == "frameImages" {
			t.Errorf("unexpected frameImages warning on wan3: %+v", w)
		}
	}
}

// TestBuildRequest_Wan3_ResolutionTiers mirrors "should send resolution tiers, including 480P" and
// "should warn on a resolution outside the three tiers".
func TestBuildRequest_Wan3_ResolutionTiers(t *testing.T) {
	model := buildOpts("wan3.0-video")

	req := model.buildRequest(&provider.VideoModelV3CallOptions{Resolution: "832x480"})
	if req.Parameters["resolution"] != "480P" {
		t.Errorf("resolution = %v, want 480P", req.Parameters["resolution"])
	}

	req2 := model.buildRequest(&provider.VideoModelV3CallOptions{Resolution: "100x100"})
	if _, exists := req2.Parameters["resolution"]; exists {
		t.Error("unsupported resolution should not be forwarded")
	}
	foundWarning := false
	for _, w := range req2.Warnings {
		if w.Feature == "resolution" {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Error("expected resolution warning")
	}
}

// TestBuildRequest_Wan3_AudioDefaultAndRatio mirrors
// "should send the audio toggle, adaptive ratio, and smart duration".
func TestBuildRequest_Wan3_AudioAndRatio(t *testing.T) {
	model := buildOpts("wan3.0-video")
	generateAudio := true
	req := model.buildRequest(&provider.VideoModelV3CallOptions{
		GenerateAudio:   &generateAudio,
		ProviderOptions: map[string]interface{}{"alibaba": map[string]interface{}{"ratio": "adaptive"}},
	})

	if req.Parameters["audio"] != true {
		t.Errorf("audio = %v, want true", req.Parameters["audio"])
	}
	if req.Parameters["ratio"] != "adaptive" {
		t.Errorf("ratio = %v, want adaptive", req.Parameters["ratio"])
	}
}

// TestBuildRequest_Wan3_ShotTypeWarning mirrors "should warn about shotType, which wan3 dropped".
func TestBuildRequest_Wan3_ShotTypeWarning(t *testing.T) {
	model := buildOpts("wan3.0-video")
	req := model.buildRequest(&provider.VideoModelV3CallOptions{
		ProviderOptions: map[string]interface{}{"alibaba": map[string]interface{}{"shotType": "multi"}},
	})
	if _, exists := req.Parameters["shot_type"]; exists {
		t.Error("wan3 should not send shot_type")
	}
	found := false
	for _, w := range req.Warnings {
		if w.Feature == "shotType" {
			found = true
		}
	}
	if !found {
		t.Error("expected shotType warning")
	}
}

// TestBuildRequest_Wan3_FirstFramePrecedence mirrors "should prefer frameImages first_frame over the legacy image option".
func TestBuildRequest_Wan3_FirstFramePrecedence(t *testing.T) {
	model := buildOpts("wan3.0-video")
	req := model.buildRequest(&provider.VideoModelV3CallOptions{
		Image: &provider.VideoModelV3File{Type: "url", URL: "https://example.com/legacy.jpg"},
		FrameImages: []provider.VideoFrameImage{
			{FrameType: provider.VideoFrameTypeFirstFrame, Image: provider.VideoModelV3File{Type: "url", URL: "https://example.com/frame.jpg"}},
		},
	})
	media := req.Input["media"].([]map[string]interface{})
	if len(media) != 1 || media[0]["url"] != "https://example.com/frame.jpg" {
		t.Errorf("media = %v", media)
	}
}

// TestBuildRequest_Wan3_MediaOverride mirrors "should use providerOptions.alibaba.media as an override".
func TestBuildRequest_Wan3_MediaOverride(t *testing.T) {
	model := buildOpts("wan3.0-video")
	req := model.buildRequest(&provider.VideoModelV3CallOptions{
		Image: &provider.VideoModelV3File{Type: "url", URL: "https://example.com/ignored.jpg"},
		ProviderOptions: map[string]interface{}{
			"alibaba": map[string]interface{}{
				"media": []interface{}{
					map[string]interface{}{"type": "reference_audio", "url": "https://example.com/audio.mp3"},
					map[string]interface{}{"type": "file", "url": "https://example.com/file.bin"},
				},
			},
		},
	})
	media := req.Input["media"].([]map[string]interface{})
	if len(media) != 2 || media[0]["type"] != "reference_audio" || media[1]["type"] != "file" {
		t.Errorf("media override = %v", media)
	}
}

// --- warnings ---

func TestBuildRequest_Warnings(t *testing.T) {
	model := buildOpts("wan2.6-t2v")
	fps := 30
	req := model.buildRequest(&provider.VideoModelV3CallOptions{
		Prompt:      "x",
		AspectRatio: "16:9",
		FPS:         &fps,
		N:           2,
	})

	features := map[string]bool{}
	for _, w := range req.Warnings {
		features[w.Feature] = true
	}
	for _, want := range []string{"aspectRatio", "fps", "n"} {
		if !features[want] {
			t.Errorf("expected warning for %q, got %+v", want, req.Warnings)
		}
	}
}

// --- DoStart / DoStatus / DoGenerate (httptest) ---

func newAlibabaVideoTestModel(t *testing.T, handler http.HandlerFunc) (*VideoModel, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	prov := New(Config{APIKey: "test-key", VideoBaseURL: server.URL})
	return NewVideoModel(prov, "wan2.6-t2v"), server
}

// TestDoStart_ReturnsOperation mirrors "should return operation with taskId".
func TestDoStart_ReturnsOperation(t *testing.T) {
	var capturedHeader string
	model, server := newAlibabaVideoTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/services/aigc/video-generation/video-synthesis" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		capturedHeader = r.Header.Get("X-DashScope-Async")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"output": map[string]interface{}{"task_id": "task_123", "task_status": "PENDING"},
		})
	})
	defer server.Close()

	result, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x", PromptSet: true},
	})
	if err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	if capturedHeader != "enable" {
		t.Errorf("X-DashScope-Async header = %q, want enable", capturedHeader)
	}
	var op alibabaOperation
	if err := json.Unmarshal(result.Operation, &op); err != nil || op.TaskID != "task_123" {
		t.Errorf("operation = %s", result.Operation)
	}
}

// TestDoStart_NoTaskID mirrors "should throw when no task_id returned".
func TestDoStart_NoTaskID(t *testing.T) {
	model, server := newAlibabaVideoTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"output": map[string]interface{}{}})
	})
	defer server.Close()

	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"},
	})
	if err == nil {
		t.Fatal("expected error when no task_id returned")
	}
}

// TestDoStatus_Completed mirrors "should return completed with video data when SUCCEEDED".
func TestDoStatus_Completed(t *testing.T) {
	model, server := newAlibabaVideoTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/task_123" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"output": map[string]interface{}{
				"task_id":     "task_123",
				"task_status": "SUCCEEDED",
				"video_url":   "https://example.com/video.mp4",
			},
		})
	})
	defer server.Close()

	op, _ := json.Marshal(alibabaOperation{TaskID: "task_123"})
	result, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil {
		t.Fatalf("DoStatus() error: %v", err)
	}
	if result.Status != provider.VideoOperationStatusCompleted {
		t.Fatalf("status = %v", result.Status)
	}
	if len(result.Videos) != 1 || result.Videos[0].URL != "https://example.com/video.mp4" {
		t.Errorf("videos = %+v", result.Videos)
	}
}

// TestDoStatus_EncodesTaskID verifies the task ID is percent-encoded as a
// single path segment before being appended to the status URL, so a
// provider-returned ID containing "/", "?", or other reserved characters
// cannot redirect the request to a different path or inject query
// parameters (Low hardening item from the Sep-23 E3 slice).
func TestDoStatus_EncodesTaskID(t *testing.T) {
	const rawTaskID = "task/123?evil=1"
	model, server := newAlibabaVideoTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		// The task ID's "/" and "?" must reach the server percent-encoded
		// (never as literal path/query separators): check the raw
		// request-line target, since r.URL.Path is decoded back to the
		// original ID by net/http.
		wantRawPath := "/api/v1/tasks/" + providerutils.EncodePathSegment(rawTaskID)
		if r.URL.EscapedPath() != wantRawPath {
			t.Fatalf("raw path = %s, want %s", r.URL.EscapedPath(), wantRawPath)
		}
		if r.URL.RawQuery != "" {
			t.Fatalf("unexpected query on request: %s", r.URL.RawQuery)
		}
		if r.URL.Path != "/api/v1/tasks/"+rawTaskID {
			t.Fatalf("decoded path = %s, want %s", r.URL.Path, "/api/v1/tasks/"+rawTaskID)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"output": map[string]interface{}{
				"task_id":     rawTaskID,
				"task_status": "SUCCEEDED",
				"video_url":   "https://example.com/video.mp4",
			},
		})
	})
	defer server.Close()

	op, _ := json.Marshal(alibabaOperation{TaskID: rawTaskID})
	result, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil {
		t.Fatalf("DoStatus() error: %v", err)
	}
	if result.Status != provider.VideoOperationStatusCompleted {
		t.Fatalf("status = %v", result.Status)
	}
}

// TestDoStatus_Pending mirrors "should return pending when PENDING"/"RUNNING".
func TestDoStatus_Pending(t *testing.T) {
	model, server := newAlibabaVideoTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"output": map[string]interface{}{"task_id": "task_123", "task_status": "RUNNING"},
		})
	})
	defer server.Close()

	op, _ := json.Marshal(alibabaOperation{TaskID: "task_123"})
	result, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil {
		t.Fatalf("DoStatus() error: %v", err)
	}
	if result.Status != provider.VideoOperationStatusPending {
		t.Fatalf("status = %v", result.Status)
	}
}

// TestDoStatus_Error mirrors "should return error status on FAILED" and "on CANCELED".
func TestDoStatus_Error(t *testing.T) {
	for _, status := range []string{"FAILED", "CANCELED"} {
		t.Run(status, func(t *testing.T) {
			model, server := newAlibabaVideoTestModel(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"output": map[string]interface{}{"task_id": "task_123", "task_status": status, "message": "boom"},
				})
			})
			defer server.Close()

			op, _ := json.Marshal(alibabaOperation{TaskID: "task_123"})
			result, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
			if err != nil {
				t.Fatalf("DoStatus() error: %v", err)
			}
			if result.Status != provider.VideoOperationStatusError {
				t.Fatalf("status = %v", result.Status)
			}
		})
	}
}

// TestDoGenerate_ComposesStartAndStatus verifies DoGenerate is built entirely
// on top of DoStart+DoStatus (no separate polling implementation), polling
// until the task reports SUCCEEDED.
func TestDoGenerate_ComposesStartAndStatus(t *testing.T) {
	pollCount := 0
	model, server := newAlibabaVideoTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/services/aigc/video-generation/video-synthesis":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"output": map[string]interface{}{"task_id": "task_123", "task_status": "PENDING"},
			})
		case "/api/v1/tasks/task_123":
			pollCount++
			if pollCount < 2 {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"output": map[string]interface{}{"task_id": "task_123", "task_status": "RUNNING"},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"output": map[string]interface{}{
					"task_id": "task_123", "task_status": "SUCCEEDED",
					"video_url": "https://example.com/video.mp4",
				},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	defer server.Close()

	result, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "x", PromptSet: true,
		ProviderOptions: map[string]interface{}{"alibaba": map[string]interface{}{"pollIntervalMs": 5}},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	if len(result.Videos) != 1 || result.Videos[0].URL != "https://example.com/video.mp4" {
		t.Fatalf("videos = %+v", result.Videos)
	}
	if pollCount < 2 {
		t.Errorf("expected at least 2 status polls, got %d", pollCount)
	}
}

func TestVideoModelSpecificationVersion(t *testing.T) {
	model := buildOpts("wan2.6-t2v")
	if model.SpecificationVersion() != "v4" {
		t.Errorf("SpecificationVersion() = %q, want v4", model.SpecificationVersion())
	}
}

func TestVideoModelProvider(t *testing.T) {
	model := buildOpts("wan2.6-t2v")
	if model.Provider() != "alibaba.video" {
		t.Errorf("Provider() = %q", model.Provider())
	}
}

func TestVideoModelID(t *testing.T) {
	modelID := "wan2.6-i2v"
	model := buildOpts(modelID)
	if model.ModelID() != modelID {
		t.Errorf("ModelID() = %q, want %q", model.ModelID(), modelID)
	}
}

func TestVideoModelAcceptsCustomModelID(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	m, err := prov.VideoModel("wan3.0-video")
	if err != nil {
		t.Fatalf("VideoModel() error: %v", err)
	}
	if m.ModelID() != "wan3.0-video" {
		t.Errorf("ModelID() = %q", m.ModelID())
	}
}
