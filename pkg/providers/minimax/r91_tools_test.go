package minimax

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/retry"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type r91toolsCall struct {
	path    string
	headers http.Header
	body    string
}

// r91toolsImageServer mirrors minimax-image-model.test.ts's test server: the
// generation and edit endpoints plus the generated image URL, on one origin.
func r91toolsImageServer(t *testing.T, generation, edit func() (int, string)) (*httptest.Server, *[]r91toolsCall, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	var calls []r91toolsCall
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, r91toolsCall{path: r.URL.Path, headers: r.Header.Clone(), body: string(body)})
		mu.Unlock()
		switch r.URL.Path {
		case "/v1/images/generations", "/v1/images/edits":
			handler := generation
			if r.URL.Path == "/v1/images/edits" {
				handler = edit
			}
			status, payload := handler()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(strings.ReplaceAll(payload, "IMAGE_URL", srv.URL+"/generated.png")))
		case "/generated.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{1, 2, 3})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls, &mu
}

const r91toolsUsage = `{"generated_images":1,"input_tokens":30,"output_tokens":40,"total_tokens":70,"input_tokens_details":{"text_tokens":10,"image_tokens":20},"output_tokens_details":{"image_tokens":40}}`

func r91toolsJSON(t *testing.T, s string) interface{} {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func r91toolsImageModel(srv *httptest.Server) *ImageModel {
	p := New(Config{APIKey: "test-key", ImageBaseURL: srv.URL + "/v1", Headers: map[string]string{"custom-header": "value"}})
	m, _ := p.Image(ModelH3Image)
	return m.(*ImageModel)
}

func r91toolsN(n int) *int { return &n }

// TS "generates and downloads images while preserving usage".
func TestR91toolsMiniMaxImageGenerate(t *testing.T) {
	image := `{"url":"IMAGE_URL","size":"1024x1024","revised_prompt":"A red circle."}`
	srv, calls, mu := r91toolsImageServer(t, func() (int, string) {
		return 200, `{"data":[` + image + `],"usage":` + r91toolsUsage + `,"output_format":"png","request_id":"request-1"}`
	}, nil)
	seed := 1
	result, err := r91toolsImageModel(srv).DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "A red circle.", N: r91toolsN(1), Size: "1024x1024", Seed: &seed,
		Headers: map[string]string{"request-header": "request"},
		ProviderOptions: map[string]interface{}{"minimax": map[string]interface{}{
			"disablePromptEnhanced": true, "outputFormat": "png", "outputCompression": 90,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := `{"model":"MiniMax-H3-Image","prompt":"A red circle.","n":1,"size":"1024x1024","output_format":"png","output_compression":90,"disable_prompt_enhanced":true,"response_format":"url"}`
	if (*calls)[0].body != want {
		t.Errorf("body = %s\nwant %s", (*calls)[0].body, want)
	}
	h := (*calls)[0].headers
	if h.Get("Authorization") != "Bearer test-key" || h.Get("Custom-Header") != "value" || h.Get("Request-Header") != "request" {
		t.Errorf("request headers = %v", h)
	}
	if d := (*calls)[1].headers; d.Get("Authorization") != "" || d.Get("Custom-Header") != "" {
		t.Errorf("download headers = %v", d)
	}
	if len(result.Images) != 1 || !reflect.DeepEqual(result.Images[0], []byte{1, 2, 3}) {
		t.Errorf("images = %v", result.Images)
	}
	if result.Usage.InputTokens != 30 || result.Usage.OutputTokens != 40 || result.Usage.TotalTokens != 70 {
		t.Errorf("usage = %+v", result.Usage)
	}
	gotMeta, _ := json.Marshal(result.ProviderMetadata["minimax"])
	wantMeta := r91toolsJSON(t, `{"images":[{"size":"1024x1024","revisedPrompt":"A red circle.","usage":`+r91toolsUsage+`,"outputFormat":"png","requestId":"request-1"}]}`)
	if !reflect.DeepEqual(r91toolsJSON(t, string(gotMeta)), wantMeta) {
		t.Errorf("metadata = %s", gotMeta)
	}
	if !reflect.DeepEqual(result.Warnings, []types.Warning{{Type: "unsupported", Feature: "seed"}}) {
		t.Errorf("warnings = %+v", result.Warnings)
	}
}

// TS "maps aspect ratio, area, and batch size without supplying a pixel size".
func TestR91toolsMiniMaxImageAspectAreaBatch(t *testing.T) {
	image := `{"url":"IMAGE_URL","size":"1024x1024","revised_prompt":"A red circle."}`
	batchUsage := strings.Replace(r91toolsUsage, `"generated_images":1`, `"generated_images":2`, 1)
	srv, calls, mu := r91toolsImageServer(t, func() (int, string) {
		return 200, `{"data":[` + image + `,` + image + `],"usage":` + batchUsage + `}`
	}, nil)
	result, err := r91toolsImageModel(srv).DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "A red circle.", N: r91toolsN(2), AspectRatio: "16:9",
		ProviderOptions: map[string]interface{}{"minimax": map[string]string{"area": "2K"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := `{"model":"MiniMax-H3-Image","prompt":"A red circle.","n":2,"aspect_ratio":"16:9","area":"2K","response_format":"url"}`; (*calls)[0].body != want {
		t.Errorf("body = %s", (*calls)[0].body)
	}
	gotMeta, _ := json.Marshal(result.ProviderMetadata["minimax"])
	wantMeta := r91toolsJSON(t, `{"images":[{"size":"1024x1024","revisedPrompt":"A red circle.","usage":`+batchUsage+`},{"size":"1024x1024","revisedPrompt":"A red circle."}]}`)
	if !reflect.DeepEqual(r91toolsJSON(t, string(gotMeta)), wantMeta) {
		t.Errorf("metadata = %s", gotMeta)
	}
}

// TS "edits binary and URL references and omits the generation-only prompt option".
func TestR91toolsMiniMaxImageEdit(t *testing.T) {
	image := `{"url":"IMAGE_URL","size":"1024x1024","revised_prompt":"A red circle."}`
	srv, calls, mu := r91toolsImageServer(t, nil, func() (int, string) { return 200, `{"data":[` + image + `],"usage":` + r91toolsUsage + `}` })
	result, err := r91toolsImageModel(srv).DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "A red circle.", N: r91toolsN(1),
		Files: []provider.ImageFile{
			{Type: "file", Data: []byte{1, 2, 3}, MediaType: "image/png"},
			{Type: "url", URL: "https://example.com/reference.png"},
		},
		ProviderOptions: map[string]interface{}{"minimax": MiniMaxImageModelOptions{DisablePromptEnhanced: func() *bool { b := true; return &b }()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if (*calls)[0].path != "/v1/images/edits" {
		t.Errorf("path = %s", (*calls)[0].path)
	}
	if want := `{"model":"MiniMax-H3-Image","prompt":"A red circle.","n":1,"images":[{"image_url":"data:image/png;base64,AQID"},{"image_url":"https://example.com/reference.png"}],"response_format":"url"}`; (*calls)[0].body != want {
		t.Errorf("body = %s", (*calls)[0].body)
	}
	if !reflect.DeepEqual(result.Warnings, []types.Warning{{Type: "unsupported", Feature: "disablePromptEnhanced", Details: "MiniMax image edits always apply prompt enhancement."}}) {
		t.Errorf("warnings = %+v", result.Warnings)
	}
}

// TS "returns successful images and reports per-image failures without
// losing request usage".
func TestR91toolsMiniMaxImagePartialFailure(t *testing.T) {
	srv, _, _ := r91toolsImageServer(t, func() (int, string) {
		return 200, `{"data":[{"url":"IMAGE_URL","size":"1024x1024","revised_prompt":"A red circle."},{"error":{"code":"1027","message":"Content safety check failed."}}],"usage":` + r91toolsUsage + `}`
	}, nil)
	result, err := r91toolsImageModel(srv).DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "A red circle.", N: r91toolsN(2)})
	if err != nil {
		t.Fatal(err)
	}
	images := result.ProviderMetadata["minimax"].(map[string]interface{})["images"].([]interface{})
	if len(result.Images) != 1 || len(images) != 1 || images[0].(map[string]interface{})["usage"] == nil {
		t.Errorf("images = %d, metadata = %v", len(result.Images), images)
	}
	if !reflect.DeepEqual(result.Warnings, []types.Warning{{Type: "other", Message: "Image 2 failed: Content safety check failed."}}) {
		t.Errorf("warnings = %+v", result.Warnings)
	}
}

// TS "rejects masks before sending an unmasked edit" and "preserves the
// provider error message"; the provider factories (minimax-provider.test.ts).
func TestR91toolsMiniMaxImageMaskErrorAndFactories(t *testing.T) {
	srv, calls, mu := r91toolsImageServer(t, func() (int, string) {
		return 402, `{"error":{"message":"Insufficient balance (1008)"}}`
	}, nil)
	m := r91toolsImageModel(srv)
	_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x", Mask: &provider.ImageFile{Type: "file", Data: []byte{1}, MediaType: "image/png"}})
	var unsupported *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &unsupported) || !strings.Contains(err.Error(), "MiniMax image masks") {
		t.Fatalf("mask err = %v", err)
	}
	mu.Lock()
	if len(*calls) != 0 {
		t.Fatalf("calls = %d", len(*calls))
	}
	mu.Unlock()
	_, err = m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x", N: r91toolsN(1)})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "Insufficient balance (1008)" || pe.StatusCode != 402 {
		t.Fatalf("err = %v", err)
	}
	p := New(Config{})
	for _, get := range []func(string) (provider.ImageModel, error){p.Image, p.ImageModel} {
		model, err := get(ModelH3ImageHighspeed)
		if err != nil {
			t.Fatal(err)
		}
		if model.Provider() != "minimax.image" || model.SpecificationVersion() != "v4" ||
			*provider.ImageModelSupportsFileInputs(model) != true || *provider.ImageModelSupportsMaskInputs(model) != false {
			t.Fatalf("model = %+v", model)
		}
	}
	if m.MaxImagesPerCall() != 10 {
		t.Fatal("maxImagesPerCall")
	}
	_, err = m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x", ProviderOptions: map[string]interface{}{"minimax": map[string]interface{}{"outputCompression": int64(101)}}})
	if err == nil || !strings.Contains(err.Error(), "invalid minimax provider options") {
		t.Fatalf("invalid option err = %v", err)
	}
}

// tools-02 (TS minimax-video-model.ts:581, postJsonToApi): a transport
// failure of the submit call is the retryable "Cannot connect to API" error,
// not a non-retryable *minimax.Error.
func TestR91toolsMiniMaxVideoSubmitNetworkErrorRetryable(t *testing.T) {
	var mu sync.Mutex
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		attempts++
		mu.Unlock()
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	t.Cleanup(srv.Close)
	model := newVideoModel(New(Config{APIKey: "k", VideoBaseURL: srv.URL}), ModelH3)
	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "a cat", PromptSet: true, N: 1}})
	var mmErr *Error
	if err == nil || errors.As(err, &mmErr) {
		t.Fatalf("err = %v (%T), want the transport error", err, err)
	}
	if !strings.Contains(err.Error(), "Cannot connect to API") || !retry.IsRetryable(err) {
		t.Fatalf("err = %v, want a retryable Cannot connect to API error", err)
	}
}

