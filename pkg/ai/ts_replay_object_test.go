package ai

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Replays packages/ai/src/generate-object/generate-object.test.ts cases
// ('error handling', 'options.repairText', 'output = array/enum/no-schema')
// through GenerateObject with the same mock responses.

func objectModel(text string) *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			res := &types.GenerateResult{FinishReason: types.FinishReasonStop, Usage: *usage(10, 20)}
			res.Text = text
			return res, nil
		},
	}
}

func contentSchema() schema.Schema {
	return schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{"content": map[string]interface{}{"type": "string"}},
		"required": []interface{}{"content"}, "additionalProperties": false,
	})
}

func TestTSReplay_GenerateObject_ErrorHandling(t *testing.T) {
	repairAppend := func(suffix string) RepairTextFunc {
		return func(_ context.Context, text string, _ error) (*string, error) { s := text + suffix; return &s, nil }
	}
	cases := []struct {
		name    string
		text    string
		repair  RepairTextFunc
		message string
	}{
		{"schema validation fails", `{ "content": 123 }`, nil, "No object generated: response did not match schema."},
		{"parsing fails", `{ broken json`, nil, "No object generated: could not parse the response."},
		{"parsing fails with repair", `{ broken json`, repairAppend("{"), "No object generated: could not parse the response."},
		{"no text", ``, nil, "No object generated: the model did not return a response."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := GenerateObject(context.Background(), GenerateObjectOptions{
				Model: objectModel(tc.text), Prompt: "prompt", Schema: contentSchema(), RepairText: tc.repair,
			})
			var nog *NoObjectGeneratedError
			if !errors.As(err, &nog) {
				t.Fatalf("error = %v (%T), want NoObjectGeneratedError", err, err)
			}
			if nog.Message != tc.message {
				t.Fatalf("message = %q, want %q", nog.Message, tc.message)
			}
			if err.Error() != tc.message {
				t.Fatalf("Error() = %q, want TS message %q", err.Error(), tc.message)
			}
			if nog.FinishReason != types.FinishReasonStop {
				t.Fatalf("finish reason = %v", nog.FinishReason)
			}
			if nog.Usage == nil || *nog.Usage.InputTokens != 10 || *nog.Usage.OutputTokens != 20 || *nog.Usage.TotalTokens != 30 {
				t.Fatalf("usage = %#v", nog.Usage)
			}
			if tc.text != "" && tc.repair == nil && nog.Text != tc.text {
				t.Fatalf("text = %q", nog.Text)
			}
		})
	}
}

