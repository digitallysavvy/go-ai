package ai

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// mockAsyncVideoModel implements VideoModelV3 + VideoModelStarter +
// VideoModelStatusChecker (and optionally VideoModelWebhookHandler) for
// exercising the async start/status/webhook orchestration in generate_video.go,
// start_video.go and get_video_status.go. Mirrors TS mock-video-model-v4.ts.
type mockAsyncVideoModel struct {
	maxPerCall *int
	startFn    func(ctx context.Context, opts *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error)
	statusFn   func(ctx context.Context, opts *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, error)
	webhookFn  func(ctx context.Context, factory provider.VideoWebhookFactory) (string, provider.VideoWebhookReceived, error)
	generateFn func(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error)
}

func (m *mockAsyncVideoModel) SpecificationVersion() string { return "v4" }
func (m *mockAsyncVideoModel) Provider() string             { return "mock" }
func (m *mockAsyncVideoModel) ModelID() string              { return "mock-async-video" }
func (m *mockAsyncVideoModel) MaxVideosPerCall() *int       { return m.maxPerCall }

func (m *mockAsyncVideoModel) DoGenerate(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
	if m.generateFn != nil {
		return m.generateFn(ctx, opts)
	}
	return nil, errors.New("doGenerate not implemented")
}

func (m *mockAsyncVideoModel) DoStart(ctx context.Context, opts *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error) {
	return m.startFn(ctx, opts)
}

func (m *mockAsyncVideoModel) DoStatus(ctx context.Context, opts *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, error) {
	return m.statusFn(ctx, opts)
}

func (m *mockAsyncVideoModel) HandleWebhookOption(ctx context.Context, factory provider.VideoWebhookFactory) (string, provider.VideoWebhookReceived, error) {
	if m.webhookFn != nil {
		return m.webhookFn(ctx, factory)
	}
	return factory(ctx)
}

func defaultVideoResponseInfo() provider.VideoModelV3ResponseInfo {
	return provider.VideoModelV3ResponseInfo{Timestamp: time.Now(), ModelID: "mock-async-video"}
}

