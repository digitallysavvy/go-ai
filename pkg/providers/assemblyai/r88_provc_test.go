package assemblyai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS assemblyai-error.test.ts / assemblyai-transcription-model.test.ts
// (c87b19ee75): the documented flat error shape gives its message.
func TestR88provcAssemblyAIFlatErrorShape(t *testing.T) {
	for _, tc := range []struct {
		status          int
		body, msg, code string
	}{
		{401, `{"error":"Invalid API key"}`, "Invalid API key", ""},
		{400, `{"error":"format_text must be a Boolean","status":"error"}`, "format_text must be a Boolean", ""},
		{401, `{"error":"Invalid API key","status":"unexpected"}`, "Invalid API key", ""},
		{429, `{"error":{"message":"Resource has been exhausted","code":429}}`, "Resource has been exhausted", "429"},
		{500, `{"foo":"bar"}`, "Internal Server Error", ""},
		{502, `not json`, "Bad Gateway", ""},
		{400, `{"error":null}`, "Bad Request", ""},
		{400, `{"error":""}`, "", ""},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).TranscriptionModel(ModelUniversal35Pro)
		_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a")})
		srv.Close()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != tc.msg || pe.StatusCode != tc.status || pe.ErrorCode != tc.code || pe.ResponseBody != tc.body {
			t.Fatalf("%s: err = %#v", tc.body, err)
		}
	}
}
