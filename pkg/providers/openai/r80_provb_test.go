package openai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r80provbJSONServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TS z.number() (files bytes, Responses batch usage) and the unvalidated
// realtime client secret accept a fractional number; Go decoded an integer
// field with encoding/json and failed the call. Go now truncates it (known
// difference "Fractional counts") like every other non-stream response.
func TestR80provbFractionalNumbersInResponses(t *testing.T) {
	t.Run("files metadata bytes", func(t *testing.T) {
		srv := r80provbJSONServer(t, `{"id":"file-1","object":"file","bytes":1024.5,"created_at":1700000000,"filename":"a.jsonl","purpose":"batch"}`)
		getter := New(Config{APIKey: "k", BaseURL: srv.URL}).Files().(provider.FileMetadataGetter)
		res, err := getter.GetFileMetadata(t.Context(), provider.FileMetadataOptions{File: types.ProviderReference{"openai": "file-1"}})
		if err != nil {
			t.Fatal(err)
		}
		if res.ByteSize == nil || *res.ByteSize != 1024 {
			t.Fatalf("ByteSize = %v", res.ByteSize)
		}
	})
	t.Run("realtime client secret expires_at", func(t *testing.T) {
		srv := r80provbJSONServer(t, `{"value":"ek_1","expires_at":1700000000.5}`)
		m := NewRealtimeModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gpt-realtime")
		res, err := m.DoCreateClientSecret(t.Context(), provider.ClientSecretOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if res.Token != "ek_1" || res.ExpiresAt == nil || *res.ExpiresAt != 1700000000 {
			t.Fatalf("result = %+v", res)
		}
	})
	t.Run("responses batch body usage", func(t *testing.T) {
		body := json.RawMessage(`{"id":"resp_1","object":"response","created_at":1700000000,"status":"completed","model":"gpt-4o","output":[{"type":"message","id":"m1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hi","annotations":[]}]}],"usage":{"input_tokens":3.5,"output_tokens":2,"total_tokens":5.5}}`)
		res, berr := convertOpenAIBatchResponseBodyBytes(body)
		if berr != nil {
			t.Fatalf("batch error: %+v", berr)
		}
		if res.Text != "hi" || res.Usage.InputTokens == nil || *res.Usage.InputTokens != 3 {
			t.Fatalf("result text %q usage %+v", res.Text, res.Usage)
		}
	})
}
