package tools

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestLegacyTools_ConstructorsAndExecutionGuards(t *testing.T) {
	t.Parallel()

	tools := []types.Tool{
		CodeExecution20250522(),
		Memory20250818(),
		TextEditor20241022(),
		TextEditor20250124(),
		TextEditor20250429(),
		Computer20241022(Computer20241022Args{DisplayWidthPx: 1280, DisplayHeightPx: 720}),
		Computer20250124(Computer20241022Args{DisplayWidthPx: 1280, DisplayHeightPx: 720}),
	}

	for _, tool := range tools {
		tool := tool
		t.Run(tool.Name, func(t *testing.T) {
			if !tool.ProviderExecuted {
				t.Fatalf("%s must be provider-executed", tool.Name)
			}
			if tool.Execute == nil {
				t.Fatalf("%s missing Execute function", tool.Name)
			}
			_, err := tool.Execute(t.Context(), map[string]interface{}{}, types.ToolExecutionOptions{})
			if err == nil {
				t.Fatalf("%s execute must return provider-executed error", tool.Name)
			}
		})
	}
}

func TestBash20241022_UsesExperimentalSandboxWhenPresent(t *testing.T) {
	t.Parallel()

	tool := Bash20241022()
	sandbox := &mockExperimentalSandbox{result: map[string]interface{}{"stdout": "hi\n"}}

	if tool.ProviderExecuted {
		t.Fatal("bash_20241022 should be locally executable with a sandbox")
	}
	out, err := tool.Execute(t.Context(), map[string]interface{}{"command": "echo hi"}, types.ToolExecutionOptions{
		ExperimentalSandbox: sandbox,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if sandbox.command != "echo hi" {
		t.Fatalf("sandbox command = %q, want echo hi", sandbox.command)
	}
	if got, want := out, (map[string]interface{}{"stdout": "hi\n"}); got.(map[string]interface{})["stdout"] != want["stdout"] {
		t.Fatalf("Execute() = %#v, want %#v", got, want)
	}
}

func TestLegacyWebTools_SerializationAndParsers(t *testing.T) {
	t.Parallel()

	max := 5
	webSearchTool := WebSearch20250305(WebSearch20260209Config{
		MaxUses:        &max,
		AllowedDomains: []string{"example.com"},
	})
	mapper, ok := webSearchTool.ProviderOptions.(interface{ ToAnthropicAPIMap() map[string]interface{} })
	if !ok {
		t.Fatalf("web search provider options missing mapper: %T", webSearchTool.ProviderOptions)
	}
	m := mapper.ToAnthropicAPIMap()
	if m["type"] != "web_search_20250305" || m["name"] != "web_search" {
		t.Fatalf("web_search_20250305 map mismatch: %#v", m)
	}
	results, err := ParseWebSearch20250305Results([]byte(`[{"type":"web_search_result","url":"https://a","title":"t","encryptedContent":"enc"}]`))
	if err != nil || len(results) != 1 {
		t.Fatalf("ParseWebSearch20250305Results failed: results=%v err=%v", results, err)
	}
	if _, err := ParseWebSearch20250305Results([]byte(`{`)); err == nil {
		t.Fatal("ParseWebSearch20250305Results expected parse error")
	}

	webFetchTool := WebFetch20250910(WebFetch20260209Config{
		MaxUses:          &max,
		AllowedDomains:   []string{"example.com"},
		MaxContentTokens: &max,
		Citations:        &WebFetchCitations{Enabled: true},
	})
	mapper, ok = webFetchTool.ProviderOptions.(interface{ ToAnthropicAPIMap() map[string]interface{} })
	if !ok {
		t.Fatalf("web fetch provider options missing mapper: %T", webFetchTool.ProviderOptions)
	}
	m = mapper.ToAnthropicAPIMap()
	if m["type"] != "web_fetch_20250910" || m["name"] != "web_fetch" {
		t.Fatalf("web_fetch_20250910 map mismatch: %#v", m)
	}
	fetchRes, err := ParseWebFetch20250910Result([]byte(`{"type":"web_fetch_result","url":"https://a","content":{"type":"text","text":"ok"}}`))
	if err != nil || fetchRes == nil {
		t.Fatalf("ParseWebFetch20250910Result failed: result=%v err=%v", fetchRes, err)
	}
	if _, err := ParseWebFetch20250910Result([]byte(`{`)); err == nil {
		t.Fatal("ParseWebFetch20250910Result expected parse error")
	}
}

func TestLegacyToolSchemasAndDescriptions(t *testing.T) {
	t.Parallel()

	legacyComputerSchema := buildLegacyComputerSchema()
	if legacyComputerSchema["type"] != "object" {
		t.Fatalf("legacy computer schema type mismatch: %#v", legacyComputerSchema)
	}
	withDisplay := 2
	desc := buildLegacyComputerDescription(1920, 1080, &withDisplay)
	if len(desc) == 0 {
		t.Fatal("buildLegacyComputerDescription returned empty description")
	}

	withUndo := buildLegacyTextEditorSchema(true)
	withoutUndo := buildLegacyTextEditorSchema(false)
	if withUndo["type"] != "object" || withoutUndo["type"] != "object" {
		t.Fatalf("legacy text editor schema type mismatch")
	}
	if d := buildLegacyTextEditorDescription(true); len(d) == 0 {
		t.Fatal("buildLegacyTextEditorDescription returned empty string")
	}
}

func TestCurrentToolOptionMappers(t *testing.T) {
	t.Parallel()

	maxChars := 2048
	textEditor := TextEditor20250728(TextEditor20250728Args{MaxCharacters: &maxChars})
	teMapper, ok := textEditor.ProviderOptions.(interface{ ToAnthropicAPIMap() map[string]interface{} })
	if !ok {
		t.Fatalf("text editor provider options missing mapper: %T", textEditor.ProviderOptions)
	}
	teMap := teMapper.ToAnthropicAPIMap()
	if teMap["type"] != "text_editor_20250728" || teMap["name"] != "str_replace_based_edit_tool" {
		t.Fatalf("text editor API map mismatch: %#v", teMap)
	}
	if teMap["max_characters"] != maxChars {
		t.Fatalf("max_characters mismatch: %#v", teMap["max_characters"])
	}

	displayNumber := 1
	computer := Computer20251124(Computer20251124Args{
		DisplayWidthPx:  1920,
		DisplayHeightPx: 1080,
		DisplayNumber:   &displayNumber,
		EnableZoom:      true,
	})
	cMapper, ok := computer.ProviderOptions.(interface{ ToAnthropicAPIMap() map[string]interface{} })
	if !ok {
		t.Fatalf("computer provider options missing mapper: %T", computer.ProviderOptions)
	}
	cMap := cMapper.ToAnthropicAPIMap()
	if cMap["type"] != "computer_20251124" || cMap["enable_zoom"] != true {
		t.Fatalf("computer API map mismatch: %#v", cMap)
	}
}
