package bfl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func newVideoTestModel(t *testing.T) *VideoModel {
	t.Helper()
	return NewVideoModel(New(Config{APIKey: "test-key"}), VideoModelFlux3Video)
}

// TestVideoModel_ProviderInfo mirrors TS "should expose correct provider and model information".
func TestVideoModel_ProviderInfo(t *testing.T) {
	m := newVideoTestModel(t)
	if m.Provider() != "bfl.video" {
		t.Errorf("Provider() = %q", m.Provider())
	}
	if m.ModelID() != VideoModelFlux3Video {
		t.Errorf("ModelID() = %q", m.ModelID())
	}
	if m.SpecificationVersion() != "v4" {
		t.Errorf("SpecificationVersion() = %q", m.SpecificationVersion())
	}
	if max := m.MaxVideosPerCall(); max == nil || *max != 1 {
		t.Errorf("MaxVideosPerCall() = %v, want 1", max)
	}
}

// TestBuildVideoArgs_T2V mirrors "should submit a t2v request with only the fields that were set".
func TestBuildVideoArgs_T2V(t *testing.T) {
	m := newVideoTestModel(t)
	body, warnings, err := m.buildVideoArgs(&provider.VideoModelV3CallOptions{Prompt: "a cat"})
	if err != nil {
		t.Fatalf("buildVideoArgs() error: %v", err)
	}
	if body["mode"] != "t2v" || body["prompt"] != "a cat" {
		t.Errorf("body = %#v", body)
	}
	if _, exists := body["keyframes"]; exists {
		t.Error("t2v should not set keyframes")
	}
	if _, exists := body["start_video"]; exists {
		t.Error("t2v should not set start_video")
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %+v", warnings)
	}
}

// TestBuildVideoArgs_EmptyPrompt mirrors "should send an empty prompt when none is provided".
func TestBuildVideoArgs_EmptyPrompt(t *testing.T) {
	m := newVideoTestModel(t)
	body, _, err := m.buildVideoArgs(&provider.VideoModelV3CallOptions{})
	if err != nil {
		t.Fatalf("buildVideoArgs() error: %v", err)
	}
	if body["prompt"] != "" {
		t.Errorf("prompt = %v, want empty string", body["prompt"])
	}
}

// TestBuildVideoArgs_GenerateAudio mirrors "should pass generateAudio through as generate_audio".
func TestBuildVideoArgs_GenerateAudio(t *testing.T) {
	m := newVideoTestModel(t)
	audio := true
	body, _, err := m.buildVideoArgs(&provider.VideoModelV3CallOptions{GenerateAudio: &audio})
	if err != nil {
		t.Fatalf("buildVideoArgs() error: %v", err)
	}
	if body["generate_audio"] != true {
		t.Errorf("generate_audio = %v", body["generate_audio"])
	}
}

// TestBuildVideoArgs_AspectRatio mirrors "should send a supported aspect ratio" and
// "should warn and omit an unsupported aspect ratio".
func TestBuildVideoArgs_AspectRatio(t *testing.T) {
	m := newVideoTestModel(t)

	body, warnings, err := m.buildVideoArgs(&provider.VideoModelV3CallOptions{AspectRatio: "16:9"})
	if err != nil {
		t.Fatalf("buildVideoArgs() error: %v", err)
	}
	if body["aspect_ratio"] != "16:9" || len(warnings) != 0 {
		t.Errorf("aspect_ratio = %v, warnings = %+v", body["aspect_ratio"], warnings)
	}

	body2, warnings2, err := m.buildVideoArgs(&provider.VideoModelV3CallOptions{AspectRatio: "5:5"})
	if err != nil {
		t.Fatalf("buildVideoArgs() error: %v", err)
	}
	if _, exists := body2["aspect_ratio"]; exists {
		t.Errorf("expected no aspect_ratio, got %v", body2["aspect_ratio"])
	}
	if len(warnings2) != 1 || warnings2[0].Feature != "aspectRatio" {
		t.Errorf("warnings = %+v", warnings2)
	}
}