// tools-08 (TS minimaxVideoErrorSchema): the API error carries the schema
// output as Data (undeclared keys stripped); a non-matching body gives the
// status text and no data.
func TestR91toolsMiniMaxVideoErrorData(t *testing.T) {
	m := &VideoModel{}
	cases := []struct {
		body    string
		message string
		data    interface{}
	}{
		{`{"error":{"message":"m","x":1},"request_id":"r"}`, "m", map[string]interface{}{"error": map[string]interface{}{"message": "m"}, "request_id": "r"}},
		{`{"type":"t","error":{"http_code":400,"type":null},"extra":true}`, "MiniMax video generation error", map[string]interface{}{"type": "t", "error": map[string]interface{}{"http_code": float64(400), "type": nil}}},
		{`{"type":5}`, "Bad Request", nil},
		{`{"error":"x"}`, "Bad Request", nil},
	}
	for _, tc := range cases {
		err := m.parseAPIError([]byte(tc.body), 400, nil)
		var e *Error
		if !errors.As(err, &e) || e.Message != tc.message || !reflect.DeepEqual(e.Data, tc.data) {
			t.Fatalf("%s: %+v", tc.body, e)
		}
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || !reflect.DeepEqual(pe.Data, tc.data) {
			t.Fatalf("%s: unwrapped ProviderError data = %#v", tc.body, pe)
		}
	}
}

