package heygen

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Ported from packages/heygen/src/heygen-video-model.test.ts.

const (
	createPath = "/v3/models/videos"
	statusPath = "/v3/models/videos/video-1"
)

var testOperation = json.RawMessage(`{"videoId":"video-1","mode":"text_to_video","resolution":"768p"}`)

type recordedCall struct {
	method  string
	path    string
	headers http.Header
	body    map[string]interface{}
}

type testServer struct {
	*httptest.Server
	mu        sync.Mutex
	calls     []recordedCall
	responses map[string]func(w http.ResponseWriter)
}

func jsonResponse(status int, body string) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	ts := &testServer{responses: map[string]func(w http.ResponseWriter){
		createPath: jsonResponse(200, `{"data":{"status":"pending","video_id":"video-1"}}`),
		statusPath: jsonResponse(200, completedBody(`{}`)),
	}}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]interface{}
		_ = json.Unmarshal(raw, &body)
		ts.mu.Lock()
		ts.calls = append(ts.calls, recordedCall{method: r.Method, path: r.URL.Path, headers: r.Header.Clone(), body: body})
		respond := ts.responses[r.URL.Path]
		ts.mu.Unlock()
		if respond == nil {
			http.NotFound(w, r)
			return
		}
		respond(w)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func (ts *testServer) set(path string, fn func(w http.ResponseWriter)) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.responses[path] = fn
}

func (ts *testServer) callCount() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return len(ts.calls)
}

func (ts *testServer) call(i int) recordedCall {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.calls[i]
}

// completedBody merges overrides into the default completed status payload.
func completedBody(overrides string) string {
	data := map[string]interface{}{
		"status": "completed", "video_url": "https://resource2.heygen.ai/video.mp4",
		"duration": 5, "width": 1344, "height": 768, "aspect_ratio": "16:9", "seed": 0,
	}
	var o map[string]interface{}
	_ = json.Unmarshal([]byte(overrides), &o)
	for k, v := range o {
		data[k] = v
	}
	b, _ := json.Marshal(map[string]interface{}{"data": data})
	return string(b)
}

func newModel(ts *testServer) *VideoModel {
	p := New(Config{APIKey: "test-key", BaseURL: ts.URL})
	return NewVideoModel(p, "heygen-video-1")
}

func opts(mutate func(o *provider.VideoModelV3CallOptions)) *provider.VideoModelV3StartOptions {
	o := provider.VideoModelV3CallOptions{Prompt: "A paper boat floats down a quiet stream.", N: 1}
	if mutate != nil {
		mutate(&o)
	}
	return &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: o}
}

func urlFile(url, mediaType string) provider.VideoModelV3File {
	return provider.VideoModelV3File{Type: "url", URL: url, MediaType: mediaType}
}

var (
	imageFile = urlFile("https://example.com/image.jpg", "image/jpeg")
	videoFile = urlFile("https://example.com/video.mp4", "video/mp4")
	audioFile = urlFile("https://example.com/audio.mp3", "audio/mpeg")
)

func heygenOpts(m map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{"heygen": m}
}

