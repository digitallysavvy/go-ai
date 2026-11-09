package harness

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"log"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// TS utils/normalize-harness-tool-model-output.ts: the error names the
// part type and, for a file, its data type; a media type without a full
// subtype ("image/") is detected. Messages recorded from TS 382d0b8eaf
// (bun) for the same parts.
func TestR89carryNormalizeToolModelOutputErrors(t *testing.T) {
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	const prefix = "Harnesses support only text and inline images in tool model output. "
	cases := []struct {
		name  string
		block types.ToolResultContentBlock
		want  string
	}{
		{"file url", types.FileContentBlock{MediaType: "image/png", URL: "https://x/a.png"}, prefix + "Unsupported content 'file' (image/png, url)."},
		{"file reference", types.FileContentBlock{MediaType: "image/png", Reference: "file_1"}, prefix + "Unsupported content 'file' (image/png, reference)."},
		{"file text", types.FileContentBlock{MediaType: "image/png", Text: "x"}, prefix + "Unsupported content 'file' (image/png, text)."},
		{"image data not an image", types.ImageContentBlock{MediaType: "text/plain", Data: png}, prefix + "Unsupported content 'image-data' (text/plain, data)."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NormalizeToolModelOutput(types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{tc.block}})
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err = %v\nwant %s", err, tc.want)
			}
		})
	}
	t.Run("image/ is detected", func(t *testing.T) {
		out, err := NormalizeToolModelOutput(types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
			types.FileContentBlock{MediaType: "image/", Data: png},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if img, ok := out.Content[0].(types.ImageContentBlock); !ok || img.MediaType != "image/png" {
			t.Fatalf("got %#v", out.Content[0])
		}
	})
}

type r89carryWeather struct {
	City string  `json:"city"`
	Temp float64 `json:"temp"`
}