func TestExperimentalStartVideo_Basic(t *testing.T) {
	var capturedHeaders map[string]string
	model := &mockAsyncVideoModel{
		startFn: func(ctx context.Context, opts *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error) {
			capturedHeaders = opts.Headers
			return &provider.VideoModelV3OperationStartResult{
				Operation:        json.RawMessage(`{"jobId":"job_123"}`),
				Warnings:         []types.Warning{{Type: "other", Message: "queued"}},
				ProviderMetadata: map[string]interface{}{"gateway": map[string]interface{}{"jobId": "job_123"}},
				Response:         defaultVideoResponseInfo(),
			}, nil
		},
	}

	result, err := ExperimentalStartVideo(context.Background(), StartVideoOptions{
		Model:  model,
		Prompt: VideoPrompt{Text: "a sunset"},
	})
	if err != nil {
		t.Fatalf("ExperimentalStartVideo error = %v", err)
	}
	if string(result.Operation) != `{"jobId":"job_123"}` {
		t.Fatalf("Operation = %s, want job_123 payload", result.Operation)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Message != "queued" {
		t.Fatalf("Warnings = %+v", result.Warnings)
	}
	if _, ok := capturedHeaders["idempotency-key"]; !ok {
		t.Fatal("expected an idempotency-key header to be generated")
	}
}

func TestExperimentalStartVideo_HonorsCallerIdempotencyKey(t *testing.T) {
	var capturedHeaders map[string]string
	model := &mockAsyncVideoModel{
		startFn: func(ctx context.Context, opts *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error) {
			capturedHeaders = opts.Headers
			return &provider.VideoModelV3OperationStartResult{Operation: json.RawMessage(`{}`), Response: defaultVideoResponseInfo()}, nil
		},
	}

	_, err := ExperimentalStartVideo(context.Background(), StartVideoOptions{
		Model:   model,
		Prompt:  VideoPrompt{Text: "x"},
		Headers: map[string]string{"idempotency-key": "caller-key"},
	})
	if err != nil {
		t.Fatalf("ExperimentalStartVideo error = %v", err)
	}
	if capturedHeaders["idempotency-key"] != "caller-key" {
		t.Fatalf("idempotency-key = %q, want caller-key", capturedHeaders["idempotency-key"])
	}
}

func TestExperimentalStartVideo_RejectsWhenNoDoStart(t *testing.T) {
	model := &mockVideoModel{generateFn: func(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
		return nil, nil
	}}
	_, err := ExperimentalStartVideo(context.Background(), StartVideoOptions{Model: model, Prompt: VideoPrompt{Text: "x"}})
	if err == nil {
		t.Fatal("expected error for model without doStart")
	}
}

func TestExperimentalStartVideo_RejectsExceedingMaxVideosPerCall(t *testing.T) {
	max := 1
	model := &mockAsyncVideoModel{maxPerCall: &max}
	_, err := ExperimentalStartVideo(context.Background(), StartVideoOptions{
		Model: model, Prompt: VideoPrompt{Text: "x"}, N: 2,
	})
	if err == nil {
		t.Fatal("expected error for n exceeding maxVideosPerCall")
	}
}

func TestExperimentalGetVideoStatus_Completed(t *testing.T) {
	model := &mockAsyncVideoModel{
		statusFn: func(ctx context.Context, opts *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, error) {
			return &provider.VideoModelV3OperationStatusResult{
				Status: provider.VideoOperationStatusCompleted,
				Videos: []provider.VideoModelV3VideoData{{Type: "url", URL: "https://cdn.example.com/v.mp4", MediaType: "video/mp4"}},
				Response: defaultVideoResponseInfo(),
			}, nil
		},
	}
	result, err := ExperimentalGetVideoStatus(context.Background(), model, GetVideoStatusOptions{Operation: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatalf("ExperimentalGetVideoStatus error = %v", err)
	}
	if result.Status != provider.VideoOperationStatusCompleted || len(result.Videos) != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestExperimentalGetVideoStatus_Error(t *testing.T) {
	model := &mockAsyncVideoModel{
		statusFn: func(ctx context.Context, opts *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, error) {
			return &provider.VideoModelV3OperationStatusResult{Status: provider.VideoOperationStatusError, Error: "job failed", Response: defaultVideoResponseInfo()}, nil
		},
	}
	result, err := ExperimentalGetVideoStatus(context.Background(), model, GetVideoStatusOptions{Operation: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatalf("ExperimentalGetVideoStatus error = %v", err)
	}
	if result.Status != provider.VideoOperationStatusError || result.Error != "job failed" {
		t.Fatalf("result = %+v", result)
	}
}

func TestExperimentalGetVideoStatus_RejectsWhenNoDoStatus(t *testing.T) {
	model := &mockVideoModel{}
	_, err := ExperimentalGetVideoStatus(context.Background(), model, GetVideoStatusOptions{Operation: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("expected error for model without doStatus")
	}
}

func TestGenerateVideo_PollFlow_CompletesAfterPending(t *testing.T) {
	var statusCalls int32
	model := &mockAsyncVideoModel{
		startFn: func(ctx context.Context, opts *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error) {
			return &provider.VideoModelV3OperationStartResult{Operation: json.RawMessage(`{"id":1}`), Response: defaultVideoResponseInfo()}, nil
		},
		statusFn: func(ctx context.Context, opts *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, error) {
			n := atomic.AddInt32(&statusCalls, 1)
			if n < 2 {
				return &provider.VideoModelV3OperationStatusResult{Status: provider.VideoOperationStatusPending, Response: defaultVideoResponseInfo()}, nil
			}
			return &provider.VideoModelV3OperationStatusResult{
				Status: provider.VideoOperationStatusCompleted,
				Videos: []provider.VideoModelV3VideoData{{Type: "binary", Binary: []byte{0, 0, 0, 0}, MediaType: "video/mp4"}},
				Response: defaultVideoResponseInfo(),
			}, nil
		},
	}

	zero := 0
	result, err := GenerateVideo(context.Background(), GenerateVideoOptions{
		Model:  model,
		Prompt: VideoPrompt{Text: "x"},
		Poll:   &VideoPollOptions{IntervalMs: &zero},
	})
	if err != nil {
		t.Fatalf("GenerateVideo poll flow error = %v", err)
	}
	if len(result.Videos) != 1 {
		t.Fatalf("Videos = %d, want 1", len(result.Videos))
	}
	if atomic.LoadInt32(&statusCalls) < 2 {
		t.Fatalf("statusCalls = %d, want >= 2", statusCalls)
	}
}

func TestGenerateVideo_PollFlow_TimesOut(t *testing.T) {
	model := &mockAsyncVideoModel{
		startFn: func(ctx context.Context, opts *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error) {
			return &provider.VideoModelV3OperationStartResult{Operation: json.RawMessage(`{}`), Response: defaultVideoResponseInfo()}, nil
		},
		statusFn: func(ctx context.Context, opts *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, error) {
			return &provider.VideoModelV3OperationStatusResult{Status: provider.VideoOperationStatusPending, Response: defaultVideoResponseInfo()}, nil
		},
	}

	zero := 0
	timeout := 20
	_, err := GenerateVideo(context.Background(), GenerateVideoOptions{
		Model:  model,
		Prompt: VideoPrompt{Text: "x"},
		Poll:   &VideoPollOptions{IntervalMs: &zero, TimeoutMs: &timeout},
	})
	if err == nil {
		t.Fatal("expected polling timeout error")
	}
}

func TestGenerateVideo_WebhookFlow_WaitsForReceived(t *testing.T) {
	model := &mockAsyncVideoModel{
		startFn: func(ctx context.Context, opts *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error) {
			if opts.WebhookURL != "https://example.com/hook" {
				t.Fatalf("WebhookURL = %q, want https://example.com/hook", opts.WebhookURL)
			}
			return &provider.VideoModelV3OperationStartResult{Operation: json.RawMessage(`{}`), Response: defaultVideoResponseInfo()}, nil
		},
		statusFn: func(ctx context.Context, opts *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, error) {
			return &provider.VideoModelV3OperationStatusResult{
				Status:   provider.VideoOperationStatusCompleted,
				Videos:   []provider.VideoModelV3VideoData{{Type: "binary", Binary: []byte{0, 0, 0, 0}, MediaType: "video/mp4"}},
				Response: defaultVideoResponseInfo(),
			}, nil
		},
	}

	webhook := func(ctx context.Context) (string, provider.VideoWebhookReceived, error) {
		ch := make(chan struct{})
		go func() {
			time.Sleep(5 * time.Millisecond)
			close(ch)
		}()
		return "https://example.com/hook", func(waitCtx context.Context) (*provider.VideoOperationWebhook, error) {
			select {
			case <-ch:
				return &provider.VideoOperationWebhook{Body: []byte(`{"status":"done"}`)}, nil
			case <-waitCtx.Done():
				return nil, waitCtx.Err()
			}
		}, nil
	}

	result, err := GenerateVideo(context.Background(), GenerateVideoOptions{
		Model:   model,
		Prompt:  VideoPrompt{Text: "x"},
		Webhook: webhook,
	})
	if err != nil {
		t.Fatalf("GenerateVideo webhook flow error = %v", err)
	}
	if len(result.Videos) != 1 {
		t.Fatalf("Videos = %d, want 1", len(result.Videos))
	}
}

func TestGenerateVideo_FallsBackToDoGenerateWithoutPollOrWebhook(t *testing.T) {
	var startCalled, generateCalled bool
	model := &mockAsyncVideoModel{
		startFn: func(ctx context.Context, opts *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error) {
			startCalled = true
			return nil, errors.New("should not be called")
		},
		generateFn: func(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
			generateCalled = true
			return &provider.VideoModelV3Response{
				Videos:   []provider.VideoModelV3VideoData{{Type: "binary", Binary: []byte{0, 0, 0, 0}, MediaType: "video/mp4"}},
				Response: defaultVideoResponseInfo(),
			}, nil
		},
	}

	_, err := GenerateVideo(context.Background(), GenerateVideoOptions{Model: model, Prompt: VideoPrompt{Text: "x"}})
	if err != nil {
		t.Fatalf("GenerateVideo error = %v", err)
	}
	if startCalled {
		t.Fatal("doStart should not be called without Poll/Webhook")
	}
	if !generateCalled {
		t.Fatal("doGenerate should be called without Poll/Webhook")
	}
}

func TestNormalizeVideoInputs_FrameImagesPrecedenceOverInputReferences(t *testing.T) {
	normalized, err := normalizeVideoInputs(
		VideoPrompt{Text: "x"},
		[]VideoFrameImageInput{{Image: VideoPromptImage{Data: []byte{0x89, 'P', 'N', 'G'}}, FrameType: provider.VideoFrameTypeFirstFrame}},
		[]VideoReferenceInput{{Data: VideoPromptImage{URL: "https://example.com/ref.png"}}},
	)
	if err != nil {
		t.Fatalf("normalizeVideoInputs error = %v", err)
	}
	if normalized.InputReferences != nil {
		t.Fatalf("InputReferences = %+v, want nil (ignored because frameImages provided)", normalized.InputReferences)
	}
	if len(normalized.Warnings) != 1 {
		t.Fatalf("Warnings = %+v, want 1 warning about frameImages/inputReferences conflict", normalized.Warnings)
	}
}

func TestNormalizeVideoInputs_FirstFrameOverridesPromptImage(t *testing.T) {
	normalized, err := normalizeVideoInputs(
		VideoPrompt{Text: "x", Image: &VideoPromptImage{URL: "https://example.com/prompt.png"}},
		[]VideoFrameImageInput{{Image: VideoPromptImage{URL: "https://example.com/first.png"}, FrameType: provider.VideoFrameTypeFirstFrame}},
		nil,
	)
	if err != nil {
		t.Fatalf("normalizeVideoInputs error = %v", err)
	}
	if normalized.Image == nil || normalized.Image.URL != "https://example.com/first.png" {
		t.Fatalf("Image = %+v, want first_frame image to take precedence", normalized.Image)
	}
	if len(normalized.Warnings) != 1 {
		t.Fatalf("Warnings = %+v, want 1 warning about prompt.image override", normalized.Warnings)
	}
}

func TestNormalizeVideoInputs_InputReferenceDetectsVideoMediaType(t *testing.T) {
	// MP4 ftyp box signature (no explicit MediaType supplied).
	mp4 := []byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}
	normalized, err := normalizeVideoInputs(
		VideoPrompt{Text: "x"},
		nil,
		[]VideoReferenceInput{{Data: VideoPromptImage{Data: mp4}}},
	)
	if err != nil {
		t.Fatalf("normalizeVideoInputs error = %v", err)
	}
	if len(normalized.InputReferences) != 1 || normalized.InputReferences[0].MediaType != "video/mp4" {
		t.Fatalf("InputReferences = %+v, want video/mp4 detected", normalized.InputReferences)
	}
}
