package topaz

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
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

func r92toolsTopazExpressServer(t *testing.T, bodies *[]map[string]interface{}) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		*bodies = append(*bodies, body)
		_, _ = w.Write([]byte(`{"requestId":"r1"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func r92toolsTopazURLStart(resolution string) *provider.VideoModelV3StartOptions {
	return &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
		N: 1, Resolution: resolution,
		InputReferences: []provider.VideoModelV3File{{Type: "url", URL: "https://example.com/in.mp4", MediaType: "video/mp4"}},
	}}
}

// TS topaz-video-model.ts doStart resolves config.headers (loadApiKey) right
// after parsing the provider options and before the input checks, once per
// call: a missing key wins over a missing input video.
func TestR92toolsTopazVideoKeyResolvedBeforeInputChecks(t *testing.T) {
	t.Setenv("TOPAZ_API_KEY", "")
	model := NewVideoModel(New(Config{BaseURL: "http://127.0.0.1:1"}), VideoModelStarlightPrecise26)
	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{N: 1}})
	var keyErr *providererrors.LoadAPIKeyError
	if !errors.As(err, &keyErr) {
		t.Fatalf("err = %T %v, want a LoadAPIKeyError", err, err)
	}

	var bodies []map[string]interface{}
	srv := r92toolsTopazExpressServer(t, &bodies)
	calls := 0
	model = NewVideoModel(New(Config{BaseURL: srv.URL, APIKeyFunc: func(context.Context) (string, error) {
		calls++
		return "k", nil
	}}), VideoModelStarlightPrecise26)
	if _, err := model.DoStart(context.Background(), r92toolsTopazURLStart("1280x720")); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("APIKeyFunc calls = %d, want 1", calls)
	}
}

// TS parseResolution is resolution.split('x').map(Number): a fraction is
// kept, non-numeric text is NaN (not nullish, sent as null), and a missing
// height part is undefined.
func TestR92toolsTopazVideoResolutionJSNumbers(t *testing.T) {
	for _, tc := range []struct {
		resolution string
		want       interface{}
		wantErr    bool
	}{
		{"1920.5x1080", map[string]interface{}{"width": 1920.5, "height": 1080.0}, false},
		{"abcx1080", map[string]interface{}{"width": nil, "height": 1080.0}, false},
		{"1920", nil, true},
	} {
		var bodies []map[string]interface{}
		srv := r92toolsTopazExpressServer(t, &bodies)
		model := NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), VideoModelStarlightPrecise26)
		_, err := model.DoStart(context.Background(), r92toolsTopazURLStart(tc.resolution))
		if tc.wantErr {
			if err == nil || !strings.Contains(err.Error(), "Topaz needs the output resolution") {
				t.Fatalf("%s: err = %v", tc.resolution, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", tc.resolution, err)
		}
		got := bodies[0]["output"].(map[string]interface{})["resolution"]
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: resolution = %#v, want %#v", tc.resolution, got, tc.want)
		}
	}
}
