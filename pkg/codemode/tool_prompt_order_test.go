package codemode

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

const orderedSchemaJSON = `{"type":"object","properties":{"zeta":{"type":"string"},"alpha":{"type":"object","properties":{"yy":{"type":"number"},"bb":{"type":"boolean"}},"required":["yy","bb"]},"mid":{"type":"string"}},"required":["zeta","alpha","mid"]}`

func TestToolPromptFollowsSchemaOrder(t *testing.T) {
	os, err := schema.NewOrderedSchema([]byte(orderedSchemaJSON))
	if err != nil {
		t.Fatal(err)
	}
	tree, _ := jsonorder.Parse([]byte(orderedSchemaJSON))
	var m map[string]interface{}
	if m = os.JSONSchema(); m == nil {
		t.Fatal("nil map")
	}
	cases := map[string]types.Tool{
		"ordered schema": {Name: "t", Parameters: os},
		"hinted":         {Name: "t", Parameters: jsonorder.Hinted{V: m, Hint: tree}},
		"key order":      {Name: "t", Parameters: m, ParameterKeyOrder: tree},
	}
	for name, tool := range cases {
		t.Run(name, func(t *testing.T) {
			got := buildCodeModeToolDescriptionOrdered([]types.Tool{tool}, ToolDiscoveryDescription)
			if !strings.Contains(got, `(input: { zeta: string; alpha: { yy: number; bb: boolean; }; mid: string; })`) {
				t.Fatalf("type not in schema order:\n%s", got)
			}
			want := `tools.t({"zeta":"string","alpha":{"yy":1,"bb":true},"mid":"string"})`
			if !strings.Contains(got, want) {
				t.Fatalf("example not in schema order, want %s in:\n%s", want, got)
			}
		})
	}
}

func TestToolPromptProjectionUsesFirstSchemaProperty(t *testing.T) {
	os, _ := schema.NewOrderedSchema([]byte(orderedSchemaJSON))
	tool := types.Tool{Name: "t", OutputSchema: os}
	if got := renderToolProjection("result", tool); got != "{ zeta: result.zeta }" {
		t.Fatalf("got %s", got)
	}
}
