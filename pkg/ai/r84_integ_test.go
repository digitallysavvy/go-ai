package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type r84integURLModel struct {
	*testutil.MockLanguageModel
	supported map[string][]string
}

func (m *r84integURLModel) SupportedURLs() map[string][]string { return m.supported }

// r84 gen fix 2 dropped the prompt-time rejection of file:, javascript:,
// vbscript: and hostless URL file parts (TS accepts any URL there). The
// default downloader must still refuse every one of them, and private or
// loopback hosts, before any fetch (TS validateDownloadUrl), and the model
// must never be called with them inlined. (The pkg/ai downloader reports
// these as the v0.5.2 *SSRFError texts: a recorded known difference.)
func TestR84integUnsafeFileURLsAreNeverFetched(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("secret"))
	}))
	defer srv.Close()
	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]

	cases := []struct {
		url  string
		want string
	}{
		{"file:///etc/passwd", "only http, https, and data are permitted"},
		{"FILE:///etc/passwd", "only http, https, and data are permitted"},
		{"file://localhost/etc/passwd", "only http, https, and data are permitted"},
		{"javascript:alert(1)", "only http, https, and data are permitted"},
		{"vbscript:msgbox(1)", "only http, https, and data are permitted"},
		{"mailto:a@example.com", "only http, https, and data are permitted"},
		{"urn:isbn:123", "only http, https, and data are permitted"},
		{"ftp://example.com/x.png", "only http, https, and data are permitted"},
		{srv.URL + "/x.png", "loopback"},
		{"http://localhost:" + port + "/x.png", "not allowed"},
		{"http://LOCALHOST.:" + port + "/x.png", "not allowed"},
		{"http://0x7f.1:" + port + "/x.png", "loopback"},
		{"http://2130706433:" + port + "/x.png", "loopback"},
		{"http://%6c%6f%63%61%6c%68%6f%73%74:" + port + "/x.png", "not allowed"},
		{"http://[::1]:" + port + "/x.png", "loopback"},
		{"http://[::ffff:127.0.0.1]:" + port + "/x.png", "loopback"},
		{"http://169.254.169.254/latest/meta-data", "link-local"},
		{"http://10.0.0.1/x.png", "private address"},
		{"http://metadata.google.internal/x", "not allowed"},
	}
	for _, tc := range cases {
		for _, tagged := range []bool{false, true} {
			part := types.FileContent{MediaType: "image/png", URL: tc.url}
			if tagged {
				part = types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: tc.url}}
			}
			model := &r84integURLModel{MockLanguageModel: &testutil.MockLanguageModel{
				DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
					t.Fatalf("%s: model called", tc.url)
					return nil, nil
				},
			}}
			_, err := GenerateText(context.Background(), GenerateTextOptions{
				Model:    model,
				Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{part}}},
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s (tagged %v): err = %v, want %q", tc.url, tagged, err, tc.want)
			}
			if !providererrors.IsDownloadError(err) && !providererrors.IsSSRFError(err) {
				t.Errorf("%s: err %T is not a download/SSRF error", tc.url, err)
			}
		}
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("loopback server was fetched %d times", n)
	}
}

// A URL the model supports is forwarded (in WHATWG form) and never fetched,
// whatever its scheme: TS hands it to the provider as url.toString().
func TestR84integSupportedUnsafeURLForwardedNotFetched(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "javascript:alert(1)", "http://127.0.0.1/x.png"} {
		var got types.FileContent
		model := &r84integURLModel{
			MockLanguageModel: &testutil.MockLanguageModel{
				DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
					got = opts.Prompt.Messages[0].Content[0].(types.FileContent)
					return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
				},
			},
			supported: map[string][]string{"*": {`^.*$`}},
		}
		var downloaded atomic.Int32
		_, err := GenerateText(context.Background(), GenerateTextOptions{
			Model:    model,
			Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.FileContent{MediaType: "image/png", URL: raw}}}},
			ExperimentalDownload: func(ctx context.Context, requests []DownloadRequest) ([]*DownloadResult, error) {
				for _, r := range requests {
					if !r.IsURLSupportedByModel {
						downloaded.Add(1)
					}
				}
				return DefaultDownload(ctx, requests)
			},
		})
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if downloaded.Load() != 0 || got.FileData.Type != types.FileDataTypeURL || got.FileData.URL != raw || len(got.FileData.Data) != 0 {
			t.Fatalf("%s: provider part = %+v (downloads %d)", raw, got, downloaded.Load())
		}
	}
}
