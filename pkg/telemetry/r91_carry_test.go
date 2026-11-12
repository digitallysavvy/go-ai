package telemetry

import (
	"context"
	"errors"
	"fmt"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func r91carryTracer(t *testing.T) (*tracetest.SpanRecorder, *sdktrace.TracerProvider) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return rec, tp
}

func r91carryExceptionType(s sdktrace.ReadOnlySpan) (string, bool) {
	for _, ev := range s.Events() {
		if ev.Name != "exception" {
			continue
		}
		for _, kv := range ev.Attributes {
			if kv.Key == "exception.type" {
				return kv.Value.AsString(), true
			}
		}
	}
	return "", false
}

// core1-F01: the exception event's exception.type is the TS error.name
// (record-span.ts 58, legacy-open-telemetry.ts 59-65), not the Go type.
func TestR91carrySpanExceptionTypeIsTSName(t *testing.T) {
	rec, tp := r91carryTracer(t)
	tr := tp.Tracer("t")
	apiErr := providererrors.NewProviderError("p", 429, "", "slow", nil)
	cases := []struct {
		err  error
		want string
	}{
		{apiErr, "AI_APICallError"},
		{&providererrors.RetryError{Message: "r", LastError: apiErr}, "AI_RetryError"},
		{context.Canceled, "AbortError"},
		{errors.New("x"), "Error"},
	}
	for i, tc := range cases {
		_, span := tr.Start(context.Background(), fmt.Sprintf("gen%d", i))
		RecordErrorOnSpan(span, tc.err)
		span.End()
		_, legacy := tr.Start(context.Background(), fmt.Sprintf("legacy%d", i))
		legacyRecordSpanError(legacy, tc.err)
		legacy.End()
	}
	ended := rec.Ended()
	for i, tc := range cases {
		for j, s := range ended[2*i : 2*i+2] {
			got, ok := r91carryExceptionType(s)
			if !ok || got != tc.want {
				t.Fatalf("case %d span %d: exception.type = %q (ok=%v), want %q", i, j, got, ok, tc.want)
			}
		}
	}
}

// core1-F02: http.response.status_code only for the error itself or a
// RetryError's lastError (record-span.ts 51-58), not any wrapper.
func TestR91carrySpanStatusCodeNoUnwrap(t *testing.T) {
	rec, tp := r91carryTracer(t)
	tr := tp.Tracer("t")
	pe := &providererrors.ProviderError{StatusCode: 429}
	_, a := tr.Start(context.Background(), "wrapped")
	RecordErrorOnSpan(a, fmt.Errorf("x: %w", pe))
	a.End()
	_, b := tr.Start(context.Background(), "retry")
	RecordErrorOnSpan(b, &providererrors.RetryError{Message: "r", LastError: pe})
	b.End()
	_, c := tr.Start(context.Background(), "retry-wrapped")
	RecordErrorOnSpan(c, &providererrors.RetryError{Message: "r", LastError: fmt.Errorf("x: %w", pe)})
	c.End()
	ended := rec.Ended()
	if _, ok := attrValue(ended[0], "http.response.status_code"); ok {
		t.Fatal("wrapped ProviderError must not set http.response.status_code")
	}
	if v, ok := attrValue(ended[1], "http.response.status_code"); !ok || v.(int64) != 429 {
		t.Fatalf("RetryError lastError: %v %v", v, ok)
	}
	if _, ok := attrValue(ended[2], "http.response.status_code"); ok {
		t.Fatal("RetryError with a wrapped lastError must not set http.response.status_code")
	}
}
