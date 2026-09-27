package responses

import (
	"encoding/json"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

func TestPrepareTools_Nil(t *testing.T) {
	result := PrepareTools(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestPrepareTools_Empty(t *testing.T) {
	result := PrepareTools([]types.Tool{})
	if result != nil {
		t.Errorf("expected nil for empty slice, got %v", result)
	}
}

func TestPrepareTools_FunctionTool(t *testing.T) {
	tool := types.Tool{
		Name:        "get_weather",
		Description: "Get current weather",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}

	result := PrepareTools([]types.Tool{tool})
	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}

	def, ok := result[0].(FunctionToolDef)
	if !ok {
		t.Fatalf("expected FunctionToolDef, got %T", result[0])
	}
	if def.Type != "function" {
		t.Errorf("Type: got %q, want %q", def.Type, "function")
	}
	if def.Name != "get_weather" {
		t.Errorf("Name: got %q", def.Name)
	}
	if def.Description != "Get current weather" {
		t.Errorf("Description: got %q", def.Description)
	}
}

func TestPrepareTools_FunctionTool_Strict(t *testing.T) {
	tool := types.Tool{
		Name:   "strict_tool",
		Strict: types.BoolPtr(true),
	}

	result := PrepareTools([]types.Tool{tool})
	def, ok := result[0].(FunctionToolDef)
	if !ok {
		t.Fatalf("expected FunctionToolDef, got %T", result[0])
	}
	if def.Strict == nil || !*def.Strict {
		t.Error("expected Strict to be true")
	}
}

// TestPrepareTools_FunctionTool_StrictExplicitFalse verifies that an
// explicit `Strict: false` is forwarded to the wire (not dropped), matching
// TS's `tool.strict != null ? {strict: tool.strict} : {}` (any non-null
// value, not only `true`).
func TestPrepareTools_FunctionTool_StrictExplicitFalse(t *testing.T) {
	tool := types.Tool{
		Name:   "non_strict_tool",
		Strict: types.BoolPtr(false),
	}

	result := PrepareTools([]types.Tool{tool})
	def, ok := result[0].(FunctionToolDef)
	if !ok {
		t.Fatalf("expected FunctionToolDef, got %T", result[0])
	}
	if def.Strict == nil || *def.Strict {
		t.Error("expected Strict to be an explicit false, not nil/true")
	}
}

// TestPrepareTools_FunctionTool_StrictUnset verifies that an unset Strict
// omits the field entirely, rather than forwarding a default.
func TestPrepareTools_FunctionTool_StrictUnset(t *testing.T) {
	tool := types.Tool{Name: "unspecified_tool"}

	result := PrepareTools([]types.Tool{tool})
	def, ok := result[0].(FunctionToolDef)
	if !ok {
		t.Fatalf("expected FunctionToolDef, got %T", result[0])
	}
	if def.Strict != nil {
		t.Errorf("expected Strict to be nil (omitted) when unset, got %v", *def.Strict)
	}
}

func TestPrepareTools_FunctionTool_DeferLoading(t *testing.T) {
	tool := types.Tool{
		Name:        "get_weather",
		Description: "Get weather",
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"deferLoading": true,
			},
		},
	}

	result := PrepareTools([]types.Tool{tool})
	def, ok := result[0].(FunctionToolDef)
	if !ok {
		t.Fatalf("expected FunctionToolDef, got %T", result[0])
	}
	if def.DeferLoading == nil || !*def.DeferLoading {
		t.Fatalf("defer_loading = %#v, want true", def.DeferLoading)
	}

	data, err := json.Marshal(def)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if raw["defer_loading"] != true {
		t.Fatalf("wire def = %#v, want defer_loading true", raw)
	}
}

