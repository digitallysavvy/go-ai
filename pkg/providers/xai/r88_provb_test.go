package xai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const r88provbSignedURL = "https://storage.example.com/video.mp4?signature=secret"

// TS 2e44144812 (xai-video-model.test.ts "upload delivery"): the signed URL
// is sent as output.upload_url (typed or the raw v0.5.2 form), the result is
// an uploaded video without a URL, and neither the operation reference, the
// metadata nor errors carry the signed URL.
func TestR88provbXAIVideoUploadDelivery(t *testing.T) {
	var bodies []map[string]interface{}
	statusBody := `{"status":"done","video":{"url":"https://vidgen.x.ai/output/video-001.mp4","duration":5}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			var b map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&b)
			bodies = append(bodies, b)
			_, _ = io.WriteString(w, `{"request_id":"req-123"}`)
			return
		}
		_, _ = io.WriteString(w, statusBody)
	}))
	defer srv.Close()
	m := NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "grok-imagine-video")
	ctx := context.Background()

	for _, output := range []map[string]interface{}{{"uploadUrl": r88provbSignedURL}, {"upload_url": r88provbSignedURL}} {
		bodies = nil
		start, err := m.DoStart(ctx, &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
			Prompt: "p", ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"output": output}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := json.Marshal(bodies[0]["output"]); string(got) != `{"upload_url":"`+r88provbSignedURL+`"}` {
			t.Errorf("%v: body output = %s", output, got)
		}
		if string(start.Operation) != `{"requestId":"req-123","hasUploadUrl":true}` {
			t.Errorf("operation = %s", start.Operation)
		}
		status, err := m.DoStatus(ctx, &provider.VideoModelV3StatusOptions{Operation: start.Operation})
		if err != nil {
			t.Fatal(err)
		}
		if status.Status != provider.VideoOperationStatusCompleted || len(status.Videos) != 1 ||
			status.Videos[0].Type != provider.VideoDataTypeUploaded || status.Videos[0].URL != "" || status.Videos[0].MediaType != "video/mp4" {
			t.Errorf("status = %+v", status)
		}
		if meta, _ := status.ProviderMetadata["xai"].(map[string]interface{}); meta["videoUrl"] != nil || meta["requestId"] != "req-123" {
			t.Errorf("metadata = %v", status.ProviderMetadata)
		}
	}

	// A failed upload job does not echo the provider's error details.
	statusBody = `{"status":"failed","error":{"message":"` + r88provbSignedURL + `"}}`
	status, err := m.DoStatus(ctx, &provider.VideoModelV3StatusOptions{Operation: json.RawMessage(`{"requestId":"req-123","hasUploadUrl":true}`)})
	if err != nil || status.Error != "Video generation failed." {
		t.Errorf("failed status = %+v, %v", status, err)
	}

	// Invalid destinations fail before the request, without the URL.
	for _, bad := range []interface{}{"http://storage.example.com/v.mp4", "https://user:pw@storage.example.com/v.mp4", "https://storage.example.com/v.mp4#frag", "not a url", 5, map[string]interface{}{}} {
		bodies = nil
		output := map[string]interface{}{"uploadUrl": bad}
		if m, ok := bad.(map[string]interface{}); ok {
			output = m
		}
		_, err := m.DoStart(ctx, &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
			Prompt: "p", ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"output": output}},
		}})
		var ia *providererrors.InvalidArgumentError
		if !errors.As(err, &ia) || !strings.Contains(err.Error(), "Invalid xAI video provider options for upload delivery.") || len(bodies) != 0 {
			t.Errorf("%v: err = %v (%d requests)", bad, err, len(bodies))
		}
	}
	// Any other option error is reported the same way when output is set.
	_, err = m.DoStart(ctx, &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
		Prompt: "p", ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"output": map[string]interface{}{"uploadUrl": r88provbSignedURL}, "resolution": "4k"}},
	}})
	if err == nil || strings.Contains(err.Error(), "signature") || !strings.Contains(err.Error(), "upload delivery") {
		t.Errorf("other option error = %v", err)
	}
}

// TS 2e44144812 ("redacts signed URLs from start API errors while
// preserving retryability"): API errors of an upload delivery call carry no
// request, response body or cause.
func TestR88provbXAIVideoUploadErrorRedaction(t *testing.T) {
	status := http.StatusTooManyRequests
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error":"bad destination `+r88provbSignedURL+`"}`)
	}))
	defer srv.Close()
	m := NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "grok-imagine-video")
	_, err := m.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
		Prompt: "p", ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"output": map[string]interface{}{"uploadUrl": r88provbSignedURL}}},
	}})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "xAI video upload request failed." || pe.StatusCode != 429 || !pe.IsRetryable() || pe.ResponseBody != "" || pe.Cause != nil {
		t.Fatalf("start err = %#v", err)
	}
	if strings.Contains(err.Error(), "signature") {
		t.Fatalf("start error leaks the signed URL: %v", err)
	}
	status = http.StatusBadRequest
	_, err = m.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: json.RawMessage(`{"requestId":"req-1","hasUploadUrl":true}`)})
	if !errors.As(err, &pe) || pe.Message != "xAI video upload status request failed." || pe.StatusCode != 400 || pe.IsRetryable() || strings.Contains(err.Error(), "signature") {
		t.Fatalf("status err = %#v", err)
	}
}

// TS 41465fd1b6 (xai-transcription-model.ts): word timings are reported in
// Words; a segment request warns, and a word request answered without words
// warns.
func TestR88provbXAITranscriptionWords(t *testing.T) {
	body := `{"text":"Hello","language":"en","duration":1,"words":[{"text":"Hello","start":0,"end":1}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()
	m := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "grok-stt")
	res, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav", TimestampGranularities: []string{"segment"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Words) != 1 || res.Words[0] != (types.TranscriptionWord{Text: "Hello", StartSecond: 0, EndSecond: 1}) {
		t.Errorf("words = %+v", res.Words)
	}
	if len(res.Warnings) != 1 || res.Warnings[0].Details != "xAI transcription returns word timings and does not provide separate caption segments." {
		t.Errorf("warnings = %+v", res.Warnings)
	}
	body = `{"text":" Hello ","words":[]}`
	res, err = m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav", TimestampGranularities: []string{"word"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Words != nil || len(res.Warnings) != 1 || res.Warnings[0].Type != "other" || res.Warnings[0].Message != "The xAI transcription response omitted the requested word timings." {
		t.Errorf("words %+v, warnings %+v", res.Words, res.Warnings)
	}
}