func TestDoStart_TextToVideoDefaults(t *testing.T) {
	ts := newTestServer(t)
	result, err := newModel(ts).DoStart(context.Background(), opts(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{
		"model": "heygen-video-1", "mode": "text_to_video", "prompt": "A paper boat floats down a quiet stream.",
		"duration": 5.0, "resolution": "768p", "aspect_ratio": "16:9",
	}
	c := ts.call(0)
	if !reflect.DeepEqual(c.body, want) {
		t.Fatalf("body = %#v", c.body)
	}
	if c.headers.Get("x-api-key") != "test-key" || c.method != http.MethodPost {
		t.Fatalf("headers = %#v", c.headers)
	}
	if string(result.Operation) != `{"videoId":"video-1","mode":"text_to_video","resolution":"768p"}` {
		t.Fatalf("operation = %s", result.Operation)
	}
	if len(result.Warnings) != 0 || result.Response.ModelID != "heygen-video-1" || result.Response.Timestamp.IsZero() {
		t.Fatalf("result = %#v", result)
	}
	meta := result.ProviderMetadata["heygen"].(map[string]interface{})
	if meta["videoId"] != "video-1" || meta["mode"] != "text_to_video" || meta["resolution"] != "768p" {
		t.Fatalf("metadata = %#v", meta)
	}
}

func TestDoStart_ForwardsOptionsAndIdempotencyHeader(t *testing.T) {
	ts := newTestServer(t)
	seed := 0
	d := 15.0
	_, err := newModel(ts).DoStart(context.Background(), opts(func(o *provider.VideoModelV3CallOptions) {
		o.Duration = &d
		o.Seed = &seed
		o.AspectRatio = "9:16"
		o.Headers = map[string]string{"Idempotency-Key": "generation-1"}
		o.ProviderOptions = heygenOpts(map[string]interface{}{"resolution": "2k", "promptEnhancement": "disabled"})
	}))
	if err != nil {
		t.Fatal(err)
	}
	c := ts.call(0)
	if c.body["duration"] != 15.0 || c.body["seed"] != 0.0 || c.body["aspect_ratio"] != "9:16" ||
		c.body["resolution"] != "2k" || c.body["prompt_enhancement"] != "disabled" {
		t.Fatalf("body = %#v", c.body)
	}
	if c.headers.Get("Idempotency-Key") != "generation-1" {
		t.Fatalf("headers = %#v", c.headers)
	}
}

func TestDoStart_ImageIsFirstFrameAndOmitsAspectRatio(t *testing.T) {
	ts := newTestServer(t)
	result, err := newModel(ts).DoStart(context.Background(), opts(func(o *provider.VideoModelV3CallOptions) {
		o.Image = &imageFile
		o.AspectRatio = "1:1"
	}))
	if err != nil {
		t.Fatal(err)
	}
	body := ts.call(0).body
	if body["mode"] != "image_to_video" || !reflect.DeepEqual(body["image"], map[string]interface{}{"type": "url", "url": imageFile.URL}) {
		t.Fatalf("body = %#v", body)
	}
	if _, ok := body["aspect_ratio"]; ok {
		t.Fatalf("aspect_ratio must be omitted: %#v", body)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "aspectRatio" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestDoStart_FirstFrameAtHighResolutionWithoutAspectRatio(t *testing.T) {
	ts := newTestServer(t)
	_, err := newModel(ts).DoStart(context.Background(), opts(func(o *provider.VideoModelV3CallOptions) {
		o.FrameImages = []provider.VideoFrameImage{{FrameType: provider.VideoFrameTypeFirstFrame, Image: imageFile}}
		o.ProviderOptions = heygenOpts(map[string]interface{}{"resolution": "1080p"})
	}))
	if err != nil {
		t.Fatal(err)
	}
	body := ts.call(0).body
	if body["mode"] != "image_to_video" || body["resolution"] != "1080p" {
		t.Fatalf("body = %#v", body)
	}
}

func TestDoStart_EncodesFileBytes(t *testing.T) {
	ts := newTestServer(t)
	_, err := newModel(ts).DoStart(context.Background(), opts(func(o *provider.VideoModelV3CallOptions) {
		o.InputReferences = []provider.VideoModelV3File{
			{Type: "file", MediaType: "image/png", Data: []byte{1, 2, 3}},
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	body := ts.call(0).body
	want := []interface{}{map[string]interface{}{"type": "base64", "media_type": "image/png", "data": "AQID"}}
	if body["mode"] != "reference_to_video" || body["aspect_ratio"] != "adaptive" || !reflect.DeepEqual(body["reference_images"], want) {
		t.Fatalf("body = %#v", body)
	}
}

func TestDoStart_GroupsMixedReferencesByMediaType(t *testing.T) {
	ts := newTestServer(t)
	_, err := newModel(ts).DoStart(context.Background(), opts(func(o *provider.VideoModelV3CallOptions) {
		o.InputReferences = []provider.VideoModelV3File{videoFile, imageFile, audioFile}
		o.ProviderOptions = heygenOpts(map[string]interface{}{
			"referenceImages": []interface{}{map[string]interface{}{"type": "asset_id", "assetId": "image-2"}},
			"referenceVideos": []interface{}{map[string]interface{}{"type": "asset_id", "assetId": "video-2"}},
			"referenceAudio":  []interface{}{map[string]interface{}{"type": "base64", "mediaType": "audio/mpeg", "data": "AQID"}},
		})
	}))
	if err != nil {
		t.Fatal(err)
	}
	body := ts.call(0).body
	cases := map[string][]interface{}{
		"reference_images": {map[string]interface{}{"type": "url", "url": imageFile.URL}, map[string]interface{}{"type": "asset_id", "asset_id": "image-2"}},
		"reference_videos": {map[string]interface{}{"type": "url", "url": videoFile.URL}, map[string]interface{}{"type": "asset_id", "asset_id": "video-2"}},
		"reference_audio":  {map[string]interface{}{"type": "url", "url": audioFile.URL}, map[string]interface{}{"type": "base64", "media_type": "audio/mpeg", "data": "AQID"}},
	}
	for k, want := range cases {
		if !reflect.DeepEqual(body[k], want) {
			t.Fatalf("%s = %#v, want %#v", k, body[k], want)
		}
	}
}

func TestDoStart_AssetAsFirstFrame(t *testing.T) {
	ts := newTestServer(t)
	_, err := newModel(ts).DoStart(context.Background(), opts(func(o *provider.VideoModelV3CallOptions) {
		o.ProviderOptions = heygenOpts(map[string]interface{}{"image": map[string]interface{}{"type": "asset_id", "assetId": "first-frame"}})
	}))
	if err != nil {
		t.Fatal(err)
	}
	body := ts.call(0).body
	if body["mode"] != "image_to_video" || !reflect.DeepEqual(body["image"], map[string]interface{}{"type": "asset_id", "asset_id": "first-frame"}) {
		t.Fatalf("body = %#v", body)
	}
}

func TestDoStart_MapsDocumentedFrameSizes(t *testing.T) {
	for _, tc := range []struct{ size, tier, ratio string }{
		{"960x416", "480p", "21:9"}, {"1344x768", "768p", "16:9"}, {"768x1344", "768p", "9:16"},
		{"1080x1890", "1080p", "9:16"}, {"2688x1536", "2k", "16:9"},
	} {
		ts := newTestServer(t)
		_, err := newModel(ts).DoStart(context.Background(), opts(func(o *provider.VideoModelV3CallOptions) { o.Resolution = tc.size }))
		if err != nil {
			t.Fatal(err)
		}
		body := ts.call(0).body
		if body["resolution"] != tc.tier || body["aspect_ratio"] != tc.ratio {
			t.Fatalf("%s: body = %#v", tc.size, body)
		}
	}
}

func TestDoStart_ProviderResolutionTakesPrecedence(t *testing.T) {
	ts := newTestServer(t)
	_, err := newModel(ts).DoStart(context.Background(), opts(func(o *provider.VideoModelV3CallOptions) {
		o.Resolution = "1920x1080"
		o.ProviderOptions = heygenOpts(map[string]interface{}{"resolution": "480p"})
	}))
	if err != nil {
		t.Fatal(err)
	}
	if ts.call(0).body["resolution"] != "480p" {
		t.Fatalf("body = %#v", ts.call(0).body)
	}
}

func TestDoStart_UnsupportedSettingsWarn(t *testing.T) {
	ts := newTestServer(t)
	fps := 30
	audio := false
	result, err := newModel(ts).DoStart(context.Background(), opts(func(o *provider.VideoModelV3CallOptions) {
		o.N = 2
		o.FPS = &fps
		o.GenerateAudio = &audio
	}))
	if err != nil {
		t.Fatal(err)
	}
	var features []string
	for _, w := range result.Warnings {
		features = append(features, w.Feature)
	}
	if !reflect.DeepEqual(features, []string{"n", "fps", "generateAudio"}) {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	body := ts.call(0).body
	for _, k := range []string{"fps", "generate_audio", "n"} {
		if _, ok := body[k]; ok {
			t.Fatalf("%s must not be sent: %#v", k, body)
		}
	}
}

func TestDoStart_RejectsInvalidInputsBeforeSubmitting(t *testing.T) {
	dur := func(v float64) *float64 { return &v }
	seed := func(v int) *int { return &v }
	many := func(f provider.VideoModelV3File, n int) []provider.VideoModelV3File {
		out := make([]provider.VideoModelV3File, n)
		for i := range out {
			out[i] = f
		}
		return out
	}
	cases := []struct {
		name   string
		mutate func(o *provider.VideoModelV3CallOptions)
	}{
		{"empty prompt", func(o *provider.VideoModelV3CallOptions) { o.Prompt = "" }},
		{"long prompt", func(o *provider.VideoModelV3CallOptions) { o.Prompt = strings.Repeat("x", 32001) }},
		{"short duration", func(o *provider.VideoModelV3CallOptions) { o.Duration = dur(4) }},
		{"long duration", func(o *provider.VideoModelV3CallOptions) { o.Duration = dur(16) }},
		{"fractional duration", func(o *provider.VideoModelV3CallOptions) { o.Duration = dur(5.5) }},
		{"negative seed", func(o *provider.VideoModelV3CallOptions) { o.Seed = seed(-1) }},
		{"large seed", func(o *provider.VideoModelV3CallOptions) { o.Seed = seed(aboveUint32Seed()) }},
		{"unknown resolution", func(o *provider.VideoModelV3CallOptions) { o.Resolution = "1920x1080" }},
		{"text adaptive ratio", func(o *provider.VideoModelV3CallOptions) { o.AspectRatio = "adaptive" }},
		{"unsupported ratio", func(o *provider.VideoModelV3CallOptions) { o.AspectRatio = "2:1" }},
		{"high resolution square", func(o *provider.VideoModelV3CallOptions) {
			o.AspectRatio = "1:1"
			o.ProviderOptions = heygenOpts(map[string]interface{}{"resolution": "2k"})
		}},
		{"high resolution adaptive references", func(o *provider.VideoModelV3CallOptions) {
			o.InputReferences = []provider.VideoModelV3File{imageFile}
			o.ProviderOptions = heygenOpts(map[string]interface{}{"resolution": "1080p"})
		}},
		{"audio-only references", func(o *provider.VideoModelV3CallOptions) {
			o.InputReferences = []provider.VideoModelV3File{audioFile}
		}},
		{"untyped reference", func(o *provider.VideoModelV3CallOptions) {
			o.InputReferences = []provider.VideoModelV3File{{Type: "url", URL: imageFile.URL}}
		}},
		{"insecure URL", func(o *provider.VideoModelV3CallOptions) {
			f := provider.VideoModelV3File{Type: "url", URL: "http://example.com/image.png"}
			o.Image = &f
		}},
		{"video as image", func(o *provider.VideoModelV3CallOptions) { o.Image = &videoFile }},
		{"last frame", func(o *provider.VideoModelV3CallOptions) {
			o.FrameImages = []provider.VideoFrameImage{{FrameType: provider.VideoFrameTypeLastFrame, Image: imageFile}}
		}},
		{"multiple first frames", func(o *provider.VideoModelV3CallOptions) {
			o.FrameImages = []provider.VideoFrameImage{
				{FrameType: provider.VideoFrameTypeFirstFrame, Image: imageFile},
				{FrameType: provider.VideoFrameTypeFirstFrame, Image: imageFile},
			}
		}},
		{"two first-frame sources", func(o *provider.VideoModelV3CallOptions) {
			o.Image = &imageFile
			o.FrameImages = []provider.VideoFrameImage{{FrameType: provider.VideoFrameTypeFirstFrame, Image: imageFile}}
		}},
		{"image and references", func(o *provider.VideoModelV3CallOptions) {
			o.Image = &imageFile
			o.InputReferences = []provider.VideoModelV3File{videoFile}
		}},
		{"two image sources", func(o *provider.VideoModelV3CallOptions) {
			o.Image = &imageFile
			o.ProviderOptions = heygenOpts(map[string]interface{}{"image": map[string]interface{}{"type": "asset_id", "assetId": "another"}})
		}},
		{"image mode without image", func(o *provider.VideoModelV3CallOptions) {
			o.ProviderOptions = heygenOpts(map[string]interface{}{"mode": "image_to_video"})
		}},
		{"reference mode without references", func(o *provider.VideoModelV3CallOptions) {
			o.ProviderOptions = heygenOpts(map[string]interface{}{"mode": "reference_to_video"})
		}},
		{"text mode with image", func(o *provider.VideoModelV3CallOptions) {
			o.Image = &imageFile
			o.ProviderOptions = heygenOpts(map[string]interface{}{"mode": "text_to_video"})
		}},
		{"text mode with references", func(o *provider.VideoModelV3CallOptions) {
			o.InputReferences = []provider.VideoModelV3File{imageFile}
			o.ProviderOptions = heygenOpts(map[string]interface{}{"mode": "text_to_video"})
		}},
		{"too many images", func(o *provider.VideoModelV3CallOptions) { o.InputReferences = many(imageFile, 10) }},
		{"too many videos", func(o *provider.VideoModelV3CallOptions) { o.InputReferences = many(videoFile, 4) }},
		{"too many audio references", func(o *provider.VideoModelV3CallOptions) {
			o.InputReferences = append([]provider.VideoModelV3File{imageFile}, many(audioFile, 4)...)
		}},
		{"too many combined references", func(o *provider.VideoModelV3CallOptions) {
			refs := many(imageFile, 9)
			refs = append(refs, many(videoFile, 3)...)
			o.InputReferences = append(refs, audioFile)
		}},
		{"combined image limit", func(o *provider.VideoModelV3CallOptions) {
			o.InputReferences = many(imageFile, 9)
			o.ProviderOptions = heygenOpts(map[string]interface{}{
				"referenceImages": []interface{}{map[string]interface{}{"type": "asset_id", "assetId": "extra"}},
			})
		}},
		{"wrong reference media type", func(o *provider.VideoModelV3CallOptions) {
			o.ProviderOptions = heygenOpts(map[string]interface{}{
				"referenceImages": []interface{}{map[string]interface{}{"type": "base64", "mediaType": "audio/mpeg", "data": "AQID"}},
			})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "large seed" && int64(math.MaxInt) < 1<<32 {
				t.Skip("no int reaches 2^32 on a 32-bit platform")
			}
			ts := newTestServer(t)
			_, err := newModel(ts).DoStart(context.Background(), opts(tc.mutate))
			if !providererrors.IsInvalidArgumentError(err) {
				t.Fatalf("err = %v, want InvalidArgumentError", err)
			}
			if ts.callCount() != 0 {
				t.Fatalf("expected no requests, got %d", ts.callCount())
			}
		})
	}
}

func TestDoStart_ValidatesProviderOptions(t *testing.T) {
	for _, bad := range []map[string]interface{}{
		{"promptEnhancement": "unknown"},
		{"resolution": "8k"},
		{"mode": "other"},
		{"image": map[string]interface{}{"type": "url", "url": "http://insecure.example/a.png"}},
		{"referenceImages": make([]interface{}, 10)},
	} {
		ts := newTestServer(t)
		_, err := newModel(ts).DoStart(context.Background(), opts(func(o *provider.VideoModelV3CallOptions) {
			o.ProviderOptions = heygenOpts(bad)
		}))
		if err == nil || ts.callCount() != 0 {
			t.Fatalf("%v: err=%v calls=%d", bad, err, ts.callCount())
		}
	}
}

func TestDoStatus_PendingAndProcessing(t *testing.T) {
	for _, status := range []string{"pending", "processing"} {
		ts := newTestServer(t)
		ts.set(statusPath, jsonResponse(200, `{"data":{"status":"`+status+`"}}`))
		res, err := newModel(ts).DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: testOperation})
		if err != nil || res.Status != provider.VideoOperationStatusPending {
			t.Fatalf("%s: res=%#v err=%v", status, res, err)
		}
	}
}

func TestDoStatus_CompletedReturnsSignedURLAndMetadata(t *testing.T) {
	ts := newTestServer(t)
	res, err := newModel(ts).DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: testOperation})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != provider.VideoOperationStatusCompleted || len(res.Videos) != 1 ||
		res.Videos[0].Type != "url" || res.Videos[0].URL != "https://resource2.heygen.ai/video.mp4" || res.Videos[0].MediaType != "video/mp4" {
		t.Fatalf("res = %#v", res)
	}
	want := map[string]interface{}{"heygen": map[string]interface{}{"videos": []map[string]interface{}{{
		"videoId": "video-1", "mode": "text_to_video", "resolution": "768p",
		"duration": 5.0, "width": 1344.0, "height": 768.0, "aspectRatio": "16:9", "seed": 0.0,
	}}}}
	if !reflect.DeepEqual(res.ProviderMetadata, want) {
		t.Fatalf("metadata = %#v", res.ProviderMetadata)
	}
	if ts.callCount() != 1 || ts.call(0).method != http.MethodGet || ts.call(0).headers.Get("x-api-key") != "test-key" {
		t.Fatalf("calls = %d", ts.callCount())
	}
}

func TestDoStatus_ReportsOutputDurationAndTimingWithoutCreditsOrTokens(t *testing.T) {
	ts := newTestServer(t)
	ts.set(statusPath, jsonResponse(200, completedBody(`{"duration":5.042,"timings":{"inference":3.7}}`)))
	res, err := newModel(ts).DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: testOperation})
	if err != nil {
		t.Fatal(err)
	}
	video := res.ProviderMetadata["heygen"].(map[string]interface{})["videos"].([]map[string]interface{})[0]
	if video["duration"] != 5.042 || !reflect.DeepEqual(video["timings"], map[string]interface{}{"inference": 3.7}) {
		t.Fatalf("video metadata = %#v", video)
	}
	heygen := res.ProviderMetadata["heygen"].(map[string]interface{})
	if _, ok := heygen["credits"]; ok {
		t.Fatal("credits must not be reported")
	}
	if _, ok := heygen["tokens"]; ok {
		t.Fatal("tokens must not be reported")
	}
}

func TestDoStatus_PreservesZeroTimingAndFailureMetadata(t *testing.T) {
	ts := newTestServer(t)
	ts.set(statusPath, jsonResponse(200, `{"data":{"status":"failed","failure_code":"generation_failed","timings":{"inference":0}}}`))
	res, err := newModel(ts).DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: testOperation})
	if err != nil {
		t.Fatal(err)
	}
	meta := res.ProviderMetadata["heygen"].(map[string]interface{})
	if res.Status != provider.VideoOperationStatusError || meta["failureCode"] != "generation_failed" ||
		!reflect.DeepEqual(meta["timings"], map[string]interface{}{"inference": 0.0}) {
		t.Fatalf("res = %#v", res)
	}
}

func TestDoStatus_AcceptsNullOptionalMetadata(t *testing.T) {
	ts := newTestServer(t)
	ts.set(statusPath, jsonResponse(200, `{"data":{"status":"completed","video_url":"https://resource2.heygen.ai/video.mp4","duration":null,"width":null}}`))
	res, err := newModel(ts).DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: testOperation})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"heygen": map[string]interface{}{"videos": []map[string]interface{}{
		{"videoId": "video-1", "mode": "text_to_video", "resolution": "768p"},
	}}}
	if !reflect.DeepEqual(res.ProviderMetadata, want) {
		t.Fatalf("metadata = %#v", res.ProviderMetadata)
	}
}