func TestPrepareTools_FunctionTool_NamespaceGrouping(t *testing.T) {
	result, err := PrepareToolsWithError([]types.Tool{
		{
			Name:        "get_customer_profile",
			Description: "Fetch a customer profile by customer ID.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"customer_id": map[string]interface{}{"type": "string"}},
				"required":   []string{"customer_id"},
			},
			ProviderOptions: map[string]interface{}{
				"openai": map[string]interface{}{
					"namespace": map[string]interface{}{
						"name":        "crm",
						"description": "CRM tools for customer lookup and order management.",
					},
				},
			},
		},
		{
			Name:        "get_weather",
			Description: "Get the current weather",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"location": map[string]interface{}{"type": "string"}},
			},
		},
		{
			Name:        "list_open_orders",
			Description: "List open orders for a customer ID.",
			Strict:      types.BoolPtr(true),
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"customer_id": map[string]interface{}{"type": "string"}},
				"required":   []string{"customer_id"},
			},
			ProviderOptions: map[string]interface{}{
				"openai": map[string]interface{}{
					"deferLoading": true,
					"namespace": map[string]interface{}{
						"name":        "crm",
						"description": "CRM tools for customer lookup and order management.",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("PrepareToolsWithError() error = %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("len(result) = %d, want 2", len(result))
	}
	namespace, ok := result[0].(*NamespaceToolDef)
	if !ok {
		t.Fatalf("result[0] = %T, want *NamespaceToolDef", result[0])
	}
	if namespace.Type != "namespace" || namespace.Name != "crm" || namespace.Description != "CRM tools for customer lookup and order management." {
		t.Fatalf("namespace = %#v", namespace)
	}
	if len(namespace.Tools) != 2 {
		t.Fatalf("namespace tools len = %d, want 2", len(namespace.Tools))
	}
	if namespace.Tools[0].Name != "get_customer_profile" || namespace.Tools[1].Name != "list_open_orders" {
		t.Fatalf("namespace tool order = %#v", namespace.Tools)
	}
	if namespace.Tools[1].Strict == nil || !*namespace.Tools[1].Strict || namespace.Tools[1].DeferLoading == nil || !*namespace.Tools[1].DeferLoading {
		t.Fatalf("strict/defer_loading not preserved: %#v", namespace.Tools[1])
	}
	if function, ok := result[1].(FunctionToolDef); !ok || function.Name != "get_weather" {
		t.Fatalf("result[1] = %#v, want ungrouped get_weather function", result[1])
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var wire []map[string]interface{}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal wire: %v", err)
	}
	if wire[0]["type"] != "namespace" || wire[0]["name"] != "crm" {
		t.Fatalf("wire namespace = %#v", wire[0])
	}
}

func TestPrepareTools_FunctionTool_NamespaceConflictingDescription(t *testing.T) {
	_, err := PrepareToolsWithError([]types.Tool{
		{
			Name: "get_customer_profile",
			ProviderOptions: map[string]interface{}{
				"openai": map[string]interface{}{
					"namespace": map[string]interface{}{"name": "crm", "description": "CRM tools."},
				},
			},
		},
		{
			Name: "list_open_orders",
			ProviderOptions: map[string]interface{}{
				"openai": map[string]interface{}{
					"namespace": map[string]interface{}{"name": "crm", "description": "Different CRM tools."},
				},
			},
		},
	})
	if err == nil {
		t.Fatal("PrepareToolsWithError() error = nil, want conflict")
	}
	if got, want := err.Error(), `'conflicting descriptions for OpenAI tool namespace "crm"' functionality not supported.`; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
	if !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("error = %#v, want an UnsupportedFunctionalityError", err)
	}
}

func TestPrepareTools_FunctionTool_DefaultParametersIncludeObjectType(t *testing.T) {
	result := PrepareTools([]types.Tool{{Name: "empty_tool"}})
	def, ok := result[0].(FunctionToolDef)
	if !ok {
		t.Fatalf("expected FunctionToolDef, got %T", result[0])
	}
	params, ok := def.Parameters.(map[string]interface{})
	if !ok {
		t.Fatalf("Parameters = %T %#v", def.Parameters, def.Parameters)
	}
	if params["type"] != "object" {
		t.Fatalf("parameters.type = %v, want object", params["type"])
	}
	if _, ok := params["properties"].(map[string]interface{}); !ok {
		t.Fatalf("parameters.properties = %#v", params["properties"])
	}
}

func TestPrepareTools_FunctionTool_ImplicitObjectSchemaGetsType(t *testing.T) {
	result := PrepareTools([]types.Tool{{
		Name: "implicit",
		Parameters: map[string]interface{}{
			"properties": map[string]interface{}{},
		},
	}})
	def := result[0].(FunctionToolDef)
	params := def.Parameters.(map[string]interface{})
	if params["type"] != "object" {
		t.Fatalf("parameters.type = %v, want object", params["type"])
	}
}

func TestPrepareTools_WebSearch(t *testing.T) {
	external := false
	result := PrepareTools([]types.Tool{openaitool.WebSearch(openaitool.WebSearchConfig{
		ExternalWebAccess: &external,
		Filters:           &openaitool.WebSearchFilters{AllowedDomains: []string{"example.com"}},
		SearchContextSize: "high",
		UserLocation:      &openaitool.WebSearchLocation{Type: "approximate", Country: "US"},
	})})
	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}
	def, ok := result[0].(WebSearchToolDef)
	if !ok {
		t.Fatalf("expected WebSearchToolDef, got %T", result[0])
	}
	if def.Type != "web_search" || def.SearchContextSize != "high" {
		t.Fatalf("web search def = %#v", def)
	}
	if def.ExternalWebAccess == nil || *def.ExternalWebAccess {
		t.Fatalf("external_web_access = %#v, want false", def.ExternalWebAccess)
	}
	domains := def.Filters["allowed_domains"].([]string)
	if domains[0] != "example.com" {
		t.Fatalf("filters = %#v", def.Filters)
	}
	location := def.UserLocation.(map[string]interface{})
	if location["type"] != "approximate" || location["country"] != "US" {
		t.Fatalf("user_location = %#v", location)
	}
}

