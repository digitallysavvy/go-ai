package minimax

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

const (
	minimaxTestPrompt = "A white kitten chases a butterfly across a sunlit garden."
	minimaxTestTaskID = "task-123"
)

// minimaxProviderOptions builds providerOptions.minimax with fast polling
// (matching the TS test file's minimaxOptions helper), merging in extra.
func minimaxProviderOptions(extra map[string]interface{}) map[string]interface{} {
	opts := map[string]interface{}{
		"pollIntervalMs": 10,
		"pollTimeoutMs":  5000,
	}
	for k, v := range extra {
		opts[k] = v
	}
	return map[string]interface{}{"minimax": opts}
}

func providerForMinimaxServer(serverURL string) *Provider {
	return New(Config{APIKey: "test-key", VideoBaseURL: serverURL})
}

// newMinimaxTestServer creates a mock server for the two MiniMax video
// endpoints. statusResponses is cycled on each poll call (the last entry
// repeats once exhausted), mirroring the TS mock server's per-call cycling.
func newMinimaxTestServer(t *testing.T, taskID string, statusResponses []string) (server *httptest.Server, createBodies *[]map[string]interface{}, pollCount *int) {
	t.Helper()
	bodies := []map[string]interface{}{}
	count := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/video_generation", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"task_id": %q}`, taskID)
	})
	mux.HandleFunc("/v2/query/video_generation/"+taskID, func(w http.ResponseWriter, r *http.Request) {
		idx := count
		if idx >= len(statusResponses) {
			idx = len(statusResponses) - 1
		}
		count++
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, statusResponses[idx])
	})
	s := httptest.NewServer(mux)
	return s, &bodies, &count
}

func minimaxSucceededResponse() string {
	return `{"task":{"id":"task-123","model":"MiniMax-H3","status":"succeeded","content":{"url":"https://cdn.minimax.io/output/video-001.mp4"},"resolution":"2K","duration":5,"ratio":"16:9","usage":{"total_seconds":5,"input_seconds":0,"output_seconds":5}}}`
}

func minimaxDefaultCallOptions(providerOpts map[string]interface{}) *provider.VideoModelV3CallOptions {
	if providerOpts == nil {
		providerOpts = minimaxProviderOptions(nil)
	}
	return &provider.VideoModelV3CallOptions{
		Prompt:          minimaxTestPrompt,
		PromptSet:       true,
		N:               1,
		ProviderOptions: providerOpts,
	}
}

// TestVideoModel_ConstructorMetadata mirrors "constructor > should expose
// correct provider and model information".
func TestVideoModel_ConstructorMetadata(t *testing.T) {
	prov := providerForMinimaxServer("https://api.example.com")
	model := newVideoModel(prov, ModelH3)

	if model.Provider() != "minimax.video" {
		t.Errorf("Provider() = %q, want %q", model.Provider(), "minimax.video")
	}
	if model.ModelID() != ModelH3 {
		t.Errorf("ModelID() = %q, want %q", model.ModelID(), ModelH3)
	}
	if model.SpecificationVersion() != "v4" {
		t.Errorf("SpecificationVersion() = %q, want v4", model.SpecificationVersion())
	}
	if got := model.MaxVideosPerCall(); got == nil || *got != 1 {
		t.Errorf("MaxVideosPerCall() = %v, want 1", got)
	}
}

// TestDoStart_CallbackURL mirrors "doStart / doStatus > should submit once
// with callback_url without polling" / "should omit callback_url when
// webhookUrl is not provided".
func TestDoStart_CallbackURL(t *testing.T) {
	server, bodies, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{minimaxSucceededResponse()})
	defer server.Close()
	prov := providerForMinimaxServer(server.URL)
	model := newVideoModel(prov, ModelH3)

	result, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: *minimaxDefaultCallOptions(map[string]interface{}{}),
		WebhookURL:              "https://example.com/callback",
	})
	if err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	if len(*bodies) != 1 {
		t.Fatalf("expected 1 create call, got %d", len(*bodies))
	}
	body := (*bodies)[0]
	if body["callback_url"] != "https://example.com/callback" {
		t.Errorf("callback_url = %v", body["callback_url"])
	}
	if body["model"] != ModelH3 {
		t.Errorf("model = %v", body["model"])
	}
	if body["resolution"] != "2K" || body["ratio"] != "16:9" {
		t.Errorf("resolution/ratio = %v/%v", body["resolution"], body["ratio"])
	}
	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings, got %+v", result.Warnings)
	}

	var op minimaxOperation
	if err := json.Unmarshal(result.Operation, &op); err != nil || op.TaskID != minimaxTestTaskID {
		t.Errorf("operation = %s", result.Operation)
	}
}

