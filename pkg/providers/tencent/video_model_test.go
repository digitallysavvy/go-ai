package tencent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Ported from tencent-video-model.test.ts.

const testTaskID = "task-123"

var testCurrentDate = time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)

const submittedResponse = `{"code":0,"message":"SUCCEED","request_id":"req-1","data":{"id":"task-123","status":"submitted"}}`

const succeededStatus = `{"code":0,"message":"SUCCEED","request_id":"req-2","data":[{"id":"task-123","status":"succeeded","outputs":[{"type":"video","id":"video-1","url":"https://cdn.example.com/video.mp4","duration":"5.041"}]}],"tokenhub_usage":{"total_tokens":12345}}`

// videoServer serves the TokenHub Kling endpoints under /v1 and VOD at /.
// Responses can be replaced per path.
type videoServer struct {
	*httptest.Server
	mu        sync.Mutex
	responses map[string]response
	calls     []recordedCall
}

type response struct {
	status int
	body   string
}

func newVideoServer(t *testing.T) *videoServer {
	t.Helper()
	s := &videoServer{responses: map[string]response{
		"/v1/wand/kling/text-to-video":       {200, submittedResponse},
		"/v1/wand/kling/image-to-video":      {200, submittedResponse},
		"/v1/wand/kling/omni-video":          {200, submittedResponse},
		"/v1/wand/kling/motion-control":      {200, submittedResponse},
		"/v1/wand/kling/tasks/" + testTaskID: {200, succeededStatus},
		"/":                                  {200, `{"Response":{"TaskId":"task-123","RequestId":"req-vod-1"}}`},
	}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.calls = append(s.calls, recordedCall{url: r.Method + " " + r.URL.Path, headers: r.Header.Clone(), body: string(b)})
		resp, ok := s.responses[r.URL.Path]
		s.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.status)
		_, _ = io.WriteString(w, resp.body)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *videoServer) set(path string, status int, body string) {
	s.mu.Lock()
	s.responses[path] = response{status, body}
	s.mu.Unlock()
}

func (s *videoServer) call(i int) recordedCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[i]
}

func (s *videoServer) model(t *testing.T, modelID string) *VideoModel {
	t.Helper()
	p := New(Config{
		APIKey: "test-key", BaseURL: s.URL + "/v1", VodBaseURL: s.URL,
		SecretID: "AKIDEXAMPLE", SecretKey: "EXAMPLESECRET", SubAppID: "1500000000",
	})
	m, err := NewVideoModel(p, modelID)
	if err != nil {
		t.Fatal(err)
	}
	m.currentDate = func() time.Time { return testCurrentDate }
	return m
}

func defaultVideoOptions() provider.VideoModelV3CallOptions {
	return provider.VideoModelV3CallOptions{Prompt: "A cat walking on a beach", N: 1, ProviderOptions: map[string]interface{}{}}
}

func start(t *testing.T, m *VideoModel, opts provider.VideoModelV3CallOptions) *provider.VideoModelV3OperationStartResult {
	t.Helper()
	r, err := m.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: opts})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	return r
}

func status(t *testing.T, m *VideoModel) *provider.VideoModelV3OperationStatusResult {
	t.Helper()
	r, err := m.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: json.RawMessage(`{"taskId":"task-123"}`)})
	if err != nil {
		t.Fatalf("DoStatus: %v", err)
	}
	return r
}

func features(r *provider.VideoModelV3OperationStartResult) []string {
	out := []string{}
	for _, w := range r.Warnings {
		out = append(out, w.Feature)
	}
	return out
}

func f64(v float64) *float64 { return &v }
func boolp(v bool) *bool     { return &v }
func intp(v int) *int        { return &v }

func urlFile(url, mediaType string) provider.VideoModelV3File {
	return provider.VideoModelV3File{Type: "url", URL: url, MediaType: mediaType}
}

func TestVideoModelExposesProviderAndModelInformation(t *testing.T) {
	m := newVideoServer(t).model(t, "kling-video-v3")
	if m.Provider() != "tencent.video" || m.ModelID() != "kling-video-v3" || m.SpecificationVersion() != "v4" || *m.MaxVideosPerCall() != 1 {
		t.Errorf("model = %+v", m)
	}
}

