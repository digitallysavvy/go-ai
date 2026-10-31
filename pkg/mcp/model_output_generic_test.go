package mcp

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS mcpToModelOutput converts any object with a content array, not only a
// typed CallToolResult.
func TestConvertToModelOutputGenericContentArray(t *testing.T) {
	converter := NewMCPToolConverter(NewMCPClient(newMockTransport(), MCPClientConfig{}))

	type wrapper struct {
		Content []map[string]interface{} `json:"content"`
		Extra   string                   `json:"extra"`
	}
	inputs := map[string]interface{}{
		"map": map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "hi <b>"},
				map[string]interface{}{"type": "image", "data": "AQI=", "mimeType": "image/png"},
				map[string]interface{}{"type": "resource_link", "uri": "file://x"},
			},
			"isError": false,
		},
		"struct ptr": &wrapper{Content: []map[string]interface{}{
			{"type": "text", "text": "hi <b>"},
			{"type": "image", "data": "AQI=", "mimeType": "image/png"},
			{"type": "resource_link", "uri": "file://x"},
		}},
	}
	for name, in := range inputs {
		t.Run(name, func(t *testing.T) {
			out := converter.convertToModelOutput(in)
			if out.Type != types.ToolResultOutputContent || len(out.Content) != 3 {
				t.Fatalf("out = %#v, want 3 content blocks", out)
			}
			if tb, ok := out.Content[0].(types.TextContentBlock); !ok || tb.Text != "hi <b>" {
				t.Fatalf("block 0 = %#v", out.Content[0])
			}
			fb, ok := out.Content[1].(types.FileContentBlock)
			if !ok || fb.MediaType != "image/png" || fb.FileData.DataString != "AQI=" || fb.FileData.Type != types.FileDataTypeData {
				t.Fatalf("block 1 = %#v", out.Content[1])
			}
			if tb, ok := out.Content[2].(types.TextContentBlock); !ok || tb.Text != `{"type":"resource_link","uri":"file://x"}` {
				t.Fatalf("block 2 = %#v", out.Content[2])
			}
		})
	}

	// Empty content array is still content (TS Array.isArray([]) is true).
	if out := converter.convertToModelOutput(map[string]interface{}{"content": []interface{}{}}); out.Type != types.ToolResultOutputContent || len(out.Content) != 0 {
		t.Fatalf("empty content: %#v", out)
	}
	// Non-array content stays JSON with the original value.
	in := map[string]interface{}{"content": "text"}
	if out := converter.convertToModelOutput(in); out.Type != types.ToolResultOutputJSON {
		t.Fatalf("string content: %#v", out)
	}
	if out := converter.convertToModelOutput([]interface{}{1}); out.Type != types.ToolResultOutputJSON {
		t.Fatalf("array output: %#v", out)
	}
}