// TestBuildVideoArgs_ResolutionTiers mirrors "should map 1280x720 onto hd without warning" and
// "should map 1920x1080 onto fhd without warning".
func TestBuildVideoArgs_ResolutionTiers(t *testing.T) {
	m := newVideoTestModel(t)

	body, warnings, err := m.buildVideoArgs(&provider.VideoModelV3CallOptions{Resolution: "1280x720"})
	if err != nil {
		t.Fatalf("buildVideoArgs() error: %v", err)
	}
	if body["resolution"] != "hd" || len(warnings) != 0 {
		t.Errorf("resolution = %v, warnings = %+v", body["resolution"], warnings)
	}

	body2, warnings2, err := m.buildVideoArgs(&provider.VideoModelV3CallOptions{Resolution: "1920x1080"})
	if err != nil {
		t.Fatalf("buildVideoArgs() error: %v", err)
	}
	if body2["resolution"] != "fhd" || len(warnings2) != 0 {
		t.Errorf("resolution = %v, warnings = %+v", body2["resolution"], warnings2)
	}
}

// TestBuildVideoArgs_ResolutionSnapWarns mirrors "should snap an in-between resolution to a tier and report it".
func TestBuildVideoArgs_ResolutionSnapWarns(t *testing.T) {
	m := newVideoTestModel(t)
	body, warnings, err := m.buildVideoArgs(&provider.VideoModelV3CallOptions{Resolution: "900x600"})
	if err != nil {
		t.Fatalf("buildVideoArgs() error: %v", err)
	}
	if body["resolution"] != "hd" {
		t.Errorf("resolution = %v, want hd", body["resolution"])
	}
	if len(warnings) != 1 || warnings[0].Type != "compatibility" {
		t.Errorf("warnings = %+v", warnings)
	}
}

// TestBuildVideoArgs_ProviderOptionResolutionWins mirrors
// "should prefer providerOptions.resolution over the top-level value".
func TestBuildVideoArgs_ProviderOptionResolutionWins(t *testing.T) {
	m := newVideoTestModel(t)
	body, warnings, err := m.buildVideoArgs(&provider.VideoModelV3CallOptions{
		Resolution:      "1920x1080",
		ProviderOptions: map[string]interface{}{"blackForestLabs": map[string]interface{}{"resolution": "hd"}},
	})
	if err != nil {
		t.Fatalf("buildVideoArgs() error: %v", err)
	}
	if body["resolution"] != "hd" {
		t.Errorf("resolution = %v, want hd (provider option wins)", body["resolution"])
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warning when the top-level value also resolves, got %+v", warnings)
	}
}

// TestBuildVideoArgs_Duration mirrors "should clamp a duration above the maximum and warn",
// "should clamp a duration below the minimum and warn", and "should round a fractional duration and warn".
func TestBuildVideoArgs_Duration(t *testing.T) {
	m := newVideoTestModel(t)

	tests := []struct {
		name     string
		duration float64
		want     float64
		warn     bool
	}{
		{"in range", 10, 10, false},
		{"above max", 30, 20, true},
		{"below min", 2, 5, true},
		{"fractional", 7.4, 7, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := tt.duration
			body, warnings, err := m.buildVideoArgs(&provider.VideoModelV3CallOptions{Duration: &d})
			if err != nil {
				t.Fatalf("buildVideoArgs() error: %v", err)
			}
			if body["duration"] != tt.want {
				t.Errorf("duration = %v, want %v", body["duration"], tt.want)
			}
			if tt.warn && len(warnings) == 0 {
				t.Error("expected a duration warning")
			}
			if !tt.warn && len(warnings) != 0 {
				t.Errorf("expected no warning, got %+v", warnings)
			}
		})
	}
}

// TestBuildVideoArgs_StandaloneImageBecomesKeyframe mirrors "should turn a standalone image into a single keyframe".
func TestBuildVideoArgs_StandaloneImageBecomesKeyframe(t *testing.T) {
	m := newVideoTestModel(t)
	body, _, err := m.buildVideoArgs(&provider.VideoModelV3CallOptions{
		Image: &provider.VideoModelV3File{Type: "url", URL: "https://example.com/start.jpg"},
	})
	if err != nil {
		t.Fatalf("buildVideoArgs() error: %v", err)
	}
	if body["mode"] != "i2v" {
		t.Errorf("mode = %v, want i2v", body["mode"])
	}
	keyframes, ok := body["keyframes"].([]interface{})
	if !ok || len(keyframes) != 1 || keyframes[0] != "https://example.com/start.jpg" {
		t.Errorf("keyframes = %#v", body["keyframes"])
	}
}

