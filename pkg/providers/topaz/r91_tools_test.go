package topaz

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// tools-11 (TS topaz-video-model.ts:705 requireRequestId, :266): a missing
// requestId and a complete status without download URL are
// InvalidResponseDataErrors (data: the requestId value / the parsed
// status); an empty requestId proceeds, as TS (== null).
func TestR91toolsTopazVideoInvalidResponseData(t *testing.T) {
	for _, tc := range []struct {
		express string
		wantErr bool
	}{{`{}`, true}, {`{"requestId":null}`, true}, {`{"requestId":""}`, false}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(tc.express)) }))
		model := NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), VideoModelStarlightPrecise26)
		opts := defaultStartOptions()
		opts.InputReferences = []provider.VideoModelV3File{{Type: "url", URL: "https://example.com/in.mp4", MediaType: "video/mp4"}}
		_, err := model.DoStart(context.Background(), opts)
		srv.Close()
		var ird *providererrors.InvalidResponseDataError
		if tc.wantErr {
			if !errors.As(err, &ird) || ird.Message != "Topaz did not return a requestId for the video request." || ird.Data != nil {
				t.Errorf("%s: err = %v", tc.express, err)
			}
		} else if errors.As(err, &ird) {
			t.Errorf("%s: err = %v", tc.express, err)
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"complete"}`))
	}))
	defer srv.Close()
	model := NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), VideoModelStarlightPrecise26)
	_, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: []byte(`{"requestId":"r","outputContainer":"mp4"}`)})
	var ird *providererrors.InvalidResponseDataError
	if !errors.As(err, &ird) || ird.Message != "Topaz reported request r complete but returned no download URL." {
		t.Fatalf("status err = %v", err)
	}
	if data, _ := ird.Data.(map[string]interface{}); data["status"] != "complete" {
		t.Fatalf("data = %#v", ird.Data)
	}
}
