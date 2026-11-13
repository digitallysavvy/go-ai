package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	promptutils "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
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

// TS ui/convert-to-model-messages.ts 174/242/250: `new URL(part.url)`; a URL
// the WHATWG parser rejects is TypeError "Invalid URL" (net/url accepted
// http://x:65536/ and rejected http://x.com/%zz). The URL is kept as given.
func TestR84integUIFilePartURLUsesWHATWGParser(t *testing.T) {
	for _, bad := range []string{"http://x:65536/a.png", "http://256.0.0.1/a.png", "/relative.png"} {
		_, err := ConvertToModelMessages(context.Background(), []UIMessage{{Role: UIMessageRoleUser, Parts: []UIMessagePart{
			FileUIPart{MediaType: "image/png", URL: bad},
		}}})
		var invalid *promptutils.InvalidURLError
		if !errors.As(err, &invalid) || err.Error() != "Invalid URL" {
			t.Fatalf("%s: err = %v", bad, err)
		}
		_, err = ConvertToModelMessages(context.Background(), []UIMessage{{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
			ReasoningFileUIPart{MediaType: "image/png", URL: bad},
		}}})
		if !errors.As(err, &invalid) || err.Error() != "Invalid URL" {
			t.Fatalf("reasoning %s: err = %v", bad, err)
		}
	}
	for _, good := range []string{"http://x.com/%zz", "HTTPS://X.com/a b.png"} {
		msgs, err := ConvertToModelMessages(context.Background(), []UIMessage{{Role: UIMessageRoleUser, Parts: []UIMessagePart{
			FileUIPart{MediaType: "image/png", URL: good},
		}}})
		if err != nil {
			t.Fatalf("%s: %v", good, err)
		}
		if f := msgs[0].Content[0].(types.FileContent); f.FileData.URL != good || f.FileData.OriginalURL != "" {
			t.Fatalf("%s: part = %+v", good, f)
		}
	}
}

// Stream CR item 1 via gen CR 3: GenerateText's step timeout reads like TS
// setAbortTimeout({label: 'Step'}) (generate-text.ts 1010-1012).
func TestR84integGenerateTextStepTimeoutText(t *testing.T) {
	step := 30 * time.Millisecond
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "go", Timeout: &TimeoutConfig{PerStep: &step}})
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) || timeoutErr.Reason != TimeoutReasonStep || timeoutErr.Timeout != step ||
		err.Error() != "Step timeout of 30ms exceeded" || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v (%#v)", err, timeoutErr)
	}
}

// A tool that fails with its timed-out context reaches the model as TS
// getErrorMessage(abortSignal.reason): "TimeoutError: The operation was
// aborted due to timeout" (execute-tool-call.ts 120, create-tool-model-output.ts 18).
func TestR84integToolTimeoutErrorTextForModel(t *testing.T) {
	toolTimeout := 10 * time.Millisecond
	var second string
	calls := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls == 1 {
				return &types.GenerateResult{
					ToolCalls:    []types.ToolCall{{ID: "tc1", ToolName: "slow", Arguments: map[string]interface{}{}}},
					FinishReason: types.FinishReasonToolCalls,
				}, nil
			}
			b, _ := json.Marshal(opts.Prompt.Messages)
			second = string(b)
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		},
	}
	slow := types.Tool{Name: "slow", Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "go", Tools: []types.Tool{slow},
		StopWhen: []StopCondition{StepCountIs(2)},
		Timeout:  &TimeoutConfig{ToolMs: &toolTimeout},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(second, "TimeoutError: The operation was aborted due to timeout") {
		t.Fatalf("second prompt = %s", second)
	}
}

// gen CR 4: StreamText logs the "image" content part deprecation once per
// user message on every step's prompt conversion (TS
// stream-language-model-call.ts convertToLanguageModelPrompt), as
// GenerateText does.
func TestR84integStreamTextImagePartDeprecationPerStep(t *testing.T) {
	var batches [][]types.Warning
	SetLogWarnings(func(o LogWarningsOptions) {
		if len(o.Warnings) > 0 && o.Warnings[0].Setting == `"image" content part` && o.Provider == "" && o.Model == "" {
			batches = append(batches, o.Warnings)
		}
	})
	defer SetLogWarnings(nil)
	calls := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			calls++
			if calls == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: model,
		Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
			types.ImageContent{Image: []byte{1, 2, 3}, MimeType: "image/png"},
			types.ImageContent{Image: []byte{4, 5}, MimeType: "image/jpeg"},
		}}},
		Tools: []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		}}},
		StopWhen: []StopCondition{StepCountIs(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	if r.Err() != nil || calls != 2 {
		t.Fatalf("err = %v, calls = %d", r.Err(), calls)
	}
	if len(batches) != 2 || len(batches[0]) != 2 || len(batches[1]) != 2 {
		t.Fatalf("batches = %+v, want two steps of two warnings", batches)
	}
}

// gen CR 4: the batch path logs the deprecation too (TS batch.ts 201
// convertToLanguageModelPrompt).
func TestR84integBatchImagePartDeprecation(t *testing.T) {
	var batches [][]types.Warning
	SetLogWarnings(func(o LogWarningsOptions) {
		if len(o.Warnings) > 0 && o.Warnings[0].Setting == `"image" content part` {
			batches = append(batches, o.Warnings)
		}
	})
	defer SetLogWarnings(nil)
	_, err := buildBatchProviderRequest(context.Background(), BatchRequest{Text: &BatchTextRequest{ID: "r", Model: "m",
		Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.ImageContent{Image: []byte{1, 2, 3}, MimeType: "image/png"}}}}}},
		nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 1 || len(batches[0]) != 1 {
		t.Fatalf("batches = %+v", batches)
	}
}

// A tool that swallows the step deadline and returns: the next iteration
// rejects with the step TimeoutError (TS throwIfAborted), with its TS text.
func TestR84integStepTimeoutAfterSwallowingTool(t *testing.T) {
	step := 30 * time.Millisecond
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				ToolCalls:    []types.ToolCall{{ID: "tc1", ToolName: "stubborn", Arguments: map[string]interface{}{}}},
				FinishReason: types.FinishReasonToolCalls,
			}, nil
		},
	}
	stubborn := types.Tool{Name: "stubborn", Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
		<-ctx.Done()
		return "ignored the abort", nil
	}}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "go", Tools: []types.Tool{stubborn},
		StopWhen: []StopCondition{StepCountIs(3)},
		Timeout:  &TimeoutConfig{PerStep: &step},
	})
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) || timeoutErr.Reason != TimeoutReasonStep || err.Error() != "Step timeout of 30ms exceeded" {
		t.Fatalf("err = %v", err)
	}
}