func TestDoStart_OmitsCallbackURL(t *testing.T) {
	server, bodies, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{minimaxSucceededResponse()})
	defer server.Close()
	prov := providerForMinimaxServer(server.URL)
	model := newVideoModel(prov, ModelH3)

	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: *minimaxDefaultCallOptions(nil),
	})
	if err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	if _, ok := (*bodies)[0]["callback_url"]; ok {
		t.Error("expected callback_url to be omitted")
	}
}

// TestDoGenerate_DefaultPayload mirrors "doGenerate > should send a
// text-to-video request with required fields" / "should send the documented
// default payload for MiniMax-H3-Max" / "should return the video URL from
// the completed task".
func TestDoGenerate_DefaultPayload(t *testing.T) {
	for _, tt := range []struct {
		name           string
		modelID        string
		wantResolution string
	}{
		{"MiniMax-H3", ModelH3, "2K"},
		{"MiniMax-H3-Max", ModelH3Max, "768P"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server, bodies, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{minimaxSucceededResponse()})
			defer server.Close()
			prov := providerForMinimaxServer(server.URL)
			model := newVideoModel(prov, tt.modelID)

			result, err := model.DoGenerate(context.Background(), minimaxDefaultCallOptions(nil))
			if err != nil {
				t.Fatalf("DoGenerate() error: %v", err)
			}
			body := (*bodies)[0]
			if body["resolution"] != tt.wantResolution {
				t.Errorf("resolution = %v, want %v", body["resolution"], tt.wantResolution)
			}
			if body["duration"] != float64(5) {
				t.Errorf("duration = %v, want 5", body["duration"])
			}
			if body["ratio"] != "16:9" {
				t.Errorf("ratio = %v, want 16:9", body["ratio"])
			}
			if len(result.Videos) != 1 || result.Videos[0].URL != "https://cdn.minimax.io/output/video-001.mp4" {
				t.Errorf("videos = %+v", result.Videos)
			}
			if result.Videos[0].MediaType != "video/mp4" {
				t.Errorf("mediaType = %q", result.Videos[0].MediaType)
			}
		})
	}
}

// TestDoGenerate_AspectRatioAndDuration mirrors "should map aspectRatio and
// duration into the request".
func TestDoGenerate_AspectRatioAndDuration(t *testing.T) {
	server, bodies, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{minimaxSucceededResponse()})
	defer server.Close()
	prov := providerForMinimaxServer(server.URL)
	model := newVideoModel(prov, ModelH3)

	dur := 9.0
	opts := minimaxDefaultCallOptions(nil)
	opts.AspectRatio = "9:16"
	opts.Duration = &dur

	if _, err := model.DoGenerate(context.Background(), opts); err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	body := (*bodies)[0]
	if body["ratio"] != "9:16" {
		t.Errorf("ratio = %v", body["ratio"])
	}
	if body["duration"] != float64(9) {
		t.Errorf("duration = %v", body["duration"])
	}
}

// TestDoGenerate_DurationClamping mirrors "should clamp a duration above the
// maximum and warn" / "below the minimum" / "should round a fractional
// duration and warn" / "round and then clamp a fractional out-of-range
// duration".
func TestDoGenerate_DurationClamping(t *testing.T) {
	tests := []struct {
		name     string
		modelID  string
		duration float64
		want     float64
		feature  string
	}{
		{"above max", ModelH3, 20, 15, "duration"},
		{"below min (H3-Max, min 5)", ModelH3Max, 3, 5, "duration"},
		{"fractional rounds", ModelH3, 5.6, 6, "duration"},
		{"fractional rounds then clamps", ModelH3, 17.6, 15, "duration"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, bodies, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{minimaxSucceededResponse()})
			defer server.Close()
			prov := providerForMinimaxServer(server.URL)
			model := newVideoModel(prov, tt.modelID)

			opts := minimaxDefaultCallOptions(nil)
			opts.Duration = &tt.duration
			result, err := model.DoGenerate(context.Background(), opts)
			if err != nil {
				t.Fatalf("DoGenerate() error: %v", err)
			}
			body := (*bodies)[0]
			if body["duration"] != tt.want {
				t.Errorf("duration = %v, want %v", body["duration"], tt.want)
			}
			found := false
			for _, w := range result.Warnings {
				if w.Feature == tt.feature {
					found = true
				}
			}
			if !found {
				t.Errorf("expected a %q warning, got %+v", tt.feature, result.Warnings)
			}
		})
	}
}