func TestVideoModelRejectsUnsupportedModelIDs(t *testing.T) {
	for _, id := range []string{"unknown-model", "vod/VS", "vod//2.0", "vod/VS/"} {
		var nsm *providererrors.NoSuchModelError
		if _, err := NewVideoModel(New(Config{}), id); !errors.As(err, &nsm) {
			t.Errorf("%s: err = %v", id, err)
		}
	}
}

func TestTokenHubTextToVideoSendsPromptAndSettings(t *testing.T) {
	s := newVideoServer(t)
	opts := defaultVideoOptions()
	opts.AspectRatio, opts.Duration, opts.Resolution, opts.GenerateAudio = "16:9", f64(5), "1920x1080", boolp(true)
	r := start(t, s.model(t, "kling-video-v3"), opts)
	c := s.call(0)
	if c.url != "POST /v1/wand/kling/text-to-video" || c.headers.Get("Authorization") != "Bearer test-key" {
		t.Errorf("call = %s %q", c.url, c.headers.Get("Authorization"))
	}
	if want := `{"model":"kling-video-v3","prompt":"A cat walking on a beach","settings":{"resolution":"1080p","aspect_ratio":"16:9","duration":5,"audio":"native"}}`; c.body != want {
		t.Errorf("body = %s\nwant %s", c.body, want)
	}
	if string(r.Operation) != `{"taskId":"task-123"}` || len(r.Warnings) != 0 || !r.Response.Timestamp.Equal(testCurrentDate) || r.Response.ModelID != "kling-video-v3" {
		t.Errorf("result = %+v", r)
	}
}

func TestTokenHubSendsMultiShotWatermarkAndExternalTaskID(t *testing.T) {
	s := newVideoServer(t)
	opts := defaultVideoOptions()
	opts.ProviderOptions = map[string]interface{}{"tencent": map[string]interface{}{"multiShot": false, "watermark": true, "externalTaskId": "my-task", "resolution": "4k"}}
	start(t, s.model(t, "kling-video-v3"), opts)
	if want := `{"model":"kling-video-v3","prompt":"A cat walking on a beach","settings":{"resolution":"4k","multi_shot":false},"options":{"external_task_id":"my-task","watermark_info":{"enabled":true}}}`; s.call(0).body != want {
		t.Errorf("body = %s", s.call(0).body)
	}
}

func TestTokenHubOmitsAudioAndMultiShotForModelsThatDoNotSupportThem(t *testing.T) {
	s := newVideoServer(t)
	opts := defaultVideoOptions()
	opts.GenerateAudio = boolp(true)
	opts.ProviderOptions = map[string]interface{}{"tencent": map[string]interface{}{"multiShot": true}}
	r := start(t, s.model(t, "kling-video-v4"), opts)
	if want := `{"model":"kling-video-v4","prompt":"A cat walking on a beach"}`; s.call(0).body != want {
		t.Errorf("body = %s", s.call(0).body)
	}
	if got := features(r); !reflect.DeepEqual(got, []string{"generateAudio", "providerOptions.tencent.multiShot"}) {
		t.Errorf("warnings = %v", got)
	}
}

