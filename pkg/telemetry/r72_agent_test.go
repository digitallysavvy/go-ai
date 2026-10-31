package telemetry

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/sdkversion"
)

// Round 72 ports of TS packages/ai/src/telemetry/create-telemetry-dispatcher.test.ts
// and telemetry-registry.test.ts.

type r72agentRec struct {
	mu     sync.Mutex
	events []string
	starts []TelemetryStartEvent
}

func (r *r72agentRec) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, s)
}

func (r *r72agentRec) get() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

type r72agentIntegration struct {
	NoopTelemetryIntegration
	name   string
	rec    *r72agentRec
	panics bool
}

func (i *r72agentIntegration) hit(event string) {
	i.rec.add(i.name + ":" + event)
	if i.panics {
		panic(i.name)
	}
}

func (i *r72agentIntegration) OnStart(ctx context.Context, e TelemetryStartEvent) context.Context {
	i.rec.mu.Lock()
	i.rec.starts = append(i.rec.starts, e)
	i.rec.mu.Unlock()
	i.hit("start")
	return ctx
}

func (i *r72agentIntegration) OnStepStart(ctx context.Context, e TelemetryStepStartEvent) context.Context {
	i.hit("stepStart")
	return ctx
}

func (i *r72agentIntegration) OnStepEnd(ctx context.Context, e TelemetryStepEndEvent) {
	i.hit("stepEnd")
}

func (i *r72agentIntegration) OnEnd(ctx context.Context, e TelemetryFinishEvent) {
	i.hit("end")
}

func (i *r72agentIntegration) OnError(ctx context.Context, e TelemetryErrorEvent) {
	i.hit("error")
}

func r72agentFireAll(s *Settings) {
	ctx := context.Background()
	ctx = FireOnStart(ctx, TelemetryStartEvent{Settings: s, CallID: "c"})
	ctx = FireOnStepStart(ctx, TelemetryStepStartEvent{Settings: s, CallID: "c"})
	FireOnStepEnd(ctx, TelemetryStepEndEvent{Settings: s, CallID: "c"})
	FireOnEnd(ctx, TelemetryFinishEvent{Settings: s, CallID: "c"})
}

// r72agentWithGlobals replaces the global registry for one test.
func r72agentWithGlobals(t *testing.T, globals ...TelemetryIntegration) {
	t.Helper()
	before := snapshot()
	ClearTelemetryIntegrations()
	for _, g := range globals {
		AddTelemetryIntegration(g)
	}
	t.Cleanup(func() {
		ClearTelemetryIntegrations()
		for _, g := range before {
			AddTelemetryIntegration(g)
		}
	})
}

// TS "broadcasts an event to all integrations that implement the method",
// "works with all lifecycle methods", "swallows sync errors thrown by
// integrations" (a panicking integration does not stop the others).
func TestR72agentDispatcherBroadcastsAndSwallowsPanics(t *testing.T) {
	r72agentWithGlobals(t)
	rec := &r72agentRec{}
	a := &r72agentIntegration{name: "a", rec: rec, panics: true}
	b := &r72agentIntegration{name: "b", rec: rec}
	r72agentFireAll(&Settings{Integrations: []TelemetryIntegration{a, b}})
	want := []string{"a:start", "b:start", "a:stepStart", "b:stepStart", "a:stepEnd", "b:stepEnd", "a:end", "b:end"}
	if got := rec.get(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v", got)
	}
}

// TS "isEnabled filter": isEnabled false invokes no integration, local or
// global; true and unset invoke them.
func TestR72agentDispatcherIsEnabled(t *testing.T) {
	rec := &r72agentRec{}
	r72agentWithGlobals(t, &r72agentIntegration{name: "global", rec: rec})
	local := &r72agentIntegration{name: "local", rec: rec}
	off, on := false, true
	r72agentFireAll(&Settings{IsEnabled: &off, Integrations: []TelemetryIntegration{local}})
	r72agentFireAll(&Settings{IsEnabled: &off})
	if got := rec.get(); len(got) != 0 {
		t.Fatalf("disabled telemetry reached integrations: %v", got)
	}
	r72agentFireAll(&Settings{IsEnabled: &on, Integrations: []TelemetryIntegration{local}})
	r72agentFireAll(&Settings{})
	want := []string{
		"local:start", "local:stepStart", "local:stepEnd", "local:end",
		"global:start", "global:stepStart", "global:stepEnd", "global:end",
	}
	if got := rec.get(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v", got)
	}
}

// TS "global vs local integration resolution": local integrations replace
// the globals; without local ones the globals (in registration order) run;
// an empty local list means none.
func TestR72agentDispatcherGlobalVsLocal(t *testing.T) {
	rec := &r72agentRec{}
	r72agentWithGlobals(t)
	RegisterTelemetryIntegration(&r72agentIntegration{name: "g1", rec: rec})
	RegisterTelemetryIntegration(&r72agentIntegration{name: "g2", rec: rec}, &r72agentIntegration{name: "g3", rec: rec})
	ctx := context.Background()
	FireOnStart(ctx, TelemetryStartEvent{CallID: "c"})
	FireOnStart(ctx, TelemetryStartEvent{CallID: "c", Settings: &Settings{Integrations: []TelemetryIntegration{&r72agentIntegration{name: "local", rec: rec}}}})
	FireOnStart(ctx, TelemetryStartEvent{CallID: "c", Settings: &Settings{Integrations: []TelemetryIntegration{}}})
	want := []string{"g1:start", "g2:start", "g3:start", "local:start"}
	if got := rec.get(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v", got)
	}
}

// TS "adds the emitting SDK version without modifying the original event".
func TestR72agentDispatcherAddsAIVersion(t *testing.T) {
	r72agentWithGlobals(t)
	rec := &r72agentRec{}
	e := TelemetryStartEvent{CallID: "c", Settings: &Settings{Integrations: []TelemetryIntegration{&r72agentIntegration{name: "a", rec: rec}}}}
	FireOnStart(context.Background(), e)
	if e.AIVersion != "" {
		t.Fatalf("caller's event modified: %q", e.AIVersion)
	}
	if len(rec.starts) != 1 || rec.starts[0].AIVersion != sdkversion.Version || rec.starts[0].AIVersion == "" {
		t.Fatalf("starts = %+v", rec.starts)
	}
}

type r72agentStepFinishIntegration struct {
	r72agentIntegration
}

func (i *r72agentStepFinishIntegration) OnStepFinish(ctx context.Context, e TelemetryStepFinishEvent) {
	i.rec.add(i.name + ":stepFinish")
}

// TS "fans out step-end events to deprecated onStepFinish integrations":
// the dispatcher's onStepEnd is mergeCallbacks(onStepEnd, onStepFinish), so
// every integration's OnStepEnd runs before the deprecated OnStepFinish
// handlers, and a panicking OnStepEnd does not skip its OnStepFinish.
func TestR72agentDispatcherStepEndFansOutToDeprecatedStepFinish(t *testing.T) {
	r72agentWithGlobals(t)
	rec := &r72agentRec{}
	a := &r72agentStepFinishIntegration{r72agentIntegration{name: "a", rec: rec, panics: true}}
	b := &r72agentIntegration{name: "b", rec: rec}
	FireOnStepEnd(context.Background(), TelemetryStepEndEvent{CallID: "c", Settings: &Settings{Integrations: []TelemetryIntegration{a, b}}})
	want := []string{"a:stepEnd", "b:stepEnd", "a:stepFinish"}
	if got := rec.get(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v", got)
	}
}