func TestPrepareTools_ProviderDefinedDispatchesByProviderID(t *testing.T) {
	external := false
	result := PrepareTools([]types.Tool{
		{
			Type:       types.ToolTypeProviderDefined,
			Name:       "browser_search",
			ProviderID: "openai.web_search",
			ProviderOptions: openaitool.WebSearchConfig{
				ExternalWebAccess: &external,
				SearchContextSize: "low",
			},
		},
		{
			Type:       types.ToolTypeProviderDefined,
			Name:       "python",
			ProviderID: "openai.code_interpreter",
			ProviderOptions: openaitool.CodeInterpreterConfig{
				Container: openaitool.CodeInterpreterContainer{FileIDs: []string{"file_1"}},
			},
		},
	})
	if len(result) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(result))
	}

	webSearch, ok := result[0].(WebSearchToolDef)
	if !ok {
		t.Fatalf("expected WebSearchToolDef from ProviderID dispatch, got %T", result[0])
	}
	if webSearch.Type != "web_search" || webSearch.SearchContextSize != "low" || webSearch.ExternalWebAccess == nil || *webSearch.ExternalWebAccess {
		t.Fatalf("web search def = %#v", webSearch)
	}

	code, ok := result[1].(map[string]interface{})
	if !ok {
		t.Fatalf("expected code interpreter map from ProviderID dispatch, got %T", result[1])
	}
	container := code["container"].(map[string]interface{})
	if code["type"] != "code_interpreter" || container["file_ids"].([]string)[0] != "file_1" {
		t.Fatalf("code interpreter def = %#v", code)
	}
}

func TestPrepareTools_WebSearchPreview(t *testing.T) {
	result := PrepareTools([]types.Tool{openaitool.WebSearchPreview(openaitool.WebSearchPreviewConfig{
		SearchContextSize: "low",
	})})
	def, ok := result[0].(WebSearchPreviewToolDef)
	if !ok {
		t.Fatalf("expected WebSearchPreviewToolDef, got %T", result[0])
	}
	if def.Type != "web_search_preview" || def.SearchContextSize != "low" {
		t.Fatalf("web search preview def = %#v", def)
	}
}

func TestPrepareTools_MCP(t *testing.T) {
	readOnly := true
	result := PrepareTools([]types.Tool{openaitool.MCP(openaitool.MCPConfig{
		ServerLabel:       "docs",
		AllowedTools:      openaitool.MCPAllowedTools{ReadOnly: &readOnly, ToolNames: []string{"search_docs"}},
		Authorization:     "Bearer token",
		ConnectorID:       "conn_123",
		Headers:           map[string]string{"x-team": "sdk"},
		RequireApproval:   openaitool.MCPRequireApproval{Never: &openaitool.MCPApprovalFilter{ToolNames: []string{"search_docs"}}},
		ServerDescription: "Documentation search",
		ServerURL:         "https://mcp.example.com",
	})})
	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}
	def, ok := result[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected MCP map, got %T", result[0])
	}
	if def["type"] != "mcp" || def["server_label"] != "docs" || def["authorization"] != "Bearer token" || def["connector_id"] != "conn_123" || def["server_url"] != "https://mcp.example.com" {
		t.Fatalf("mcp def = %#v", def)
	}
	allowed := def["allowed_tools"].(map[string]interface{})
	if allowed["read_only"] != true || allowed["tool_names"].([]string)[0] != "search_docs" {
		t.Fatalf("allowed_tools = %#v", allowed)
	}
	approval := def["require_approval"].(map[string]interface{})
	never := approval["never"].(map[string]interface{})
	if never["tool_names"].([]string)[0] != "search_docs" {
		t.Fatalf("require_approval = %#v", approval)
	}
}

func TestPrepareTools_MCP_DefaultRequireApprovalNever(t *testing.T) {
	result := PrepareTools([]types.Tool{openaitool.MCP(openaitool.MCPConfig{
		ServerLabel: "docs",
		ServerURL:   "https://mcp.example.com",
	})})
	def := result[0].(map[string]interface{})
	if def["require_approval"] != "never" {
		t.Fatalf("require_approval = %#v, want never", def["require_approval"])
	}

	result = PrepareTools([]types.Tool{openaitool.MCP(openaitool.MCPConfig{
		ServerLabel:     "docs",
		ServerURL:       "https://mcp.example.com",
		RequireApproval: openaitool.MCPRequireApproval{},
	})})
	def = result[0].(map[string]interface{})
	if def["require_approval"] != "never" {
		t.Fatalf("empty require_approval object = %#v, want never", def["require_approval"])
	}

	result = PrepareTools([]types.Tool{openaitool.MCP(openaitool.MCPConfig{
		ServerLabel:     "docs",
		ServerURL:       "https://mcp.example.com",
		RequireApproval: openaitool.MCPRequireApproval{Never: &openaitool.MCPApprovalFilter{}},
	})})
	def = result[0].(map[string]interface{})
	approval := def["require_approval"].(map[string]interface{})
	never := approval["never"].(map[string]interface{})
	if _, ok := never["tool_names"]; ok {
		t.Fatalf("require_approval.never = %#v, want tool_names omitted when unset", never)
	}
}