// TestBuildVideoArgs_FirstAndLastFrame mirrors "should order a first_frame and last_frame into two keyframes".
func TestBuildVideoArgs_FirstAndLastFrame(t *testing.T) {
	m := newVideoTestModel(t)
	body, _, err := m.buildVideoArgs(&provider.VideoModelV3CallOptions{
		FrameImages: []provider.VideoFrameImage{
			{FrameType: provider.VideoFrameTypeFirstFrame, Image: provider.VideoModelV3File{Type: "url", URL: "https://example.com/first.jpg"}},
			{FrameType: provider.VideoFrameTypeLastFrame, Image: provider.VideoModelV3File{Type: "url", URL: "https://example.com/last.jpg"}},
		},
	})
	if err != nil {
		t.Fatalf("buildVideoArgs() error: %v", err)
	}
	keyframes, ok := body["keyframes"].([]interface{})
	if !ok || len(keyframes) != 2 || keyframes[0] != "https://example.com/first.jpg" || keyframes[1] != "https://example.com/last.jpg" {
		t.Errorf("keyframes = %#v", body["keyframes"])
	}
}

// TestBuildVideoArgs_LastFrameWithoutFirstDropped mirrors "should drop a last_frame with no first_frame and warn".
func TestBuildVideoArgs_LastFrameWithoutFirstDropped(t *testing.T) {
	m := newVideoTestModel(t)
	body, warnings, err := m.buildVideoArgs(&provider.VideoModelV3CallOptions{
		FrameImages: []provider.VideoFrameImage{
			{FrameType: provider.VideoFrameTypeLastFrame, Image: provider.VideoModelV3File{Type: "url", URL: "https://example.com/last.jpg"}},
		},
	})
	if err != nil {
		t.Fatalf("buildVideoArgs() error: %v", err)
	}
	if body["mode"] != "t2v" {
		t.Errorf("mode = %v, want t2v (no keyframes)", body["mode"])
	}
	found := false
	for _, w := range warnings {
		if w.Feature == "frameImages" {
			found = true
		}
	}
	if !found {
		t.Error("expected a frameImages warning")
	}
}

// TestBuildVideoArgs_RejectVideoAsStartImage mirrors "should reject a video passed as the starting image and warn".
func TestBuildVideoArgs_RejectVideoAsStartImage(t *testing.T) {
	m := newVideoTestModel(t)
	body, warnings, err := m.buildVideoArgs(&provider.VideoModelV3CallOptions{
		Image: &provider.VideoModelV3File{Type: "url", URL: "https://example.com/clip.mp4", MediaType: "video/mp4"},
	})
	if err != nil {
		t.Fatalf("buildVideoArgs() error: %v", err)
	}
	if body["mode"] != "t2v" {
		t.Errorf("mode = %v, want t2v", body["mode"])
	}
	found := false
	for _, w := range warnings {
		if w.Feature == "image" {
			found = true
		}
	}
	if !found {
		t.Error("expected an image warning rejecting the video")
	}
}

// TestBuildVideoArgs_VideoContinuation mirrors "should continue from a video reference".
func TestBuildVideoArgs_VideoContinuation(t *testing.T) {
	m := newVideoTestModel(t)
	body, _, err := m.buildVideoArgs(&provider.VideoModelV3CallOptions{
		InputReferences: []provider.VideoModelV3File{
			{Type: "url", URL: "https://example.com/clip.mp4", MediaType: "video/mp4"},
		},
	})
	if err != nil {
		t.Fatalf("buildVideoArgs() error: %v", err)
	}
	if body["mode"] != "v2v" || body["start_video"] != "https://example.com/clip.mp4" {
		t.Errorf("body = %#v", body)
	}
}

