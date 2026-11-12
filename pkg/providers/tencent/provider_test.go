package tencent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from tencent-provider.test.ts.

type recordedCall struct {
	url     string
	headers http.Header
	body    string
}

type recorder struct {
	mu    sync.Mutex
	calls []recordedCall
}

func (r *recorder) record(req *http.Request) {
	b, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	r.calls = append(r.calls, recordedCall{url: req.URL.String(), headers: req.Header.Clone(), body: string(b)})
	r.mu.Unlock()
}

func (r *recorder) all() []recordedCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedCall(nil), r.calls...)
}

const chatResponse = `{"id":"chatcmpl-1","object":"chat.completion","created":1794120000,"model":"hy4","choices":[{"index":0,"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`

// providerServer serves TokenHub chat (/v1/chat/completions) and VOD (/).
func providerServer(t *testing.T) (*httptest.Server, *recorder) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/chat/completions" {
			_, _ = io.WriteString(w, chatResponse)
			return
		}
		_, _ = io.WriteString(w, `{"Response":{"TaskId":"task-1","RequestId":"req-1"}}`)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

var hiPrompt = []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hi"}}}}

func generate(t *testing.T, p *Provider, providerOptions map[string]interface{}) (*types.GenerateResult, error) {
	t.Helper()
	model, err := p.LanguageModel("hy4")
	if err != nil {
		t.Fatal(err)
	}
	return model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:          types.Prompt{Messages: hiPrompt},
		ProviderOptions: providerOptions,
	})
}

func TestCreatesTokenHubChatModels(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model, err := p.LanguageModel("hy4")
	if err != nil {
		t.Fatal(err)
	}
	if model.Provider() != "tencent.chat" || model.ModelID() != "hy4" {
		t.Errorf("model = %s %s", model.Provider(), model.ModelID())
	}
	if m, err := p.ChatModel("hy3"); err != nil || m.ModelID() != "hy3" {
		t.Errorf("ChatModel = %v %v", m, err)
	}
}

func TestCallsTokenHubChatCompletionsWithTheAPIKeyAndUsage(t *testing.T) {
	srv, rec := providerServer(t)
	result, err := generate(t, New(Config{APIKey: "test-key", BaseURL: srv.URL + "/v1"}), map[string]interface{}{
		"tencent": map[string]interface{}{"reasoningEffort": "high", "thinking": map[string]interface{}{"type": "enabled"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	c := rec.all()[0]
	if c.headers.Get("Authorization") != "Bearer test-key" {
		t.Errorf("Authorization = %q", c.headers.Get("Authorization"))
	}
	if !strings.Contains(c.headers.Get("User-Agent"), "ai-sdk-tencent/") {
		t.Errorf("User-Agent = %q", c.headers.Get("User-Agent"))
	}
	var body map[string]interface{}
	_ = json.Unmarshal([]byte(c.body), &body)
	want := map[string]interface{}{
		"model":            "hy4",
		"messages":         []interface{}{map[string]interface{}{"role": "user", "content": "Hi"}},
		"reasoning_effort": "high",
		"thinking":         map[string]interface{}{"type": "enabled"},
	}
	gotJSON, _ := json.Marshal(body)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("body = %s\nwant %s", gotJSON, wantJSON)
	}
	if result.Text != "Hello" || result.Usage.InputTokens == nil || *result.Usage.InputTokens != 3 {
		t.Errorf("result = %q usage %+v", result.Text, result.Usage)
	}
}

func TestReadsTheAPIKeyFromTheEnvironment(t *testing.T) {
	t.Setenv("TENCENT_TOKENHUB_API_KEY", "env-key")
	srv, rec := providerServer(t)
	if _, err := generate(t, New(Config{BaseURL: srv.URL + "/v1"}), nil); err != nil {
		t.Fatal(err)
	}
	if got := rec.all()[0].headers.Get("Authorization"); got != "Bearer env-key" {
		t.Errorf("Authorization = %q", got)
	}
}

func TestThrowsWhenTheAPIKeyIsMissing(t *testing.T) {
	t.Setenv("TENCENT_TOKENHUB_API_KEY", "")
	srv, _ := providerServer(t)
	_, err := generate(t, New(Config{BaseURL: srv.URL + "/v1"}), nil)
	var keyErr *providererrors.LoadAPIKeyError
	if !errors.As(err, &keyErr) {
		t.Fatalf("err = %T %v", err, err)
	}
}

func TestKeepsTheTokenHubEndpointAfterSerialization(t *testing.T) {
	t.Setenv("TENCENT_TOKENHUB_API_KEY", "test-key")
	srv, rec := providerServer(t)
	model, _ := New(Config{BaseURL: srv.URL + "/v1"}).LanguageModel("hy4")
	serializable, ok := model.(provider.SerializableModel)
	if !ok {
		t.Fatalf("%T is not serializable", model)
	}
	encoded, _ := json.Marshal(serializable.Serialize())
	var serialized provider.SerializedModel
	if err := json.Unmarshal(encoded, &serialized); err != nil {
		t.Fatal(err)
	}
	restored, err := provider.DeserializeModel(serialized)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Provider() != "tencent.chat" {
		t.Errorf("provider = %s", restored.Provider())
	}
	if _, err := restored.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: hiPrompt}}); err != nil {
		t.Fatal(err)
	}
	c := rec.all()[0]
	if c.url != "/v1/chat/completions" || c.headers.Get("Authorization") != "Bearer test-key" {
		t.Errorf("call = %s %q", c.url, c.headers.Get("Authorization"))
	}
}

