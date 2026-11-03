package xai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

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
