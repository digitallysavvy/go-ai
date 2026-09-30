package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestNewTakoSearchSchemaAndProviderExecutedStub(t *testing.T) {
	tool := NewTakoSearch(TakoSearchConfig{}).ToTool()
	if tool.Name != "tako_search" {
		t.Fatalf("name = %q", tool.Name)
	}
	if tool.Title != "Tako Search" || !tool.ProviderExecuted {
		t.Fatalf("tool metadata = %#v", tool)
	}
	if tool.Type != types.ToolTypeProviderDefined || tool.ProviderID != "gateway.tako_search" {
		t.Fatalf("provider tool identity = type:%q id:%q", tool.Type, tool.ProviderID)
	}

	params, ok := tool.Parameters.(map[string]interface{})
	if !ok {
		t.Fatalf("parameters type = %T", tool.Parameters)
	}
	required := params["required"].([]string)
	if len(required) != 1 || required[0] != "query" {
		t.Fatalf("required = %#v", required)
	}
	properties := params["properties"].(map[string]interface{})
	for _, name := range []string{"query", "effort", "sources", "location", "country_code", "locale", "timezone", "output_settings", "include_related"} {
		if _, ok := properties[name]; !ok {
			t.Fatalf("missing property %q in %#v", name, properties)
		}
	}
	effort := properties["effort"].(map[string]interface{})
	if enum, ok := effort["enum"].([]string); !ok || !containsString(enum, "deep") || !containsString(enum, "fast") || !containsString(enum, "instant") {
		t.Fatalf("effort enum = %#v", effort["enum"])
	}

	result, err := tool.Execute(nil, nil, types.ToolExecutionOptions{ToolCallID: "call_1"})
	if err == nil || result != nil {
		t.Fatalf("Execute result=%#v err=%v, want provider-executed error", result, err)
	}
	toolErr, ok := err.(*types.ToolExecutionError)
	if !ok || !toolErr.ProviderExecuted || toolErr.ToolName != "tako_search" || toolErr.ToolCallID != "call_1" {
		t.Fatalf("tool error = %#v", err)
	}
}

// TestTakoSearchInputSchemaDescribesDataSurchargeControls mirrors the TS test
// "describes data surcharge controls in the input schema"
// (tool/tako-search.test.ts).
func TestTakoSearchInputSchemaDescribesDataSurchargeControls(t *testing.T) {
	tool := NewTakoSearch(TakoSearchConfig{}).ToTool()
	properties := tool.Parameters.(map[string]interface{})["properties"].(map[string]interface{})
	dataSource := properties["sources"].(map[string]interface{})["properties"].(map[string]interface{})["data"].(map[string]interface{})["properties"].(map[string]interface{})

	if desc, _ := dataSource["count"].(map[string]interface{})["description"].(string); !strings.Contains(desc, "data surcharge") {
		t.Fatalf("count description = %q, want to contain 'data surcharge'", desc)
	}
	if desc, _ := dataSource["include_contents"].(map[string]interface{})["description"].(string); !strings.Contains(desc, "cards.content.export_pricing") {
		t.Fatalf("include_contents description = %q, want to contain 'cards.content.export_pricing'", desc)
	}
	if desc, _ := dataSource["max_rows"].(map[string]interface{})["description"].(string); !strings.Contains(desc, "per 1,000 exported rows") {
		t.Fatalf("max_rows description = %q, want to contain 'per 1,000 exported rows'", desc)
	}
}

// TestTakoSearchOutputSchemaOmitsInternalFields mirrors the TS test "does not
// declare internal response fields in the output schema" (5533946): the
// internal-only relevance_score and citation_number fields must never appear
// in the public output schema.
func TestTakoSearchOutputSchemaOmitsInternalFields(t *testing.T) {
	tool := NewTakoSearch(TakoSearchConfig{}).ToTool()
	serialized, err := json.Marshal(tool.OutputSchema)
	if err != nil {
		t.Fatalf("marshal output schema: %v", err)
	}
	if strings.Contains(string(serialized), "relevance_score") {
		t.Fatalf("output schema must not contain relevance_score: %s", serialized)
	}
	if strings.Contains(string(serialized), "citation_number") {
		t.Fatalf("output schema must not contain citation_number: %s", serialized)
	}
}