// tools-09 (TS minimax-video-model.ts:524): !Number.isInteger(Infinity) and
// (NaN) is true, so both warn "rounded to Infinity / NaN"; Infinity is then
// clamped to 15 and NaN is sent as null (JSON.stringify(NaN)).
func TestR91toolsMiniMaxVideoDurationNonFinite(t *testing.T) {
	for _, tc := range []struct {
		duration float64
		warnings []string
		wire     string
	}{
		{math.Inf(1), []string{"rounded to Infinity", "clamped to 15"}, `"duration":15`},
		{math.NaN(), []string{"rounded to NaN"}, `"duration":null`},
	} {
		var body string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			body = string(b)
			_, _ = w.Write([]byte(`{"task_id":"t1"}`))
		}))
		model := newVideoModel(New(Config{APIKey: "k", VideoBaseURL: srv.URL}), ModelH3)
		d := tc.duration
		res, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "a cat", PromptSet: true, N: 1, Duration: &d}})
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, w := range res.Warnings {
			if w.Feature == "duration" {
				got = append(got, w.Details)
			}
		}
		if len(got) != len(tc.warnings) {
			t.Fatalf("%v: warnings %q", tc.duration, got)
		}
		for i, want := range tc.warnings {
			if !strings.Contains(got[i], want) {
				t.Errorf("%v: warning %q lacks %q", tc.duration, got[i], want)
			}
		}
		if !strings.Contains(body, tc.wire) {
			t.Errorf("%v: body %s lacks %s", tc.duration, body, tc.wire)
		}
	}
}