func TestDoStatus_FailedAndCancelledMapToErrors(t *testing.T) {
	for _, status := range []string{"failed", "cancelled"} {
		ts := newTestServer(t)
		ts.set(statusPath, jsonResponse(200, `{"data":{"status":"`+status+`","failure_code":"generation_failed","failure_message":"Generation did not finish."}}`))
		res, err := newModel(ts).DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: testOperation})
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != provider.VideoOperationStatusError || res.Error != "Generation did not finish. (generation_failed)" {
			t.Fatalf("%s: res = %#v", status, res)
		}
		if res.ProviderMetadata["heygen"].(map[string]interface{})["failureCode"] != "generation_failed" {
			t.Fatalf("metadata = %#v", res.ProviderMetadata)
		}
	}
}

func TestDoStatus_RejectsUnusableResponses(t *testing.T) {
	for _, data := range []string{`{"status":"completed"}`, `{"status":"completed","video_url":""}`, `{"status":"unexpected"}`} {
		ts := newTestServer(t)
		ts.set(statusPath, jsonResponse(200, `{"data":`+data+`}`))
		_, err := newModel(ts).DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: testOperation})
		var invalid *providererrors.InvalidResponseDataError
		if !errors.As(err, &invalid) {
			t.Fatalf("%s: err = %v", data, err)
		}
	}
}