func TestPrepareTools_UnknownProviderToolProducesEmptyToolList(t *testing.T) {
	result := PrepareTools([]types.Tool{{
		Type:       types.ToolTypeProviderDefined,
		Name:       "unknown",
		ProviderID: "openai.unknown",
	}})
	if result == nil || len(result) != 0 {
		t.Fatalf("result = %#v, want explicit empty list for unknown provider tool", result)
	}
}

func TestPrepareTools_HostedOpenAITools(t *testing.T) {
	maxResults := 5
	scoreThreshold := 0.75
	compression := 80
	partialImages := 2

	result := PrepareTools([]types.Tool{
		openaitool.CodeInterpreter(openaitool.CodeInterpreterConfig{
			Container: openaitool.CodeInterpreterContainer{FileIDs: []string{"file_1"}},
		}),
		openaitool.FileSearch(openaitool.FileSearchConfig{
			VectorStoreIDs: []string{"vs_1"},
			MaxNumResults:  &maxResults,
			Ranking: &openaitool.FileSearchRanking{
				Ranker:         "auto",
				ScoreThreshold: &scoreThreshold,
			},
			Filters: map[string]interface{}{"type": "eq", "key": "topic", "value": "go"},
		}),
		openaitool.ImageGeneration(openaitool.ImageGenerationConfig{
			Background:        "transparent",
			InputFidelity:     "high",
			InputImageMask:    &openaitool.ImageGenerationMask{FileID: "file_mask", ImageURL: "data:image/png;base64,abc"},
			Model:             "gpt-image-1",
			Moderation:        "auto",
			OutputCompression: &compression,
			OutputFormat:      "png",
			PartialImages:     &partialImages,
			Quality:           "high",
			Size:              "1024x1024",
		}),
	})
	if len(result) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(result))
	}

	code := result[0].(map[string]interface{})
	container := code["container"].(map[string]interface{})
	if code["type"] != "code_interpreter" || container["type"] != "auto" || container["file_ids"].([]string)[0] != "file_1" {
		t.Fatalf("code interpreter def = %#v", code)
	}

	fileSearch := result[1].(map[string]interface{})
	ranking := fileSearch["ranking_options"].(map[string]interface{})
	if fileSearch["type"] != "file_search" || fileSearch["vector_store_ids"].([]string)[0] != "vs_1" || fileSearch["max_num_results"] != maxResults {
		t.Fatalf("file search def = %#v", fileSearch)
	}
	if ranking["ranker"] != "auto" || ranking["score_threshold"] != scoreThreshold {
		t.Fatalf("ranking_options = %#v", ranking)
	}

	image := result[2].(map[string]interface{})
	mask := image["input_image_mask"].(map[string]interface{})
	if image["type"] != "image_generation" || image["background"] != "transparent" || image["input_fidelity"] != "high" || image["model"] != "gpt-image-1" {
		t.Fatalf("image generation def = %#v", image)
	}
	if mask["file_id"] != "file_mask" || mask["image_url"] != "data:image/png;base64,abc" {
		t.Fatalf("image mask = %#v", mask)
	}
	if image["output_compression"] != compression || image["partial_images"] != partialImages || image["quality"] != "high" || image["size"] != "1024x1024" {
		t.Fatalf("image generation options = %#v", image)
	}
}

func TestPrepareTools_LocalShell(t *testing.T) {
	tool := NewLocalShellTool()
	result := PrepareTools([]types.Tool{tool})
	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}

	def, ok := result[0].(LocalShellToolDef)
	if !ok {
		t.Fatalf("expected LocalShellToolDef, got %T", result[0])
	}
	if def.Type != "local_shell" {
		t.Errorf("Type: got %q, want %q", def.Type, "local_shell")
	}
}

func TestPrepareTools_Shell_NoEnvironment(t *testing.T) {
	tool := NewShellTool()
	result := PrepareTools([]types.Tool{tool})

	def, ok := result[0].(ShellToolDef)
	if !ok {
		t.Fatalf("expected ShellToolDef, got %T", result[0])
	}
	if def.Type != "shell" {
		t.Errorf("Type: got %q, want %q", def.Type, "shell")
	}
	if def.Environment != nil {
		t.Error("expected nil Environment")
	}
}

