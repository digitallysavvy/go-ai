package telemetry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// portcarrytelStatePart has the shape of provider.DecisionStatePart (that
// package imports telemetry, so a test here cannot use it).
type portcarrytelStatePart struct {
	Type string
	Text string
	File *types.FileContent
}

func portcarrytelState() []portcarrytelStatePart {
	return []portcarrytelStatePart{
		{Type: "text", Text: "hi"},
		{Type: "file", File: &types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://x.example/a.png?sig=secret"}}},
		{Type: "file", File: &types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeReference, Reference: map[string]string{"openai": "file-secret"}}}},
		{Type: "file", File: &types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "data:image/png;base64,AAAA"}}},
		{Type: "file", File: &types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AAAA"}}},
		{Type: "file", File: &types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte{1}}}},
	}
}

// TS restricted-telemetry-dispatcher: file locations are redacted, inline
// data (also a data: URL) is kept, the input is not modified.
func TestPortcarrytelRedactFileLocations(t *testing.T) {
	in := portcarrytelState()
	out, ok := redactFileLocations(in).([]portcarrytelStatePart)
	if !ok {
		t.Fatal("not a state slice")
	}
	for i, wantRedacted := range []bool{false, true, true, false, false, false} {
		if i == 0 {
			continue
		}
		got := out[i].File.FileData
		isRedacted := got.DataString == "[redacted]"
		if isRedacted != wantRedacted {
			t.Errorf("part %d redacted = %v, want %v (%+v)", i, isRedacted, wantRedacted, got)
		}
	}
	if in[1].File.FileData.URL != "https://x.example/a.png?sig=secret" || in[2].File.FileData.Reference == nil {
		t.Errorf("input state modified")
	}
	if s := "x"; redactFileLocations(s) != interface{}(s) {
		t.Errorf("non-list state changed")
	}
}

type portcarrytelRec struct {
	TelemetryIntegration
	starts []ExtractStartEvent
	ends   []ExtractEndEvent
	mcs    []ExtractionModelCallStartEvent
	mce    []ExtractionModelCallEndEvent
}

func (r *portcarrytelRec) OnExtractStart(ctx context.Context, e ExtractStartEvent) context.Context {
	r.starts = append(r.starts, e)
	return ctx
}
func (r *portcarrytelRec) OnExtractEnd(_ context.Context, e ExtractEndEvent) {
	r.ends = append(r.ends, e)
}
func (r *portcarrytelRec) OnExtractionModelCallStart(_ context.Context, e ExtractionModelCallStartEvent) {
	r.mcs = append(r.mcs, e)
}
func (r *portcarrytelRec) OnExtractionModelCallEnd(_ context.Context, e ExtractionModelCallEndEvent) {
	r.mce = append(r.mce, e)
}

func TestPortcarrytelExtractEventsAreRestricted(t *testing.T) {
	rec := &portcarrytelRec{}
	set := &Settings{Integrations: []TelemetryIntegration{rec}}
	maxTok := 5
	start := ExtractStartEvent{Settings: set, CallID: t.Name(), OperationID: "ai.extract", ModelID: "m", ModelProvider: "p",
		State: portcarrytelState(), Fields: map[string]interface{}{"a": 1}, ReferenceDate: "2026-01-02", MaxOutputTokens: &maxTok,
		Headers: map[string]string{"Authorization": "secret"}, ProviderOptions: map[string]interface{}{"x": "secret"}}
	var diag []ExtractStartEvent
	unsub := SubscribeDiagnosticTyped(DiagnosticEventOnExtractStart, func(_ context.Context, e ExtractStartEvent) error {
		diag = append(diag, e)
		return nil
	})
	defer unsub()
	ctx := FireOnExtractStart(context.Background(), start)
	FireOnExtractionModelCallStart(ctx, ExtractionModelCallStartEvent{Settings: set, CallID: t.Name(), State: portcarrytelState()})
	FireOnExtractionModelCallEnd(ctx, ExtractionModelCallEndEvent{ExtractionModelCallStartEvent: ExtractionModelCallStartEvent{Settings: set, CallID: t.Name(), State: portcarrytelState()}})
	FireOnExtractEnd(ctx, ExtractEndEvent{ExtractStartEvent: start, Answers: map[string]interface{}{"a": "b"}})

	if len(rec.starts) != 1 || len(rec.ends) != 1 || len(rec.mcs) != 1 || len(rec.mce) != 1 {
		t.Fatalf("handler calls = %d/%d/%d/%d", len(rec.starts), len(rec.mcs), len(rec.mce), len(rec.ends))
	}
	for _, e := range []ExtractStartEvent{rec.starts[0], rec.ends[0].ExtractStartEvent} {
		if e.Headers != nil || e.ProviderOptions != nil {
			t.Errorf("headers / provider options reach telemetry: %v %v", e.Headers, e.ProviderOptions)
		}
		if e.AIVersion == "" && rec.ends[0].AIVersion == "" {
			t.Errorf("no ai version")
		}
		if got := e.State.([]portcarrytelStatePart)[1].File.FileData.DataString; got != "[redacted]" {
			t.Errorf("state file location = %q", got)
		}
	}
	for _, st := range []interface{}{rec.mcs[0].State, rec.mce[0].State} {
		if got := st.([]portcarrytelStatePart)[1].File.FileData.DataString; got != "[redacted]" {
			t.Errorf("model call state file location = %q", got)
		}
	}
	if len(diag) != 1 || diag[0].Headers != nil {
		t.Errorf("diagnostic event = %+v", diag)
	}
	if start.Headers == nil || start.ProviderOptions == nil {
		t.Errorf("caller event modified")
	}
}