// TS harness-agent.ts _resolveResponseFormat (1238-1255) forwards
// `responseFormat.schema` whatever its source. ai.ObjectOutput with a struct
// schema (or an *schema.OrderedSchema) hands an *schema.OrderedSchema; the
// harness turn must still get the JSON schema, or every adapter fails the
// turn with "requires a JSON schema for structured output".
func TestR89carryResponseFormatKeepsOrderedSchema(t *testing.T) {
	ordered, err := schema.NewOrderedSchema([]byte(`{"type":"object","properties":{"a":{"type":"string"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]schema.Schema{
		"struct":  schema.NewSimpleStructSchema(reflect.TypeOf(r89carryWeather{})),
		"ordered": ordered,
	} {
		t.Run(name, func(t *testing.T) {
			rf, err := resolveOutputResponseFormat(context.Background(), ai.ObjectOutput[map[string]any](ai.ObjectOutputOptions{Schema: s, Name: "w"}))
			if err != nil {
				t.Fatal(err)
			}
			if rf == nil || rf.Type != ResponseFormatJSON || rf.Name != "w" {
				t.Fatalf("rf = %#v", rf)
			}
			if rf.Schema == nil || rf.Schema["type"] != "object" || rf.Schema["properties"] == nil {
				t.Fatalf("schema = %#v", rf.Schema)
			}
		})
	}
}

// TS normalize-harness-tool-model-output.ts checks the top-level media type
// before detection: an empty media type fails (getTopLevelMediaType(”) is
// ”).
func TestR89carryNormalizeEmptyMediaType(t *testing.T) {
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	const prefix = "Harnesses support only text and inline images in tool model output. "
	for _, tc := range []struct {
		block types.ToolResultContentBlock
		want  string
	}{
		{types.ImageContentBlock{Data: png}, prefix + "Unsupported content 'image-data' (, data)."},
		{types.FileContentBlock{Data: png}, prefix + "Unsupported content 'file' (, data)."},
	} {
		_, err := NormalizeToolModelOutput(types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{tc.block}})
		if err == nil || err.Error() != tc.want {
			t.Errorf("err = %v, want %s", err, tc.want)
		}
	}
}

// TS convert-harness-tool-model-output.ts getNormalizedInlineImage: only a
// file part with inline data and a full image/* media type converts.
func TestR89carryConvertToolModelOutputImages(t *testing.T) {
	const prefix = "Harnesses support only text and inline images in tool model output; unsupported content "
	_, _, err := ConvertToolModelOutput(types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
		types.ImageContentBlock{Data: []byte{1}, MediaType: "image/*"},
	}})
	if err == nil || err.Error() != prefix+"'image-data'." {
		t.Fatalf("image/*: err = %v", err)
	}
	out, isErr, err := ConvertToolModelOutput(types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
		types.FileContentBlock{FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte{1, 2}}, MediaType: "image/png"},
	}})
	if err != nil || isErr || len(out) != 1 || out[0].Type != ToolModelOutputContentImage || out[0].Data != "AQI=" || out[0].MediaType != "image/png" {
		t.Fatalf("file: out = %#v, err = %v", out, err)
	}
	_, _, err = ConvertToolModelOutput(types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
		types.FileContentBlock{URL: "https://x/a.png", MediaType: "image/png"},
	}})
	if err == nil || err.Error() != prefix+"'file'." {
		t.Fatalf("file url: err = %v", err)
	}
}

// TS resolve-observability.ts resolveDebugConfig: `env.HARNESS_DEBUG_LEVEL
// ?? 'debug'` uses a set variable as given, unvalidated.
func TestR89carryDebugLevelFromEnv(t *testing.T) {
	env := func(m map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
	}
	if got := resolveDebugConfig(nil, env(map[string]string{"HARNESS_DEBUG_LEVEL": "verbose"})).Level; got != "verbose" {
		t.Errorf("invalid level: got %q, want verbose", got)
	}
	if got := resolveDebugConfig(nil, env(map[string]string{"HARNESS_DEBUG_LEVEL": ""})).Level; got != "" {
		t.Errorf("empty level: got %q, want empty", got)
	}
	if got := resolveDebugConfig(nil, env(nil)).Level; got != DebugLevelDebug {
		t.Errorf("unset level: got %q", got)
	}
	if got := resolveDebugConfig(&DebugConfig{Level: DebugLevelWarn}, env(map[string]string{"HARNESS_DEBUG_LEVEL": "trace"})).Level; got != DebugLevelWarn {
		t.Errorf("code level: got %q", got)
	}
}

// TS HarnessAgent constructor warns when the deprecated `sandbox` setting
// is used.
func TestR89carrySandboxDeprecationWarning(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)
	mock := newMockHarness(mockHarnessOptions{})
	if _, err := NewAgent(AgentSettings{Harness: mock.harness}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("warned without Sandbox: %q", buf.String())
	}
	if _, err := NewAgent(AgentSettings{Harness: mock.harness, Sandbox: r89carrySandboxProvider{}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "HarnessAgent: `Sandbox` is deprecated. Supply `SandboxSession` to CreateSession() instead.") {
		t.Fatalf("log = %q", buf.String())
	}
}

type r89carrySandboxProvider struct{}

func (r89carrySandboxProvider) SpecificationVersion() string { return "harness-sandbox-v1" }
func (r89carrySandboxProvider) ProviderID() string           { return "r89carry" }
func (r89carrySandboxProvider) CreateSession(context.Context, CreateSandboxSessionOptions) (NetworkSandboxSession, error) {
	return nil, nil
}

// TS harness-agent.ts _prepareTurnSettings: the prepared tools are merged
// over the harness builtins, so a PrepareCall that returns only user tools
// keeps the builtins.
func TestR89carryPreparedToolsKeepBuiltins(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{builtinTools: map[string]BuiltinTool{"bash": {Tool: types.Tool{Description: "builtin"}}}})
	got := preparedTurnTools(mock.harness, map[string]types.Tool{"weather": {Description: "user"}})
	if len(got) != 2 || got["bash"].Name != "bash" || got["weather"].Name != "weather" {
		t.Fatalf("tools = %#v", got)
	}
	got = preparedTurnTools(mock.harness, map[string]types.Tool{"bash": {Description: "user bash"}})
	if got["bash"].Description != "user bash" {
		t.Fatalf("user tool must win: %#v", got["bash"])
	}
}

type r89carryFailingOutput struct{}

func (r89carryFailingOutput) ResponseFormat(context.Context) (*provider.ResponseFormat, error) {
	return nil, r89carryErrFormat
}

var r89carryErrFormat = errors.New("format boom")

// TS awaits `output.responseFormat` and lets its error through unchanged.
func TestR89carryResponseFormatErrorUnwrapped(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{})
	a, err := NewAgent(AgentSettings{Harness: mock.harness, Output: r89carryFailingOutput{}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Stream(context.Background(), agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: s}})
	if err != r89carryErrFormat {
		t.Fatalf("err = %v", err)
	}
}

// The PrepareCall path of a turn uses the merged tool set.
func TestR89carryPrepareCallToolsKeepBuiltinsInTurn(t *testing.T) {
	gate := make(chan struct{})
	mock := newMockHarness(mockHarnessOptions{
		builtinTools: map[string]BuiltinTool{"bash": {Tool: types.Tool{Description: "builtin"}}},
		emitGate:     func() <-chan struct{} { return gate },
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	a, err := NewAgent(AgentSettings{Harness: mock.harness, PrepareCall: func(ctx context.Context, opts PrepareCallOptions) (PrepareCallResult, error) {
		return PrepareCallResult{Tools: map[string]types.Tool{"weather": {Description: "user"}}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.Stream(context.Background(), agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: s}})
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	tools := s.activeTurn.tools
	s.mu.Unlock()
	close(gate)
	_ = res.ConsumeStream()
	if _, ok := tools["bash"]; !ok {
		t.Fatalf("turn tools = %v", tools)
	}
}
