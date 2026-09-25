package providerutils

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

func TestChatResponseFormat(t *testing.T) {
	objSchema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}}
	tests := []struct {
		name     string
		rf       *provider.ResponseFormat
		opts     ChatResponseFormatOptions
		want     string // "" = omitted
		warnings int
	}{
		{name: "nil", rf: nil, want: ""},
		{name: "text", rf: &provider.ResponseFormat{Type: "text"}, want: ""},
		{name: "json without schema", rf: &provider.ResponseFormat{Type: "json"}, opts: ChatResponseFormatOptions{StructuredOutputs: true}, want: `{"type":"json_object"}`},
		{
			name: "json with schema, strict, default name",
			rf:   &provider.ResponseFormat{Type: "json", Schema: objSchema},
			opts: ChatResponseFormatOptions{StructuredOutputs: true, StrictJSONSchema: true},
			want: `{"type":"json_schema","json_schema":{"schema":{"type":"object","properties":{"value":{"type":"string"}}},"strict":true,"name":"response"}}`,
		},
		{
			name: "json_schema type with name, description, schema.Schema, non-strict",
			rf:   &provider.ResponseFormat{Type: "json_schema", Schema: schema.NewSimpleJSONSchema(objSchema), Name: "test-name", Description: "test description"},
			opts: ChatResponseFormatOptions{StructuredOutputs: true},
			want: `{"type":"json_schema","json_schema":{"schema":{"type":"object","properties":{"value":{"type":"string"}}},"strict":false,"name":"test-name","description":"test description"}}`,
		},
		{
			name:     "schema without structured outputs falls back to json_object with warning",
			rf:       &provider.ResponseFormat{Type: "json", Schema: objSchema},
			opts:     ChatResponseFormatOptions{StructuredOutputs: false, WarnWhenSchemaUnsupported: true},
			want:     `{"type":"json_object"}`,
			warnings: 1,
		},
		{name: "explicit json_object ignores schema", rf: &provider.ResponseFormat{Type: "json_object", Schema: objSchema}, opts: ChatResponseFormatOptions{StructuredOutputs: true}, want: `{"type":"json_object"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, warnings := ChatResponseFormat(tt.rf, tt.opts)
			if len(warnings) != tt.warnings {
				t.Fatalf("warnings = %#v, want %d", warnings, tt.warnings)
			}
			if tt.want == "" {
				if got != nil {
					t.Fatalf("got %#v, want nil", got)
				}
				return
			}
			gotJSON, _ := json.Marshal(got)
			var gotV, wantV interface{}
			_ = json.Unmarshal(gotJSON, &gotV)
			_ = json.Unmarshal([]byte(tt.want), &wantV)
			if !reflect.DeepEqual(gotV, wantV) {
				t.Fatalf("got %s, want %s", gotJSON, tt.want)
			}
		})
	}
}