func TestTokenHubWarnsOnUnsupportedOptions(t *testing.T) {
	s := newVideoServer(t)
	m := s.model(t, "kling-video-v3")
	opts := defaultVideoOptions()
	opts.Seed, opts.FPS, opts.N, opts.Resolution = intp(42), intp(24), 2, "640x480"
	opts.ProviderOptions = map[string]interface{}{"tencent": map[string]interface{}{"negativePrompt": "dogs"}}
	r, err := m.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: opts, WebhookURL: "https://example.com/webhook"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"model":"kling-video-v3","prompt":"A cat walking on a beach"}`; s.call(0).body != want {
		t.Errorf("body = %s", s.call(0).body)
	}
	if got := features(r); !reflect.DeepEqual(got, []string{"fps", "n", "seed", "webhookUrl", "providerOptions.tencent.negativePrompt", "resolution"}) {
		t.Errorf("warnings = %v", got)
	}
	for _, w := range r.Warnings {
		if w.Type != "unsupported" {
			t.Errorf("warning type = %s", w.Type)
		}
	}
}

func TestTokenHubImageToVideoSendsFirstAndLastFrames(t *testing.T) {
	s := newVideoServer(t)
	opts := defaultVideoOptions()
	opts.AspectRatio, opts.Duration = "9:16", f64(10)
	opts.FrameImages = []provider.VideoFrameImage{
		{FrameType: "first_frame", Image: urlFile("https://example.com/first.png", "")},
		{FrameType: "last_frame", Image: provider.VideoModelV3File{Type: "file", MediaType: "image/png", Data: []byte{1, 2, 3}}},
	}
	opts.ProviderOptions = map[string]interface{}{"tencent": map[string]interface{}{"elements": []interface{}{map[string]interface{}{"elementId": "element-1"}}}}
	r := start(t, s.model(t, "kling-video-v3"), opts)
	c := s.call(0)
	if c.url != "POST /v1/wand/kling/image-to-video" {
		t.Errorf("url = %s", c.url)
	}
	if want := `{"model":"kling-video-v3","contents":[{"type":"prompt","text":"A cat walking on a beach"},{"type":"first_frame","url":"https://example.com/first.png"},{"type":"last_frame","url":"data:image/png;base64,AQID"},{"type":"element","element_id":"element-1"}],"settings":{"duration":10}}`; c.body != want {
		t.Errorf("body = %s", c.body)
	}
	if got := features(r); !reflect.DeepEqual(got, []string{"aspectRatio"}) {
		t.Errorf("warnings = %v", got)
	}
}

func TestTokenHubUsesTheLegacyImageAsTheFirstFrame(t *testing.T) {
	s := newVideoServer(t)
	opts := defaultVideoOptions()
	opts.Image = &provider.VideoModelV3File{Type: "file", MediaType: "image/jpeg", Data: []byte{0, 0, 0}}
	start(t, s.model(t, "kling-video-v3-turbo"), opts)
	if want := `{"model":"kling-video-v3-turbo","contents":[{"type":"prompt","text":"A cat walking on a beach"},{"type":"first_frame","url":"data:image/jpeg;base64,AAAA"}]}`; s.call(0).body != want {
		t.Errorf("body = %s", s.call(0).body)
	}
}

func TestTokenHubIgnoresReferencesOnModelsWithoutOmniSupport(t *testing.T) {
	s := newVideoServer(t)
	opts := defaultVideoOptions()
	opts.InputReferences = []provider.VideoModelV3File{urlFile("https://example.com/ref.png", "")}
	r := start(t, s.model(t, "kling-video-v3"), opts)
	if s.call(0).url != "POST /v1/wand/kling/text-to-video" || !reflect.DeepEqual(features(r), []string{"inputReferences"}) {
		t.Errorf("url %s warnings %v", s.call(0).url, features(r))
	}
}

func TestTokenHubOmniSendsReferenceImagesAndVideos(t *testing.T) {
	s := newVideoServer(t)
	opts := defaultVideoOptions()
	opts.AspectRatio = "1:1"
	opts.InputReferences = []provider.VideoModelV3File{urlFile("https://example.com/ref.png", ""), urlFile("https://example.com/ref.mp4", "video/mp4")}
	opts.ProviderOptions = map[string]interface{}{"tencent": map[string]interface{}{"referenceVideoType": "base", "audio": "original"}}
	start(t, s.model(t, "kling-video-v3-omni"), opts)
	c := s.call(0)
	if want := `{"model":"kling-video-v3-omni","contents":[{"type":"prompt","text":"A cat walking on a beach"},{"type":"refer_image","url":"https://example.com/ref.png"},{"type":"base_video","url":"https://example.com/ref.mp4"}],"settings":{"aspect_ratio":"1:1","audio":"original"}}`; c.url != "POST /v1/wand/kling/omni-video" || c.body != want {
		t.Errorf("call = %s %s", c.url, c.body)
	}
}

func TestTokenHubRoutesKlingV4WithReferencesAndVoicesToOmni(t *testing.T) {
	s := newVideoServer(t)
	opts := defaultVideoOptions()
	opts.AspectRatio = "adaptive"
	opts.InputReferences = []provider.VideoModelV3File{urlFile("https://example.com/motion.mp4", "video/mp4")}
	opts.ProviderOptions = map[string]interface{}{"tencent": map[string]interface{}{"voices": []interface{}{map[string]interface{}{"voiceId": "voice-1", "id": "1"}}}}
	start(t, s.model(t, "kling-video-v4"), opts)
	c := s.call(0)
	if want := `{"model":"kling-video-v4","contents":[{"type":"prompt","text":"A cat walking on a beach"},{"type":"feature_video","url":"https://example.com/motion.mp4"},{"type":"voice","voice_id":"voice-1","id":"1"}],"settings":{"aspect_ratio":"adaptive"}}`; c.url != "POST /v1/wand/kling/omni-video" || c.body != want {
		t.Errorf("call = %s %s", c.url, c.body)
	}
}

func TestTokenHubRoutesKlingV4WithElementsAndNoFirstFrameToOmni(t *testing.T) {
	s := newVideoServer(t)
	opts := defaultVideoOptions()
	opts.ProviderOptions = map[string]interface{}{"tencent": map[string]interface{}{"elements": []interface{}{map[string]interface{}{"elementId": "element-1"}}}}
	r := start(t, s.model(t, "kling-video-v4"), opts)
	c := s.call(0)
	if want := `{"model":"kling-video-v4","contents":[{"type":"prompt","text":"A cat walking on a beach"},{"type":"element","element_id":"element-1"}]}`; c.url != "POST /v1/wand/kling/omni-video" || c.body != want || len(r.Warnings) != 0 {
		t.Errorf("call = %s %s %v", c.url, c.body, r.Warnings)
	}
}

func TestTokenHubMotionControlSendsTheCharacterImageAndMotionVideo(t *testing.T) {
	s := newVideoServer(t)
	opts := defaultVideoOptions()
	opts.Duration, opts.GenerateAudio = f64(5), boolp(false)
	opts.Image = &provider.VideoModelV3File{Type: "url", URL: "https://example.com/character.png"}
	opts.InputReferences = []provider.VideoModelV3File{urlFile("https://example.com/motion.mp4", "video/mp4")}
	opts.ProviderOptions = map[string]interface{}{"tencent": map[string]interface{}{
		"characterOrientation": "video",
		"elements":             []interface{}{map[string]interface{}{"elementId": "element-1", "id": "e1"}},
	}}
	r := start(t, s.model(t, "kling-video-motion-control-v3.0"), opts)
	c := s.call(0)
	if want := `{"model":"kling-video-motion-control-v3.0","contents":[{"type":"prompt","text":"A cat walking on a beach"},{"type":"image","url":"https://example.com/character.png"},{"type":"video","url":"https://example.com/motion.mp4"},{"type":"element","element_id":"element-1","id":"e1"}],"settings":{"character_orientation":"video","audio":"off"}}`; c.url != "POST /v1/wand/kling/motion-control" || c.body != want {
		t.Errorf("call = %s %s", c.url, c.body)
	}
	if !reflect.DeepEqual(features(r), []string{"duration"}) {
		t.Errorf("warnings = %v", features(r))
	}
}

func TestTokenHubMotionControlUsesTheFirstImageReference(t *testing.T) {
	s := newVideoServer(t)
	opts := defaultVideoOptions()
	opts.Prompt = ""
	opts.InputReferences = []provider.VideoModelV3File{urlFile("https://example.com/character.png", ""), urlFile("https://example.com/motion.mp4", "video/mp4")}
	opts.ProviderOptions = map[string]interface{}{"tencent": map[string]interface{}{"characterOrientation": "image"}}
	start(t, s.model(t, "kling-video-motion-control-v2.6"), opts)
	if want := `{"model":"kling-video-motion-control-v2.6","contents":[{"type":"image","url":"https://example.com/character.png"},{"type":"video","url":"https://example.com/motion.mp4"}],"settings":{"character_orientation":"image"}}`; s.call(0).body != want {
		t.Errorf("body = %s", s.call(0).body)
	}
}

func TestTokenHubThrowsWhenTheResponseHasANonZeroCode(t *testing.T) {
	s := newVideoServer(t)
	s.set("/v1/wand/kling/text-to-video", 200, `{"code":1201,"message":"Invalid model","request_id":"r"}`)
	_, err := s.model(t, "kling-video-v3").DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: defaultVideoOptions()})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.Message != "Invalid model" || perr.IsRetryable() {
		t.Fatalf("err = %v", err)
	}
}

func TestTokenHubSurfacesHTTPErrorMessages(t *testing.T) {
	s := newVideoServer(t)
	s.set("/v1/wand/kling/text-to-video", 401, `{"error":{"message":"Invalid API key","type":"auth_error"}}`)
	_, err := s.model(t, "kling-video-v3").DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: defaultVideoOptions()})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.StatusCode != 401 || perr.Message != "Invalid API key" {
		t.Fatalf("err = %v", err)
	}
}

func TestTokenHubThrowsWhenNoTaskIDIsReturned(t *testing.T) {
	s := newVideoServer(t)
	s.set("/v1/wand/kling/text-to-video", 200, `{"code":0,"data":{}}`)
	_, err := s.model(t, "kling-video-v3").DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: defaultVideoOptions()})
	if err == nil || err.Error() != "No task ID returned from TokenHub." {
		t.Fatalf("err = %v", err)
	}
}

func TestTokenHubStatusReturnsVideosAndUsage(t *testing.T) {
	s := newVideoServer(t)
	r := status(t, s.model(t, "kling-video-v3"))
	c := s.call(0)
	if c.url != "GET /v1/wand/kling/tasks/task-123" || c.headers.Get("Authorization") != "Bearer test-key" {
		t.Errorf("call = %s %q", c.url, c.headers.Get("Authorization"))
	}
	if r.Status != "completed" || len(r.Videos) != 1 || !reflect.DeepEqual(r.Videos[0], provider.VideoModelV3VideoData{Type: "url", URL: "https://cdn.example.com/video.mp4", MediaType: "video/mp4"}) {
		t.Errorf("result = %+v", r)
	}
	meta, _ := json.Marshal(r.ProviderMetadata)
	if want := `{"tencent":{"requestId":"req-2","taskId":"task-123","usage":{"totalTokens":12345},"videos":[{"duration":5.041,"id":"video-1","url":"https://cdn.example.com/video.mp4"}]}}`; string(meta) != want {
		t.Errorf("metadata = %s", meta)
	}
	if !r.Response.Timestamp.Equal(testCurrentDate) || r.Response.ModelID != "kling-video-v3" || r.Response.Headers == nil {
		t.Errorf("response = %+v", r.Response)
	}
}

func TestTokenHubStatusVariants(t *testing.T) {
	s := newVideoServer(t)
	m := s.model(t, "kling-video-v3")
	path := "/v1/wand/kling/tasks/" + testTaskID
	s.set(path, 200, `{"code":0,"data":{"id":"task-123","status":"succeeded","outputs":[{"url":"https://cdn.example.com/video.mp4"}]}}`)
	if r := status(t, m); r.Status != "completed" {
		t.Errorf("single task object: %+v", r)
	}
	s.set(path, 200, `{"code":0,"data":[{"id":"task-123","status":"processing"}]}`)
	if r := status(t, m); r.Status != "pending" {
		t.Errorf("processing: %+v", r)
	}
	s.set(path, 200, `{"code":0,"data":[{"id":"task-123","status":"failed","message":"risk control"}]}`)
	if r := status(t, m); r.Status != "error" || r.Error != "Video generation failed: risk control (task task-123)" {
		t.Errorf("failed: %+v", r)
	}
}

func TestVodSubmitsASignedCreateAigcVideoTaskRequest(t *testing.T) {
	s := newVideoServer(t)
	opts := defaultVideoOptions()
	opts.AspectRatio, opts.Duration, opts.Resolution, opts.Seed, opts.GenerateAudio = "16:9", f64(8), "1280x720", intp(7), boolp(true)
	opts.FrameImages = []provider.VideoFrameImage{{FrameType: "first_frame", Image: urlFile("https://example.com/first.png", "")}}
	opts.InputReferences = []provider.VideoModelV3File{
		{Type: "file", MediaType: "video/mp4", Data: []byte{1, 2, 3}},
		urlFile("https://example.com/voice.mp3", "audio/mpeg"),
	}
	opts.ProviderOptions = map[string]interface{}{"tencent": map[string]interface{}{
		"negativePrompt": "dogs", "enhancePrompt": false, "inputRegion": "Oversea",
		"watermark": false, "referenceVideoType": "feature", "sessionId": "session-1",
	}}
	r := start(t, s.model(t, "vod/VS/2.0"), opts)
	c := s.call(0)
	if c.url != "POST /" || c.headers.Get("X-Tc-Action") != "CreateAigcVideoTask" || c.headers.Get("X-Tc-Version") != "2018-07-17" || c.headers.Get("X-Tc-Timestamp") != "1794096000" {
		t.Errorf("call = %s %v", c.url, c.headers)
	}
	if !regexp.MustCompile(`^TC3-HMAC-SHA256 Credential=AKIDEXAMPLE/2026-11-08/vod/tc3_request, SignedHeaders=content-type;host;x-tc-action, Signature=[0-9a-f]{64}$`).MatchString(c.headers.Get("Authorization")) {
		t.Errorf("Authorization = %q", c.headers.Get("Authorization"))
	}
	want := `{"SubAppId":1500000000,"ModelName":"VS","ModelVersion":"2.0","Prompt":"A cat walking on a beach","NegativePrompt":"dogs","EnhancePrompt":"Disabled",` +
		`"FileInfos":[{"Type":"Url","Url":"https://example.com/first.png","Category":"Image","Usage":"FirstFrame"},` +
		`{"Type":"Base64","Base64":"AQID","Category":"Video","Usage":"Reference","ReferenceType":"feature"},` +
		`{"Type":"Url","Url":"https://example.com/voice.mp3","Category":"Audio","Usage":"Reference"}],` +
		`"OutputConfig":{"StorageMode":"Temporary","Resolution":"720P","AspectRatio":"16:9","Duration":8,"AudioGeneration":"Enabled","LogoAdd":"Disabled"},` +
		`"InputRegion":"Oversea","Seed":7,"SessionId":"session-1"}`
	if c.body != want {
		t.Errorf("body = %s\nwant %s", c.body, want)
	}
	if string(r.Operation) != `{"taskId":"task-123"}` || len(r.Warnings) != 0 {
		t.Errorf("result = %+v", r)
	}
}

func TestVodWarnsAboutTokenHubOnlyOptions(t *testing.T) {
	s := newVideoServer(t)
	opts := defaultVideoOptions()
	opts.ProviderOptions = map[string]interface{}{"tencent": map[string]interface{}{"multiShot": true, "characterOrientation": "image"}}
	r := start(t, s.model(t, "vod/Kling/3.0"), opts)
	if got := features(r); !reflect.DeepEqual(got, []string{"providerOptions.tencent.multiShot", "providerOptions.tencent.characterOrientation"}) {
		t.Errorf("warnings = %v", got)
	}
}

func fileInfos(t *testing.T, body string) string {
	t.Helper()
	var m map[string]json.RawMessage
	_ = json.Unmarshal([]byte(body), &m)
	return string(m["FileInfos"])
}

func TestVodSendsAssetsAsURLFileInputs(t *testing.T) {
	s := newVideoServer(t)
	opts := defaultVideoOptions()
	opts.ProviderOptions = map[string]interface{}{"tencent": map[string]interface{}{"assets": []interface{}{
		map[string]interface{}{"url": "asset://asset-1"},
		map[string]interface{}{"url": "asset://asset-2", "usage": "FirstFrame"},
		map[string]interface{}{"url": "asset://asset-3", "category": "Video"},
	}}}
	start(t, s.model(t, "vod/VS/2.0"), opts)
	if want := `[{"Type":"Url","Url":"asset://asset-1","Category":"Image","Usage":"Reference"},{"Type":"Url","Url":"asset://asset-2","Category":"Image","Usage":"FirstFrame"},{"Type":"Url","Url":"asset://asset-3","Category":"Video","Usage":"Reference"}]`; fileInfos(t, s.call(0).body) != want {
		t.Errorf("FileInfos = %s", fileInfos(t, s.call(0).body))
	}
}

func TestVodIgnoresAssetFramesWhenThatFrameIsAlreadySet(t *testing.T) {
	s := newVideoServer(t)
	opts := defaultVideoOptions()
	opts.Image = &provider.VideoModelV3File{Type: "url", URL: "https://example.com/first.png"}
	opts.ProviderOptions = map[string]interface{}{"tencent": map[string]interface{}{"assets": []interface{}{
		map[string]interface{}{"url": "asset://asset-1", "usage": "FirstFrame"},
		map[string]interface{}{"url": "asset://asset-2", "usage": "LastFrame"},
		map[string]interface{}{"url": "asset://asset-3", "usage": "LastFrame"},
	}}}
	r := start(t, s.model(t, "vod/VS/2.0"), opts)
	if want := `[{"Type":"Url","Url":"https://example.com/first.png","Category":"Image","Usage":"FirstFrame"},{"Type":"Url","Url":"asset://asset-2","Category":"Image","Usage":"LastFrame"}]`; fileInfos(t, s.call(0).body) != want {
		t.Errorf("FileInfos = %s", fileInfos(t, s.call(0).body))
	}
	if len(r.Warnings) != 2 || r.Warnings[0].Details != "VOD accepts one first frame; the asset asset://asset-1 was ignored." || r.Warnings[1].Details != "VOD accepts one last frame; the asset asset://asset-3 was ignored." {
		t.Errorf("warnings = %+v", r.Warnings)
	}
}

func TestVodThrowsAnAPICallErrorForResponseError(t *testing.T) {
	s := newVideoServer(t)
	s.set("/", 200, `{"Response":{"Error":{"Code":"InvalidParameterValue.SubAppId","Message":"Invalid SubAppId."},"RequestId":"req-vod-err"}}`)
	_, err := s.model(t, "vod/VS/2.0").DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: defaultVideoOptions()})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.StatusCode != 400 || perr.Message != "InvalidParameterValue.SubAppId: Invalid SubAppId." {
		t.Fatalf("err = %v", err)
	}
}

func TestVodStatusReturnsVideosAndUsageForAFinishedTask(t *testing.T) {
	s := newVideoServer(t)
	s.set("/", 200, `{"Response":{"TaskType":"AigcVideoTask","Status":"FINISH","RequestId":"req-vod-2","AigcVideoTask":{"TaskId":"task-123","Status":"FINISH","ErrCode":0,"Message":"","Progress":100,`+
		`"Input":{"ModelName":"VS","ModelVersion":"2.0","OutputConfig":{"StorageMode":"Temporary","Duration":8,"Resolution":"720P","AspectRatio":"16:9","AudioGeneration":""}},`+
		`"Output":{"FileInfos":[{"FileUrl":"https://vod.example.com/out.mp4","FileType":"mp4","FileId":"","MetaData":{"Duration":8.04}}],`+
		`"Usage":{"InputTokens":0,"ThoughtTokens":0,"InputImageCount":0,"InputSeconds":5,"OutputSeconds":8,"TotalSeconds":13,"SpecificationUsages":[{"Specification":"SV2.0_720P","Category":"aigc_video_sv2_720"}]}}}}}`)
	r := status(t, s.model(t, "vod/VS/2.0"))
	c := s.call(0)
	if c.headers.Get("X-Tc-Action") != "DescribeTaskDetail" || c.body != `{"TaskId":"task-123","SubAppId":1500000000}` {
		t.Errorf("call = %v %s", c.headers.Get("X-Tc-Action"), c.body)
	}
	if r.Status != "completed" || len(r.Videos) != 1 || r.Videos[0].MediaType != "video/mp4" || r.Videos[0].URL != "https://vod.example.com/out.mp4" {
		t.Errorf("result = %+v", r)
	}
	meta, _ := json.Marshal(r.ProviderMetadata)
	if want := `{"tencent":{"requestId":"req-vod-2","resolution":"720P","taskId":"task-123","usage":{"inputImageCount":0,"inputSeconds":5,"inputTokens":0,"outputSeconds":8,"specificationUsages":[{"category":"aigc_video_sv2_720","specification":"SV2.0_720P"}],"thoughtTokens":0,"totalSeconds":13},"videos":[{"duration":8.04,"url":"https://vod.example.com/out.mp4"}]}}`; string(meta) != want {
		t.Errorf("metadata = %s", meta)
	}
}

func TestVodStatusReturnsTheLastFrameAsMetadata(t *testing.T) {
	s := newVideoServer(t)
	s.set("/", 200, `{"Response":{"TaskType":"AigcVideoTask","Status":"FINISH","AigcVideoTask":{"TaskId":"task-123","Status":"FINISH","ErrCode":0,"Output":{"FileInfos":[`+
		`{"FileUrl":"https://vod.example.com/out.mp4","FileType":"","UsageType":"","MetaData":{"Duration":4.04}},`+
		`{"FileUrl":"https://vod.example.com/last.png","FileType":"","UsageType":"last_frame_url","MetaData":null}],"Usage":{"OutputSeconds":4}}}}}`)
	r := status(t, s.model(t, "vod/VS/2.0"))
	if len(r.Videos) != 1 || r.Videos[0].URL != "https://vod.example.com/out.mp4" {
		t.Errorf("videos = %+v", r.Videos)
	}
	meta, _ := json.Marshal(r.ProviderMetadata)
	if want := `{"tencent":{"lastFrameUrl":"https://vod.example.com/last.png","taskId":"task-123","usage":{"outputSeconds":4},"videos":[{"duration":4.04,"url":"https://vod.example.com/out.mp4"}]}}`; string(meta) != want {
		t.Errorf("metadata = %s", meta)
	}
}

func TestVodStatusSkipsEmptyURLsAndEmptyValues(t *testing.T) {
	s := newVideoServer(t)
	s.set("/", 200, `{"Response":{"TaskType":"AigcVideoTask","Status":"FINISH","AigcVideoTask":{"TaskId":"task-123","Status":"FINISH","ErrCode":0,"Input":{"OutputConfig":{"Resolution":""}},`+
		`"Output":{"FileInfos":[{"FileUrl":""},{"FileUrl":"https://vod.example.com/out.mp4"}],"Usage":{"OutputSeconds":4,"SpecificationUsages":[{"Specification":"","Category":""}]}}}}}`)
	r := status(t, s.model(t, "vod/VS/2.0"))
	meta, _ := json.Marshal(r.ProviderMetadata)
	if want := `{"tencent":{"taskId":"task-123","usage":{"outputSeconds":4},"videos":[{"duration":4,"url":"https://vod.example.com/out.mp4"}]}}`; r.Status != "completed" || len(r.Videos) != 1 || string(meta) != want {
		t.Errorf("result = %+v %s", r, meta)
	}
}

func TestVodStatusPendingFailedAndAborted(t *testing.T) {
	s := newVideoServer(t)
	m := s.model(t, "vod/VS/2.0")
	for _, st := range []string{"WAITING", "PROCESSING"} {
		s.set("/", 200, `{"Response":{"TaskType":"AigcVideoTask","Status":"`+st+`"}}`)
		if r := status(t, m); r.Status != "pending" {
			t.Errorf("%s: %+v", st, r)
		}
	}
	s.set("/", 200, `{"Response":{"TaskType":"AigcVideoTask","Status":"FINISH","AigcVideoTask":{"Status":"FINISH","ErrCode":70000,"ErrCodeExt":"ContentModerationFailed","Message":"Content moderation failed."}}}`)
	if r := status(t, m); r.Status != "error" || r.Error != "Video generation failed (ContentModerationFailed): Content moderation failed. (task task-123)" {
		t.Errorf("failed: %+v", r)
	}
	s.set("/", 200, `{"Response":{"TaskType":"AigcVideoTask","Status":"ABORTED"}}`)
	if r := status(t, m); r.Status != "error" {
		t.Errorf("aborted: %+v", r)
	}
}
