package azure

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	stdhttp "net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Ported from TS #22094 (azure-mai-image-model.test.ts).

const maiImageResponse = `{"created":1791229019,"model":"MAI-Image-2.6-Flash","size":"768x768","usage":{"num_output_tokens":576,"num_input_text_tokens":10,"num_input_image_tokens":0},"data":[{"b64_json":"iVBORw0KGgo="}]}`

type maiCapture struct {
	mu       sync.Mutex
	paths    []string
	apiKey   string
	jsonBody map[string]interface{}
	form     map[string][]string
	files    []string
	fileType []string
}

func newMaiServer(t *testing.T, respond func(w stdhttp.ResponseWriter, r *stdhttp.Request)) (*httptest.Server, *maiCapture) {
	t.Helper()
	c := &maiCapture{}
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		c.mu.Lock()
		c.paths = append(c.paths, r.URL.Path)
		c.apiKey = r.Header.Get("api-key")
		mt, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if mt == "multipart/form-data" {
			mr := multipart.NewReader(r.Body, params["boundary"])
			c.form = map[string][]string{}
			for {
				part, err := mr.NextPart()
				if err != nil {
					break
				}
				data, _ := io.ReadAll(part)
				if part.FileName() != "" {
					c.files = append(c.files, part.FileName())
					c.fileType = append(c.fileType, part.Header.Get("Content-Type"))
				} else {
					c.form[part.FormName()] = append(c.form[part.FormName()], string(data))
				}
			}
		} else {
			c.jsonBody = map[string]interface{}{}
			_ = json.NewDecoder(r.Body).Decode(&c.jsonBody)
		}
		c.mu.Unlock()
		if respond != nil {
			respond(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(maiImageResponse))
	}))
	t.Cleanup(server.Close)
	return server, c
}

