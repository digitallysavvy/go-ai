package moonshot

import (
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from normalize-json-schema-for-mfjs.test.ts "root unions of objects"
// and the moonshotai chat wire tests (TS #23867).

func mfjsCity() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}},
		"required":   []interface{}{"city"},
	}
}

func mfjsCoords() map[string]interface{} {
	return map[string]interface{}{
		"properties": map[string]interface{}{"lat": map[string]interface{}{"type": "number"}},
		"required":   []interface{}{"lat"},
	}
}

func TestNormalizeJSONSchemaForMFJS_RootUnionsOfObjects(t *testing.T) {
	for _, key := range []string{"anyOf", "oneOf"} {
		out, err := NormalizeJSONSchemaForMFJS(map[string]interface{}{key: []interface{}{mfjsCity(), mfjsCoords()}})
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		want := map[string]interface{}{"type": "object", key: []interface{}{mfjsCity(), mfjsCoords()}}
		if !reflect.DeepEqual(out, want) {
			t.Errorf("%s: got %#v want %#v", key, out, want)
		}
	}

	t.Run("resolves local $ref branches", func(t *testing.T) {
		schema := map[string]interface{}{
			"$defs":       map[string]interface{}{"City": mfjsCity(), "Coords": mfjsCoords()},
			"anyOf":       []interface{}{map[string]interface{}{"$ref": "#/$defs/City"}, map[string]interface{}{"$ref": "#/definitions/Coords"}},
			"definitions": map[string]interface{}{"Coords": mfjsCoords()},
		}
		out, err := NormalizeJSONSchemaForMFJS(schema)
		if err != nil {
			t.Fatal(err)
		}
		got := out.(map[string]interface{})
		if got["type"] != "object" || got["anyOf"] == nil || got["$defs"] == nil {
			t.Fatalf("got %#v", got)
		}
	})

	t.Run("keeps type object at the root beside anyOf", func(t *testing.T) {
		schema := map[string]interface{}{"type": "object", "anyOf": []interface{}{mfjsCity(), mfjsCoords()}}
		out, err := NormalizeJSONSchemaForMFJS(schema)
		if err != nil || !reflect.DeepEqual(out, schema) {
			t.Fatalf("got %#v err %v", out, err)
		}
	})

	t.Run("allOf-wrapped union passes through unchanged", func(t *testing.T) {
		schema := map[string]interface{}{"type": "object", "allOf": []interface{}{map[string]interface{}{"anyOf": []interface{}{mfjsCity(), mfjsCoords()}}}}
		out, err := NormalizeJSONSchemaForMFJS(schema)
		if err != nil || !reflect.DeepEqual(out, schema) {
			t.Fatalf("got %#v err %v", out, err)
		}
	})

	for name, schema := range map[string]map[string]interface{}{
		"non-local $ref":    {"anyOf": []interface{}{mfjsCity(), map[string]interface{}{"$ref": "https://example.com/coords.json"}}},
		"mixed anyOf/oneOf": {"anyOf": []interface{}{mfjsCity(), mfjsCoords()}, "oneOf": []interface{}{mfjsCity(), map[string]interface{}{"type": "string"}}},
		"nullable object":   {"anyOf": []interface{}{mfjsCity(), map[string]interface{}{"type": []interface{}{"object", "null"}, "properties": map[string]interface{}{}}}},
		"non-object branch": {"anyOf": []interface{}{mfjsCity(), map[string]interface{}{"type": "string"}}},
		"empty union":       {"anyOf": []interface{}{}},
	} {
		if _, err := NormalizeJSONSchemaForMFJS(schema); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestNormalizeJSONSchemaForMFJS_RootErrorMessage(t *testing.T) {
	_, err := NormalizeJSONSchemaForMFJS(map[string]interface{}{"type": "array"})
	if err == nil || !strings.Contains(err.Error(), `tool and response schemas must be a JSON Schema object with type "object" for moonshotai (MFJS)`) {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildRequestBody_RootUnionOfObjectsSentWithTypeObject(t *testing.T) {
	model := newMoonshotTestModel("kimi-k3")
	union := func() map[string]interface{} {
		return map[string]interface{}{"anyOf": []interface{}{mfjsCity(), mfjsCoords()}}
	}
	want := map[string]interface{}{"type": "object", "anyOf": []interface{}{mfjsCity(), mfjsCoords()}}

	body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:         textPrompt("Hello"),
		ResponseFormat: &provider.ResponseFormat{Type: "json", Name: "location", Schema: union()},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	js := body["response_format"].(map[string]interface{})["json_schema"].(map[string]interface{})
	if !reflect.DeepEqual(js["schema"], want) {
		t.Errorf("response_format schema = %#v", js["schema"])
	}

	typed := union()
	typed["type"] = "object"
	body, _, err = model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: textPrompt("Hello"),
		Tools: []types.Tool{
			{Name: "untyped", Parameters: union()},
			{Name: "typed", Parameters: typed},
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	tools := body["tools"].([]moonshotFunctionTool)
	if len(tools) != 2 {
		t.Fatalf("tools = %#v", tools)
	}
	for _, tool := range tools {
		if !reflect.DeepEqual(tool.Function.Parameters, want) {
			t.Errorf("tool %s parameters = %#v", tool.Function.Name, tool.Function.Parameters)
		}
	}
}
