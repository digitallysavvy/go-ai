package xai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
