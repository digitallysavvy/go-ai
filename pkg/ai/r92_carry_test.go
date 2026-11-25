package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type r92carrySpeechRecorder struct {
	telemetry.NoopTelemetryIntegration
	end []telemetry.TelemetryFinishEvent
}

func (r *r92carrySpeechRecorder) OnEnd(ctx context.Context, e telemetry.TelemetryFinishEvent) {
	r.end = append(r.end, e)
}

// TS generate-speech.ts getTelemetryResponseMetadata: the end event's
// response has timestamp, modelId and headers, never the response body.
func TestR92carrySpeechEndEventResponseHasNoBody(t *testing.T) {
	model := &testutil.MockSpeechModel{
		DoGenerateFunc: func(context.Context, *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
			return &types.SpeechResult{
				Audio:    []byte("RIFF....WAVEfmt "),
				Response: &types.ResponseMetadata{ModelID: "tts", Headers: map[string]string{"x": "y"}, Body: []byte("RIFF....WAVEfmt ")},
			}, nil
		},
	}
	rec := &r92carrySpeechRecorder{}
	res, err := GenerateSpeech(context.Background(), GenerateSpeechOptions{
		Model: model, Text: "Hello",
		Telemetry: &TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{rec}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.end) != 1 || rec.end[0].Response == nil {
		t.Fatalf("end events = %+v", rec.end)
	}
	if r := rec.end[0].Response; r.Body != nil || r.ModelID != "tts" || r.Headers["x"] != "y" {
		t.Fatalf("end response = %+v, want no body", r)
	}
	// The result's responses keep the body (TS responses: [result.response]).
	if len(res.Responses) != 1 || res.Responses[0].Body == nil {
		t.Fatalf("result responses = %+v", res.Responses)
	}
}

// TS handle-fetch-error.ts: a response-header timeout is the retryable
// "Cannot connect to API" APICallError, so the core retry retries it once a
// provider error handler wrapped it.
func TestR92carryTransportTimeoutIsRetried(t *testing.T) {
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-done:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(done)
	hc := &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: 30 * time.Millisecond}}
	_, err := internalhttp.NewClient(internalhttp.Config{BaseURL: srv.URL, HTTPClient: hc}).Do(context.Background(), internalhttp.Request{Method: http.MethodGet, Path: "/"})
	if err == nil {
		t.Fatal("no error")
	}
	if _, ok := internalhttp.NoResponseError(err); ok {
		t.Fatal("transport timeout returned as an abort")
	}
	wrapped := providererrors.NewProviderError("p", 0, "", err.Error(), err)
	if !IsGatewayCallRetryable(wrapped) || !IsGatewayCallRetryable(err) {
		t.Fatalf("transport timeout not retryable: %v", err)
	}
	// The caller's own deadline is never retried.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = internalhttp.NewClient(internalhttp.Config{BaseURL: srv.URL}).Do(ctx, internalhttp.Request{Method: http.MethodGet, Path: "/"})
	if IsGatewayCallRetryable(err) {
		t.Fatalf("caller deadline retryable: %v", err)
	}
}
