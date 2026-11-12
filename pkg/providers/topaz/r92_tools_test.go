package topaz

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS InvalidResponseDataError data is the zod-parsed response
// (topaz-video-model.ts:267 `data: status`, topaz-image-model.ts:110
// `data: submitResponse`, :405 `data: downloadResponse`): z.object strips the
// undeclared keys, nested objects (estimates, download) included.
func TestR92toolsTopazInvalidResponseDataIsSchemaOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"complete","requestId":"r","message":null,"estimates":{"cost":[1,2],"extra":1},"download":{"expiresAt":5,"extra":2},"other":true}`))
	}))
	defer srv.Close()
	model := NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), VideoModelStarlightPrecise26)
	_, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: []byte(`{"requestId":"r","outputContainer":"mp4"}`)})
	var ird *providererrors.InvalidResponseDataError
	if !errors.As(err, &ird) {
		t.Fatalf("status err = %v", err)
	}
	want := map[string]interface{}{
		"status": "complete", "message": nil,
		"estimates": map[string]interface{}{"cost": []interface{}{1.0, 2.0}},
		"download":  map[string]interface{}{"expiresAt": 5.0},
	}
	if !reflect.DeepEqual(ird.Data, want) {
		t.Fatalf("status data = %#v, want %#v", ird.Data, want)
	}

	for _, tc := range []struct {
		name, submit, download string
		want                   interface{}
	}{
		{"submit", `{"process_id":null,"extra":1}`, "", map[string]interface{}{"process_id": nil}},
		{"download", `{"process_id":"p"}`, `{"extra":1}`, map[string]interface{}{}},
	} {
		mux := http.NewServeMux()
		mux.HandleFunc("/image/v1/enhance-gen/async", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(tc.submit)) })
		mux.HandleFunc("/image/v1/status/p", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"status":"Completed"}`)) })
		mux.HandleFunc("/image/v1/download/p", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(tc.download)) })
		imgSrv := httptest.NewServer(mux)
		m := NewImageModel(New(Config{APIKey: "k", BaseURL: imgSrv.URL}), ImageModelWonder35)
		_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
			Files:           []provider.ImageFile{inputImageFile()},
			ProviderOptions: map[string]interface{}{"topaz": map[string]interface{}{"pollIntervalMillis": 1}},
		})
		imgSrv.Close()
		var imgErr *providererrors.InvalidResponseDataError
		if !errors.As(err, &imgErr) || !reflect.DeepEqual(imgErr.Data, tc.want) {
			t.Fatalf("%s: err = %v, data = %#v, want %#v", tc.name, err, imgErr, tc.want)
		}
	}
}
