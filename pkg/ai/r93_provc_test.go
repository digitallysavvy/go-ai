package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR93provcPollFallbackWarningLoggedOnce: TS logs the "poll/webhook
// options were provided but the model does not support doStart/doStatus"
// warning once, before the calls are parallelized; Go logged it per call.
func TestR93provcPollFallbackWarningLoggedOnce(t *testing.T) {
	var mu sync.Mutex
	var logged []string
	SetLogWarnings(func(o LogWarningsOptions) {
		mu.Lock()
		defer mu.Unlock()
		for _, w := range o.Warnings {
			logged = append(logged, w.Message)
		}
	})
	t.Cleanup(func() { resetLogWarningsState(nil) })

	one := 1
	model := &mockVideoModel{maxPerCall: &one, generateFn: func(context.Context, *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
		return &provider.VideoModelV3Response{Videos: []provider.VideoModelV3VideoData{{Type: "binary", Binary: []byte("v"), MediaType: "video/mp4"}}}, nil
	}}
	interval := 1
	if _, err := GenerateVideo(context.Background(), GenerateVideoOptions{Model: model, Prompt: VideoPrompt{Text: "x"}, N: 3, Poll: &VideoPollOptions{IntervalMs: &interval}}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	count := 0
	for _, m := range logged {
		if strings.Contains(m, "Falling back to doGenerate") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("fallback warning logged %d times, want 1 (%q)", count, logged)
	}
}

// TestR93provcCallerDeadlineIsNotPollTimeout: only the poll deadline is the
// polling timeout (TS statusTimeoutController); the caller's own deadline
// ends the status call with the caller's error.
func TestR93provcCallerDeadlineIsNotPollTimeout(t *testing.T) {
	model := &mockAsyncVideoModel{
		startFn: func(context.Context, *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error) {
			return &provider.VideoModelV3OperationStartResult{Operation: json.RawMessage(`{}`), Response: defaultVideoResponseInfo()}, nil
		},
		statusFn: func(ctx context.Context, _ *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	interval, timeout, zero := 1, 60000, 0
	_, err := GenerateVideo(ctx, GenerateVideoOptions{Model: model, Prompt: VideoPrompt{Text: "x"}, MaxRetries: &zero, Poll: &VideoPollOptions{IntervalMs: &interval, TimeoutMs: &timeout}})
	if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "Video generation timed out") {
		t.Errorf("err = %v, want the caller's deadline", err)
	}
}

// TestR93provcVideoURLIsWHATWGHref: TS downloads new URL(videoData.url), the
// WHATWG serialization of the provider's URL.
func TestR93provcVideoURLIsWHATWGHref(t *testing.T) {
	model := &mockVideoModel{generateFn: func(context.Context, *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
		return &provider.VideoModelV3Response{Videos: []provider.VideoModelV3VideoData{{Type: "url", URL: "HTTPS://CDN.Example.com:443/a b.mp4", MediaType: "video/mp4"}}}, nil
	}}
	var got string
	_, err := GenerateVideo(context.Background(), GenerateVideoOptions{Model: model, Prompt: VideoPrompt{Text: "x"},
		DownloadWithMetadata: func(_ context.Context, url string) (*DownloadResult, error) {
			got = url
			return &DownloadResult{Data: []byte("v")}, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://cdn.example.com/a%20b.mp4" {
		t.Errorf("downloaded %q", got)
	}
}

// TestR93provcSplitDataURLMediaType: TS splitDataUrl takes
// header.split(';')[0].split(':')[1], the text between the first and second
// colon.
func TestR93provcSplitDataURLMediaType(t *testing.T) {
	cases := map[string]string{
		"data:image/png;base64,AAAA":   "image/png",
		"data:image/png:x;base64,AAAA": "image/png",
		"data:;base64,AAAA":            "",
	}
	for in, want := range cases {
		if got, _ := splitVideoDataURL(in); got != want {
			t.Errorf("splitVideoDataURL(%q) media type = %q, want %q", in, got, want)
		}
	}
}