func TestDoStatus_ValidatesOperationReferences(t *testing.T) {
	for _, op := range []string{`null`, `{}`, `{"videoId":"","mode":"text_to_video","resolution":"768p"}`, ``} {
		ts := newTestServer(t)
		_, err := newModel(ts).DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: json.RawMessage(op)})
		if err == nil || ts.callCount() != 0 {
			t.Fatalf("%q: err=%v calls=%d", op, err, ts.callCount())
		}
	}
}

func TestOperationSurvivesSerialization(t *testing.T) {
	ts := newTestServer(t)
	model := newModel(ts)
	started, err := model.DoStart(context.Background(), opts(func(o *provider.VideoModelV3CallOptions) {
		o.ProviderOptions = heygenOpts(map[string]interface{}{"resolution": "2k"})
	}))
	if err != nil {
		t.Fatal(err)
	}
	serialized := model.Serialize()
	serialized.Config["BaseURL"] = ts.URL
	restoredModel, err := deserializeVideoModel(serialized)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HEYGEN_API_KEY", "env-key")
	restored := restoredModel.(*VideoModel)
	res, err := restored.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: started.Operation})
	if err != nil {
		t.Fatal(err)
	}
	video := res.ProviderMetadata["heygen"].(map[string]interface{})["videos"].([]map[string]interface{})[0]
	if video["resolution"] != "2k" {
		t.Fatalf("video = %#v", video)
	}
}