func TestPrepareTools_Shell_WithContainerAutoEnvironment(t *testing.T) {
	memLimit := "4g"
	tool := NewShellTool(WithShellEnvironment(ShellEnvironment{
		Type:        "container_auto",
		MemoryLimit: &memLimit,
		FileIDs:     []string{"file_1", "file_2"},
	}))

	result := PrepareTools([]types.Tool{tool})
	def, ok := result[0].(ShellToolDef)
	if !ok {
		t.Fatalf("expected ShellToolDef, got %T", result[0])
	}
	if def.Environment == nil {
		t.Fatal("expected non-nil Environment")
	}
	if def.Environment.Type != "container_auto" {
		t.Errorf("Environment.Type: got %q", def.Environment.Type)
	}
	if def.Environment.MemoryLimit == nil || *def.Environment.MemoryLimit != "4g" {
		t.Error("MemoryLimit mismatch")
	}
	if len(def.Environment.FileIDs) != 2 {
		t.Errorf("FileIDs count: got %d, want 2", len(def.Environment.FileIDs))
	}
}

func TestPrepareTools_Shell_WithContainerReferenceEnvironment(t *testing.T) {
	containerID := "container_abc"
	tool := NewShellTool(WithShellEnvironment(ShellEnvironment{
		Type:        "container_reference",
		ContainerID: &containerID,
	}))

	result := PrepareTools([]types.Tool{tool})
	def, ok := result[0].(ShellToolDef)
	if !ok {
		t.Fatalf("expected ShellToolDef, got %T", result[0])
	}
	if def.Environment == nil {
		t.Fatal("expected non-nil Environment")
	}
	if def.Environment.Type != "container_reference" {
		t.Errorf("Environment.Type: got %q", def.Environment.Type)
	}
	if def.Environment.ContainerID == nil || *def.Environment.ContainerID != "container_abc" {
		t.Error("ContainerID mismatch")
	}
}

func TestPrepareTools_ApplyPatch(t *testing.T) {
	tool := NewApplyPatchTool()
	result := PrepareTools([]types.Tool{tool})

	def, ok := result[0].(ApplyPatchToolDef)
	if !ok {
		t.Fatalf("expected ApplyPatchToolDef, got %T", result[0])
	}
	if def.Type != "apply_patch" {
		t.Errorf("Type: got %q, want %q", def.Type, "apply_patch")
	}
}

func TestPrepareTools_CustomTool_WithGrammarFormat(t *testing.T) {
	syntax := "lark"
	def := `start: WORD`
	ct := openaitool.NewCustomTool(
		openaitool.WithDescription("Extract JSON"),
		openaitool.WithFormat(openaitool.CustomToolFormat{
			Type:       "grammar",
			Syntax:     &syntax,
			Definition: &def,
		}),
	)
	sdkTool := ct.ToTool("json-extractor")

	result := PrepareTools([]types.Tool{sdkTool})
	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}

	toolDef, ok := result[0].(CustomToolDef)
	if !ok {
		t.Fatalf("expected CustomToolDef, got %T", result[0])
	}
	if toolDef.Type != "custom" {
		t.Errorf("Type: got %q, want %q", toolDef.Type, "custom")
	}
	if toolDef.Name != "json-extractor" {
		t.Errorf("Name: got %q, want %q", toolDef.Name, "json-extractor")
	}
	if toolDef.Description == nil || *toolDef.Description != "Extract JSON" {
		t.Error("Description mismatch")
	}
	if toolDef.Format == nil {
		t.Fatal("Format should not be nil")
	}
	if toolDef.Format.Type != "grammar" {
		t.Errorf("Format.Type: got %q", toolDef.Format.Type)
	}
	if toolDef.Format.Syntax == nil || *toolDef.Format.Syntax != "lark" {
		t.Error("Syntax mismatch")
	}
}

func TestPrepareTools_CustomTool_WithTextFormat(t *testing.T) {
	ct := openaitool.NewCustomTool(
		openaitool.WithFormat(openaitool.CustomToolFormat{Type: "text"}),
	)
	sdkTool := ct.ToTool("text-tool")

	result := PrepareTools([]types.Tool{sdkTool})
	toolDef, ok := result[0].(CustomToolDef)
	if !ok {
		t.Fatalf("expected CustomToolDef, got %T", result[0])
	}
	if toolDef.Format == nil {
		t.Fatal("Format should not be nil")
	}
	if toolDef.Format.Type != "text" {
		t.Errorf("Format.Type: got %q, want text", toolDef.Format.Type)
	}
}

func TestPrepareTools_CustomTool_NoFormat(t *testing.T) {
	ct := openaitool.NewCustomTool()
	sdkTool := ct.ToTool("simple-tool")

	result := PrepareTools([]types.Tool{sdkTool})
	toolDef, ok := result[0].(CustomToolDef)
	if !ok {
		t.Fatalf("expected CustomToolDef, got %T", result[0])
	}
	if toolDef.Format != nil {
		t.Error("Format should be nil")
	}
	if toolDef.Description != nil {
		t.Error("Description should be nil")
	}
	if toolDef.Name != "simple-tool" {
		t.Errorf("Name: got %q, want %q", toolDef.Name, "simple-tool")
	}
}

