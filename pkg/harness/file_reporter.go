package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness/internal/jsstr"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// FileReporterOptions configures NewFileReporter. Mirrors TS
// `FileReporterOptions`.
type FileReporterOptions struct {
	// Dir is the directory for the events file (created if absent). Required.
	Dir string

	// FailOnly buffers a turn's records in memory and writes them only if the
	// turn produced an error (an error-level diagnostic, a failed tool, or an
	// error finish). Default false (write everything).
	FailOnly bool

	// FileName is the file name within Dir. Default "events.jsonl". Like TS
	// (`${dir}/${fileName}`) it is not validated; a name the OS cannot open
	// surfaces through Err, never through a turn.
	FileName string
}

// FileReporter is a harness observability reporter that writes a unified,
// non-lossy events.jsonl containing both the telemetry span lifecycle (turn,
// step, tool) and the forwarded bridge diagnostics (console lines and
// structured events). It is a single value registered in
// AgentSettings.Telemetry.Integrations: the telemetry dispatcher drives its
// span methods and the agent calls IngestDiagnostic for diagnostics (enable
// them with AgentSettings.Debug or HARNESS_DEBUG).
//
// No external collector or OpenTelemetry setup is required. Records are
// buffered per turn and appended, one JSON object per line, when the turn
// ends. Input and output content is recorded unless the call's
// telemetry.Options has RecordInputs / RecordOutputs false; note that a
// non-nil telemetry.Options with those fields left at their zero value
// disables recording, so start from telemetry.DefaultSettings(). Call Close
// when done to release the file handle.
//
// A FileReporter is safe for concurrent use. Mirrors TS `FileReporter`
// (createFileReporter).
type FileReporter struct {
	telemetry.NoopTelemetryIntegration

	dir      string
	path     string
	failOnly bool

	mu         sync.Mutex
	turns      map[string]*reporterTurn
	lastOpen   string
	hasLast    bool
	file       *os.File
	dirReady   bool
	closed     bool
	writeError error
}

type reporterTurn struct {
	lines   [][]byte
	errored bool
}

// NewFileReporter creates a FileReporter. It returns an error if Dir is empty.
func NewFileReporter(opts FileReporterOptions) (*FileReporter, error) {
	if opts.Dir == "" {
		return nil, errors.New("harness: FileReporterOptions.Dir is required")
	}
	name := opts.FileName
	if name == "" {
		name = "events.jsonl"
	}
	return &FileReporter{
		dir:      opts.Dir,
		path:     opts.Dir + "/" + name,
		failOnly: opts.FailOnly,
		turns:    map[string]*reporterTurn{},
	}, nil
}

// Path returns the path of the file the reporter appends to.
func (r *FileReporter) Path() string { return r.path }

// Err returns the first error encountered writing the file, if any. Writing
// is best-effort and never fails a turn, so this is the only way to observe
// a failed write.
func (r *FileReporter) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.writeError
}

// Close flushes the records of any turn that is still open (unless FailOnly
// is set, since an unfinished turn is not known to have failed), then closes
// the file. Records arriving after Close are dropped. Close is idempotent.
func (r *FileReporter) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	if !r.failOnly {
		for id, t := range r.turns {
			r.flushLocked(t.lines)
			delete(r.turns, id)
		}
	}
	r.closed = true
	if r.file == nil {
		return nil
	}
	err := r.file.Close()
	r.file = nil
	return err
}

// orderedRecord is a JSON object that preserves insertion order, matching
// the key order of the JS object literals the TS reporter serializes.
type orderedRecord struct {
	keys []string
	vals []interface{}
}

func (o *orderedRecord) set(key string, val interface{}) *orderedRecord {
	o.keys = append(o.keys, key)
	o.vals = append(o.vals, val)
	return o
}

// setOpt adds key only when val is non-nil (JS omits undefined properties).
func (o *orderedRecord) setOpt(key string, val interface{}) *orderedRecord {
	if val == nil {
		return o
	}
	return o.set(key, val)
}