// TS filter-telemetry-event.ts: hidden inputs empty the fields and drop the
// reference date; hidden outputs empty the answers.
func TestPortcarrytelExtractEventsFilterRecordingFlags(t *testing.T) {
	rec := &portcarrytelRec{}
	off := false
	set := &Settings{Integrations: []TelemetryIntegration{rec}, RecordInputs: &off, RecordOutputs: &off}
	start := ExtractStartEvent{Settings: set, CallID: t.Name(), Fields: map[string]interface{}{"a": 1}, ReferenceDate: "2026-01-02", State: "s"}
	ctx := FireOnExtractStart(context.Background(), start)
	FireOnExtractEnd(ctx, ExtractEndEvent{ExtractStartEvent: start, Answers: map[string]interface{}{"a": "b"}})
	got := rec.starts[0]
	if f, _ := got.Fields.(map[string]interface{}); len(f) != 0 || got.ReferenceDate != "" {
		t.Errorf("fields = %v, reference date = %q", got.Fields, got.ReferenceDate)
	}
	if a, _ := rec.ends[0].Answers.(map[string]interface{}); len(a) != 0 {
		t.Errorf("answers = %v", rec.ends[0].Answers)
	}
}

func portcarrytelSpans(t *testing.T, opts OpenTelemetryOptions, run func(set *Settings, id string)) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	opts.Tracer = tp.Tracer("t")
	set := &Settings{Integrations: []TelemetryIntegration{NewOpenTelemetry(opts)}}
	run(set, t.Name())
	_ = tp.Shutdown(context.Background())
	return rec
}

// TS open-telemetry.test.ts extract integration: a root and a nested
// "extract {model}" span, the supplemental extraction attributes gated by
// ExperimentalExtraction.
func TestPortcarrytelOpenTelemetryExtractSpans(t *testing.T) {
	in, out := int64(3), int64(4)
	run := func(set *Settings, id string) {
		start := ExtractStartEvent{Settings: set, CallID: id, OperationID: "ai.extract", ModelID: "m", ModelProvider: "openai.chat",
			State: []string{"s"}, Fields: map[string]interface{}{"city": "text"}, ReferenceDate: "2026-01-02"}
		ctx := FireOnExtractStart(context.Background(), start)
		FireOnExtractionModelCallStart(ctx, ExtractionModelCallStartEvent{Settings: set, CallID: id, OperationID: "ai.extract.doExtract", ModelID: "m", ModelProvider: "openai.chat",
			State: []string{"s"}, Fields: map[string]interface{}{"city": "text"}, ReferenceDate: "2026-01-02"})
		FireOnExtractionModelCallEnd(ctx, ExtractionModelCallEndEvent{ExtractionModelCallStartEvent: ExtractionModelCallStartEvent{Settings: set, CallID: id},
			Answers: map[string]interface{}{"city": "Paris"}, Usage: TelemetryUsage{InputTokens: &in, OutputTokens: &out}, ResponseID: "r1", ResponseModelID: "m-2"})
		FireOnExtractEnd(ctx, ExtractEndEvent{ExtractStartEvent: start, Answers: map[string]interface{}{"city": "Paris"}})
	}
	for _, enabled := range []bool{true, false} {
		rec := portcarrytelSpans(t, OpenTelemetryOptions{ExperimentalExtraction: enabled}, run)
		spans := findSpans(rec, "extract m")
		if len(spans) != 2 {
			t.Fatalf("enabled=%v: extract spans = %d", enabled, len(spans))
		}
		var root, child sdktrace.ReadOnlySpan
		for _, s := range spans {
			if s.Parent().IsValid() {
				child = s
			} else {
				root = s
			}
		}
		if root == nil || child == nil {
			t.Fatalf("root / child missing")
		}
		for _, s := range []sdktrace.ReadOnlySpan{root, child} {
			if v, _ := attrValue(s, "gen_ai.operation.name"); v != "extract" {
				t.Errorf("operation name = %v", v)
			}
			_, hasState := attrValue(s, "ai.extraction.state")
			_, hasFields := attrValue(s, "ai.extraction.fields")
			_, hasDate := attrValue(s, "ai.extraction.referenceDate")
			_, hasAnswers := attrValue(s, "ai.extraction.answers")
			if hasState != enabled || hasFields != enabled || hasDate != enabled || hasAnswers != enabled {
				t.Errorf("enabled=%v %s: state/fields/date/answers = %v/%v/%v/%v", enabled, s.Name(), hasState, hasFields, hasDate, hasAnswers)
			}
		}
		if enabled {
			if v, _ := attrValue(root, "ai.extraction.answers"); v != `{"city":"Paris"}` {
				t.Errorf("answers = %v", v)
			}
			if v, _ := attrValue(root, "ai.extraction.referenceDate"); v != "2026-01-02" {
				t.Errorf("reference date = %v", v)
			}
		}
		if v, _ := attrValue(child, "gen_ai.response.id"); v != "r1" {
			t.Errorf("response id = %v", v)
		}
		if v, _ := attrValue(child, "gen_ai.response.model"); v != "m-2" {
			t.Errorf("response model = %v", v)
		}
		if v, _ := attrValue(child, "gen_ai.usage.input_tokens"); v != int64(3) {
			t.Errorf("input tokens = %v", v)
		}
		if root.EndTime().IsZero() || child.EndTime().IsZero() {
			t.Errorf("a span never ended")
		}
	}
}