func TestPrepareTools_MixedTools_SerializesToJSON(t *testing.T) {
	// Verify the mixed result marshals correctly
	tools := []types.Tool{
		{Name: "get_weather", Description: "Get weather"},
		NewLocalShellTool(),
		NewShellTool(),
		NewApplyPatchTool(),
		openaitool.NewCustomTool().ToTool("my-tool"),
	}

	result := PrepareTools(tools)
	if len(result) != 5 {
		t.Fatalf("expected 5 tools, got %d", len(result))
	}

	// Verify JSON marshaling works for all
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var raw []map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	expectedTypes := []string{"function", "local_shell", "shell", "apply_patch", "custom"}
	for i, expected := range expectedTypes {
		got, ok := raw[i]["type"].(string)
		if !ok || got != expected {
			t.Errorf("tool[%d].type: got %q, want %q", i, got, expected)
		}
	}
}

// TestPrepareTools_Shell_NetworkPolicy verifies network policy serialization
func TestPrepareTools_Shell_NetworkPolicy(t *testing.T) {
	tool := NewShellTool(WithShellEnvironment(ShellEnvironment{
		Type: "container_auto",
		NetworkPolicy: &ShellNetworkPolicy{
			Type:           "allowlist",
			AllowedDomains: []string{"example.com"},
		},
	}))

	result := PrepareTools([]types.Tool{tool})
	def, ok := result[0].(ShellToolDef)
	if !ok {
		t.Fatalf("expected ShellToolDef, got %T", result[0])
	}
	if def.Environment == nil || def.Environment.NetworkPolicy == nil {
		t.Fatal("expected non-nil NetworkPolicy")
	}
	if def.Environment.NetworkPolicy.Type != "allowlist" {
		t.Errorf("NetworkPolicy.Type: got %q", def.Environment.NetworkPolicy.Type)
	}
	if len(def.Environment.NetworkPolicy.AllowedDomains) != 1 {
		t.Error("AllowedDomains count mismatch")
	}
}

// TestPrepareTools_ToolSearch_ServerMode verifies tool_search in server mode.
func TestPrepareTools_ToolSearch_ServerMode(t *testing.T) {
	tool := openaitool.ToolSearch(openaitool.ToolSearchArgs{})

	result := PrepareTools([]types.Tool{tool})
	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}

	def, ok := result[0].(ToolSearchToolDef)
	if !ok {
		t.Fatalf("expected ToolSearchToolDef, got %T", result[0])
	}
	if def.Type != "tool_search" {
		t.Errorf("Type: got %q, want %q", def.Type, "tool_search")
	}
	// Server mode: Execution field is omitted (OpenAI default)
	if def.Execution != "" {
		t.Errorf("Execution: got %q, want empty (server default)", def.Execution)
	}
}

// TestPrepareTools_ToolSearch_ClientMode verifies tool_search in client mode.
func TestPrepareTools_ToolSearch_ClientMode(t *testing.T) {
	tool := openaitool.ToolSearch(openaitool.ToolSearchArgs{
		Execution:   "client",
		Description: "Find tools matching a query",
		Parameters: map[string]interface{}{
			"type": "object",
		},
	})

	result := PrepareTools([]types.Tool{tool})
	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}

	def, ok := result[0].(ToolSearchToolDef)
	if !ok {
		t.Fatalf("expected ToolSearchToolDef, got %T", result[0])
	}
	if def.Type != "tool_search" {
		t.Errorf("Type: got %q, want %q", def.Type, "tool_search")
	}
	if def.Execution != "client" {
		t.Errorf("Execution: got %q, want %q", def.Execution, "client")
	}
	if def.Description != "Find tools matching a query" {
		t.Errorf("Description: got %q", def.Description)
	}
	if def.Parameters == nil {
		t.Error("Parameters should not be nil for client mode")
	}
}

// TestPrepareTools_ToolSearch_SerializesToJSON verifies JSON wire format.
func TestPrepareTools_ToolSearch_SerializesToJSON(t *testing.T) {
	tool := openaitool.ToolSearch(openaitool.ToolSearchArgs{Execution: "client", Description: "search"})
	result := PrepareTools([]types.Tool{tool})

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var raw []map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(raw) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(raw))
	}
	if raw[0]["type"] != "tool_search" {
		t.Errorf("type: got %v, want tool_search", raw[0]["type"])
	}
	if raw[0]["execution"] != "client" {
		t.Errorf("execution: got %v, want client", raw[0]["execution"])
	}
}

