package responses

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

// r75provbZodCase is one args value of an OpenAI provider tool and the
// message TS validateTypes produces for it (zod 4.4.3 safeParseAsync of the
// packages/openai/src/tool/*.ts args schema at e64ed82f27, args keys sorted
// so JSON.stringify matches Go's map order; "" when the args are valid).
type r75provbZodCase struct {
	ID   string `json:"id"`
	Args string `json:"args"`
	Msg  string `json:"msg"`
}

// TestR75ProvbOpenAIToolArgsZodIssues: the nine OpenAI args schemas report
// zod's exact issue list (int, number bounds, regex, strict object, zod v4
// union reporting, recursive file_search filters, discriminated unions).
// Before r75 the issue list was the Go JSON Schema validator's text.
func TestR75ProvbOpenAIToolArgsZodIssues(t *testing.T) {
	f, err := os.Open("testdata/r75_openai_tool_args_zod.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var cases []r75provbZodCase
	if err := json.NewDecoder(zr).Decode(&cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 70 {
		t.Fatalf("only %d cases", len(cases))
	}
	for i, c := range cases {
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(c.Args), &args); err != nil {
			t.Fatal(err)
		}
		tool := types.Tool{Name: "t", Type: types.ToolTypeProviderDefined, ProviderID: c.ID, ProviderArgs: args}
		err := validateOpenAIToolArgsSchema(tool)
		if err == nil {
			err = validateMCPToolArgs(tool)
		}
		got := ""
		if err != nil {
			got = err.Error()
		}
		if got != c.Msg {
			t.Errorf("case %d %s %s:\n got %s\nwant %s", i, c.ID, c.Args, got, c.Msg)
		}
	}
}

// TestR75ProvbTSShapedEmptyStringArgs: TS copies a validated arg as given,
// so a TS-shaped tool's empty-string args reach the wire, and the mcpArgsSchema
// refine (`serverUrl != null || connectorId != null`) accepts serverUrl "".
// Go treated "" as unset (omitted, and the refine failed).
func TestR75ProvbTSShapedEmptyStringArgs(t *testing.T) {
	tsTool := func(id string, args map[string]interface{}) types.Tool {
		return types.Tool{Name: "t", Type: types.ToolTypeProviderDefined, ProviderID: id, ProviderArgs: args}
	}
	wire := func(tool types.Tool) string {
		t.Helper()
		got, err := PrepareToolsWithError([]types.Tool{tool})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(got[0])
		return string(b)
	}
	if got := wire(tsTool("openai.mcp", map[string]interface{}{"serverLabel": "x", "serverUrl": "", "authorization": ""})); got != `{"authorization":"","require_approval":"never","server_label":"x","server_url":"","type":"mcp"}` {
		t.Errorf("mcp = %s", got)
	}
	if got := wire(tsTool("openai.image_generation", map[string]interface{}{"model": "", "inputImageMask": map[string]interface{}{"fileId": ""}})); got != `{"input_image_mask":{"file_id":""},"model":"","type":"image_generation"}` {
		t.Errorf("image generation = %s", got)
	}
	if got := wire(tsTool("openai.web_search", map[string]interface{}{"userLocation": map[string]interface{}{"type": "approximate", "city": ""}})); got != `{"type":"web_search","user_location":{"city":"","type":"approximate"}}` {
		t.Errorf("web search = %s", got)
	}
	if got := wire(tsTool("openai.file_search", map[string]interface{}{"vectorStoreIds": []interface{}{"v"}, "ranking": map[string]interface{}{"ranker": ""}})); got != `{"ranking_options":{"ranker":""},"type":"file_search","vector_store_ids":["v"]}` {
		t.Errorf("file search = %s", got)
	}
	// The Go constructors keep treating "" as unset, and still need a URL.
	if got := wire(openaitool.ImageGeneration(openaitool.ImageGenerationConfig{})); got != `{"type":"image_generation"}` {
		t.Errorf("constructor image generation = %s", got)
	}
	if _, err := PrepareToolsWithError([]types.Tool{openaitool.MCP(openaitool.MCPConfig{ServerLabel: "x"})}); err == nil {
		t.Error("constructor mcp without url must fail the refine")
	}
	if _, err := PrepareToolsWithError([]types.Tool{tsTool("openai.mcp", map[string]interface{}{"serverLabel": "x"})}); err == nil {
		t.Error("TS-shaped mcp without serverUrl/connectorId must fail the refine")
	}
}

// TestR75ProvbEmptyAllowedCallers: TS sends `allowed_callers` whenever
// providerOptions.openai.allowedCallers is not null, so an empty list is
// sent as []; absent stays absent.
func TestR75ProvbEmptyAllowedCallers(t *testing.T) {
	fn := func(opts map[string]interface{}) string {
		got, err := PrepareToolsWithError([]types.Tool{{Name: "f", ProviderOptions: map[string]interface{}{"openai": opts}}})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(got[0])
		return string(b)
	}
	if got := fn(map[string]interface{}{"allowedCallers": []interface{}{}}); got != `{"type":"function","name":"f","parameters":{"properties":{},"type":"object"},"strict":false,"allowed_callers":[]}` {
		t.Errorf("empty allowedCallers = %s", got)
	}
	if got := fn(map[string]interface{}{"allowedCallers": []string{"programmatic"}}); got != `{"type":"function","name":"f","parameters":{"properties":{},"type":"object"},"strict":false,"allowed_callers":["programmatic"]}` {
		t.Errorf("allowedCallers = %s", got)
	}
	if got := fn(map[string]interface{}{}); got != `{"type":"function","name":"f","parameters":{"properties":{},"type":"object"},"strict":false}` {
		t.Errorf("no allowedCallers = %s", got)
	}
}