func maiImageModel(t *testing.T, server *httptest.Server, id string) provider.ImageModel {
	t.Helper()
	p, err := New(Config{APIKey: "test-key", ResourceName: "test-resource", MaiBaseURL: server.URL + "/mai/v1/", BaseURL: server.URL + "/openai"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := p.ImageModel(id)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAzureMaiImage_RoutingByModelID(t *testing.T) {
	cases := []struct {
		id, api string
		wantMai bool
	}{
		{"MAI-Image-2.6", "", true},
		{"mai-image-2.6-flash", "", true},
		{"MAI-Image-2.5-Pro", "", true},
		{"gpt-image-1", "", false},
		{"mai-image-custom", "", false},
		{"MAI-Image-2.6", "openai", false},
		{"my-mai-deployment", "mai", true},
	}
	for _, tc := range cases {
		t.Run(tc.id+"/"+tc.api, func(t *testing.T) {
			server, c := newMaiServer(t, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				w.Header().Set("Content-Type", "application/json")
				if tc.wantMai {
					_, _ = w.Write([]byte(maiImageResponse))
				} else {
					_, _ = w.Write([]byte(`{"data":[{"b64_json":"aGk="}]}`))
				}
			})
			opts := &provider.ImageGenerateOptions{Prompt: "A red apple"}
			if tc.api != "" {
				opts.ProviderOptions = map[string]interface{}{"azure": map[string]interface{}{"api": tc.api}}
			}
			if _, err := maiImageModel(t, server, tc.id).DoGenerate(context.Background(), opts); err != nil {
				t.Fatal(err)
			}
			want := "/openai/images/generations"
			if tc.wantMai {
				want = "/mai/v1/images/generations"
			}
			if len(c.paths) != 1 || c.paths[0] != want {
				t.Errorf("paths = %v, want %s", c.paths, want)
			}
		})
	}
}

func TestAzureMaiImage_Capabilities(t *testing.T) {
	server, _ := newMaiServer(t, nil)
	mai := maiImageModel(t, server, "MAI-Image-2.6")
	if f := provider.ImageModelSupportsFileInputs(mai); f == nil || !*f {
		t.Errorf("MAI supportsFileInputs = %v, want true", f)
	}
	if mk := provider.ImageModelSupportsMaskInputs(mai); mk == nil || *mk {
		t.Errorf("MAI supportsMaskInputs = %v, want false", mk)
	}
	other := maiImageModel(t, server, "my-dalle-deployment")
	if f := provider.ImageModelSupportsFileInputs(other); f != nil {
		t.Errorf("non-MAI supportsFileInputs = %v, want nil", *f)
	}
}

func TestAzureMaiImage_WarnsAboutMaiOptionsOnOpenAI(t *testing.T) {
	server, _ := newMaiServer(t, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"aGk="}]}`))
	})
	result, err := maiImageModel(t, server, "gpt-image-1").DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:          "A red apple",
		ProviderOptions: map[string]interface{}{"azure": map[string]interface{}{"webGrounding": true, "someFutureOption": "v"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range result.Warnings {
		if w.Type == "unsupported" && w.Feature == "providerOptions.azure.webGrounding" && w.Details == "This option requires the MAI image API." {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %#v", result.Warnings)
	}
}

func TestAzureMaiImage_GenerationsRequestAndResult(t *testing.T) {
	server, c := newMaiServer(t, nil)
	result, err := maiImageModel(t, server, "MAI-Image-2.6-Flash").DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "A red apple",
		Size:   "1536x1024",
		ProviderOptions: map[string]interface{}{"azure": map[string]interface{}{
			"autoAspectRatio": true, "webGrounding": false,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{
		"model": "MAI-Image-2.6-Flash", "prompt": "A red apple",
		"width": float64(1536), "height": float64(1024),
		"auto_aspect_ratio": true, "web_grounding": false,
	}
	if !reflect.DeepEqual(c.jsonBody, want) {
		t.Errorf("body = %#v, want %#v", c.jsonBody, want)
	}
	if c.apiKey != "test-key" {
		t.Errorf("api-key = %q", c.apiKey)
	}
	if len(result.Images) != 1 || result.Base64Image != "iVBORw0KGgo=" {
		t.Errorf("images = %v / %q", result.Images, result.Base64Image)
	}
	if result.Usage.InputTokens != 10 || result.Usage.OutputTokens != 576 || result.Usage.TotalTokens != 586 {
		t.Errorf("usage = %#v", result.Usage)
	}
	meta := result.ProviderMetadata["azure"].(map[string]interface{})["images"].([]map[string]interface{})
	wantMeta := []map[string]interface{}{{"created": 1791229019.0, "size": "768x768", "textTokens": 10, "imageTokens": 0}}
	if !reflect.DeepEqual(meta, wantMeta) {
		t.Errorf("metadata = %#v", meta)
	}
	if result.Response.ModelID != "MAI-Image-2.6-Flash" {
		t.Errorf("response model = %q", result.Response.ModelID)
	}
}

func TestAzureMaiImage_DimensionsAndWarnings(t *testing.T) {
	server, c := newMaiServer(t, nil)
	model := maiImageModel(t, server, "MAI-Image-2.6")

	if _, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.jsonBody, map[string]interface{}{"model": "MAI-Image-2.6", "prompt": "p"}) {
		t.Errorf("body = %#v", c.jsonBody)
	}

	if _, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p", AspectRatio: "16:9"}); err != nil {
		t.Fatal(err)
	}
	if c.jsonBody["width"] != float64(1360) || c.jsonBody["height"] != float64(768) {
		t.Errorf("16:9 body = %#v", c.jsonBody)
	}

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p", Size: "1024x1024", AspectRatio: "16:9"})
	if err != nil {
		t.Fatal(err)
	}
	if c.jsonBody["width"] != float64(1024) || len(result.Warnings) != 1 || result.Warnings[0].Details != "aspectRatio is ignored when size is set." {
		t.Errorf("body=%#v warnings=%#v", c.jsonBody, result.Warnings)
	}

	seed := 42
	result, err = model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p", Seed: &seed})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "seed" {
		t.Errorf("warnings = %#v", result.Warnings)
	}
}

func TestAzureMaiImage_OneRequestPerImage(t *testing.T) {
	server, c := newMaiServer(t, nil)
	n := 2
	result, err := maiImageModel(t, server, "MAI-Image-2.6").DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p", N: &n})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.paths) != 2 || len(result.Images) != 2 {
		t.Errorf("paths=%v images=%d", c.paths, len(result.Images))
	}
	if result.Usage.InputTokens != 20 || result.Usage.OutputTokens != 1152 || result.Usage.TotalTokens != 1172 {
		t.Errorf("usage = %#v", result.Usage)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("warnings = %#v", result.Warnings)
	}
}

func TestAzureMaiImage_ResponseWithoutUsage(t *testing.T) {
	server, _ := newMaiServer(t, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"aGk="}]}`))
	})
	result, err := maiImageModel(t, server, "MAI-Image-2.6").DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Usage.InputTokens != 0 || result.Usage.TotalTokens != 0 {
		t.Errorf("usage = %#v", result.Usage)
	}
	meta := result.ProviderMetadata["azure"].(map[string]interface{})["images"].([]map[string]interface{})
	if len(meta) != 1 || len(meta[0]) != 0 {
		t.Errorf("metadata = %#v", meta)
	}
}

