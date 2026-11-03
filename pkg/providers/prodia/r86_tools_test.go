package prodia

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// testdata/r86_tools_multipart_ts.json holds inputs and the output of TS
// parseMultipart (prodia-api.ts at 30548ee7f9, run with node) plus the TS
// boundary regex: quoted boundaries keep their quotes, LF-only separators,
// a delimiter inside a body, header lines without a colon, a part without a
// header block, and non-UTF-8 header bytes.
func TestR86toolsMultipartMatchesTS(t *testing.T) {
	data, err := os.ReadFile("testdata/r86_tools_multipart_ts.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name        string `json:"name"`
		ContentType string `json:"contentType"`
		Body        string `json:"body"`
		Want        struct {
			Boundary *string `json:"boundary"`
			Parts    []struct {
				Headers map[string]string `json:"headers"`
				Body    string            `json:"body"`
			} `json:"parts"`
		} `json:"want"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		boundary, ok := prodiaMultipartBoundary(c.ContentType)
		if c.Want.Boundary == nil {
			if ok {
				t.Errorf("%s: boundary %q, want none", c.Name, boundary)
			}
			continue
		}
		if !ok || boundary != *c.Want.Boundary {
			t.Errorf("%s: boundary %q %v, want %q", c.Name, boundary, ok, *c.Want.Boundary)
			continue
		}
		body, _ := base64.StdEncoding.DecodeString(c.Body)
		got := parseProdiaMultipart(body, boundary)
		if len(got) != len(c.Want.Parts) {
			t.Errorf("%s: %d parts, want %d", c.Name, len(got), len(c.Want.Parts))
			continue
		}
		for i, p := range got {
			want := c.Want.Parts[i]
			if !reflect.DeepEqual(p.headers, want.Headers) {
				t.Errorf("%s part %d: headers %q, want %q", c.Name, i, p.headers, want.Headers)
			}
			if b64 := base64.StdEncoding.EncodeToString(p.body); b64 != want.Body {
				t.Errorf("%s part %d: body %q, want %q", c.Name, i, b64, want.Body)
			}
		}
	}
}

// TS matches `name="job"` as a substring of Content-Disposition, so a
// `filename="job"` part is the job part; a quoted boundary is used with its
// quotes; a response without a Content-Type fails with the TS text instead
// of being sniffed; an empty output video is still an output.
func TestR86toolsMultipartDispositionAndBoundary(t *testing.T) {
	body := "--b\r\nContent-Disposition: attachment; filename=\"job\"\r\n\r\n{\"id\":\"j1\"}\r\n--b\r\nContent-Disposition: form-data; name=\"output\"\r\nContent-Type: video/mp4; codecs=avc1\r\n\r\nV\r\n--b--\r\n"
	job, out, mt, err := parseMultipartResponse("multipart/form-data; boundary=b", []byte(body))
	if err != nil || job.ID != "j1" || string(out) != "V" || mt != "video/mp4; codecs=avc1" {
		t.Fatalf("got %+v %q %q %v", job, out, mt, err)
	}
	quoted := "--\"q\"\r\nContent-Disposition: form-data; name=\"job\"\r\n\r\n{\"id\":\"j2\"}\r\n--\"q\"--"
	if job, _, _, err := parseMultipartResponse(`multipart/form-data; boundary="q"`, []byte(quoted)); err != nil || job.ID != "j2" {
		t.Fatalf("quoted boundary: %+v %v", job, err)
	}
	if _, _, _, err := parseMultipartResponse(`multipart/form-data; boundary="q"`, []byte("--q\r\nContent-Disposition: form-data; name=\"job\"\r\n\r\n{\"id\":\"j\"}\r\n--q--")); err == nil || err.Error() != "Prodia multipart response missing job part" {
		t.Fatalf("unquoted body with quoted boundary: %v", err)
	}

	for _, tc := range []struct {
		name, contentType, body, wantErr string
	}{
		{"missing content type", "", "--b\r\nContent-Disposition: form-data; name=\"job\"\r\n\r\n{\"id\":\"j\"}\r\n--b--", "Prodia response missing multipart boundary in content-type: "},
		{"empty video", "multipart/form-data; boundary=b", "--b\r\nContent-Disposition: form-data; name=\"job\"\r\n\r\n{\"id\":\"j\"}\r\n--b\r\nContent-Disposition: form-data; name=\"output\"\r\n\r\n\r\n--b--", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header()["Content-Type"] = nil
				if tc.contentType != "" {
					w.Header().Set("Content-Type", tc.contentType)
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			p := New(Config{APIKey: "k", BaseURL: srv.URL})
			m, err := p.VideoModel("inference.wan2-2.lightning.txt2vid.v0")
			if err != nil {
				t.Fatal(err)
			}
			res, err := m.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{Prompt: "p"})
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Videos) != 1 || len(res.Videos[0].Binary) != 0 || res.Videos[0].MediaType != "video/mp4" {
				t.Fatalf("videos = %+v", res.Videos)
			}
		})
	}
}