// TestDoGenerate_ResolutionMapping mirrors "should map a 768P frame size on
// MiniMax-H3-Max" / "should warn and use the default for an unrecognized
// resolution" / "should prefer providerOptions.resolution over the
// top-level value".
func TestDoGenerate_ResolutionMapping(t *testing.T) {
	t.Run("maps a pixel size to a supported tier", func(t *testing.T) {
		server, bodies, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{minimaxSucceededResponse()})
		defer server.Close()
		prov := providerForMinimaxServer(server.URL)
		model := newVideoModel(prov, ModelH3Max)

		opts := minimaxDefaultCallOptions(nil)
		opts.Resolution = "1366x768"
		result, err := model.DoGenerate(context.Background(), opts)
		if err != nil {
			t.Fatalf("DoGenerate() error: %v", err)
		}
		if (*bodies)[0]["resolution"] != "768P" {
			t.Errorf("resolution = %v", (*bodies)[0]["resolution"])
		}
		if len(result.Warnings) != 0 {
			t.Errorf("expected no warnings, got %+v", result.Warnings)
		}
	})

	t.Run("warns and falls back to the default for an unrecognized resolution", func(t *testing.T) {
		server, bodies, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{minimaxSucceededResponse()})
		defer server.Close()
		prov := providerForMinimaxServer(server.URL)
		model := newVideoModel(prov, ModelH3)

		opts := minimaxDefaultCallOptions(nil)
		opts.Resolution = "999x999"
		result, err := model.DoGenerate(context.Background(), opts)
		if err != nil {
			t.Fatalf("DoGenerate() error: %v", err)
		}
		if (*bodies)[0]["resolution"] != "2K" {
			t.Errorf("resolution = %v", (*bodies)[0]["resolution"])
		}
		found := false
		for _, w := range result.Warnings {
			if w.Feature == "resolution" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected a resolution warning, got %+v", result.Warnings)
		}
	})

	t.Run("prefers providerOptions.resolution over the top-level value", func(t *testing.T) {
		server, bodies, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{minimaxSucceededResponse()})
		defer server.Close()
		prov := providerForMinimaxServer(server.URL)
		model := newVideoModel(prov, ModelH3)

		opts := minimaxDefaultCallOptions(minimaxProviderOptions(map[string]interface{}{"resolution": "768P"}))
		opts.Resolution = "2048x2048" // maps to 2K, should be overridden
		if _, err := model.DoGenerate(context.Background(), opts); err != nil {
			t.Fatalf("DoGenerate() error: %v", err)
		}
		if (*bodies)[0]["resolution"] != "768P" {
			t.Errorf("resolution = %v, want 768P", (*bodies)[0]["resolution"])
		}
	})
}