func TestDoStatus_RefreshesResultURL(t *testing.T) {
	ts := newTestServer(t)
	m := newModel(ts)
	if _, err := m.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: testOperation}); err != nil {
		t.Fatal(err)
	}
	refreshed := "https://resource2.heygen.ai/video.mp4?signature=new"
	ts.set(statusPath, jsonResponse(200, completedBody(`{"video_url":"`+refreshed+`"}`)))
	res, err := m.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: testOperation})
	if err != nil || res.Videos[0].URL != refreshed {
		t.Fatalf("res=%#v err=%v", res, err)
	}
}

func TestAPIErrorsArePreserved(t *testing.T) {
	body := `{"error":{"code":"invalid_parameter","message":"Invalid duration","param":"duration"}}`
	for _, method := range []string{"start", "status"} {
		ts := newTestServer(t)
		path := createPath
		if method == "status" {
			path = statusPath
		}
		ts.set(path, jsonResponse(400, body))
		var err error
		if method == "start" {
			_, err = newModel(ts).DoStart(context.Background(), opts(nil))
		} else {
			_, err = newModel(ts).DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: testOperation})
		}
		var perr *providererrors.ProviderError
		if !errors.As(err, &perr) || perr.StatusCode != 400 || perr.Message != "Invalid duration (invalid_parameter) [duration]" {
			t.Fatalf("%s: err = %v", method, err)
		}
	}
}

