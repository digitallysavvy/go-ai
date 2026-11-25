package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	promptutils "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
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

// array-output-stream.test.ts 80 / 46 (util): 'falls back on schema errors
// for legacy array output' (StreamObject's parser, skipInvalidElements
// false, falls back on an invalid element) and 'withholds the last element
// until a new element starts or the object closes' on the same parser.
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

// TS generated file URLs are URL objects (the provider's new URL(...)): the
// download function and the DownloadError get url.toString(), the WHATWG
// serialization.
func TestR92genGeneratedFileURLSerialized(t *testing.T) {
	var got []promptutils.DownloadRequest
	_, err := GenerateText(context.Background(), GenerateTextOptions{Model: fileModel(urlFile("https://files.example.com/a b.png")), Prompt: "image",
		ExperimentalDownload: func(_ context.Context, reqs []promptutils.DownloadRequest) ([]*promptutils.DownloadResult, error) {
			got = append(got, reqs...)
			results := make([]*promptutils.DownloadResult, len(reqs))
			for i := range reqs {
				results[i] = &promptutils.DownloadResult{Data: []byte("x")}
			}
			return results, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].URL != "https://files.example.com/a%20b.png" {
		t.Fatalf("download requests = %+v", got)
	}
}

// generated-file-download.test.ts 161 'downloads %s file URLs through the
// configured download function instead of global fetch' (generate): the
// prompt's batch (no URLs, empty) and then the generated file's batch.
func TestR92genGeneratedFileDownloadBatches(t *testing.T) {
	var calls [][]promptutils.DownloadRequest
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: fileModel(urlFile("https://example.com/generated.txt")), Prompt: "image",
		ExperimentalDownload: func(_ context.Context, reqs []promptutils.DownloadRequest) ([]*promptutils.DownloadResult, error) {
			calls = append(calls, append([]promptutils.DownloadRequest{}, reqs...))
			results := make([]*promptutils.DownloadResult, len(reqs))
			for i := range reqs {
				results[i] = &promptutils.DownloadResult{Data: []byte("Hello World"), MediaType: "text/plain"}
			}
			return results, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 || string(res.Files[0].Data) != "Hello World" {
		t.Fatalf("files = %+v", res.Files)
	}
	want := [][]promptutils.DownloadRequest{{}, {{URL: "https://example.com/generated.txt"}}}
	if len(calls) != 2 || len(calls[0]) != 0 || len(calls[1]) != 1 || calls[1][0] != want[1][0] {
		t.Fatalf("download calls = %+v, want %+v", calls, want)
	}
}

// generated-file-download.test.ts 202 'uses the fetch transport of
// createDownload for generated file URLs': the generated file goes through
// the transport given to CreateDownload.
func TestR92genGeneratedFileUsesCreateDownloadTransport(t *testing.T) {
	var urls []string
	download := CreateDownload(&DownloadOptions{Transport: r91genRoundTripper(func(r *http.Request) (*http.Response, error) {
		urls = append(urls, r.URL.String())
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("Hello World")), Request: r}, nil
	})})
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: fileModel(urlFile("https://93.184.215.14/generated.txt")), Prompt: "image", ExperimentalDownload: download})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 || string(res.Files[0].Data) != "Hello World" {
		t.Fatalf("files = %+v", res.Files)
	}
	if len(urls) != 1 || urls[0] != "https://93.184.215.14/generated.txt" {
		t.Fatalf("transport calls = %v", urls)
	}
}

// generated-file-download.test.ts 109 'cancels a %s file download when
// aborted (custom download: true)' (generate): a CreateDownload transport
// sees the call's context, and cancelling it ends the call.
func TestR92genGeneratedFileCustomDownloadAbort(t *testing.T) {
	started := make(chan struct{})
	download := CreateDownload(&DownloadOptions{Transport: r91genRoundTripper(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := GenerateText(ctx, GenerateTextOptions{Model: fileModel(urlFile("https://93.184.215.14/generated.txt")), Prompt: "image", ExperimentalDownload: download})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("download never started")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error after abort")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("download was not cancelled by the abort")
	}
}