// TestDoGenerate_FrameImages mirrors "should send first/last frame images
// with roles and ignore ratio".
func TestDoGenerate_FrameImages(t *testing.T) {
	server, bodies, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{minimaxSucceededResponse()})
	defer server.Close()
	prov := providerForMinimaxServer(server.URL)
	model := newVideoModel(prov, ModelH3)

	opts := minimaxDefaultCallOptions(nil)
	opts.AspectRatio = "9:16"
	opts.FrameImages = []provider.VideoFrameImage{
		{FrameType: provider.VideoFrameTypeFirstFrame, Image: provider.VideoModelV3File{Type: "url", URL: "https://cdn.example.com/first.png", MediaType: "image/png"}},
		{FrameType: provider.VideoFrameTypeLastFrame, Image: provider.VideoModelV3File{Type: "url", URL: "https://cdn.example.com/last.png", MediaType: "image/png"}},
	}

	result, err := model.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	body := (*bodies)[0]
	if _, ok := body["ratio"]; ok {
		t.Errorf("expected ratio to be omitted (derived from frame image), got %v", body["ratio"])
	}
	content, ok := body["content"].([]interface{})
	if !ok || len(content) != 3 {
		t.Fatalf("content = %+v", body["content"])
	}
	firstPart := content[1].(map[string]interface{})
	if firstPart["role"] != "first_frame" {
		t.Errorf("content[1].role = %v", firstPart["role"])
	}
	lastPart := content[2].(map[string]interface{})
	if lastPart["role"] != "last_frame" {
		t.Errorf("content[2].role = %v", lastPart["role"])
	}
	foundRatioWarning := false
	for _, w := range result.Warnings {
		if w.Feature == "aspectRatio" {
			foundRatioWarning = true
		}
	}
	if !foundRatioWarning {
		t.Errorf("expected an aspectRatio warning, got %+v", result.Warnings)
	}
}

// TestDoGenerate_ReferenceRouting mirrors "should route inputReferences into
// reference_image/reference_video" / "should treat a URL reference without a
// mediaType as an image and warn" / "should ignore a reference that is
// neither an image nor a video and warn".
func TestDoGenerate_ReferenceRouting(t *testing.T) {
	t.Run("routes image and video references by media type", func(t *testing.T) {
		server, bodies, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{minimaxSucceededResponse()})
		defer server.Close()
		prov := providerForMinimaxServer(server.URL)
		model := newVideoModel(prov, ModelH3)

		opts := minimaxDefaultCallOptions(nil)
		opts.InputReferences = []provider.VideoModelV3File{
			{Type: "url", URL: "https://cdn.example.com/ref-image.png", MediaType: "image/png"},
			{Type: "url", URL: "https://cdn.example.com/ref-video.mp4", MediaType: "video/mp4"},
		}
		if _, err := model.DoGenerate(context.Background(), opts); err != nil {
			t.Fatalf("DoGenerate() error: %v", err)
		}
		content := (*bodies)[0]["content"].([]interface{})
		if len(content) != 3 {
			t.Fatalf("content = %+v", content)
		}
		imgPart := content[1].(map[string]interface{})
		if imgPart["type"] != "image_url" || imgPart["role"] != "reference_image" {
			t.Errorf("content[1] = %+v", imgPart)
		}
		vidPart := content[2].(map[string]interface{})
		if vidPart["type"] != "video_url" || vidPart["role"] != "reference_video" {
			t.Errorf("content[2] = %+v", vidPart)
		}
	})

	t.Run("treats a URL reference without a mediaType as an image and warns", func(t *testing.T) {
		server, _, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{minimaxSucceededResponse()})
		defer server.Close()
		prov := providerForMinimaxServer(server.URL)
		model := newVideoModel(prov, ModelH3)

		opts := minimaxDefaultCallOptions(nil)
		opts.InputReferences = []provider.VideoModelV3File{
			{Type: "url", URL: "https://cdn.example.com/ref.bin"},
		}
		result, err := model.DoGenerate(context.Background(), opts)
		if err != nil {
			t.Fatalf("DoGenerate() error: %v", err)
		}
		found := false
		for _, w := range result.Warnings {
			if w.Feature == "inputReferences" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected an inputReferences warning, got %+v", result.Warnings)
		}
	})

	t.Run("ignores a reference that is neither image nor video and warns", func(t *testing.T) {
		server, bodies, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{minimaxSucceededResponse()})
		defer server.Close()
		prov := providerForMinimaxServer(server.URL)
		model := newVideoModel(prov, ModelH3)

		opts := minimaxDefaultCallOptions(nil)
		opts.InputReferences = []provider.VideoModelV3File{
			{Type: "url", URL: "https://cdn.example.com/ref.pdf", MediaType: "application/pdf"},
		}
		result, err := model.DoGenerate(context.Background(), opts)
		if err != nil {
			t.Fatalf("DoGenerate() error: %v", err)
		}
		content := (*bodies)[0]["content"].([]interface{})
		if len(content) != 1 {
			t.Errorf("expected the reference to be dropped, content = %+v", content)
		}
		found := false
		for _, w := range result.Warnings {
			if w.Feature == "inputReferences" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected an inputReferences warning, got %+v", result.Warnings)
		}
	})
}