// TestPrepareTools_WebSearch_BlockedDomains covers row 96a237d: the web
// search tool's filters must serialize blockedDomains as blocked_domains
// alongside allowed_domains.
func TestPrepareTools_WebSearch_BlockedDomains(t *testing.T) {
	result := PrepareTools([]types.Tool{openaitool.WebSearch(openaitool.WebSearchConfig{
		Filters: &openaitool.WebSearchFilters{
			AllowedDomains: []string{"example.com"},
			BlockedDomains: []string{"blocked.example.com"},
		},
	})})
	def, ok := result[0].(WebSearchToolDef)
	if !ok {
		t.Fatalf("expected WebSearchToolDef, got %T", result[0])
	}
	allowed := def.Filters["allowed_domains"].([]string)
	blocked := def.Filters["blocked_domains"].([]string)
	if allowed[0] != "example.com" || blocked[0] != "blocked.example.com" {
		t.Fatalf("filters = %#v", def.Filters)
	}
}

// TestPrepareTools_ImageGeneration_Action covers row b54e551: the
// image_generation tool must serialize the "action" field.
func TestPrepareTools_ImageGeneration_Action(t *testing.T) {
	result := PrepareTools([]types.Tool{
		openaitool.ImageGeneration(openaitool.ImageGenerationConfig{Action: "edit"}),
	})
	def, ok := result[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", result[0])
	}
	if def["action"] != "edit" {
		t.Fatalf("image generation def = %#v, want action=edit", def)
	}
}

// TestResolveAllowedTools_MCPServerLabel covers the MCP branch of a062795:
// an allowed MCP tool resolves to {type:"mcp", server_label}.
func TestResolveAllowedTools_MCPServerLabel(t *testing.T) {
	tools := []types.Tool{{
		Type:            types.ToolTypeProviderDefined,
		Name:            "docs",
		ProviderID:      "openai.mcp",
		ProviderOptions: openaitool.MCPConfig{ServerLabel: "docs-server"},
	}}
	choice, warnings, err := ResolveAllowedTools(tools, []string{"docs"}, "")
	if err != nil {
		t.Fatalf("ResolveAllowedTools failed: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	if choice.Mode != "auto" || choice.Tools[0].Type != "mcp" || choice.Tools[0].ServerLabel != "docs-server" {
		t.Fatalf("choice = %#v, want mcp entry with server_label", choice)
	}
}

// TestResolveAllowedTools_NamespacedToolDropped covers the namespace branch
// of a062795: a namespaced function tool cannot be allow-listed.
func TestResolveAllowedTools_NamespacedToolDropped(t *testing.T) {
	tools := []types.Tool{{
		Type: types.ToolTypeFunction,
		Name: "grouped_tool",
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"namespace": map[string]interface{}{"name": "ns", "description": "d"},
			},
		},
	}}
	_, warnings, err := ResolveAllowedTools(tools, []string{"grouped_tool"}, "")
	if err == nil {
		t.Fatal("expected an error since the only entry is dropped")
	}
	if len(warnings) != 1 || warnings[0].Feature != `allowedTools entry "grouped_tool"` {
		t.Fatalf("warnings = %#v, want namespace warning", warnings)
	}
}

// TestResolveAllowedTools_EmptyReturnsNil covers the no-op path: no
// allowedTools requested means no tool_choice override.
func TestResolveAllowedTools_EmptyReturnsNil(t *testing.T) {
	choice, warnings, err := ResolveAllowedTools(nil, nil, "")
	if choice != nil || warnings != nil || err != nil {
		t.Fatalf("ResolveAllowedTools(nil) = %#v, %#v, %v, want all nil", choice, warnings, err)
	}
}

