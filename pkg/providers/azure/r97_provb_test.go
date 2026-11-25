package azure

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"golang.org/x/net/websocket"
)

// TestR97provbMaiImageFractionalTokenMetadata: TS maiImageResponseSchema
// token counts are z.number(), and providerMetadata.azure.images keeps
// textTokens / imageTokens as received; Go truncated them into *int. The
// ImageUsage int fields still truncate the sums.
func TestR97provbMaiImageFractionalTokenMetadata(t *testing.T) {
	server, _ := newMaiServer(t, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1,"usage":{"num_input_text_tokens":10.5,"num_input_image_tokens":2,"num_output_tokens":3.5},"data":[{"b64_json":"iVBORw0KGgo="}]}`))
	})
	result, err := maiImageModel(t, server, "MAI-Image-2.6").DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	meta := result.ProviderMetadata["azure"].(map[string]interface{})["images"].([]map[string]interface{})
	want := []map[string]interface{}{{"created": 1.0, "textTokens": 10.5, "imageTokens": 2}}
	if !reflect.DeepEqual(meta, want) {
		t.Fatalf("metadata = %#v, want %#v", meta, want)
	}
	// TS usage: input 12.5, output 3.5, total 16; Go's ints truncate the sums.
	if result.Usage.InputTokens != 12 || result.Usage.OutputTokens != 3 || result.Usage.TotalTokens != 16 {
		t.Fatalf("usage = %#v", result.Usage)
	}
}

// TestR97provbImageOptionsNullInvalid: TS azureImageModelOptions keys are
// z.enum / z.boolean().optional(), not nullish, so a present null throws
// "invalid azure provider options" (Go skipped it like an absent key).
func TestR97provbImageOptionsNullInvalid(t *testing.T) {
	for _, key := range []string{"api", "autoAspectRatio", "webGrounding"} {
		_, err := parseAzureImageProviderOptions(map[string]interface{}{"azure": map[string]interface{}{key: nil}})
		if err == nil {
			t.Errorf("%s: null accepted, want invalid azure provider options", key)
		}
	}
	if _, err := parseAzureImageProviderOptions(map[string]interface{}{"azure": map[string]interface{}{"other": nil}}); err != nil {
		t.Errorf("an undeclared key is stripped (non-strict object): %v", err)
	}
}

// TestR97provbMaiImageEditUppercaseJpegName: TS toFormData names an image
// by blob.type, which the Blob constructor lowercases, so "IMAGE/JPEG" is
// image-1.jpg (Go compared the raw media type and wrote image-1.png).
func TestR97provbMaiImageEditUppercaseJpegName(t *testing.T) {
	server, c := newMaiServer(t, nil)
	_, err := maiImageModel(t, server, "MAI-Image-2.6").DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "p",
		Files:  []provider.ImageFile{{Type: "file", Data: []byte{1}, MediaType: "IMAGE/JPEG"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.files) != 1 || c.files[0] != "image-1.jpg" || c.fileType[0] != "image/jpeg" {
		t.Fatalf("files = %v, types = %v", c.files, c.fileType)
	}
}

// TestR97provbMaiStreamHeadersBeforeURL: TS AzureMaiTranscriptionModel
// awaits config.headers() before it calls config.url(), so without an API
// key and a resource name the missing key is the error (Go reported the
// missing resource name).
func TestR97provbMaiStreamHeadersBeforeURL(t *testing.T) {
	t.Setenv("AZURE_API_KEY", "")
	t.Setenv("AZURE_RESOURCE_NAME", "")
	p, err := New(Config{BaseURL: "https://example.test/openai"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := p.TranscriptionModel("MAI-Transcribe-2-Streaming")
	if err != nil {
		t.Fatal(err)
	}
	sm, ok := m.(provider.TranscriptionStreamer)
	if !ok {
		t.Fatalf("%T is not a streaming model", m)
	}
	rate := 24000
	_, err = sm.DoStream(context.Background(), &provider.TranscriptionStreamOptions{InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate}})
	if err == nil || !providererrors.IsLoadAPIKeyError(err) {
		t.Fatalf("err = %v, want the LoadAPIKeyError", err)
	}
}

// TestR97provbMaiNullMessageTypeError: TS safeParseJSON parses a JSON null
// message and then reads event.type of it; the TypeError fails the stream
// through onProcessingError (Go ignored the message).
func TestR97provbMaiNullMessageTypeError(t *testing.T) {
	ts := httptest.NewServer(websocket.Server{Handler: func(conn *websocket.Conn) {
		_ = websocket.Message.Send(conn, "null")
		var msg string
		_ = websocket.Message.Receive(conn, &msg)
	}})
	defer ts.Close()
	p, err := New(Config{APIKey: "k", ResourceName: "r", MaiBaseURL: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, _ := p.TranscriptionModel("mai-transcribe-2-streaming")
	rate := 16000
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := model.(provider.TranscriptionStreamer).DoStream(ctx, &provider.TranscriptionStreamOptions{
		Audio: newChanAudioStream([]byte{1, 2}), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	for {
		_, nextErr := result.Stream.Next()
		if nextErr == nil {
			continue
		}
		if nextErr.Error() != "Cannot read properties of null (reading 'type')" {
			t.Fatalf("err = %v, want the TypeError", nextErr)
		}
		return
	}
}