// TestDoGenerate_UnsupportedOptionWarnings mirrors "should warn about
// unsupported fps, seed, and n".
func TestDoGenerate_UnsupportedOptionWarnings(t *testing.T) {
	server, _, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{minimaxSucceededResponse()})
	defer server.Close()
	prov := providerForMinimaxServer(server.URL)
	model := newVideoModel(prov, ModelH3)

	fps := 30
	seed := 7
	opts := minimaxDefaultCallOptions(nil)
	opts.FPS = &fps
	opts.Seed = &seed
	opts.N = 3
	generateAudio := true
	opts.GenerateAudio = &generateAudio

	result, err := model.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	wantFeatures := map[string]bool{"fps": false, "seed": false, "n": false, "generateAudio": false}
	for _, w := range result.Warnings {
		if _, ok := wantFeatures[w.Feature]; ok {
			wantFeatures[w.Feature] = true
		}
	}
	for feature, found := range wantFeatures {
		if !found {
			t.Errorf("expected a %q warning, got %+v", feature, result.Warnings)
		}
	}
}

// TestDoGenerate_AigcWatermark mirrors "should map aigcWatermark to
// aigc_watermark" / "should omit aigc_watermark when unset".
func TestDoGenerate_AigcWatermark(t *testing.T) {
	t.Run("explicitly enabled", func(t *testing.T) {
		server, bodies, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{minimaxSucceededResponse()})
		defer server.Close()
		prov := providerForMinimaxServer(server.URL)
		model := newVideoModel(prov, ModelH3)

		opts := minimaxDefaultCallOptions(minimaxProviderOptions(map[string]interface{}{"aigcWatermark": true}))
		if _, err := model.DoGenerate(context.Background(), opts); err != nil {
			t.Fatalf("DoGenerate() error: %v", err)
		}
		if (*bodies)[0]["aigc_watermark"] != true {
			t.Errorf("aigc_watermark = %v", (*bodies)[0]["aigc_watermark"])
		}
	})

	t.Run("unset omits the field", func(t *testing.T) {
		server, bodies, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{minimaxSucceededResponse()})
		defer server.Close()
		prov := providerForMinimaxServer(server.URL)
		model := newVideoModel(prov, ModelH3)

		if _, err := model.DoGenerate(context.Background(), minimaxDefaultCallOptions(nil)); err != nil {
			t.Fatalf("DoGenerate() error: %v", err)
		}
		if _, ok := (*bodies)[0]["aigc_watermark"]; ok {
			t.Errorf("expected aigc_watermark to be omitted")
		}
	})
}

// TestDoGenerate_RatioPreferenceAndAdaptive mirrors "should prefer
// providerOptions.minimax.ratio over the top-level aspectRatio" / "should
// support the adaptive ratio on reference-to-video" / "should warn and fall
// back to the default ratio when adaptive is requested for text-to-video".
func TestDoGenerate_RatioPreferenceAndAdaptive(t *testing.T) {
	t.Run("providerOptions.ratio overrides aspectRatio", func(t *testing.T) {
		server, bodies, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{minimaxSucceededResponse()})
		defer server.Close()
		prov := providerForMinimaxServer(server.URL)
		model := newVideoModel(prov, ModelH3)

		opts := minimaxDefaultCallOptions(minimaxProviderOptions(map[string]interface{}{"ratio": "4:3"}))
		opts.AspectRatio = "9:16"
		if _, err := model.DoGenerate(context.Background(), opts); err != nil {
			t.Fatalf("DoGenerate() error: %v", err)
		}
		if (*bodies)[0]["ratio"] != "4:3" {
			t.Errorf("ratio = %v, want 4:3", (*bodies)[0]["ratio"])
		}
	})

	t.Run("adaptive ratio is accepted for reference-to-video", func(t *testing.T) {
		server, bodies, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{minimaxSucceededResponse()})
		defer server.Close()
		prov := providerForMinimaxServer(server.URL)
		model := newVideoModel(prov, ModelH3)

		opts := minimaxDefaultCallOptions(nil)
		opts.InputReferences = []provider.VideoModelV3File{
			{Type: "url", URL: "https://cdn.example.com/ref.png", MediaType: "image/png"},
		}
		opts.AspectRatio = "adaptive"
		if _, err := model.DoGenerate(context.Background(), opts); err != nil {
			t.Fatalf("DoGenerate() error: %v", err)
		}
		if (*bodies)[0]["ratio"] != "adaptive" {
			t.Errorf("ratio = %v, want adaptive", (*bodies)[0]["ratio"])
		}
	})

	t.Run("adaptive falls back to default for text-to-video and warns", func(t *testing.T) {
		server, bodies, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{minimaxSucceededResponse()})
		defer server.Close()
		prov := providerForMinimaxServer(server.URL)
		model := newVideoModel(prov, ModelH3)

		opts := minimaxDefaultCallOptions(nil)
		opts.AspectRatio = "adaptive"
		result, err := model.DoGenerate(context.Background(), opts)
		if err != nil {
			t.Fatalf("DoGenerate() error: %v", err)
		}
		if (*bodies)[0]["ratio"] != "16:9" {
			t.Errorf("ratio = %v, want 16:9", (*bodies)[0]["ratio"])
		}
		found := false
		for _, w := range result.Warnings {
			if w.Feature == "aspectRatio" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected an aspectRatio warning, got %+v", result.Warnings)
		}
	})
}