// TestBuildVideoArgs_ImageReferenceIgnored mirrors "should ignore an image reference and point at the keyframe inputs".
func TestBuildVideoArgs_ImageReferenceIgnored(t *testing.T) {
	m := newVideoTestModel(t)
	body, warnings, err := m.buildVideoArgs(&provider.VideoModelV3CallOptions{
		InputReferences: []provider.VideoModelV3File{
			{Type: "url", URL: "https://example.com/ref.jpg", MediaType: "image/jpeg"},
		},
	})
	if err != nil {
		t.Fatalf("buildVideoArgs() error: %v", err)
	}
	if body["mode"] != "t2v" {
		t.Errorf("mode = %v, want t2v", body["mode"])
	}
	found := false
	for _, w := range warnings {
		if w.Feature == "inputReferences" {
			found = true
		}
	}
	if !found {
		t.Error("expected an inputReferences warning")
	}
}

// TestBuildVideoArgs_UntimedKeyframesNeedDuration mirrors
// "should reject 3 or more untimed keyframes without a duration".
func TestBuildVideoArgs_UntimedKeyframesNeedDuration(t *testing.T) {
	m := newVideoTestModel(t)
	_, _, err := m.buildVideoArgs(&provider.VideoModelV3CallOptions{
		ProviderOptions: map[string]interface{}{
			"blackForestLabs": map[string]interface{}{
				"keyframes": []interface{}{"a", "b", "c"},
			},
		},
	})
	if err == nil {
		t.Fatal("expected an error for 3+ untimed keyframes without a duration")
	}
}

// TestBuildVideoArgs_DraftEnhance mirrors "should replay a bundle with only mode and draft_cache" and
// "should warn about every generation option the bundle pins".
func TestBuildVideoArgs_DraftEnhance(t *testing.T) {
	m := newVideoTestModel(t)
	body, warnings, err := m.buildVideoArgs(&provider.VideoModelV3CallOptions{
		Prompt:      "ignored",
		AspectRatio: "16:9",
		ProviderOptions: map[string]interface{}{
			"blackForestLabs": map[string]interface{}{"draftCache": "bundle-data"},
		},
	})
	if err != nil {
		t.Fatalf("buildVideoArgs() error: %v", err)
	}
	if body["mode"] != "draft_enhance" || body["draft_cache"] != "bundle-data" {
		t.Errorf("body = %#v", body)
	}
	if _, exists := body["prompt"]; exists {
		t.Error("draft_enhance body should not carry prompt")
	}
	foundPrompt := false
	foundAspect := false
	for _, w := range warnings {
		if w.Feature == "prompt" {
			foundPrompt = true
		}
		if w.Feature == "aspectRatio" {
			foundAspect = true
		}
	}
	if !foundPrompt || !foundAspect {
		t.Errorf("expected warnings for pinned prompt and aspectRatio, got %+v", warnings)
	}
}

// ─── DoStart / DoStatus / DoGenerate (httptest), SSRF ──────────────────────────

func newVideoHTTPModel(t *testing.T, handler http.HandlerFunc) (*VideoModel, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	return NewVideoModel(prov, VideoModelFlux3Video), server
}

// TestDoStart_ReturnsOperation mirrors "should expose a serializable operation from doStart".
func TestDoStart_ReturnsOperation(t *testing.T) {
	// serverURL is set after the server starts; the handler only reads it at
	// request time, once the server (and thus its URL) already exists.
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/"+VideoModelFlux3Video, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "req-1", "polling_url": serverURL + "/get_result", "cost": 0.5,
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	serverURL = server.URL

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, VideoModelFlux3Video)

	result, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"},
	})
	if err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	var op bflVideoOperation
	if err := json.Unmarshal(result.Operation, &op); err != nil || op.RequestID != "req-1" {
		t.Fatalf("operation = %s", result.Operation)
	}
}

// TestDoStatus_Completed mirrors "should return a completed video from doStatus".
func TestDoStatus_Completed(t *testing.T) {
	model, server := newVideoHTTPModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "Ready",
			"result": map[string]interface{}{"sample": "https://cdn.example.com/video.mp4"},
		})
	})
	defer server.Close()

	op, _ := json.Marshal(bflVideoOperation{RequestID: "req-1", PollingURL: server.URL + "/get_result"})
	result, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil {
		t.Fatalf("DoStatus() error: %v", err)
	}
	if result.Status != provider.VideoOperationStatusCompleted {
		t.Fatalf("status = %v", result.Status)
	}
	if len(result.Videos) != 1 || result.Videos[0].URL != "https://cdn.example.com/video.mp4" {
		t.Errorf("videos = %+v", result.Videos)
	}
}

