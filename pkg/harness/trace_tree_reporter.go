package harness

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// TraceTreeReporterOptions configures NewTraceTreeReporter. Mirrors TS
// `TraceTreeReporterOptions`.
type TraceTreeReporterOptions struct {
	// Write receives the rendered tree. Default writes to os.Stderr.
	Write func(chunk string)
}

type traceNode struct {
	label    string
	start    time.Time
	end      time.Time
	ended    bool
	children []*traceNode
}

type traceTurn struct {
	root  *traceNode
	step  *traceNode
	tools map[string]*traceNode
}

// TraceTreeReporter renders an ASCII trace tree of a turn's span lifecycle
// (turn, steps, tools) at turn end. Register it in
// AgentSettings.Telemetry.Integrations. It is useful for zero-setup local
// debugging when no OpenTelemetry collector is wired up; a real OTel backend
// is a strict superset. Safe for concurrent use. Mirrors TS
// createTraceTreeReporter.
type TraceTreeReporter struct {
	telemetry.NoopTelemetryIntegration

	write func(string)
	mu    sync.Mutex
	turns map[string]*traceTurn
}

// NewTraceTreeReporter creates a TraceTreeReporter.
func NewTraceTreeReporter(opts TraceTreeReporterOptions) *TraceTreeReporter {
	write := opts.Write
	if write == nil {
		write = func(chunk string) { _, _ = io.WriteString(os.Stderr, chunk) }
	}
	return &TraceTreeReporter{write: write, turns: map[string]*traceTurn{}}
}

func renderTraceNode(sb *strings.Builder, n *traceNode, depth int) {
	dur := "(open)"
	if n.ended {
		ms := n.end.Sub(n.start).Milliseconds()
		if ms < 0 {
			ms = 0
		}
		dur = fmt.Sprintf("%dms", ms)
	}
	fmt.Fprintf(sb, "%s- %s %s\n", strings.Repeat("  ", depth), n.label, dur)
	for _, c := range n.children {
		renderTraceNode(sb, c, depth+1)
	}
}

// OnStart opens the turn's root node.
func (r *TraceTreeReporter) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	label := e.OperationType
	if label == "" {
		label = "turn"
	}
	if e.ModelID != "" {
		label += " " + e.ModelID
	}
	r.mu.Lock()
	r.turns[e.CallID] = &traceTurn{root: &traceNode{label: label, start: time.Now()}, tools: map[string]*traceNode{}}
	r.mu.Unlock()
	return ctx
}

// OnStepStart opens a step node under the turn.
func (r *TraceTreeReporter) OnStepStart(ctx context.Context, e telemetry.TelemetryStepStartEvent) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.turns[e.CallID]
	if t == nil {
		return ctx
	}
	step := &traceNode{label: fmt.Sprintf("step %d", e.StepNumber+1), start: time.Now()}
	t.step = step
	t.root.children = append(t.root.children, step)
	return ctx
}

// OnToolExecutionStart opens a tool node under the current step (or the
// turn when no step is open).
func (r *TraceTreeReporter) OnToolExecutionStart(ctx context.Context, e telemetry.TelemetryToolCallStartEvent) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.turns[e.CallID]
	if t == nil {
		return ctx
	}
	parent := t.step
	if parent == nil {
		parent = t.root
	}
	n := &traceNode{label: "tool " + e.ToolName, start: time.Now()}
	t.tools[e.ToolCallID] = n
	parent.children = append(parent.children, n)
	return ctx
}

// OnToolCallStart is the deprecated alias of OnToolExecutionStart.
func (r *TraceTreeReporter) OnToolCallStart(ctx context.Context, e telemetry.TelemetryToolCallStartEvent) context.Context {
	return r.OnToolExecutionStart(ctx, e)
}

// OnToolExecutionEnd closes the tool node, marking failures.
func (r *TraceTreeReporter) OnToolExecutionEnd(_ context.Context, e telemetry.TelemetryToolCallFinishEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.turns[e.CallID]
	if t == nil {
		return
	}
	n := t.tools[e.ToolCallID]
	if n == nil {
		return
	}
	n.end, n.ended = time.Now(), true
	if e.Error != nil {
		n.label += " [error]"
	}
}

// OnToolCallFinish is the deprecated alias of OnToolExecutionEnd.
func (r *TraceTreeReporter) OnToolCallFinish(ctx context.Context, e telemetry.TelemetryToolCallFinishEvent) {
	r.OnToolExecutionEnd(ctx, e)
}

// OnStepEnd closes the current step node.
func (r *TraceTreeReporter) OnStepEnd(_ context.Context, e telemetry.TelemetryStepEndEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t := r.turns[e.CallID]; t != nil && t.step != nil {
		t.step.end, t.step.ended = time.Now(), true
		t.step = nil
	}
}

// OnEnd closes the turn and writes the rendered tree.
func (r *TraceTreeReporter) OnEnd(_ context.Context, e telemetry.TelemetryFinishEvent) {
	r.mu.Lock()
	t := r.turns[e.CallID]
	if t == nil {
		r.mu.Unlock()
		return
	}
	t.root.end, t.root.ended = time.Now(), true
	delete(r.turns, e.CallID)
	var sb strings.Builder
	sb.WriteString("\n")
	renderTraceNode(&sb, t.root, 0)
	r.mu.Unlock()
	r.write(sb.String())
}

// OnError drops the turn's state without rendering (the turn never ended
// cleanly).
func (r *TraceTreeReporter) OnError(_ context.Context, e telemetry.TelemetryErrorEvent) {
	r.mu.Lock()
	delete(r.turns, e.CallID)
	r.mu.Unlock()
}

// OnAbort drops the turn's state without rendering.
func (r *TraceTreeReporter) OnAbort(_ context.Context, e telemetry.TelemetryAbortEvent) {
	r.mu.Lock()
	delete(r.turns, e.CallID)
	r.mu.Unlock()
}

// OnFinish is the deprecated alias of OnEnd.
func (r *TraceTreeReporter) OnFinish(ctx context.Context, e telemetry.TelemetryFinishEvent) {
	r.OnEnd(ctx, e)
}

var _ telemetry.TelemetryIntegration = (*TraceTreeReporter)(nil)