// TestDoGenerate_Polling mirrors "polling > should keep polling through
// queued and running until succeeded" / "should throw when polling exceeds
// pollTimeoutMs".
func TestDoGenerate_Polling(t *testing.T) {
	t.Run("keeps polling until succeeded", func(t *testing.T) {
		server, _, pollCount := newMinimaxTestServer(t, minimaxTestTaskID, []string{
			`{"task":{"id":"task-123","status":"queued"}}`,
			`{"task":{"id":"task-123","status":"running"}}`,
			minimaxSucceededResponse(),
		})
		defer server.Close()
		prov := providerForMinimaxServer(server.URL)
		model := newVideoModel(prov, ModelH3)

		result, err := model.DoGenerate(context.Background(), minimaxDefaultCallOptions(nil))
		if err != nil {
			t.Fatalf("DoGenerate() error: %v", err)
		}
		if *pollCount < 3 {
			t.Errorf("expected at least 3 poll calls, got %d", *pollCount)
		}
		if len(result.Videos) != 1 {
			t.Errorf("videos = %+v", result.Videos)
		}
	})

	t.Run("times out when polling exceeds pollTimeoutMs", func(t *testing.T) {
		server, _, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{
			`{"task":{"id":"task-123","status":"queued"}}`,
		})
		defer server.Close()
		prov := providerForMinimaxServer(server.URL)
		model := newVideoModel(prov, ModelH3)

		opts := minimaxDefaultCallOptions(minimaxProviderOptions(map[string]interface{}{
			"pollIntervalMs": 5,
			"pollTimeoutMs":  30,
		}))
		_, err := model.DoGenerate(context.Background(), opts)
		if err == nil {
			t.Fatal("expected a timeout error")
		}
		mmErr, ok := err.(*Error)
		if !ok || mmErr.Name != "MINIMAX_VIDEO_GENERATION_TIMEOUT" {
			t.Errorf("unexpected error: %v (%T)", err, err)
		}
	})
}