func TestTSReplay_GenerateObject_RepairText(t *testing.T) {
	t.Run("repairs a JSON parse error", func(t *testing.T) {
		var gotText string
		var gotErr error
		r, err := GenerateObject(context.Background(), GenerateObjectOptions{
			Model: objectModel(`{ "content": "provider metadata test" `), Prompt: "prompt", Schema: contentSchema(),
			RepairText: func(_ context.Context, text string, e error) (*string, error) {
				gotText, gotErr = text, e
				s := text + "}"
				return &s, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(r.Object, map[string]interface{}{"content": "provider metadata test"}) {
			t.Fatalf("object = %#v", r.Object)
		}
		if gotText != `{ "content": "provider metadata test" ` || gotErr == nil {
			t.Fatalf("repair got %q, %v", gotText, gotErr)
		}
	})
	t.Run("repairs a validation error", func(t *testing.T) {
		r, err := GenerateObject(context.Background(), GenerateObjectOptions{
			Model: objectModel(`{ "content-a": "provider metadata test" }`), Prompt: "prompt", Schema: contentSchema(),
			RepairText: func(context.Context, string, error) (*string, error) {
				s := `{ "content": "provider metadata test" }`
				return &s, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(r.Object, map[string]interface{}{"content": "provider metadata test"}) {
			t.Fatalf("object = %#v", r.Object)
		}
	})
	t.Run("repair returning nil fails the call", func(t *testing.T) {
		_, err := GenerateObject(context.Background(), GenerateObjectOptions{
			Model: objectModel(`{ "content-a": "provider metadata test" }`), Prompt: "prompt", Schema: contentSchema(),
			RepairText: func(context.Context, string, error) (*string, error) { return nil, nil },
		})
		var nog *NoObjectGeneratedError
		if !errors.As(err, &nog) || nog.Message != "No object generated: response did not match schema." {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("deprecated option works and stable wins", func(t *testing.T) {
		model := objectModel(`{ "content": "repaired"`)
		r, err := GenerateObject(context.Background(), GenerateObjectOptions{
			Model: model, Prompt: "prompt", Schema: contentSchema(),
			RepairText: repairWith(" }"),
			ExperimentalRepairText: func(context.Context, string, error) (*string, error) {
				t.Error("deprecated alias must not be called when repairText is set")
				return nil, nil
			},
		})
		if err != nil || !reflect.DeepEqual(r.Object, map[string]interface{}{"content": "repaired"}) {
			t.Fatalf("object=%#v err=%v", r, err)
		}
		r, err = GenerateObject(context.Background(), GenerateObjectOptions{
			Model: model, Prompt: "prompt", Schema: contentSchema(), ExperimentalRepairText: repairWith(" }"),
		})
		if err != nil || !reflect.DeepEqual(r.Object, map[string]interface{}{"content": "repaired"}) {
			t.Fatalf("deprecated: object=%#v err=%v", r, err)
		}
	})
}

func repairWith(suffix string) RepairTextFunc {
	return func(_ context.Context, text string, _ error) (*string, error) { s := text + suffix; return &s, nil }
}

func responseFormatSchema(t *testing.T, rf *provider.ResponseFormat) map[string]interface{} {
	t.Helper()
	if rf == nil || rf.Schema == nil {
		return nil
	}
	// TS sends the JSON schema itself; accept a map or a schema value.
	var raw interface{} = rf.Schema
	if js, ok := rf.Schema.(interface{ JSONSchema() map[string]interface{} }); ok {
		raw = js.JSONSchema()
	}
	// normalize through JSON so []string and []interface{} compare equal
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// generate-object.test.ts: output = array / enum / no-schema, including the
// response format schema each mode sends to the model.
func TestTSReplay_GenerateObject_OutputModes(t *testing.T) {
	t.Run("array", func(t *testing.T) {
		model := objectModel(`{"elements":[{"content":"element 1"},{"content":"element 2"},{"content":"element 3"}]}`)
		r, err := GenerateObject(context.Background(), GenerateObjectOptions{
			Model: model, Prompt: "prompt", Schema: contentSchema(), OutputMode: ObjectModeArray,
		})
		if err != nil {
			t.Fatal(err)
		}
		want := []interface{}{
			map[string]interface{}{"content": "element 1"}, map[string]interface{}{"content": "element 2"}, map[string]interface{}{"content": "element 3"},
		}
		if !reflect.DeepEqual(r.Array, want) {
			t.Fatalf("array = %#v", r.Array)
		}
		wantSchema := map[string]interface{}{
			"$schema": "http://json-schema.org/draft-07/schema#", "type": "object", "additionalProperties": false,
			"required": []interface{}{"elements"},
			"properties": map[string]interface{}{"elements": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object", "additionalProperties": false, "required": []interface{}{"content"},
					"properties": map[string]interface{}{"content": map[string]interface{}{"type": "string"}},
				},
			}},
		}
		if got := responseFormatSchema(t, model.GenerateCalls[0].ResponseFormat); !reflect.DeepEqual(got, wantSchema) {
			gb, _ := json.Marshal(got)
			wb, _ := json.Marshal(wantSchema)
			t.Fatalf("response schema\n got %s\nwant %s", gb, wb)
		}
	})
	t.Run("enum", func(t *testing.T) {
		model := objectModel(`{"result":"sunny"}`)
		r, err := GenerateObject(context.Background(), GenerateObjectOptions{
			Model: model, Prompt: "prompt", OutputMode: ObjectModeEnum, EnumValues: []string{"sunny", "rainy", "snowy"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if r.EnumValue != "sunny" {
			t.Fatalf("enum = %q", r.EnumValue)
		}
		wantSchema := map[string]interface{}{
			"$schema": "http://json-schema.org/draft-07/schema#", "type": "object", "additionalProperties": false,
			"required":   []interface{}{"result"},
			"properties": map[string]interface{}{"result": map[string]interface{}{"type": "string", "enum": []interface{}{"sunny", "rainy", "snowy"}}},
		}
		if got := responseFormatSchema(t, model.GenerateCalls[0].ResponseFormat); !reflect.DeepEqual(got, wantSchema) {
			gb, _ := json.Marshal(got)
			t.Fatalf("response schema %s", gb)
		}
	})
	t.Run("no-schema", func(t *testing.T) {
		model := objectModel(`{ "content": "Hello, world!" }`)
		r, err := GenerateObject(context.Background(), GenerateObjectOptions{Model: model, Prompt: "prompt", OutputMode: ObjectModeNoSchema})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(r.Object, map[string]interface{}{"content": "Hello, world!"}) {
			t.Fatalf("object = %#v", r.Object)
		}
		if rf := model.GenerateCalls[0].ResponseFormat; rf == nil || rf.Schema != nil {
			t.Fatalf("response format = %#v, want JSON mode without schema", rf)
		}
	})
}
