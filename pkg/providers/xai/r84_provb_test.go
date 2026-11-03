package xai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// The TS case suites of this package evaluate the extended assertion forms.
func init() { tsfixture.EnableExtendedAsserts() }

// r84provbVideoStatus serves one status body with the given HTTP status and
// returns what DoStatus made of it.
func r84provbVideoStatus(t *testing.T, code int, body []byte) *provider.VideoModelV3OperationStatusResult {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	model := NewVideoModel(New(Config{APIKey: "k", BaseURL: server.URL}), "grok-imagine-video")
	res, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: json.RawMessage(`{"requestId":"r1"}`)})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// TS readPendingBody (xai-video-model.ts 824-870) decodes the 202 body with
// TextDecoder, which drops a leading UTF-8 BOM, and createJsonResponseHandler
// parses response.text(), which does the same. A BOM-prefixed status body is
// therefore parsed, not treated as unparseable.
func TestR84provbVideoStatusBodyDecodedLikeTextDecoder(t *testing.T) {
	bom := []byte{0xEF, 0xBB, 0xBF}
	failed := append(append([]byte{}, bom...), []byte(`{"status":"failed","error":{"message":"boom"}}`)...)
	for _, code := range []int{http.StatusAccepted, http.StatusOK} {
		res := r84provbVideoStatus(t, code, failed)
		if res.Status != provider.VideoOperationStatusError {
			t.Fatalf("HTTP %d: status = %q, want error", code, res.Status)
		}
		if res.Error != "Video generation failed: boom" {
			t.Fatalf("HTTP %d: error = %q, want TS text", code, res.Error)
		}
	}

	// A 202 body that is only JS whitespace after decoding is pending.
	res := r84provbVideoStatus(t, http.StatusAccepted, append(append([]byte{}, bom...), []byte("  \n")...))
	if res.Status != provider.VideoOperationStatusPending {
		t.Fatalf("whitespace 202: status = %q, want pending", res.Status)
	}
}

// TS doStatus tests the status fields with == null / ??, so an empty string
// is a value: error.message "" is the detail (not error.code), status ""
// with a video URL is still pending, and video.url "" does not fall back to
// file_output.public_url (the completed job then has no video URL).
func TestR84provbVideoStatusEmptyStringsAreValues(t *testing.T) {
	res := r84provbVideoStatus(t, http.StatusOK, []byte(`{"status":"failed","error":{"message":"","code":"c1"}}`))
	if res.Status != provider.VideoOperationStatusError || res.Error != "Video generation failed: " {
		t.Fatalf("failed with empty message: %q %q", res.Status, res.Error)
	}
	res = r84provbVideoStatus(t, http.StatusOK, []byte(`{"status":"failed","error":{"message":null,"code":"c1"}}`))
	if res.Error != "Video generation failed: c1" {
		t.Fatalf("failed with null message: %q", res.Error)
	}
	res = r84provbVideoStatus(t, http.StatusOK, []byte(`{"status":"","video":{"url":"https://x.test/v.mp4"}}`))
	if res.Status != provider.VideoOperationStatusPending {
		t.Fatalf("empty status with url: %q, want pending", res.Status)
	}
	res = r84provbVideoStatus(t, http.StatusOK, []byte(`{"status":"done","video":{"url":"","file_output":{"file_id":"f","filename":"a.mp4","public_url":"https://x.test/a.mp4"}}}`))
	if res.Status != provider.VideoOperationStatusError || res.Error != "Video generation completed but no video URL was returned." {
		t.Fatalf("done with empty url: %q %q", res.Status, res.Error)
	}
	res = r84provbVideoStatus(t, http.StatusOK, []byte(`{"status":"done","video":{"url":null,"file_output":{"file_id":"f","filename":"a.mp4","public_url":"https://x.test/a.mp4"}}}`))
	if res.Status != provider.VideoOperationStatusCompleted || len(res.Videos) != 1 || res.Videos[0].URL != "https://x.test/a.mp4" {
		t.Fatalf("done with null url: %+v", res)
	}
}

// TS doStart throws AISDKError "No request_id returned from xAI API." (the
// response is not part of the message).
func TestR84provbVideoMissingRequestIDMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":null}`))
	}))
	defer server.Close()
	model := NewVideoModel(New(Config{APIKey: "k", BaseURL: server.URL}), "grok-imagine-video")
	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "p"}})
	if err == nil {
		t.Fatal("want an error")
	}
	if got := err.Error(); !strings.HasSuffix(got, "No request_id returned from xAI API.") {
		t.Fatalf("error = %q", got)
	}
}

// TS buildRequestBody sets body.video = { url: xaiOptions!.videoUrl } for
// every edit/extension request, so an explicit mode without videoUrl sends
// "video": {} (JSON.stringify drops the undefined url).
func TestR84provbVideoModeWithoutVideoURLSendsEmptyVideo(t *testing.T) {
	for _, mode := range []string{"edit-video", "extend-video"} {
		mode := mode
		model := NewVideoModel(New(Config{APIKey: "k"}), "grok-imagine-video")
		opts := &provider.VideoModelV3CallOptions{Prompt: "p", ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"mode": mode}}}
		provOpts, extra, err := extractVideoProviderOptions(opts.ProviderOptions)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := model.buildRequestBody(opts, provOpts, extra, mode == "edit-video", mode == "extend-video", false)
		b, _ := json.Marshal(body["video"])
		if string(b) != `{}` {
			t.Fatalf("%s: video = %s, want {}", mode, b)
		}
	}
}

// TS xai-image-model.ts: hasAllBase64 is every(b64_json != null), so an
// empty b64_json is image data (no download), and revisedPrompt is set only
// for a truthy revised_prompt (an empty one is left out); the batch
// conversion keeps revised_prompt != null.
func TestR84provbImageEmptyStringsFollowTS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"","url":"https://x.test/never.png","revised_prompt":""},{"b64_json":"aGk=","revised_prompt":"p"}]}`))
	}))
	defer server.Close()
	model := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), "grok-imagine-image")
	res, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Base64Images) != 2 || res.Base64Images[0] != "" || res.Base64Images[1] != "aGk=" {
		t.Fatalf("base64 images = %#v", res.Base64Images)
	}
	meta := res.ProviderMetadata["xai"].(XAIImageMetadata)
	if meta.Images[0].RevisedPrompt != nil || meta.Images[1].RevisedPrompt == nil || *meta.Images[1].RevisedPrompt != "p" {
		t.Fatalf("images metadata = %+v", meta.Images)
	}
}

// TS XaiBatch#convertImageBatchResponse: an empty b64_json != null is data,
// so it is decoded instead of downloading the url.
func TestR84provbBatchImageEmptyBase64IsData(t *testing.T) {
	var resp xaiImageResponse
	if err := json.Unmarshal([]byte(`{"data":[{"b64_json":"","url":"https://x.test/never.png"}]}`), &resp); err != nil {
		t.Fatal(err)
	}
	res, err := NewBatch(New(Config{APIKey: "k"})).convertImageBatchResponse(context.Background(), resp)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Base64Images) != 1 || res.Base64Images[0] != "" {
		t.Fatalf("base64 images = %#v", res.Base64Images)
	}
}

// TS XaiRealtimeModel.doCreateClientSecret: ?model=${encodeURIComponent(modelId)}.
func TestR84provbRealtimeURLUsesEncodeURIComponent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"value": "secret"})
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	model := NewRealtimeModel(p, "grok voice*")
	secret, err := model.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(secret.URL, "/v1/realtime?model=grok%20voice*") {
		t.Fatalf("url = %q", secret.URL)
	}
}