func TestMalformedCreateResponseIsRejected(t *testing.T) {
	ts := newTestServer(t)
	ts.set(createPath, jsonResponse(200, `{"data":{"status":"pending"}}`))
	_, err := newModel(ts).DoStart(context.Background(), opts(nil))
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || !strings.Contains(perr.Message, "Invalid JSON response") {
		t.Fatalf("err = %v", err)
	}
}

func TestDoStart_AbortSignalCancelsRequest(t *testing.T) {
	ts := newTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newModel(ts).DoStart(context.Background(), opts(func(o *provider.VideoModelV3CallOptions) { o.AbortSignal = ctx }))
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if ts.callCount() != 0 {
		t.Fatalf("calls = %d", ts.callCount())
	}
}

func TestDoGenerate_StartsAndPollsToCompletion(t *testing.T) {
	ts := newTestServer(t)
	var mu sync.Mutex
	polls := 0
	ts.set(statusPath, func(w http.ResponseWriter) {
		mu.Lock()
		polls++
		n := polls
		mu.Unlock()
		if n < 2 {
			jsonResponse(200, `{"data":{"status":"processing"}}`)(w)
			return
		}
		jsonResponse(200, completedBody(`{}`))(w)
	})
	p := New(Config{APIKey: "k", BaseURL: ts.URL, PollIntervalMillis: 1, PollTimeoutMillis: 5000})
	res, err := NewVideoModel(p, "heygen-video-1").DoGenerate(context.Background(), &opts(nil).VideoModelV3CallOptions)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Videos) != 1 || res.Videos[0].URL == "" {
		t.Fatalf("res = %#v", res)
	}
}