func TestAzureMaiImage_Errors(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
		message    string
	}{
		{"json", `{"error":{"code":"unsupported_request_value","message":"'width' must be at least 768 pixels."}}`, 400, "'width' must be at least 768 pixels."},
		{"html", `<html><body>Bad Gateway</body></html>`, 502, "Bad Gateway"},
		{"empty", ``, 500, "Internal Server Error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := newMaiServer(t, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := maiImageModel(t, server, "MAI-Image-2.6").DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p", Size: "512x512"})
			var perr *providererrors.ProviderError
			if !errors.As(err, &perr) {
				t.Fatalf("err = %v", err)
			}
			if perr.StatusCode != tc.status || perr.Message != tc.message || perr.ResponseBody != tc.body {
				t.Errorf("got status=%d message=%q body=%q", perr.StatusCode, perr.Message, perr.ResponseBody)
			}
		})
	}
}

func TestAzureMaiImage_EditsMultipart(t *testing.T) {
	server, c := newMaiServer(t, nil)
	result, err := maiImageModel(t, server, "MAI-Image-2.6").DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Combine both apples",
		Size:   "1024x1024",
		Files: []provider.ImageFile{
			{Type: "file", Data: []byte{1, 2, 3}, MediaType: "image/png"},
			{Type: "file", Data: []byte{4}, MediaType: "image/jpeg"},
		},
		Mask:            &provider.ImageFile{Type: "file", Data: []byte{4}, MediaType: "image/png"},
		ProviderOptions: map[string]interface{}{"azure": map[string]interface{}{"webGrounding": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.paths[0] != "/mai/v1/images/edits" {
		t.Errorf("path = %s", c.paths[0])
	}
	for k, v := range map[string]string{"model": "MAI-Image-2.6", "prompt": "Combine both apples", "width": "1024", "height": "1024", "web_grounding": "true"} {
		if got := c.form[k]; len(got) != 1 || got[0] != v {
			t.Errorf("form[%s] = %v, want %s", k, got, v)
		}
	}
	if _, ok := c.form["mask"]; ok {
		t.Error("mask must not be sent")
	}
	if !reflect.DeepEqual(c.files, []string{"image-1.png", "image-2.jpg"}) || !reflect.DeepEqual(c.fileType, []string{"image/png", "image/jpeg"}) {
		t.Errorf("files = %v types = %v", c.files, c.fileType)
	}
	found := false
	for _, w := range result.Warnings {
		if w.Feature == "mask" && w.Details == "MAI image edits do not support masks." {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %#v", result.Warnings)
	}
}

func TestAzureMaiImage_DimensionsForAspectRatio(t *testing.T) {
	cases := []struct {
		ratio string
		w, h  int
		ok    bool
	}{
		{"1:1", 1024, 1024, true},
		{"16:9", 1360, 768, true},
		{"9:16", 768, 1360, true},
		{"4:3", 1168, 880, true},
		{"21:9", 1792, 768, true},
		{"0:1", 0, 0, false},
		{"wide", 0, 0, false},
	}
	for _, tc := range cases {
		w, h, ok := azureMaiDimensionsForAspectRatio(tc.ratio)
		if w != tc.w || h != tc.h || ok != tc.ok {
			t.Errorf("%s = %d,%d,%v want %d,%d,%v", tc.ratio, w, h, ok, tc.w, tc.h, tc.ok)
		}
	}
}
