package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// Round 92 gen tests (TS 2ebc78b654).

// create-download.test.ts 5 'forwards the fetch transport, abort signal, and
// download size limit': the transport gets the request with the caller's
// context, and MaxBytes applies (a 9-byte body over a 3-byte limit is a DownloadError).
func TestR92genCreateDownloadForwardsTransportContextAndLimit(t *testing.T) {
	type r92genCtxKey struct{}
	ctx := context.WithValue(context.Background(), r92genCtxKey{}, "signal")
	var calls int
	download := CreateDownload(&DownloadOptions{MaxBytes: 3, Transport: r91genRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != "https://93.184.215.14/file" {
			t.Errorf("url = %s", r.URL)
		}
		if r.Context().Value(r92genCtxKey{}) != "signal" {
			t.Error("the transport did not get the caller's context")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/plain"}}, Body: io.NopCloser(strings.NewReader("too large")), Request: r}, nil
	})})
	_, err := download(ctx, []DownloadRequest{{URL: "https://93.184.215.14/file"}})
	if !providererrors.IsDownloadError(err) {
		t.Fatalf("err = %v, want a DownloadError", err)
	}
	if calls != 1 {
		t.Fatalf("transport calls = %d, want 1", calls)
	}
}

// create-download.test.ts 28 'acts as an experimental_download function when
// called with a batch' is TestR91genCreateDownloadTransport (r91_gen_test.go).

// util/array-output-stream.test.ts 80 'falls back on schema errors for
// legacy array output': StreamObject's parser (skipInvalidElements false)
// falls back on an invalid element; 46 'withholds the last element until a
// new element starts or the object closes' on the same parser.
func TestR92genObjectArrayStreamParser(t *testing.T) {
	number := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "number"})
	if r := newObjectArrayStreamParser(number).next(`{"elements":[1,"invalid",2]}`); !r.fallback {
		t.Fatalf("invalid element: %+v, want fallback", r)
	}
	p := newObjectArrayStreamParser(number)
	for _, step := range []struct {
		delta   string
		changed bool
		want    string
	}{
		{`{"elements":[1,`, true, `[]`},
		{`2`, true, `[1]`},
		{`]`, false, ``},
		{`}`, true, `[1,2]`},
	} {
		r := p.next(step.delta)
		if r.fallback || r.changed != step.changed {
			t.Fatalf("%q: %+v", step.delta, r)
		}
		if r.changed {
			if b, _ := json.Marshal(r.partial); string(b) != step.want {
				t.Fatalf("%q: %s, want %s", step.delta, b, step.want)
			}
		}
	}
}
