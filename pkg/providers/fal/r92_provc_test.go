package fal

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR92provcImageBodyReservedKeys (r92 provc-09): the TS requestBody
// literal {prompt, seed, image_size, num_images} fixes those positions, so a
// passthrough option that sets image_size (fal's named sizes) is written
// third, not after the other options.
func TestR92provcImageBodyReservedKeys(t *testing.T) {
	m := &ImageModel{modelID: "fal-ai/flux/dev"}
	n := 2
	body, _, err := m.buildRequest(&provider.ImageGenerateOptions{
		Prompt: "p", N: &n,
		ProviderOptions: map[string]interface{}{"fal": map[string]interface{}{"guidanceScale": 3, "image_size": "square_hd"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(body)
	if want := `{"prompt":"p","image_size":"square_hd","num_images":2,"guidance_scale":3}`; string(got) != want {
		t.Errorf("body = %s\nwant   %s", got, want)
	}
}

// TestR92provcVideoDurationIsJSText (r92 provc-10): TS `${duration}s` writes
// the JS number text, and `if (options.duration)` skips 0 and NaN.
func TestR92provcVideoDurationIsJSText(t *testing.T) {
	m := &VideoModel{modelID: "fal-ai/luma-dream-machine"}
	for d, want := range map[float64]string{5: `"5s"`, 2.5: `"2.5s"`, 1e21: `"1e+21s"`, 1e-7: `"1e-7s"`} {
		dur := d
		body, err := m.buildStartRequestBody(&provider.VideoModelV3CallOptions{Prompt: "p", Duration: &dur})
		if err != nil {
			t.Fatal(err)
		}
		v, _ := body.Get("duration")
		if got, _ := json.Marshal(v); string(got) != want {
			t.Errorf("duration %v = %s, want %s", d, got, want)
		}
	}
	nan := math.NaN()
	body, _ := m.buildStartRequestBody(&provider.VideoModelV3CallOptions{Prompt: "p", Duration: &nan})
	if _, ok := body.Get("duration"); ok {
		t.Error("a NaN duration is falsy in TS and must be skipped")
	}
}

// TestR92provcImageSizeIsJSNumber (r92 provc-14): TS size.split('x').map(Number)
// keeps a fractional side and writes an unreadable one as null; Go's %dx%d
// scan dropped image_size.
func TestR92provcImageSizeIsJSNumber(t *testing.T) {
	m := &ImageModel{modelID: "fal-ai/flux/dev"}
	for size, want := range map[string]string{
		"1024x768":   `{"width":1024,"height":768}`,
		"1024.5x768": `{"width":1024.5,"height":768}`,
		"1024":       `{"width":1024,"height":null}`,
	} {
		body, _, err := m.buildRequest(&provider.ImageGenerateOptions{Prompt: "p", Size: size})
		if err != nil {
			t.Fatal(err)
		}
		v, _ := body.Get("image_size")
		if got, _ := json.Marshal(v); string(got) != want {
			t.Errorf("%s: image_size = %s, want %s", size, got, want)
		}
	}
}

// TestR92provcAspectRatioSizeKeyOrder (r92 provc-14): the derived
// {width, height} keeps the TS key order.
func TestR92provcAspectRatioSizeKeyOrder(t *testing.T) {
	m := &ImageModel{modelID: "fal-ai/flux/dev"}
	body, _, _ := m.buildRequest(&provider.ImageGenerateOptions{Prompt: "p", AspectRatio: "21:9"})
	v, _ := body.Get("image_size")
	if got, _ := json.Marshal(v); string(got) != `{"width":2560,"height":1080}` {
		t.Errorf("image_size = %s", got)
	}
}

// TestR92provcTranscriptionTypedOptions (r92 provc-15, typed-option
// ruling): a Go-typed providerOptions.fal literal reads like the decoded
// JSON object instead of failing the call.
func TestR92provcTranscriptionTypedOptions(t *testing.T) {
	m := &TranscriptionModel{modelID: "whisper"}
	body, err := m.buildRequestBody(&provider.TranscriptionOptions{ProviderOptions: map[string]interface{}{
		"fal": map[string]string{"language": "fr", "chunkLevel": "segment"},
	}})
	if err != nil || body["language"] != "fr" || body["chunk_level"] != "segment" {
		t.Fatalf("body = %v err = %v", body, err)
	}
}

// TestR92provcVideoStatusInvalidResponseIsErrorStatus (r92 provc-16): TS
// doStatus catches the APICallError 'Invalid JSON response' of an
// off-schema status body and reports an error status.
func TestR92provcVideoStatusInvalidResponseIsErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"video":{"url":42}}`))
	}))
	defer srv.Close()
	m := NewVideoModel(New(Config{APIKey: "k"}), "fal-ai/luma-dream-machine")
	op, _ := json.Marshal(falVideoOperation{ResponseURL: srv.URL + "/requests/1", SubmitURL: srv.URL + "/fal-ai/luma-dream-machine"})
	res, err := m.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil || res.Status != provider.VideoOperationStatusError || res.Error != "Invalid JSON response" {
		t.Fatalf("res = %+v err = %v", res, err)
	}
}
