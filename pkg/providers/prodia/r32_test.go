package prodia

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS postToApi lets a transport failure propagate unchanged.
func TestImageTransportFailureIsNotWrapped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	m, _ := New(Config{APIKey: "k", BaseURL: server.URL}).ImageModel("inference.flux-fast.schnell.txt2img.v2")
	server.Close()
	_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	var perr *providererrors.ProviderError
	if err == nil || errors.As(err, &perr) || !strings.HasPrefix(err.Error(), "Cannot connect to API") {
		t.Fatalf("err = %#v", err)
	}
}

// TS takes currentDate before the request is sent, not after the response.
func TestVideoResponseTimestampIsTakenBeforeTheRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(80 * time.Millisecond)
		w.Header().Set("Content-Type", "multipart/form-data; boundary=b")
		_, _ = w.Write([]byte("--b\r\nContent-Disposition: form-data; name=\"job\"\r\nContent-Type: application/json\r\n\r\n{\"id\":\"j\"}\r\n--b\r\nContent-Disposition: form-data; name=\"output\"\r\nContent-Type: video/mp4\r\n\r\nVIDEO\r\n--b--\r\n"))
	}))
	defer server.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: server.URL}).VideoModel("inference.wan2-2.lightning.txt2vid.v0")
	res, err := m.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{Prompt: "p"})
	if err != nil || time.Since(res.Response.Timestamp) < 60*time.Millisecond {
		t.Fatalf("timestamp %v, err %v", res.Response.Timestamp, err)
	}
}

// The failed-response body, parsed data and headers are kept on the error.
func TestProviderErrorKeepsResponseBodyAndData(t *testing.T) {
	err := newProdiaProviderError(400, []byte(`{"message":"m"}`), http.Header{"X-Request-Id": {"r"}})
	if err.ResponseBody != `{"message":"m"}` || err.Data == nil || err.ResponseHeaders["X-Request-Id"] != "r" || err.Message != "m" {
		t.Fatalf("err = %#v", err)
	}
}

// TS getHeaders adds the "ai-sdk-prodia/<version>" user agent to every call;
// the multipart endpoints (language model, image-to-video) must carry it too.
func TestMultipartCallsCarryProviderUserAgent(t *testing.T) {
	var agents []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agents = append(agents, r.Header.Get("User-Agent")+"|"+r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "multipart/form-data; boundary=b")
		_, _ = w.Write([]byte("--b\r\nContent-Disposition: form-data; name=\"job\"\r\nContent-Type: application/json\r\n\r\n{\"id\":\"j\"}\r\n--b\r\nContent-Disposition: form-data; name=\"output\"\r\nContent-Type: video/mp4\r\n\r\nVIDEO\r\n--b--\r\n"))
	}))
	defer server.Close()
	p := New(Config{APIKey: "k", BaseURL: server.URL})
	vm, _ := p.VideoModel("inference.wan2-2.lightning.img2vid.v0")
	if _, err := vm.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "p", Image: &provider.VideoModelV3File{Type: "file", Data: []byte("img"), MediaType: "image/png"},
	}); err != nil {
		t.Fatal(err)
	}
	lm, _ := p.LanguageModel("")
	_, _ = lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "p"}})
	if len(agents) != 2 {
		t.Fatalf("requests = %v", agents)
	}
	for _, a := range agents {
		if !strings.Contains(a, "ai-sdk-prodia/") || !strings.HasSuffix(a, "|Bearer k") {
			t.Errorf("headers = %q", a)
		}
	}
}

// TS postFormDataToApi lets transport and API errors propagate unchanged: no
// "prodia: " prefix is put in front of them.
func TestLanguageModelErrorsAreNotWrapped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"message":"bad"}`))
	}))
	p := New(Config{APIKey: "k", BaseURL: server.URL})
	lm, _ := p.LanguageModel("")
	_, err := lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "p"}})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.StatusCode != 400 || strings.HasPrefix(err.Error(), "prodia: ") {
		t.Fatalf("http err = %v", err)
	}
	server.Close()
	_, err = lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "p"}})
	if err == nil || !strings.HasPrefix(err.Error(), "Cannot connect to API") {
		t.Fatalf("transport err = %v", err)
	}
}
