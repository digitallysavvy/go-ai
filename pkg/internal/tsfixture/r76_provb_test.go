package tsfixture

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r76provbGet sends one request to url and returns its status and body.
func r76provbGet(ctx context.Context, url string) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(`{"n":1}`))
	if err != nil {
		return 0, "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close() //nolint:errcheck
	b, err := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), err
}

// TestR76ProvbRunReplaysResponseQueue: a TS test that queues several
// responses (`server.urls[url].response = [a, b, ...]`) gets them in order,
// one per request, and the TS test server's 404 {"error":"Not Found"} after
// the last (create-test-server.ts `handler.response[callNumber]`). Before
// r76 the extractor kept the first response only and every request got it.
func TestR76ProvbRunReplaysResponseQueue(t *testing.T) {
	var got []string
	cases := []Case{{
		Test:     "queue",
		CallKind: "generate",
		ModelID:  "m",
		Call:     map[string]interface{}{},
		Response: &Response{Kind: "json", Body: []byte(`{"first":true}`), Next: []*Response{
			{Kind: "error", Status: 429, Text: "slow down"},
			{Kind: "chunks", Chunks: []interface{}{"data: one\n\n", map[string]interface{}{"x": 1}}},
			{Kind: "empty", Status: 204},
		}},
		Asserts: []Assertion{{Expr: "server.calls.length", Matcher: "toBe", Arg: float64(5)}},
	}}
	Run(t, cases, func(baseURL, _ string) provider.LanguageModel {
		return &testutil.MockLanguageModel{DoGenerateFunc: func(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			for i := 0; i < 5; i++ {
				st, body, err := r76provbGet(ctx, baseURL)
				if err != nil {
					return nil, err
				}
				got = append(got, fmt.Sprintf("%d %s", st, body))
			}
			return &types.GenerateResult{FinishReason: types.FinishReasonStop}, nil
		}}
	}, nil)
	want := []string{
		`200 {"first":true}`,
		`429 slow down`,
		"200 data: one\n\ndata: {\"x\":1}\n\n",
		`204 `,
		`404 {"error":"Not Found"}`,
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("responses = %q\nwant        %q", got, want)
	}
}

// TestR76ProvbRunConcurrentRequests: the test server's handler runs on one
// goroutine per request; a model that sends requests concurrently must not
// race on the recorded calls (run with -race), and every call is recorded.
func TestR76ProvbRunConcurrentRequests(t *testing.T) {
	const n = 8
	cases := []Case{{
		Test:     "concurrent",
		CallKind: "generate",
		ModelID:  "m",
		Call:     map[string]interface{}{},
		Response: &Response{Kind: "json", Body: []byte(`{}`)},
		Asserts:  []Assertion{{Expr: "server.calls", Matcher: "toHaveLength", Arg: float64(n)}, {Expr: "server.calls.length", Matcher: "toBe", Arg: float64(n)}},
	}}
	Run(t, cases, func(baseURL, _ string) provider.LanguageModel {
		return &testutil.MockLanguageModel{DoGenerateFunc: func(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			var wg sync.WaitGroup
			errs := make(chan error, n)
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if _, _, err := r76provbGet(ctx, baseURL); err != nil {
						errs <- err
					}
				}()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				return nil, err
			}
			return &types.GenerateResult{FinishReason: types.FinishReasonStop}, nil
		}}
	}, nil)
}

// TestR76ProvbEvalPathArrayCalls: the stream-part filters TS tests write
// (`parts.filter(part => part.type === 'text-delta').map(part => part.delta)`,
// `.some(...)`), split over lines with a trailing comma as vitest formats
// them, evaluate over the stream; server.calls.length counts the requests;
// a Go value (typed map of json.RawMessage) is read in its JSON form.
func TestR76ProvbEvalPathArrayCalls(t *testing.T) {
	stream := []interface{}{
		map[string]interface{}{"type": "stream-start"},
		map[string]interface{}{"type": "text-delta", "delta": "Hello"},
		map[string]interface{}{"type": "finish", "finishReason": map[string]interface{}{"unified": "interrupted"}},
		map[string]interface{}{"type": "text-delta", "delta": ", partial"},
	}
	roots := map[string]interface{}{
		"result": map[string]interface{}{
			"stream":           stream,
			"providerMetadata": map[string]interface{}{"google": map[string]json.RawMessage{"token": json.RawMessage(`"t-1"`)}},
		},
		"server": map[string]interface{}{"calls": []request{{}, {}}},
	}
	aliases := map[string]string{"parts": "result.stream"}
	for _, tc := range []struct {
		expr string
		want interface{}
	}{
		{"parts.filter(part => part.type === 'text-delta').map(part => part.delta),", []interface{}{"Hello", ", partial"}},
		{"parts\n        .filter(event => event.type === 'text-delta')\n        .map(event => event.delta),", []interface{}{"Hello", ", partial"}},
		{"parts.filter(part => part.type === 'error')", []interface{}{}},
		{"parts.filter((p) => p.type === 'finish')[0].finishReason.unified", "interrupted"},
		{"parts.some(event => event.type === 'error')", false},
		{"parts.some(event => event.type === 'finish')", true},
		{"server.calls.length", float64(2)},
		{"result.providerMetadata?.google.token", "t-1"},
	} {
		got, ok := evalPath(roots, aliases, tc.expr)
		if !ok {
			t.Errorf("%q: not evaluated", tc.expr)
			continue
		}
		if d := Diff("", Normalize(tc.want), Normalize(jsonValue(got))); d != "" {
			t.Errorf("%q: %s", tc.expr, d)
		}
	}
	for _, expr := range []string{
		"parts.filter(a => b.type === 'x')",    // different parameter
		"parts.map(part => part.type === 'x')", // map with a comparison
		"parts.some(part => part.delta)",       // some without a comparison
	} {
		if _, ok := evalPath(roots, aliases, expr); ok {
			t.Errorf("%q evaluated; want not evaluated", expr)
		}
	}
}

// TestR76ProvbStreamToTSFileAndWarnings: a streamed file part carries its
// data in the TS SharedV4FileData shape, and stream-start warnings render
// as TS reports them (Go mirrors details into message for old callers).
func TestR76ProvbStreamToTSFileAndWarnings(t *testing.T) {
	parts := StreamToTS([]*provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{{Type: "other", Message: "m"}, {Type: "unsupported", Feature: "f", Details: "d", Message: "d"}}},
		{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{MediaType: "text/plain", Data: []byte{0xb5, 0xeb, 0x2d}}},
		{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://x/y.png"}}},
	})
	want := `[{"type":"stream-start","warnings":[{"message":"m","type":"other"},{"details":"d","feature":"f","type":"unsupported"}]},` +
		`{"data":{"data":"test","type":"data"},"mediaType":"text/plain","type":"file"},` +
		`{"data":{"type":"url","url":"https://x/y.png"},"mediaType":"image/png","type":"file"}]`
	if got := jsonString(jsonValue(parts)); got != want {
		t.Errorf("parts =\n%s\nwant\n%s", got, want)
	}
}