func TestRespectsACustomBaseURLAndHeaders(t *testing.T) {
	srv, rec := providerServer(t)
	if _, err := generate(t, New(Config{APIKey: "test-key", BaseURL: srv.URL + "/v1/", Headers: map[string]string{"x-custom": "value"}}), nil); err != nil {
		t.Fatal(err)
	}
	c := rec.all()[0]
	if c.url != "/v1/chat/completions" || c.headers.Get("X-Custom") != "value" {
		t.Errorf("call = %s %v", c.url, c.headers)
	}
}

func TestCreatesVideoModels(t *testing.T) {
	p := New(Config{})
	if m, err := p.VideoModel("kling-video-v3"); err != nil || m.Provider() != "tencent.video" {
		t.Errorf("kling = %v %v", m, err)
	}
	if _, err := p.VideoModel("vod/VS/2.0"); err != nil {
		t.Error(err)
	}
	var nsm *providererrors.NoSuchModelError
	if _, err := p.VideoModel("not-a-model"); !errors.As(err, &nsm) {
		t.Errorf("err = %v", err)
	}
}

func startVideo(t *testing.T, p *Provider, modelID string) error {
	t.Helper()
	m, err := p.VideoModel(modelID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.(provider.VideoModelStarter).DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "A cat", N: 1},
	})
	return err
}

func TestSignsVodRequestsWithConfiguredCredentials(t *testing.T) {
	srv, rec := providerServer(t)
	err := startVideo(t, New(Config{
		SecretID: "AKIDEXAMPLE", SecretKey: "EXAMPLESECRET", SubAppID: "1500000000",
		Region: "ap-singapore", Headers: map[string]string{"x-custom": "value"}, VodBaseURL: srv.URL,
	}), "vod/VS/2.0")
	if err != nil {
		t.Fatal(err)
	}
	c := rec.all()[0]
	if !strings.HasPrefix(c.headers.Get("Authorization"), "TC3-HMAC-SHA256 Credential=AKIDEXAMPLE/") {
		t.Errorf("Authorization = %q", c.headers.Get("Authorization"))
	}
	if c.headers.Get("X-Tc-Region") != "ap-singapore" || c.headers.Get("X-Custom") != "value" || !strings.Contains(c.headers.Get("User-Agent"), "ai-sdk-tencent/") {
		t.Errorf("headers = %v", c.headers)
	}
	if !strings.Contains(c.body, `"SubAppId":1500000000`) {
		t.Errorf("body = %s", c.body)
	}
}

func TestReadsVodCredentialsFromTheEnvironment(t *testing.T) {
	t.Setenv("TENCENTCLOUD_SECRET_ID", "AKIDENV")
	t.Setenv("TENCENTCLOUD_SECRET_KEY", "ENVSECRET")
	t.Setenv("TENCENTCLOUD_VOD_SUB_APP_ID", "1500000001")
	srv, rec := providerServer(t)
	if err := startVideo(t, New(Config{VodBaseURL: srv.URL}), "vod/VS/2.0"); err != nil {
		t.Fatal(err)
	}
	c := rec.all()[0]
	if !strings.HasPrefix(c.headers.Get("Authorization"), "TC3-HMAC-SHA256 Credential=AKIDENV/") || !strings.Contains(c.body, `"SubAppId":1500000001`) {
		t.Errorf("call = %v %s", c.headers, c.body)
	}
}

func TestRejectsAnInvalidSubAppID(t *testing.T) {
	err := startVideo(t, New(Config{SecretID: "AKIDEXAMPLE", SecretKey: "EXAMPLESECRET", SubAppID: "not-a-number"}), "vod/VS/2.0")
	var settingErr *providererrors.LoadSettingError
	if !errors.As(err, &settingErr) || err.Error() != "Tencent VOD SubAppId must be a positive integer." {
		t.Fatalf("err = %v", err)
	}
}

func TestDoesNotNeedVodCredentialsForTokenHubVideoModels(t *testing.T) {
	t.Setenv("TENCENTCLOUD_SECRET_ID", "")
	if _, err := New(Config{APIKey: "test-key"}).VideoModel("kling-video-v3"); err != nil {
		t.Fatal(err)
	}
}

func TestThrowsNoSuchModelErrorForUnsupportedModelTypes(t *testing.T) {
	p := New(Config{})
	var nsm *providererrors.NoSuchModelError
	if _, err := p.EmbeddingModel("model"); !errors.As(err, &nsm) {
		t.Error(err)
	}
	if _, err := p.TextEmbeddingModel("model"); !errors.As(err, &nsm) {
		t.Error(err)
	}
	if _, err := p.ImageModel("model"); !errors.As(err, &nsm) {
		t.Error(err)
	}
}
