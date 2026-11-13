package telemetry

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// The GenAI root and chat spans of a text call format the caller-facing
// messages like TS formatModelMessages (images as uri/blob parts, parts the
// ModelMessage converter does not map by type only), take one
// gen_ai.system_instructions part per instruction message, and report the
// root call's request settings on the chat span (TS state.settings).
func TestR50GenAITextSpansUseModelMessages(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t")})
	settings := &Settings{IsEnabled: Bool(true)}

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.TextContent{Text: "look"},
			types.ImageContent{URL: "https://example.com/a.png", MimeType: "image/png"},
			types.ImageContent{Image: []byte{1, 2, 3}},
			types.FileContent{MediaType: "audio/wav", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "https://example.com/a.wav"}},
		}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.TextContent{Text: "ok"},
			types.CustomContent{Kind: "x"},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: "a1", Approved: true},
		}},
	}
	instructions := []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "first"}}},
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "second"}}},
	}
	rootTemp, stepTemp := 0.2, 0.9
	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.generateText", Settings: settings, CallID: "c", ModelID: "m",
		Messages: messages, InstructionMessages: instructions, Temperature: &rootTemp,
	})
	ctx = integration.OnStepStart(ctx, TelemetryStepStartEvent{Settings: settings, OperationType: "ai.generateText", CallID: "c"})
	ctx = integration.OnLanguageModelCallStart(ctx, LanguageModelCallStartEvent{
		Settings: settings, CallID: "c", ModelID: "m", Temperature: &stepTemp,
		Messages: messages, InstructionMessages: instructions,
		// The language model prompt (with the instructions as system
		// messages) is not what the text chat span reports.
		Prompt: types.Prompt{Messages: append(append([]types.Message(nil), instructions...), messages...)},
	})
	integration.OnLanguageModelCallEnd(ctx, LanguageModelCallEndEvent{Settings: settings, CallID: "c", FinishReason: "stop"})

	wantMessages := `[{"role":"user","parts":[{"type":"text","content":"look"},{"type":"uri","modality":"image","mime_type":"image/png","uri":"https://example.com/a.png"},{"type":"blob","modality":"image","mime_type":null,"content":"AQID"},{"type":"uri","modality":"audio","mime_type":"audio/wav","uri":"https://example.com/a.wav"}]},{"role":"assistant","parts":[{"type":"text","content":"ok"},{"type":"custom"}]},{"role":"tool","parts":[{"type":"tool-approval-response"}]}]`
	wantInstructions := `[{"type":"text","content":"first"},{"type":"text","content":"second"}]`
	for _, name := range []string{"invoke_agent m", "chat m"} {
		span := findSpan(rec, name)
		if span == nil {
			t.Fatalf("no %s span", name)
		}
		if v, _ := attrValue(span, "gen_ai.input.messages"); v != wantMessages {
			t.Errorf("%s gen_ai.input.messages =\n%v\nwant\n%s", name, v, wantMessages)
		}
		if v, _ := attrValue(span, "gen_ai.system_instructions"); v != wantInstructions {
			t.Errorf("%s gen_ai.system_instructions = %v, want %s", name, v, wantInstructions)
		}
	}
	if v, _ := attrValue(findSpan(rec, "chat m"), "gen_ai.request.temperature"); v != 0.2 {
		t.Errorf("chat gen_ai.request.temperature = %v, want the root's 0.2", v)
	}
}
