package bfl

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS legacy body: width/height are Number(side of size.split('x')), and
// JSON.stringify writes NaN as null, so an unreadable size sends explicit nulls.
func TestBFLLegacyUnreadableSizeSendsNullDimensions(t *testing.T) {
	for size, want := range map[string][]string{
		"axb":      {`"width":null`, `"height":null`},
		"1024":     {`"width":1024`, `"height":null`},
		"1024x":    {`"width":1024`, `"height":0`},
		"1024x768": {`"width":1024`, `"height":768`},
	} {
		var body string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/flux-pro":
				b, _ := io.ReadAll(r.Body)
				body = string(b)
				_, _ = w.Write([]byte(`{"id":"r","polling_url":"http://` + r.Host + `/poll"}`))
			case "/poll":
				_, _ = w.Write([]byte(`{"status":"Ready","result":{"sample":"http://` + r.Host + `/img.png"}}`))
			default:
				_, _ = w.Write([]byte("img"))
			}
		}))
		m := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), "flux-pro")
		if _, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p", Size: size}); err != nil {
			t.Fatalf("%s: %v", size, err)
		}
		server.Close()
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Fatalf("size %q: body = %s, want %s", size, body, w)
			}
		}
	}
}