func (o *orderedRecord) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, _ := jsstr.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := jsstr.Marshal(o.vals[i])
		if err != nil {
			// Keep the record rather than losing it: replace the one value
			// that cannot be serialized.
			vb, _ = jsstr.Marshal(fmt.Sprintf("[unserializable: %v]", err))
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func newRecord(kind string, callID string) *orderedRecord {
	return (&orderedRecord{}).set("ts", time.Now().UnixMilli()).set("kind", kind).set("callId", callID)
}

// serializeReporterError mirrors TS serializeError: an error becomes
// {name, message}. Go errors have no class name, so name is the error's Go
// type (for example "fmt.wrapError").
func serializeReporterError(err error) interface{} {
	if err == nil {
		return nil
	}
	name := strings.TrimPrefix(reflect.TypeOf(err).String(), "*")
	return (&orderedRecord{}).set("name", name).set("message", err.Error())
}

// rawReporterError mirrors a raw (unserialized) JS Error in a JSON record:
// its properties are not enumerable, so JSON.stringify writes `{}`.
func rawReporterError(err error) interface{} {
	if err == nil {
		return nil
	}
	return &orderedRecord{}
}

func intPtrOpt(o *orderedRecord, key string, v *int64) {
	if v != nil {
		o.set(key, *v)
	}
}

// reporterUsage renders usage in the TS LanguageModelUsage shape.
func reporterUsage(u telemetry.TelemetryUsage) *orderedRecord {
	out := &orderedRecord{}
	intPtrOpt(out, "inputTokens", u.InputTokens)
	if u.NoCacheInputTokens != nil || u.CacheReadInputTokens != nil || u.CacheCreationInputTokens != nil {
		d := &orderedRecord{}
		intPtrOpt(d, "noCacheTokens", u.NoCacheInputTokens)
		intPtrOpt(d, "cacheReadTokens", u.CacheReadInputTokens)
		intPtrOpt(d, "cacheWriteTokens", u.CacheCreationInputTokens)
		out.set("inputTokenDetails", d)
	}
	intPtrOpt(out, "outputTokens", u.OutputTokens)
	if u.OutputTextTokens != nil || u.ReasoningTokens != nil {
		d := &orderedRecord{}
		intPtrOpt(d, "textTokens", u.OutputTextTokens)
		intPtrOpt(d, "reasoningTokens", u.ReasoningTokens)
		out.set("outputTokenDetails", d)
	}
	intPtrOpt(out, "totalTokens", u.TotalTokens)
	return out
}

func recordInputs(s *telemetry.Options) bool  { return s == nil || s.RecordInputs }
func recordOutputs(s *telemetry.Options) bool { return s == nil || s.RecordOutputs }

// turnLocked returns the buffer for callID, creating it if needed.
func (r *FileReporter) turnLocked(callID string) *reporterTurn {
	t, ok := r.turns[callID]
	if !ok {
		t = &reporterTurn{}
		r.turns[callID] = t
	}
	return t
}

// record appends rec to its turn's buffer. With useLast (a diagnostic), the
// record goes to the most recently started, still-open turn; with no open
// turn it is written standalone, best-effort.
func (r *FileReporter) record(callID string, useLast bool, rec *orderedRecord) {
	line, err := jsstr.Marshal(rec)
	if err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	id := callID
	if useLast {
		if !r.hasLast {
			r.flushLocked([][]byte{line})
			return
		}
		id = r.lastOpen
	}
	t := r.turnLocked(id)
	t.lines = append(t.lines, line)
}

func (r *FileReporter) finishTurnLocked(callID string) {
	t, ok := r.turns[callID]
	if !ok {
		return
	}
	delete(r.turns, callID)
	if r.hasLast && r.lastOpen == callID {
		r.hasLast = false
		r.lastOpen = ""
	}
	if r.failOnly && !t.errored {
		return
	}
	r.flushLocked(t.lines)
}

// flushLocked appends lines to the file, creating the directory (0700) and
// file (0600: records can contain prompts and tool output) on first use.
// Failures are remembered in Err and never surface to the turn.
func (r *FileReporter) flushLocked(lines [][]byte) {
	if len(lines) == 0 {
		return
	}
	if err := r.ensureFileLocked(); err != nil {
		if r.writeError == nil {
			r.writeError = err
		}
		return
	}
	var buf bytes.Buffer
	for _, l := range lines {
		buf.Write(l)
		buf.WriteByte('\n')
	}
	// One Write call (the file is opened O_APPEND) so a turn's lines are
	// never interleaved with another writer's partial lines.
	if _, err := r.file.Write(buf.Bytes()); err != nil && r.writeError == nil {
		r.writeError = err
	}
}

func (r *FileReporter) ensureFileLocked() error {
	if r.file != nil {
		return nil
	}
	if !r.dirReady {
		if err := os.MkdirAll(r.dir, 0o700); err != nil {
			return err
		}
		r.dirReady = true
	}
	f, err := os.OpenFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // path is the operator-configured Dir and FileName, as in TS
	if err != nil {
		return err
	}
	r.file = f
	return nil
}

func (r *FileReporter) markErrored(callID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed {
		r.turnLocked(callID).errored = true
	}
}

// OnStart records the turn-start line and makes the turn the target of
// subsequent diagnostics.
func (r *FileReporter) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	r.mu.Lock()
	r.lastOpen, r.hasLast = e.CallID, true
	r.mu.Unlock()
	rec := newRecord("turn-start", e.CallID)
	if e.OperationType != "" {
		rec.set("operationId", e.OperationType)
	}
	if e.ModelProvider != "" {
		rec.set("provider", e.ModelProvider)
	}
	if e.ModelID != "" {
		rec.set("modelId", e.ModelID)
	}
	if recordInputs(e.Settings) {
		msgs := e.Messages
		if msgs == nil {
			msgs = []types.Message{}
		}
		in := (&orderedRecord{}).set("messages", msgs)
		if e.System != "" {
			in.set("instructions", e.System)
		}
		rec.set("input", in)
	}
	r.record(e.CallID, false, rec)
	return ctx
}