// TestDoGenerate_ErrorHandling mirrors "error handling > should throw when
// the task failed" / "cancelled" / "expired" / "no video URL" / "no
// task_id".
func TestDoGenerate_ErrorHandling(t *testing.T) {
	t.Run("failed task includes message and code", func(t *testing.T) {
		server, _, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{
			`{"task":{"id":"task-123","status":"failed","error":{"message":"content violation","code":"1001"}}}`,
		})
		defer server.Close()
		prov := providerForMinimaxServer(server.URL)
		model := newVideoModel(prov, ModelH3)

		_, err := model.DoGenerate(context.Background(), minimaxDefaultCallOptions(nil))
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "content violation") || !strings.Contains(err.Error(), "1001") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("cancelled task", func(t *testing.T) {
		server, _, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{
			`{"task":{"id":"task-123","status":"cancelled"}}`,
		})
		defer server.Close()
		prov := providerForMinimaxServer(server.URL)
		model := newVideoModel(prov, ModelH3)

		_, err := model.DoGenerate(context.Background(), minimaxDefaultCallOptions(nil))
		if err == nil || !strings.Contains(err.Error(), "cancelled") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("expired task", func(t *testing.T) {
		server, _, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{
			`{"task":{"id":"task-123","status":"expired"}}`,
		})
		defer server.Close()
		prov := providerForMinimaxServer(server.URL)
		model := newVideoModel(prov, ModelH3)

		_, err := model.DoGenerate(context.Background(), minimaxDefaultCallOptions(nil))
		if err == nil || !strings.Contains(err.Error(), "expired") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("succeeded task with no video URL", func(t *testing.T) {
		server, _, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{
			`{"task":{"id":"task-123","status":"succeeded"}}`,
		})
		defer server.Close()
		prov := providerForMinimaxServer(server.URL)
		model := newVideoModel(prov, ModelH3)

		_, err := model.DoGenerate(context.Background(), minimaxDefaultCallOptions(nil))
		if err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("create call returns no task_id", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/v2/video_generation", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		})
		server := httptest.NewServer(mux)
		defer server.Close()
		prov := providerForMinimaxServer(server.URL)
		model := newVideoModel(prov, ModelH3)

		_, err := model.DoGenerate(context.Background(), minimaxDefaultCallOptions(nil))
		if err == nil {
			t.Fatal("expected an error")
		}
	})
}

// TestDoGenerate_ErrorEnvelope mirrors "should surface the MiniMax error
// message from a failed create call" / "should fall back to a generic
// message when the error envelope has no message".
func TestDoGenerate_ErrorEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/video_generation", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"prompt violates content policy","http_code":400},"request_id":"req-err-001"}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	prov := providerForMinimaxServer(server.URL)
	model := newVideoModel(prov, ModelH3)

	_, err := model.DoGenerate(context.Background(), minimaxDefaultCallOptions(nil))
	if err == nil || !strings.Contains(err.Error(), "prompt violates content policy") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestDoStatus_RejectsPrivateIPRedirect mirrors "polling redirects > should
// validate redirects after trusting the configured origin" (E2's validated
// polling redirect pattern applied to MiniMax, per a580ec8).
func TestDoStatus_RejectsPrivateIPRedirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/query/video_generation/"+minimaxTestTaskID, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	prov := providerForMinimaxServer(server.URL)
	model := newVideoModel(prov, ModelH3)

	op, _ := json.Marshal(minimaxOperation{TaskID: minimaxTestTaskID})
	_, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err == nil {
		t.Fatal("expected the redirect to a private IP to be rejected")
	}
}

// TestDoGenerate_RequestHeaders mirrors "request headers > should send the
// configured auth header on the create and poll calls" / "should merge
// per-request headers".
func TestDoGenerate_RequestHeaders(t *testing.T) {
	var createAuth, pollAuth, pollCustom string
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/video_generation", func(w http.ResponseWriter, r *http.Request) {
		createAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"task_id": %q}`, minimaxTestTaskID)
	})
	mux.HandleFunc("/v2/query/video_generation/"+minimaxTestTaskID, func(w http.ResponseWriter, r *http.Request) {
		pollAuth = r.Header.Get("Authorization")
		pollCustom = r.Header.Get("X-Custom")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, minimaxSucceededResponse())
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	prov := New(Config{APIKey: "test-key", VideoBaseURL: server.URL, Headers: map[string]string{"X-Custom": "provider-header"}})
	model := newVideoModel(prov, ModelH3)

	if _, err := model.DoGenerate(context.Background(), minimaxDefaultCallOptions(nil)); err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	if createAuth != "Bearer test-key" {
		t.Errorf("create Authorization = %q", createAuth)
	}
	if pollAuth != "Bearer test-key" {
		t.Errorf("poll Authorization = %q", pollAuth)
	}
	if pollCustom != "provider-header" {
		t.Errorf("poll X-Custom = %q", pollCustom)
	}
}

// TestDoGenerate_ResolvedInputsReferenceVideoUrls verifies that DoGenerate
// (unlike a plain DoStatus call) resolves reference-video indices back to
// their original URLs in providerMetadata.minimax.resolvedInputs (TS
// doGenerate's post-completion resolvedInputs.referenceVideoUrls
// derivation).
func TestDoGenerate_ResolvedInputsReferenceVideoUrls(t *testing.T) {
	server, _, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{minimaxSucceededResponse()})
	defer server.Close()
	prov := providerForMinimaxServer(server.URL)
	model := newVideoModel(prov, ModelH3)

	opts := minimaxDefaultCallOptions(nil)
	opts.InputReferences = []provider.VideoModelV3File{
		{Type: "url", URL: "https://cdn.example.com/ref-video.mp4", MediaType: "video/mp4"},
	}

	result, err := model.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	meta, ok := result.ProviderMetadata["minimax"].(map[string]interface{})
	if !ok {
		t.Fatalf("providerMetadata.minimax missing: %+v", result.ProviderMetadata)
	}
	resolved, ok := meta["resolvedInputs"].(map[string]interface{})
	if !ok {
		t.Fatalf("resolvedInputs missing: %+v", meta)
	}
	urls, ok := resolved["referenceVideoUrls"].([]string)
	if !ok || len(urls) != 1 || urls[0] != "https://cdn.example.com/ref-video.mp4" {
		t.Errorf("referenceVideoUrls = %v", resolved["referenceVideoUrls"])
	}
	if _, ok := resolved["referenceVideoIndices"]; ok {
		t.Errorf("expected referenceVideoIndices to be replaced by referenceVideoUrls, got %+v", resolved)
	}
}

// TestDoStatus_ReportsResolvedInputsIndices verifies that a plain DoStatus
// call (not routed through DoGenerate's enrichment) reports the raw
// referenceVideoIndices, matching TS's public doStatus behavior.
func TestDoStatus_ReportsResolvedInputsIndices(t *testing.T) {
	server, _, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{minimaxSucceededResponse()})
	defer server.Close()
	prov := providerForMinimaxServer(server.URL)
	model := newVideoModel(prov, ModelH3)

	op, _ := json.Marshal(minimaxOperation{
		TaskID:         minimaxTestTaskID,
		ResolvedInputs: minimaxResolvedInputs{ImageCount: 1, ReferenceVideoIndices: []int{2}},
	})
	result, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil {
		t.Fatalf("DoStatus() error: %v", err)
	}
	meta := result.ProviderMetadata["minimax"].(map[string]interface{})
	resolved := meta["resolvedInputs"].(map[string]interface{})
	indices, ok := resolved["referenceVideoIndices"].([]int)
	if !ok || len(indices) != 1 || indices[0] != 2 {
		t.Errorf("referenceVideoIndices = %v", resolved["referenceVideoIndices"])
	}
}

// TestDoGenerate_ResponseMetadata mirrors "response > should include the
// timestamp, modelId, and poll response headers" / "should return the full
// provider metadata from the completed task".
func TestDoGenerate_ResponseMetadata(t *testing.T) {
	server, _, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{minimaxSucceededResponse()})
	defer server.Close()
	prov := providerForMinimaxServer(server.URL)
	model := newVideoModel(prov, ModelH3)

	result, err := model.DoGenerate(context.Background(), minimaxDefaultCallOptions(nil))
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	if result.Response.ModelID != ModelH3 {
		t.Errorf("ModelID = %q", result.Response.ModelID)
	}
	if result.Response.Timestamp.IsZero() {
		t.Error("expected a non-zero Timestamp")
	}
	meta, ok := result.ProviderMetadata["minimax"].(map[string]interface{})
	if !ok {
		t.Fatalf("providerMetadata.minimax missing")
	}
	if meta["taskId"] != minimaxTestTaskID {
		t.Errorf("taskId = %v", meta["taskId"])
	}
	if meta["ratio"] != "16:9" || meta["resolution"] != "2K" {
		t.Errorf("ratio/resolution = %v/%v", meta["ratio"], meta["resolution"])
	}
}

// TestVideoModel_ProviderIntegration verifies that Provider.VideoModel wires
// up the model correctly.
func TestVideoModel_ProviderIntegration(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	m, err := prov.VideoModel(ModelH3)
	if err != nil {
		t.Fatalf("VideoModel() error: %v", err)
	}
	if m.Provider() != "minimax.video" {
		t.Errorf("Provider() = %q", m.Provider())
	}

	if _, err := prov.VideoModel(""); err == nil {
		t.Error("expected an error for an empty model ID")
	}
}