// TS onError / onAbort end the open extraction span; onError records the
// error on it.
func TestPortcarrytelExtractionSpanClosedOnErrorAndAbort(t *testing.T) {
	boom := errors.New("boom")
	for _, abort := range []bool{false, true} {
		rec := portcarrytelSpans(t, OpenTelemetryOptions{}, func(set *Settings, id string) {
			start := ExtractStartEvent{Settings: set, CallID: id, OperationID: "ai.extract", ModelID: "m", ModelProvider: "p"}
			ctx := FireOnExtractStart(context.Background(), start)
			FireOnExtractionModelCallStart(ctx, ExtractionModelCallStartEvent{Settings: set, CallID: id, OperationID: "ai.extract.doExtract", ModelID: "m", ModelProvider: "p"})
			if abort {
				FireOnAbort(ctx, TelemetryAbortEvent{Settings: set, CallID: id})
			} else {
				FireOnError(ctx, TelemetryErrorEvent{Settings: set, CallID: id, Error: boom})
			}
		})
		spans := findSpans(rec, "extract m")
		if len(spans) != 2 {
			t.Fatalf("abort=%v: spans = %d", abort, len(spans))
		}
		for _, s := range spans {
			if s.EndTime().IsZero() {
				t.Errorf("abort=%v: span never ended", abort)
			}
			if (s.Status().Code == codes.Error) == abort {
				t.Errorf("abort=%v: status = %v", abort, s.Status().Code)
			}
		}
	}
}

// TS redact-telemetry-credentials.ts redactDownloadError (15bfd6b262).
func TestPortcarrytelDownloadErrorRedacted(t *testing.T) {
	download := &providererrors.DownloadError{URL: "https://x.example/a.png?sig=secret", StatusCode: 403, StatusText: "Forbidden", Cause: errors.New("dial https://x.example/a.png?sig=secret")}
	for name, err := range map[string]error{
		"direct":  download,
		"wrapped": fmt.Errorf("extract failed: %w", download),
	} {
		got := redactCredentials(TelemetryErrorEvent{Error: err}).Error
		var d *providererrors.DownloadError
		if !errors.As(got, &d) {
			t.Fatalf("%s: not a download error: %v", name, got)
		}
		if d.URL != "[redacted]" || d.StatusCode != 403 || d.StatusText != "Forbidden" || d.Cause != nil {
			t.Errorf("%s: %+v", name, d)
		}
		if strings.Contains(got.Error(), "secret") || got.Error() != "Failed to download a file." {
			t.Errorf("%s: message %q", name, got.Error())
		}
	}
	if download.URL != "https://x.example/a.png?sig=secret" {
		t.Errorf("original modified")
	}
	other := errors.New("plain")
	if got := redactCredentials(TelemetryErrorEvent{Error: other}).Error; got != other {
		t.Errorf("unrelated error changed: %v", got)
	}
}
