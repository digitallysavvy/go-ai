package ai

import (
	"context"
	"net/http"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	promptutils "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestR92integGenerateTextToolSpanArgsKeepModelKeyOrder (cr-r92-carry-to-gen):
// TS records toolCall.input, the object parsed from the model's text, so
// ai.toolCall.args keeps the model's key order; Go passes RawArguments as
// ArgsText for the order.
func TestR92integGenerateTextToolSpanArgsKeepModelKeyOrder(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	calls := 0
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		calls++
		if calls == 1 {
			return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{{
				ID: "c1", ToolName: "w", RawArguments: `{"zeta":1,"alpha":2}`, Arguments: map[string]interface{}{"zeta": float64(1), "alpha": float64(2)},
			}}}, nil
		}
		return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
	}}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "hi",
		Tools: []types.Tool{{Name: "w", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		}}},
		StopWhen: []StopCondition{StepCountIs(2)},
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{
			telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")}),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	span := findSpanByName(rec.Ended(), "ai.toolCall")
	if span == nil {
		t.Fatal("no ai.toolCall span")
	}
	for _, kv := range span.Attributes() {
		if kv.Key == "ai.toolCall.args" {
			if got := kv.Value.AsString(); got != `{"zeta":1,"alpha":2}` {
				t.Fatalf("ai.toolCall.args = %s, want the model's key order", got)
			}
			return
		}
	}
	t.Fatal("no ai.toolCall.args")
}

// generated-file-download.test.ts 109 (stream rows, custom download false /
// true): aborting the call cancels the generated file download of a
// StreamText call, and the stream ends.
func TestR92integStreamGeneratedFileDownloadAbort(t *testing.T) {
	for _, custom := range []bool{false, true} {
		started := make(chan struct{})
		canceled := make(chan struct{})
		opts := StreamTextOptions{Prompt: "image", OnError: func(context.Context, error) {}}
		if custom {
			opts.Model = fileModel(urlFile("https://93.184.215.14/generated.txt"))
			opts.ExperimentalDownload = CreateDownload(&DownloadOptions{Transport: r91genRoundTripper(func(r *http.Request) (*http.Response, error) {
				close(started)
				<-r.Context().Done()
				close(canceled)
				return nil, r.Context().Err()
			})})
		} else {
			allowLoopbackDownloadTransport(t)
			restore := downloadURLValidator
			downloadURLValidator = func(string) error { return nil }
			server := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				<-r.Context().Done()
				close(canceled)
			}))
			opts.Model = fileModel(urlFile(server.URL))
			defer func() { downloadURLValidator = restore; server.Close() }()
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			res, err := StreamText(ctx, opts)
			if err == nil {
				_ = res.ConsumeStream()
			}
		}()
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatalf("custom=%v: download never started", custom)
		}
		cancel()
		for name, ch := range map[string]chan struct{}{"download": canceled, "stream": done} {
			select {
			case <-ch:
			case <-time.After(5 * time.Second):
				t.Fatalf("custom=%v: %s not ended by the abort", custom, name)
			}
		}
	}
}

// generated-file-download.test.ts 161 (stream row): StreamText downloads a
// generated file URL through ExperimentalDownload, after the empty prompt
// batch.
func TestR92integStreamGeneratedFileDownloadBatches(t *testing.T) {
	var calls [][]promptutils.DownloadRequest
	res, err := StreamText(context.Background(), StreamTextOptions{Model: fileModel(urlFile("https://example.com/generated.txt")), Prompt: "image",
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
	if err := res.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	files := res.Files()
	if len(files) != 1 || string(files[0].Data) != "Hello World" {
		t.Fatalf("files = %+v", files)
	}
	if len(calls) != 2 || len(calls[0]) != 0 || len(calls[1]) != 1 || calls[1][0] != (promptutils.DownloadRequest{URL: "https://example.com/generated.txt"}) {
		t.Fatalf("download calls = %+v, want [[] [generated.txt]]", calls)
	}
}
