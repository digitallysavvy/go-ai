package responses

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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
