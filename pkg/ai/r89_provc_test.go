package ai

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r89provcStartStatusOnlyModel is a video model whose TS twin has no
// doGenerate: GenerateVideo must drive DoStart/DoStatus and never call the
// DoGenerate shim.
type r89provcStartStatusOnlyModel struct {
	mu            sync.Mutex
	startHeaders  []map[string]string
	statusCalls   int
	generateCalls int
	// statuses are returned in order; the last repeats.
	statuses   []func() (*provider.VideoModelV3OperationStatusResult, error)
	intervalMs int
	timeoutMs  int
}

func (m *r89provcStartStatusOnlyModel) SpecificationVersion() string { return "v4" }
func (m *r89provcStartStatusOnlyModel) Provider() string             { return "r89provc.video" }
func (m *r89provcStartStatusOnlyModel) ModelID() string              { return "start-status-only" }
func (m *r89provcStartStatusOnlyModel) MaxVideosPerCall() *int       { return nil }

func (m *r89provcStartStatusOnlyModel) DoGenerate(context.Context, *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
	m.mu.Lock()
	m.generateCalls++
	m.mu.Unlock()
	return nil, providererrors.NewProviderError("r89provc", 500, "", "DoGenerate shim must not be used", nil)
}

func (m *r89provcStartStatusOnlyModel) DoStart(_ context.Context, opts *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h := map[string]string{}
	for k, v := range opts.Headers {
		h[k] = v
	}
	m.startHeaders = append(m.startHeaders, h)
	return &provider.VideoModelV3OperationStartResult{Operation: json.RawMessage(`{"id":"op"}`), Warnings: []types.Warning{}}, nil
}

func (m *r89provcStartStatusOnlyModel) DoStatus(context.Context, *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, error) {
	m.mu.Lock()
	i := m.statusCalls
	m.statusCalls++
	m.mu.Unlock()
	if i >= len(m.statuses) {
		i = len(m.statuses) - 1
	}
	return m.statuses[i]()
}

func (m *r89provcStartStatusOnlyModel) VideoStartStatusPollDefaults(map[string]interface{}) (int, int) {
	return m.intervalMs, m.timeoutMs
}

func r89provcCompleted() (*provider.VideoModelV3OperationStatusResult, error) {
	return &provider.VideoModelV3OperationStatusResult{
		Status: provider.VideoOperationStatusCompleted,
		Videos: []provider.VideoModelV3VideoData{{Type: "base64", Data: "AAAA", MediaType: "video/mp4"}},
	}, nil
}

func r89provcPending() (*provider.VideoModelV3OperationStatusResult, error) {
	return &provider.VideoModelV3OperationStatusResult{Status: provider.VideoOperationStatusPending}, nil
}

// TS generate-video.ts useStartStatus = hasStartStatus && (poll != null ||
// webhook != null || model.doGenerate == null): a start/status-only model is
// polled by the core flow without Poll, its start carries one minted
// idempotency key, and a transient status failure retries doStatus only
// (generate-video.test.ts "should use doStart/doStatus when model only has
// doStart/doStatus (no doGenerate)", "should retry doStatus without
// restarting the operation").
func TestR89provcGenerateVideoStartStatusOnlyModel(t *testing.T) {
	t.Run("core flow without poll", func(t *testing.T) {
		model := &r89provcStartStatusOnlyModel{intervalMs: 1, statuses: []func() (*provider.VideoModelV3OperationStatusResult, error){r89provcPending, r89provcCompleted}}
		result, err := GenerateVideo(context.Background(), GenerateVideoOptions{Model: model, Prompt: VideoPrompt{Text: "a cat"}})
		if err != nil {
			t.Fatal(err)
		}
		if model.generateCalls != 0 {
			t.Fatalf("DoGenerate called %d times", model.generateCalls)
		}
		if len(model.startHeaders) != 1 || !strings.HasPrefix(model.startHeaders[0]["idempotency-key"], "aisdk_vid_") {
			t.Fatalf("start headers = %v, want one start with an aisdk_vid_ idempotency key", model.startHeaders)
		}
		if model.statusCalls != 2 || len(result.Videos) != 1 {
			t.Fatalf("status calls %d, videos %d", model.statusCalls, len(result.Videos))
		}
	})

	t.Run("transient status failure retries doStatus only", func(t *testing.T) {
		transient := func() (*provider.VideoModelV3OperationStatusResult, error) {
			return nil, providererrors.NewProviderError("r89provc", 503, "", "Service Unavailable", nil)
		}
		model := &r89provcStartStatusOnlyModel{intervalMs: 1, statuses: []func() (*provider.VideoModelV3OperationStatusResult, error){transient, r89provcCompleted}}
		if _, err := GenerateVideo(context.Background(), GenerateVideoOptions{Model: model, Prompt: VideoPrompt{Text: "a cat"}}); err != nil {
			t.Fatal(err)
		}
		if len(model.startHeaders) != 1 || model.generateCalls != 0 {
			t.Fatalf("starts %d, DoGenerate calls %d: want one start and no DoGenerate", len(model.startHeaders), model.generateCalls)
		}
	})

	t.Run("failed status gives the TS error text", func(t *testing.T) {
		failed := func() (*provider.VideoModelV3OperationStatusResult, error) {
			return &provider.VideoModelV3OperationStatusResult{Status: provider.VideoOperationStatusError, Error: "Video generation failed: boom"}, nil
		}
		model := &r89provcStartStatusOnlyModel{intervalMs: 1, statuses: []func() (*provider.VideoModelV3OperationStatusResult, error){failed}}
		_, err := GenerateVideo(context.Background(), GenerateVideoOptions{Model: model, Prompt: VideoPrompt{Text: "a cat"}})
		if err == nil || err.Error() != "Video generation failed: boom" {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("model poll defaults apply and Poll wins", func(t *testing.T) {
		model := &r89provcStartStatusOnlyModel{intervalMs: 1, timeoutMs: 30, statuses: []func() (*provider.VideoModelV3OperationStatusResult, error){r89provcPending}}
		_, err := GenerateVideo(context.Background(), GenerateVideoOptions{Model: model, Prompt: VideoPrompt{Text: "a cat"}})
		if err == nil || err.Error() != "Video generation timed out after 30ms." {
			t.Fatalf("err = %v", err)
		}
		timeout := 20
		model = &r89provcStartStatusOnlyModel{intervalMs: 1, timeoutMs: 30, statuses: []func() (*provider.VideoModelV3OperationStatusResult, error){r89provcPending}}
		_, err = GenerateVideo(context.Background(), GenerateVideoOptions{Model: model, Prompt: VideoPrompt{Text: "a cat"}, Poll: &VideoPollOptions{TimeoutMs: &timeout}})
		if err == nil || err.Error() != "Video generation timed out after 20ms." {
			t.Fatalf("err = %v", err)
		}
	})
}
