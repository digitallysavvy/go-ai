package telemetry

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TS 934100cad4 server-attributes.test.ts.
func TestR87carryServerAttrs(t *testing.T) {
	for url, want := range map[string][]attribute.KeyValue{
		"https://model.example.test:8443/private/v1?key=secret#fragment": {attribute.String("server.address", "model.example.test"), attribute.Int("server.port", 8443)},
		"https://user:password@model.example.test/v1":                    {attribute.String("server.address", "model.example.test"), attribute.Int("server.port", 443)},
		"http://model.example.test/v1":                                   {attribute.String("server.address", "model.example.test"), attribute.Int("server.port", 80)},
		"https://[2001:db8::1]:8443/v1":                                  {attribute.String("server.address", "2001:db8::1"), attribute.Int("server.port", 8443)},
		"HTTPS://Model.Example.TEST:443/":                                {attribute.String("server.address", "model.example.test"), attribute.Int("server.port", 443)},
		"":                                                               nil,
		"/relative/path":                                                 nil,
		"not a URL":                                                      nil,
		"file:///etc/passwd":                                             nil,
		"data:text/plain,secret":                                         nil,
		"wss://model.example.test/v1":                                    nil,
	} {
		if got := serverAttrs(url); !reflect.DeepEqual(got, want) {
			t.Errorf("serverAttrs(%q) = %v, want %v", url, got, want)
		}
	}
}

// TS open-telemetry.test.ts "records the endpoint reported by a %s call
// without URL secrets" (generate / object) and "does not guess an endpoint
// from a provider name".
func TestR87carryChatSpanServerAttributes(t *testing.T) {
	for _, op := range []string{"ai.generateText", "ai.streamText", "ai.generateObject", "ai.streamObject"} {
		for _, url := range []string{"https://model.example.test:8443", ""} {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			i := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("g")})
			set := &Settings{IsEnabled: Bool(true), RecordInputs: Bool(false), RecordOutputs: Bool(false)}
			id := t.Name() + op + url
			ctx := i.OnStart(context.Background(), TelemetryStartEvent{OperationType: op, Settings: set, CallID: id, ModelID: "gpt-4", ModelProvider: "openai.chat"})
			ctx = i.OnStepStart(ctx, TelemetryStepStartEvent{Settings: set, OperationType: op, CallID: id})
			ctx = i.OnLanguageModelCallStart(ctx, LanguageModelCallStartEvent{Settings: set, CallID: id, ModelID: "gpt-4", ModelProvider: "openai.chat"})
			i.OnLanguageModelCallEnd(ctx, LanguageModelCallEndEvent{Settings: set, CallID: id, ModelID: "gpt-4", ModelProvider: "openai.chat", FinishReason: "stop", RequestURL: url})
			_ = tp.Shutdown(context.Background())
			var chat sdktrace.ReadOnlySpan
			for _, s := range rec.Ended() {
				if strings.HasPrefix(s.Name(), "chat ") {
					chat = s
				}
			}
			if chat == nil {
				t.Fatalf("%s: no chat span", op)
			}
			got := map[string]attribute.Value{}
			for _, kv := range chat.Attributes() {
				got[string(kv.Key)] = kv.Value
			}
			addr, hasAddr := got["server.address"]
			port, hasPort := got["server.port"]
			if url == "" {
				if hasAddr || hasPort {
					t.Errorf("%s: guessed an endpoint: %v %v", op, addr, port)
				}
				continue
			}
			if addr.AsString() != "model.example.test" || port.AsInt64() != 8443 {
				t.Errorf("%s: server.address = %v, server.port = %v", op, addr, port)
			}
		}
	}
}
