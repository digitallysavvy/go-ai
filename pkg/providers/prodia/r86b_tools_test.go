package prodia

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// V061 item 25 (+ the fetch Headers.get class): TS reads the response
// Content-Type with headers.get (repeated lines joined with ", ", each byte
// one character), matches /boundary=([^\s;]+)/ with JS \s (a raw 0xA0 ends
// the boundary) and splits the body on the UTF-8 of `--${boundary}`.
// Recorded with node fetch against the same raw header bytes
// (testdata/r86b_tools_boundary_header_ts.json); each case runs through the
// image, video and language models.
func TestR86btoolsBoundaryHeaderMatchesTS(t *testing.T) {
	data, err := os.ReadFile("testdata/r86b_tools_boundary_header_ts.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name string `json:"name"`
			Raw  string `json:"raw"`
			TS   struct {
				ContentType  string  `json:"contentType"`
				DelimiterHex *string `json:"delimiterHex"`
			} `json:"ts"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range fixture.Cases {
		raw, err := base64.StdEncoding.DecodeString(c.Raw)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(raw)), nil)
		if err != nil {
			t.Fatal(err)
		}
		delimiter := []byte("--never-matches")
		if c.TS.DelimiterHex != nil {
			if delimiter, err = hex.DecodeString(*c.TS.DelimiterHex); err != nil {
				t.Fatal(err)
			}
		}
		for _, kind := range []string{"image", "video", "language"} {
			t.Run(c.Name+"/"+kind, func(t *testing.T) {
				outputType := map[string]string{"image": "image/png", "video": "video/mp4", "language": "text/plain"}[kind]
				var body bytes.Buffer
				body.Write(delimiter)
				body.WriteString("\r\nContent-Disposition: form-data; name=\"job\"\r\nContent-Type: application/json\r\n\r\n{\"id\":\"j1\",\"state\":{\"current\":\"completed\"}}\r\n")
				body.Write(delimiter)
				body.WriteString("\r\nContent-Disposition: form-data; name=\"output\"; filename=\"out\"\r\nContent-Type: " + outputType + "\r\n\r\nOUT\r\n")
				body.Write(delimiter)
				body.WriteString("--\r\n")
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header()["Content-Type"] = resp.Header.Values("Content-Type")
					_, _ = w.Write(body.Bytes())
				}))
				defer srv.Close()
				p := New(Config{APIKey: "k", BaseURL: srv.URL})
				var callErr error
				var got string
				switch kind {
				case "image":
					res, err := NewImageModel(p, "inference.flux-fast.schnell.txt2img.v2").DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
					callErr = err
					if err == nil {
						got = string(res.Image)
					}
				case "video":
					m, err := p.VideoModel("inference.wan2-2.lightning.txt2vid.v0")
					if err != nil {
						t.Fatal(err)
					}
					res, err := m.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{Prompt: "p"})
					callErr = err
					if err == nil && len(res.Videos) == 1 {
						got = string(res.Videos[0].Binary)
					}
				case "language":
					res, err := NewLanguageModel(p, LanguageModelNanoBananaImgToImgV2).DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "p"}})
					callErr = err
					if err == nil {
						got = res.Text
					}
				}
				if c.TS.DelimiterHex == nil {
					want := "Prodia response missing multipart boundary in content-type: " + c.TS.ContentType
					if callErr == nil || callErr.Error() != want {
						t.Fatalf("err = %v, want %q", callErr, want)
					}
					return
				}
				if callErr != nil || got != "OUT" {
					t.Fatalf("output %q, err = %v; want OUT (TS boundary %q)", got, callErr, delimiter)
				}
			})
		}
	}
}