// OnStepStart records a step-start line.
func (r *FileReporter) OnStepStart(ctx context.Context, e telemetry.TelemetryStepStartEvent) context.Context {
	r.record(e.CallID, false, newRecord("step-start", e.CallID).set("step", e.StepNumber))
	return ctx
}

// OnToolExecutionStart records a tool-start line.
func (r *FileReporter) OnToolExecutionStart(ctx context.Context, e telemetry.TelemetryToolCallStartEvent) context.Context {
	r.record(e.CallID, false, newRecord("tool-start", e.CallID).
		set("toolName", e.ToolName).set("toolCallId", e.ToolCallID).set("input", e.Args))
	return ctx
}

// OnToolCallStart is the deprecated alias of OnToolExecutionStart; the
// dispatcher only calls OnToolExecutionStart.
func (r *FileReporter) OnToolCallStart(ctx context.Context, e telemetry.TelemetryToolCallStartEvent) context.Context {
	return r.OnToolExecutionStart(ctx, e)
}

// OnToolExecutionEnd records a tool-end line; a failed tool marks the turn
// as errored.
func (r *FileReporter) OnToolExecutionEnd(_ context.Context, e telemetry.TelemetryToolCallFinishEvent) {
	isError := e.Error != nil
	if isError {
		r.markErrored(e.CallID)
	}
	rec := newRecord("tool-end", e.CallID).set("toolCallId", e.ToolCallID).set("isError", isError)
	if isError {
		// TS writes the raw thrown value (`toolOutput.error`); an Error
		// instance JSON-stringifies to `{}`.
		rec.set("output", rawReporterError(e.Error))
	} else {
		rec.setOpt("output", e.Result)
	}
	r.record(e.CallID, false, rec)
}

// OnToolCallFinish is the deprecated alias of OnToolExecutionEnd.
func (r *FileReporter) OnToolCallFinish(ctx context.Context, e telemetry.TelemetryToolCallFinishEvent) {
	r.OnToolExecutionEnd(ctx, e)
}