// TestDoStatus_SettledCostPreferred mirrors "should prefer the settled cost from the result over the submit estimate".
func TestDoStatus_SettledCostPreferred(t *testing.T) {
	model, server := newVideoHTTPModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "Ready",
			"cost":   1.25,
			"result": map[string]interface{}{"sample": "https://cdn.example.com/video.mp4"},
		})
	})
	defer server.Close()

	op, _ := json.Marshal(bflVideoOperation{RequestID: "req-1", PollingURL: server.URL + "/get_result", Cost: floatPtr(0.5)})
	result, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil {
		t.Fatalf("DoStatus() error: %v", err)
	}
	meta := result.ProviderMetadata["blackForestLabs"].(map[string]interface{})
	videos := meta["videos"].([]map[string]interface{})
	if videos[0]["cost"] != 1.25 {
		t.Errorf("cost = %v, want settled 1.25", videos[0]["cost"])
	}
}

func floatPtr(f float64) *float64 { return &f }

// TestDoStatus_TerminalFailure mirrors "should return an error from doStatus for terminal failures".
func TestDoStatus_TerminalFailure(t *testing.T) {
	model, server := newVideoHTTPModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "Content Moderated"})
	})
	defer server.Close()

	op, _ := json.Marshal(bflVideoOperation{RequestID: "req-1", PollingURL: server.URL + "/get_result"})
	result, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil {
		t.Fatalf("DoStatus() error: %v", err)
	}
	if result.Status != provider.VideoOperationStatusError {
		t.Fatalf("status = %v", result.Status)
	}
	if !strings.Contains(result.Error, "Content Moderated") {
		t.Errorf("error = %q", result.Error)
	}
}

// TestDoStatus_AppendsMissingID mirrors "should append the request id to a polling URL that lacks one".
func TestDoStatus_AppendsMissingID(t *testing.T) {
	var capturedQuery string
	model, server := newVideoHTTPModel(t, func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "Pending"})
	})
	defer server.Close()

	op, _ := json.Marshal(bflVideoOperation{RequestID: "req-42", PollingURL: server.URL + "/get_result"})
	_, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil {
		t.Fatalf("DoStatus() error: %v", err)
	}
	if capturedQuery != "id=req-42" {
		t.Errorf("query = %q, want id=req-42", capturedQuery)
	}
}

// TestDoGenerate_ComposesStartAndStatus verifies DoGenerate is built entirely
// on top of DoStart+DoStatus, polling until Ready.
func TestDoGenerate_ComposesStartAndStatus(t *testing.T) {
	pollCount := 0
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/" + VideoModelFlux3Video:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "req-1", "polling_url": serverURL + "/get_result"})
		case "/get_result":
			pollCount++
			if pollCount < 2 {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "Pending"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "Ready",
				"result": map[string]interface{}{"sample": "https://cdn.example.com/video.mp4"},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	serverURL = server.URL

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, VideoModelFlux3Video)
	model.provider.config.PollIntervalMillis = 5

	result, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{Prompt: "x"})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	if len(result.Videos) != 1 || result.Videos[0].URL != "https://cdn.example.com/video.mp4" {
		t.Fatalf("videos = %+v", result.Videos)
	}
	if pollCount < 2 {
		t.Errorf("expected at least 2 status polls, got %d", pollCount)
	}
}

// TestDoStatus_RejectsPrivateIPRedirect verifies that a poll URL redirecting
// to a private/link-local address is rejected instead of followed (matching
// the SSRF hardening already applied to bfl image polling).
func TestDoStatus_RejectsPrivateIPRedirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/get_result", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, VideoModelFlux3Video)

	op, _ := json.Marshal(bflVideoOperation{RequestID: "req-1", PollingURL: server.URL + "/get_result"})
	_, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err == nil {
		t.Fatal("expected an error rejecting the private-IP redirect target")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("expected an SSRF validation error, got: %v", err)
	}
}