func TestDoGenerate_ReportsFailureAndTimeout(t *testing.T) {
	ts := newTestServer(t)
	ts.set(statusPath, jsonResponse(200, `{"data":{"status":"failed","failure_message":"boom"}}`))
	p := New(Config{APIKey: "k", BaseURL: ts.URL, PollIntervalMillis: 1})
	_, err := NewVideoModel(p, "heygen-video-1").DoGenerate(context.Background(), &opts(nil).VideoModelV3CallOptions)
	var verr *providererrors.VideoGenerationError
	if !errors.As(err, &verr) || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}

	ts.set(statusPath, jsonResponse(200, `{"data":{"status":"pending"}}`))
	p = New(Config{APIKey: "k", BaseURL: ts.URL, PollIntervalMillis: 1, PollTimeoutMillis: 20})
	start := time.Now()
	_, err = NewVideoModel(p, "heygen-video-1").DoGenerate(context.Background(), &opts(nil).VideoModelV3CallOptions)
	if err == nil || !strings.Contains(err.Error(), "timed out") || time.Since(start) > 5*time.Second {
		t.Fatalf("err = %v", err)
	}
}

// aboveUint32Seed is 2^32, the first seed HeyGen rejects as too large. On a
// 32-bit platform no int reaches it and the case is skipped.
func aboveUint32Seed() int {
	v := int64(1) << 32
	if v > math.MaxInt {
		return math.MaxInt
	}
	return int(v)
}
