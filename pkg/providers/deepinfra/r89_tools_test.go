package deepinfra

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func r89toolsImageError(t *testing.T, body string, edit bool) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	model, err := New(Config{APIKey: "k", BaseURL: server.URL + "/inference"}).ImageModel("stabilityai/sd3.5")
	if err != nil {
		t.Fatal(err)
	}
	opts := &provider.ImageGenerateOptions{Prompt: "a cat"}
	if edit {
		opts.Files = []provider.ImageFile{{Type: "file", Data: []byte{1, 2, 3}, MediaType: "image/png"}}
	}
	_, err = model.DoGenerate(context.Background(), opts)
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("error = %T %v, want ProviderError", err, err)
	}
	return perr.Message
}

// TS deepinfra-image-model.ts createJsonErrorResponseHandler: a body that
// does not match deepInfraErrorSchema / deepInfraEditErrorSchema (or a blank
// one) gives the status text; a matching body gives errorToMessage.
func TestR89toolsDeepInfraImageErrorMessages(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		edit bool
		want string
	}{
		{"generate detail", `{"detail":{"error":"invalid model"}}`, false, "invalid model"},
		{"generate empty detail error", `{"detail":{"error":""}}`, false, ""},
		{"generate not json", `upstream failure`, false, "Bad Request"},
		{"generate schema mismatch", `{"error":"x"}`, false, "Bad Request"},
		{"generate blank", `  `, false, "Bad Request"},
		{"edit message", `{"error":{"message":"bad image"}}`, true, "bad image"},
		{"edit no error", `{}`, true, "Unknown error"},
		{"edit null error", `{"error":null}`, true, "Bad Request"},
		{"edit not json", `oops`, true, "Bad Request"},
		{"edit message missing", `{"error":{}}`, true, "Bad Request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := r89toolsImageError(t, tc.body, tc.edit); got != tc.want {
				t.Fatalf("message = %q, want %q", got, tc.want)
			}
		})
	}
}

// TS deepinfra-image-model.ts convertToFormData({model, prompt, image, mask,
// n, size, ...deepInfraOptions}, {useArrayBrackets: false}): fixed field
// order, an option named like a fixed field replaces it in place, null
// options are skipped, arrays repeat the field, numbers use JS String().
func TestR89toolsDeepInfraEditFormOrder(t *testing.T) {
	type field struct{ name, value string }
	var got []field
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mr, err := r.MultipartReader()
		if err != nil {
			t.Error(err)
			return
		}
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(part)
			v := string(b)
			if part.FileName() != "" {
				v = "<file>"
			}
			got = append(got, field{part.FormName(), v})
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"AQID"}]}`))
	}))
	defer server.Close()
	model, err := New(Config{APIKey: "k", BaseURL: server.URL + "/inference"}).ImageModel("stabilityai/sd3.5")
	if err != nil {
		t.Fatal(err)
	}
	n := 1
	mask := provider.ImageFile{Type: "file", Data: []byte{9}, MediaType: "image/png"}
	_, err = model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "a cat",
		N:      &n,
		Size:   "512x512",
		Files: []provider.ImageFile{
			{Type: "file", Data: []byte{1}, MediaType: "image/png"},
			{Type: "file", Data: []byte{2}, MediaType: "image/png"},
		},
		Mask: &mask,
		ProviderOptions: map[string]interface{}{"deepinfra": map[string]interface{}{
			"size":                "1024x1024",
			"guidance_scale":      7.5,
			"num_inference_steps": 30,
			"negative_prompt":     nil,
			"tags":                []interface{}{"a", "b"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []field{
		{"model", "stabilityai/sd3.5"}, {"prompt", "a cat"}, {"image", "<file>"}, {"image", "<file>"},
		{"mask", "<file>"}, {"n", "1"}, {"size", "1024x1024"},
		{"guidance_scale", "7.5"}, {"num_inference_steps", "30"}, {"tags", "a"}, {"tags", "b"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("form fields = %v\nwant %v", got, want)
	}
}

// provb CR (cr-r89-provb-to-tools 1): TS createJsonErrorResponseHandler
// builds an APICallError with responseHeaders, responseBody and data, so a
// retry-after(-ms) header reaches the retry loop.
func TestR89toolsDeepInfraImageErrorKeepsResponse(t *testing.T) {
	for _, tc := range []struct {
		status  int
		body    string
		want    string
		hasData bool
	}{
		{http.StatusTooManyRequests, `{"detail":{"error":"busy"}}`, "busy", true},
		{http.StatusBadGateway, `<html>bad</html>`, "Bad Gateway", false},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After-Ms", "10")
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		model, _ := New(Config{APIKey: "k", BaseURL: server.URL + "/inference"}).ImageModel("stabilityai/sd3.5")
		_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "a cat"})
		server.Close()
		var perr *providererrors.ProviderError
		if !errors.As(err, &perr) {
			t.Fatalf("error = %T %v", err, err)
		}
		if perr.Message != tc.want || perr.StatusCode != tc.status || perr.ResponseBody != tc.body || (perr.Data != nil) != tc.hasData {
			t.Fatalf("error = %#v", perr)
		}
		found := false
		for k, v := range perr.ResponseHeaders {
			if strings.EqualFold(k, "retry-after-ms") && v == "10" {
				found = true
			}
		}
		if !found {
			t.Fatalf("headers = %v", perr.ResponseHeaders)
		}
	}
}