// reporterStepContent renders the step's output parts in the TS content
// shape.
func reporterStepContent(e telemetry.TelemetryStepEndEvent) []interface{} {
	var content []interface{}
	if e.Text != "" {
		content = append(content, (&orderedRecord{}).set("type", "text").set("text", e.Text))
	}
	if e.Reasoning != "" {
		content = append(content, (&orderedRecord{}).set("type", "reasoning").set("text", e.Reasoning))
	}
	for _, f := range e.Files {
		content = append(content, (&orderedRecord{}).set("type", "file").set("file", f))
	}
	for _, tc := range e.ToolCalls {
		content = append(content, (&orderedRecord{}).set("type", "tool-call").
			set("toolCallId", tc.ID).set("toolName", tc.ToolName).set("input", tc.Arguments))
	}
	for _, tr := range e.ToolResults {
		if tr.Error != nil {
			content = append(content, (&orderedRecord{}).set("type", "tool-error").
				set("toolCallId", tr.ToolCallID).set("toolName", tr.ToolName).
				set("input", tr.Input).set("error", rawReporterError(tr.Error)))
			continue
		}
		content = append(content, (&orderedRecord{}).set("type", "tool-result").
			set("toolCallId", tr.ToolCallID).set("toolName", tr.ToolName).
			set("input", tr.Input).setOpt("output", tr.Result))
	}
	return content
}

// OnStepEnd records a step-finish line.
func (r *FileReporter) OnStepEnd(_ context.Context, e telemetry.TelemetryStepEndEvent) {
	rec := newRecord("step-finish", e.CallID).set("usage", reporterUsage(e.Usage))
	if recordOutputs(e.Settings) {
		if c := reporterStepContent(e); c != nil {
			rec.set("output", c)
		}
	}
	r.record(e.CallID, false, rec)
}

// OnEnd records the turn-finish line and flushes the turn (unless FailOnly
// and the turn did not error). Usage is the turn total.
func (r *FileReporter) OnEnd(_ context.Context, e telemetry.TelemetryFinishEvent) {
	rec := newRecord("turn-finish", e.CallID).set("finishReason", e.FinishReason).set("usage", reporterUsage(e.Usage))
	r.record(e.CallID, false, rec)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finishTurnLocked(e.CallID)
}

// OnFinish is the deprecated alias of OnEnd.
func (r *FileReporter) OnFinish(ctx context.Context, e telemetry.TelemetryFinishEvent) {
	r.OnEnd(ctx, e)
}

// OnError records an error line, marks the turn errored and flushes it.
func (r *FileReporter) OnError(_ context.Context, e telemetry.TelemetryErrorEvent) {
	r.markErrored(e.CallID)
	r.record(e.CallID, false, newRecord("error", e.CallID).set("error", serializeReporterError(e.Error)))
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finishTurnLocked(e.CallID)
}

// OnAbort records a turn-abort line and flushes the turn (unless FailOnly
// and the turn did not error).
func (r *FileReporter) OnAbort(_ context.Context, e telemetry.TelemetryAbortEvent) {
	rec := newRecord("turn-abort", e.CallID)
	if e.Reason != nil {
		rec.set("reason", serializeReporterError(e.Reason))
	}
	r.record(e.CallID, false, rec)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finishTurnLocked(e.CallID)
}

// IngestDiagnostic records a forwarded bridge diagnostic against the most
// recently started, still-open turn (diagnostics carry a session ID, not a
// call ID). An error-level diagnostic marks that turn errored. With no open
// turn the line is written standalone.
func (r *FileReporter) IngestDiagnostic(d Diagnostic) {
	r.mu.Lock()
	if d.Level == DebugLevelError && r.hasLast && !r.closed {
		r.turnLocked(r.lastOpen).errored = true
	}
	r.mu.Unlock()
	rec := (&orderedRecord{}).set("ts", d.Timestamp).set("kind", "diagnostic").set("diagnostic", d)
	r.record("", true, rec)
}

var (
	_ telemetry.TelemetryIntegration = (*FileReporter)(nil)
	_ DiagnosticConsumer             = (*FileReporter)(nil)
)