func TestTakoSearchOutputSchemaErrorEnumIncludesUnknownTool(t *testing.T) {
	tool := NewTakoSearch(TakoSearchConfig{}).ToTool()
	output := tool.OutputSchema.(map[string]interface{})
	variants := output["oneOf"].([]interface{})
	if len(variants) != 2 {
		t.Fatalf("output schema should match TS success/error union: %#v", output)
	}
	errorSchema := variants[1].(map[string]interface{})["properties"].(map[string]interface{})
	errorEnum := errorSchema["error"].(map[string]interface{})["enum"].([]string)
	if !containsString(errorEnum, "unknown_tool") {
		t.Fatalf("output error enum missing unknown_tool: %#v", errorEnum)
	}
	if !containsString(errorEnum, "execution_error") {
		t.Fatalf("output error enum missing execution_error: %#v", errorEnum)
	}
}

func TestNewTakoSearchProviderArgs(t *testing.T) {
	tool := NewTakoSearch(TakoSearchConfig{
		Effort: "deep",
		Sources: &TakoSearchSources{
			Data: &TakoDataSourceConfig{
				Count:           intPtr(5),
				IncludeContents: boolPtr(true),
				Mode:            "inline",
				ContentFormat:   "json_records",
				MaxRows:         intPtr(100),
				NodeIDs:         []string{"n1", "n2"},
				Strict:          boolPtr(true),
			},
			Web: &TakoWebSourceConfig{
				Count:                  intPtr(3),
				IncludeContents:        boolPtr(false),
				Category:               "finance",
				IncludeDomains:         []string{"wsj.com"},
				ExcludeDomains:         []string{"example.com"},
				SnippetMaxChars:        intPtr(200),
				Highlights:             boolPtr(true),
				ArticleContentMaxChars: intPtr(2000),
				PublishedAfter:         "2026-01-01",
				PublishedBefore:        "2026-06-01",
			},
		},
		Location:    &TakoSearchLocation{Latitude: 37.7, Longitude: -122.4},
		CountryCode: "US",
		Locale:      "en-US",
		Timezone:    "America/Los_Angeles",
		OutputSettings: &TakoSearchOutputSettings{
			ImageDarkMode: boolPtr(true),
			ForceRefresh:  boolPtr(false),
		},
		IncludeRelated: intPtr(4),
	}).ToTool()

	args := tool.ProviderArgs
	if args["effort"] != "deep" {
		t.Fatalf("effort = %#v", args["effort"])
	}
	sources := args["sources"].(map[string]interface{})
	data := sources["data"].(map[string]interface{})
	if data["count"] != 5 || data["includeContents"] != true || data["mode"] != "inline" || data["contentFormat"] != "json_records" || data["maxRows"] != 100 || data["strict"] != true {
		t.Fatalf("data source args = %#v", data)
	}
	nodeIDs, ok := data["nodeIds"].([]string)
	if !ok || len(nodeIDs) != 2 {
		t.Fatalf("nodeIds = %#v", data["nodeIds"])
	}
	web := sources["web"].(map[string]interface{})
	if web["count"] != 3 || web["includeContents"] != false || web["category"] != "finance" || web["snippetMaxChars"] != 200 || web["highlights"] != true || web["articleContentMaxChars"] != 2000 || web["publishedAfter"] != "2026-01-01" || web["publishedBefore"] != "2026-06-01" {
		t.Fatalf("web source args = %#v", web)
	}
	location := args["location"].(map[string]interface{})
	if location["latitude"] != 37.7 || location["longitude"] != -122.4 {
		t.Fatalf("location args = %#v", location)
	}
	if args["countryCode"] != "US" || args["locale"] != "en-US" || args["timezone"] != "America/Los_Angeles" {
		t.Fatalf("localization args = %#v", args)
	}
	outputSettings := args["outputSettings"].(map[string]interface{})
	if outputSettings["imageDarkMode"] != true || outputSettings["forceRefresh"] != false {
		t.Fatalf("outputSettings args = %#v", outputSettings)
	}
	if args["includeRelated"] != 4 {
		t.Fatalf("includeRelated = %#v", args["includeRelated"])
	}
}

func TestNewTakoSearchProviderArgsOmitEmptyFields(t *testing.T) {
	tool := NewTakoSearch(TakoSearchConfig{}).ToTool()
	args := tool.ProviderArgs
	if len(args) != 0 {
		t.Fatalf("expected empty provider args for zero-value config, got %#v", args)
	}
}
