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

// TestR99provbSpeechNullBody: TS createBinaryResponseHandler fails a null
// body ("Response body is empty", a 204 / 205 reply); an empty 200 is empty
// audio. Go returned empty audio for both.
func TestR99provbSpeechNullBody(t *testing.T) {
	for _, status := range []int{200, 204} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).SpeechModel("grok-tts")
		_, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"})
		srv.Close()
		if (status == 204) != (err != nil && strings.Contains(err.Error(), "Response body is empty")) {
			t.Errorf("%d: err = %v", status, err)
		}
	}
}

// TestR99provbErrorInfiniteCode: TS apiErrorSchema's code is
// z.union([z.string(), z.number()]).nullish() and z.number() rejects the
// Infinity JSON.parse reads for 1e400; no union member then matches, so
// the message is the status text. Go took the body's message.
func TestR99provbErrorInfiniteCode(t *testing.T) {
	if _, ok := xaiErrorMessage([]byte(`{"error":{"message":"m","code":1e400}}`)); ok {
		t.Error("an infinite code matched the error schema")
	}
	if msg, ok := xaiErrorMessage([]byte(`{"error":{"message":"m","code":400}}`)); !ok || msg != "m" {
		t.Errorf("finite code: %q, %v", msg, ok)
	}
}

// TestR99provbVideoProgressPlatformInt: a whole-number progress beyond the
// platform int stays the float64 TS reports instead of an
// implementation-defined int conversion.
func TestR99provbVideoProgressPlatformInt(t *testing.T) {
	for _, tc := range []struct {
		in   float64
		want interface{}
	}{{1e19, 1e19}, {42, 42}, {12.5, 12.5}} {
		p := tc.in
		meta := buildXAIVideoMetadata("r", &xaiVideoStatusResponse{Progress: &p})
		if meta["progress"] != tc.want {
			t.Errorf("progress(%v) = %#v, want %#v", tc.in, meta["progress"], tc.want)
		}
	}
}

// TestR99provbVideoStatusOperationKeyExact: TS reads options.operation's
// requestId with its exact key, so a wrong-case "RequestId" is not the id.
func TestR99provbVideoStatusOperationKeyExact(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"pending"}`))
	}))
	defer srv.Close()
	_, _ = NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "grok-imagine-video").DoStatus(context.Background(),
		&provider.VideoModelV3StatusOptions{Operation: json.RawMessage(`{"RequestId":"wrong_case"}`)})
	if strings.Contains(gotPath, "wrong_case") {
		t.Fatalf("wrong-case RequestId key was read as requestId (path %s)", gotPath)
	}
}
