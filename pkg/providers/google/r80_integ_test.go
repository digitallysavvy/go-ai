package google

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS convertBase64ToUint8Array (and the generated-file uint8Array getter)
// map base64url to the standard alphabet and decode with atob, which accepts
// a missing padding. Go used base64.StdEncoding, which rejected both.
const r80integInlineParts = `[{"inlineData":{"mimeType":"image/png","data":"aGk"}},{"inlineData":{"mimeType":"image/png","data":"-_8"}}]`

var r80integWantFiles = [][]byte{[]byte("hi"), {0xfb, 0xff}}

func r80integCheckFiles(t *testing.T, got [][]byte) {
	t.Helper()
	if len(got) != len(r80integWantFiles) {
		t.Fatalf("files = %v, want %v", got, r80integWantFiles)
	}
	for i := range got {
		if !bytes.Equal(got[i], r80integWantFiles[i]) {
			t.Fatalf("file %d = %v, want %v", i, got[i], r80integWantFiles[i])
		}
	}
}

func TestR80integInlineDataForgivingBase64Generate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"candidates":[{"content":{"parts":`+r80integInlineParts+`,"role":"model"},"finishReason":"STOP","index":0}]}`)
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("gemini-2.5-flash")
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	var files [][]byte
	for _, c := range res.Content {
		switch f := c.(type) {
		case types.GeneratedFileContent:
			files = append(files, f.Data)
		case *types.GeneratedFileContent:
			files = append(files, f.Data)
		}
	}
	r80integCheckFiles(t, files)
}

func TestR80integInlineDataForgivingBase64Stream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: "+`{"candidates":[{"content":{"parts":`+r80integInlineParts+`,"role":"model"},"finishReason":"STOP","index":0}]}`+"\n\n")
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("gemini-2.5-flash")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	var files [][]byte
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeFile && c.GeneratedFileContent != nil {
			files = append(files, c.GeneratedFileContent.Data)
		}
	}
	r80integCheckFiles(t, files)
}

// Gemini TTS audio: unpadded base64 PCM decodes (TS
// convertBase64ToUint8Array); v0.5.2 failed the call.
func TestR80integSpeechForgivingBase64(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/L16;rate=24000","data":"AQACAA"}}]}}]}`)
	}))
	defer srv.Close()
	model, err := New(Config{APIKey: "k", BaseURL: srv.URL}).SpeechModel(ModelGemini25FlashTTS)
	if err != nil {
		t.Fatal(err)
	}
	res, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hello"})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if len(res.Audio) != 44+4 || !strings.HasPrefix(string(res.Audio), "RIFF") || !bytes.Equal(res.Audio[44:], []byte{1, 0, 2, 0}) {
		t.Fatalf("audio = %v", res.Audio)
	}
}
