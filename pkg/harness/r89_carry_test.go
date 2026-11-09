package harness

import (
	"context"
	"encoding/base64"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// TS utils/normalize-harness-tool-model-output.ts: the error names the
// part type and, for a file, its data type; a media type without a full
// subtype ("image/") is detected. Messages recorded from TS 382d0b8eaf
// (bun) for the same parts.
func TestR89carryNormalizeToolModelOutputErrors(t *testing.T) {
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	const prefix = "Harnesses support only text and inline images in tool model output. "
	cases := []struct {
		name  string
		block types.ToolResultContentBlock
		want  string
	}{
		{"file url", types.FileContentBlock{MediaType: "image/png", URL: "https://x/a.png"}, prefix + "Unsupported content 'file' (image/png, url)."},
		{"file reference", types.FileContentBlock{MediaType: "image/png", Reference: "file_1"}, prefix + "Unsupported content 'file' (image/png, reference)."},
		{"file text", types.FileContentBlock{MediaType: "image/png", Text: "x"}, prefix + "Unsupported content 'file' (image/png, text)."},
		{"image data not an image", types.ImageContentBlock{MediaType: "text/plain", Data: png}, prefix + "Unsupported content 'image-data' (text/plain, data)."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NormalizeToolModelOutput(types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{tc.block}})
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err = %v\nwant %s", err, tc.want)
			}
		})
	}
	t.Run("image/ is detected", func(t *testing.T) {
		out, err := NormalizeToolModelOutput(types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
			types.FileContentBlock{MediaType: "image/", Data: png},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if img, ok := out.Content[0].(types.ImageContentBlock); !ok || img.MediaType != "image/png" {
			t.Fatalf("got %#v", out.Content[0])
		}
	})
}

type r89carryWeather struct {
	City string  `json:"city"`
	Temp float64 `json:"temp"`
}

// TS harness-agent.ts _resolveResponseFormat (1238-1255) forwards
// `responseFormat.schema` whatever its source. ai.ObjectOutput with a struct
// schema (or an *schema.OrderedSchema) hands an *schema.OrderedSchema; the
// harness turn must still get the JSON schema, or every adapter fails the
// turn with "requires a JSON schema for structured output".
func TestR89carryResponseFormatKeepsOrderedSchema(t *testing.T) {
	ordered, err := schema.NewOrderedSchema([]byte(`{"type":"object","properties":{"a":{"type":"string"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]schema.Schema{
		"struct":  schema.NewSimpleStructSchema(reflect.TypeOf(r89carryWeather{})),
		"ordered": ordered,
	} {
		t.Run(name, func(t *testing.T) {
			rf, err := resolveOutputResponseFormat(context.Background(), ai.ObjectOutput[map[string]any](ai.ObjectOutputOptions{Schema: s, Name: "w"}))
			if err != nil {
				t.Fatal(err)
			}
			if rf == nil || rf.Type != ResponseFormatJSON || rf.Name != "w" {
				t.Fatalf("rf = %#v", rf)
			}
			if rf.Schema == nil || rf.Schema["type"] != "object" || rf.Schema["properties"] == nil {
				t.Fatalf("schema = %#v", rf.Schema)
			}
		})
	}
}

// TS normalize-harness-tool-model-output.ts checks the top-level media type
// before detection: an empty media type fails (getTopLevelMediaType('') is
// '').
func TestR89carryNormalizeEmptyMediaType(t *testing.T) {
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	const prefix = "Harnesses support only text and inline images in tool model output. "
	for _, tc := range []struct {
		block types.ToolResultContentBlock
		want  string
	}{
		{types.ImageContentBlock{Data: png}, prefix + "Unsupported content 'image-data' (, data)."},
		{types.FileContentBlock{Data: png}, prefix + "Unsupported content 'file' (, data)."},
	} {
		_, err := NormalizeToolModelOutput(types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{tc.block}})
		if err == nil || err.Error() != tc.want {
			t.Errorf("err = %v, want %s", err, tc.want)
		}
	}
}

// TS convert-harness-tool-model-output.ts getNormalizedInlineImage: only a
// file part with inline data and a full image/* media type converts.
func TestR89carryConvertToolModelOutputImages(t *testing.T) {
	const prefix = "Harnesses support only text and inline images in tool model output; unsupported content "
	_, _, err := ConvertToolModelOutput(types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
		types.ImageContentBlock{Data: []byte{1}, MediaType: "image/*"},
	}})
	if err == nil || err.Error() != prefix+"'image-data'." {
		t.Fatalf("image/*: err = %v", err)
	}
	out, isErr, err := ConvertToolModelOutput(types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
		types.FileContentBlock{FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte{1, 2}}, MediaType: "image/png"},
	}})
	if err != nil || isErr || len(out) != 1 || out[0].Type != ToolModelOutputContentImage || out[0].Data != "AQI=" || out[0].MediaType != "image/png" {
		t.Fatalf("file: out = %#v, err = %v", out, err)
	}
	_, _, err = ConvertToolModelOutput(types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
		types.FileContentBlock{URL: "https://x/a.png", MediaType: "image/png"},
	}})
	if err == nil || err.Error() != prefix+"'file'." {
		t.Fatalf("file url: err = %v", err)
	}
}
