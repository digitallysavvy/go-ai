package tools

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// r90provgTakoVariableCostResponse is the TS 0e869b40fa test fixture
// variableCostResponse (tako-search.test.ts).
const r90provgTakoVariableCostResponse = `{
  "request_id": "req_123",
  "cards": [
    {
      "card_id": "card_traffic",
      "title": "vercel.com Traffic Summary",
      "description": "Monthly visits to vercel.com",
      "relevance": "High",
      "relevance_score": 4.2,
      "source_indexes": ["data"],
      "content": {
        "content_format": "json_records",
        "records": [{"month": "2026-09", "visits": 1000}],
        "total_rows": 1,
        "truncated": false,
        "premium_rows": {"company_record": 1},
        "redacted_cells": {"email": [0]},
        "total_matching": 12,
        "export_pricing": {
          "baseline_usd": 0.01,
          "max_rows_ceiling": 1000,
          "row_cpm_usd": 7,
          "premium_row_cpm_usd": {"company_record": 20},
          "variable_cost_max_usd": 0.05
        }
      }
    },
    {
      "card_id": "card_over_cap",
      "title": "vercel.com Backlinks",
      "description": "Withheld",
      "placeholder": {"withheld_source": "Semrush", "reason": "over_cap"}
    },
    {
      "card_id": "card_not_served",
      "title": "vercel.com Keywords",
      "placeholder": {"withheld_source": "Semrush"}
    }
  ],
  "usage": {
    "total_cost_usd": 0.08,
    "compute": {"cost_usd": 0.01},
    "data": {"cost_usd": 0.02, "datasets": 1, "data_points": 2},
    "variable_cost": {"cost_usd": 0.05}
  }
}`

func r90provgTakoValidate(t *testing.T, mutate func(v map[string]interface{})) error {
	t.Helper()
	var v map[string]interface{}
	if err := json.Unmarshal([]byte(r90provgTakoVariableCostResponse), &v); err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(v)
	}
	out := NewTakoSearch(TakoSearchConfig{}).ToTool().OutputSchema.(map[string]interface{})
	return schema.NewSimpleJSONSchema(out).Validator().Validate(v)
}

// TestR90provgTakoDeveloperArgs ports TS 0e869b40fa "passes developer-only
// configuration through as camelCase args", including the explicit null.
func TestR90provgTakoDeveloperArgs(t *testing.T) {
	off := false
	extra := map[string]interface{}{"output_settings": map[string]interface{}{"flat_results": map[string]interface{}{"max_results": 3}}}
	tool := NewTakoSearch(TakoSearchConfig{VariableCost: &off, NoMaxVariableCost: true, ExtraBody: extra}).ToTool()
	want := map[string]interface{}{"variableCost": false, "maxVariableCostUsd": nil, "extraBody": extra}
	if !reflect.DeepEqual(tool.ProviderArgs, want) {
		t.Fatalf("args = %#v, want %#v", tool.ProviderArgs, want)
	}
	if v, ok := tool.ProviderArgs["maxVariableCostUsd"]; !ok || v != nil {
		t.Fatalf("maxVariableCostUsd = %#v (present %v), want null", v, ok)
	}

	cap := 0.5
	tool = NewTakoSearch(TakoSearchConfig{MaxVariableCostUSD: &cap, NoMaxVariableCost: true}).ToTool()
	if got := tool.ProviderArgs["maxVariableCostUsd"]; got != 0.5 {
		t.Fatalf("maxVariableCostUsd = %#v, want 0.5", got)
	}
	if len(NewTakoSearch(TakoSearchConfig{}).ToTool().ProviderArgs) != 0 {
		t.Fatal("empty config must send no args")
	}
}

// TestR90provgTakoInputSchemaHasNoDeveloperFields ports TS "keeps
// developer-only fields out of the input schema".
func TestR90provgTakoInputSchemaHasNoDeveloperFields(t *testing.T) {
	b, err := json.Marshal(NewTakoSearch(TakoSearchConfig{}).ToTool().Parameters)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"variable_cost", "max_variable_cost_usd", "extra_body", "variableCost", "maxVariableCostUsd", "extraBody"} {
		if strings.Contains(string(b), field) {
			t.Errorf("input schema contains %s", field)
		}
	}
}

// TestR90provgTakoVariableCostOutput ports TS "accepts a variable-cost
// response with placeholder cards", "preserves additive fields on usage,
// pricing and placeholders" and "still accepts and preserves the deprecated
// free_rows field".
func TestR90provgTakoVariableCostOutput(t *testing.T) {
	if err := r90provgTakoValidate(t, nil); err != nil {
		t.Fatalf("variable-cost response rejected: %v", err)
	}
	additive := func(v map[string]interface{}) {
		usage := v["usage"].(map[string]interface{})
		usage["future_field"] = 1
		usage["data"].(map[string]interface{})["future_field"] = 2
		usage["variable_cost"].(map[string]interface{})["future_field"] = 3
		cards := v["cards"].([]interface{})
		cards[0].(map[string]interface{})["content"].(map[string]interface{})["export_pricing"].(map[string]interface{})["future_field"] = 4
		cards[1].(map[string]interface{})["placeholder"].(map[string]interface{})["future_field"] = 5
	}
	if err := r90provgTakoValidate(t, additive); err != nil {
		t.Fatalf("additive fields rejected: %v", err)
	}
	freeRows := func(v map[string]interface{}) {
		cards := v["cards"].([]interface{})
		cards[0].(map[string]interface{})["content"].(map[string]interface{})["export_pricing"].(map[string]interface{})["free_rows"] = 0
	}
	if err := r90provgTakoValidate(t, freeRows); err != nil {
		t.Fatalf("free_rows rejected: %v", err)
	}
	badReason := func(v map[string]interface{}) {
		cards := v["cards"].([]interface{})
		cards[1].(map[string]interface{})["placeholder"].(map[string]interface{})["reason"] = "bogus"
	}
	if err := r90provgTakoValidate(t, badReason); err == nil {
		t.Fatal("an unknown placeholder reason must be rejected (TS z.enum)")
	}
}