// TestResolveAllowedTools_CanonicalAliasResolves covers item 4 of the P1-5c
// slice: an allowedTools entry that names a provider tool's canonical wire
// identity ("file_search") rather than its own (custom) SDK Name resolves
// via the alias layer.
func TestResolveAllowedTools_CanonicalAliasResolves(t *testing.T) {
	tools := []types.Tool{{
		Type:       types.ToolTypeProviderDefined,
		Name:       "myFileSearch",
		ProviderID: "openai.file_search",
	}}
	choice, warnings, err := ResolveAllowedTools(tools, []string{"file_search"}, "")
	if err != nil {
		t.Fatalf("ResolveAllowedTools failed: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	if len(choice.Tools) != 1 || choice.Tools[0].Type != "file_search" {
		t.Fatalf("choice = %#v, want a file_search entry via alias", choice)
	}
}

// mcpTool builds an MCP provider tool with a custom SDK Name distinct from
// its fixed ProviderID, mirroring the TS test helper `mcpTool(name,
// serverLabel)` in openai-responses-prepare-tools.test.ts: two such tools
// with different server labels both canonicalize to the alias "mcp" (see
// canonicalAllowedToolName), which is exactly what makes the ambiguous case
// below reachable.
func mcpToolWithName(name, serverLabel string) types.Tool {
	return types.Tool{
		Type:       types.ToolTypeProviderDefined,
		Name:       name,
		ProviderID: "openai.mcp",
		ProviderOptions: openaitool.MCPConfig{
			ServerLabel: serverLabel,
			ServerURL:   "https://" + serverLabel + ".example.com/mcp",
		},
	}
}

// TestResolveAllowedTools_AmbiguousAliasDroppedWithWarning ports TS's
// "should drop an ambiguous canonical name when several tools share it"
// (openai-responses-prepare-tools.test.ts). Two MCP tools with different
// server labels (and therefore different custom Names) both canonicalize to
// the fixed alias "mcp"; requesting "mcp" in allowedTools is genuinely
// ambiguous between them and is dropped with a warning, leaving only the
// unambiguous function tool allowed.
func TestResolveAllowedTools_AmbiguousAliasDroppedWithWarning(t *testing.T) {
	tools := []types.Tool{
		{Type: types.ToolTypeFunction, Name: "get_weather"},
		mcpToolWithName("alpha", "alpha"),
		mcpToolWithName("beta", "beta"),
	}
	choice, warnings, err := ResolveAllowedTools(tools, []string{"get_weather", "mcp"}, "")
	if err != nil {
		t.Fatalf("ResolveAllowedTools failed: %v", err)
	}
	if len(choice.Tools) != 1 || choice.Tools[0].Type != "function" || choice.Tools[0].Name != "get_weather" {
		t.Fatalf("choice = %#v, want only the unambiguous get_weather function tool", choice)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %#v, want exactly one ambiguity warning", warnings)
	}
	if warnings[0].Feature != `allowedTools entry "mcp"` ||
		warnings[0].Details != "several tools in this request share this provider tool name; use the tool name from the tools for this request instead" {
		t.Fatalf("warning = %#v, want the ambiguous-canonical-name warning", warnings[0])
	}
}

// TestResolveAllowedTools_EachMcpServerResolvesByOwnName ports TS's "should
// still resolve each mcp server by its own tool name": even though both MCP
// tools canonicalize to the ambiguous alias "mcp", an allowedTools entry
// that names one tool's own SDK Name directly ("beta") resolves
// unambiguously to that tool, with no warnings.
func TestResolveAllowedTools_EachMcpServerResolvesByOwnName(t *testing.T) {
	tools := []types.Tool{
		mcpToolWithName("alpha", "alpha"),
		mcpToolWithName("beta", "beta"),
	}
	choice, warnings, err := ResolveAllowedTools(tools, []string{"beta"}, "")
	if err != nil {
		t.Fatalf("ResolveAllowedTools failed: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	if len(choice.Tools) != 1 || choice.Tools[0].Type != "mcp" || choice.Tools[0].ServerLabel != "beta" {
		t.Fatalf("choice = %#v, want the beta mcp server resolved directly by its own name", choice)
	}
}

// TestResolveAllowedTools_IsSameAllowedTool unit-tests the equality helper
// used to decide whether two colliding canonical aliases are "the same
// tool" (and thus not ambiguous) directly.
func TestResolveAllowedTools_IsSameAllowedTool(t *testing.T) {
	a := allowedToolResolution{supported: true, entry: AllowedToolsToolEntry{Type: "custom", Name: "one"}}
	b := allowedToolResolution{supported: true, entry: AllowedToolsToolEntry{Type: "custom", Name: "two"}}
	if isSameAllowedTool(a, b) {
		t.Fatalf("isSameAllowedTool(%#v, %#v) = true, want false (different entries)", a, b)
	}
	if !isSameAllowedTool(a, a) {
		t.Fatalf("isSameAllowedTool(a, a) = false, want true (identical entries)")
	}
	unsupportedA := allowedToolResolution{reason: "r1"}
	unsupportedB := allowedToolResolution{reason: "r2"}
	if isSameAllowedTool(unsupportedA, unsupportedB) {
		t.Fatalf("isSameAllowedTool with different reasons = true, want false")
	}
	if isSameAllowedTool(a, unsupportedA) {
		t.Fatalf("isSameAllowedTool(supported, unsupported) = true, want false")
	}
}

// TestResolveAllowedTools_DirectNameWinsOverAlias covers the first of item
// 4's two warnings: a name that matches both a tool's own SDK Name and
// another tool's canonical alias resolves to the direct match, with a
// warning explaining the overlap.
func TestResolveAllowedTools_DirectNameWinsOverAlias(t *testing.T) {
	tools := []types.Tool{
		{Type: types.ToolTypeFunction, Name: "file_search"},
		{Type: types.ToolTypeProviderDefined, Name: "myFileSearch", ProviderID: "openai.file_search"},
	}
	choice, warnings, err := ResolveAllowedTools(tools, []string{"file_search"}, "")
	if err != nil {
		t.Fatalf("ResolveAllowedTools failed: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %#v, want one overlap warning", warnings)
	}
	if len(choice.Tools) != 1 || choice.Tools[0].Type != "function" || choice.Tools[0].Name != "file_search" {
		t.Fatalf("choice = %#v, want the direct function-tool match to win", choice)
	}
}
