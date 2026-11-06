package cohere

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS convertToCohereChatPrompt treats a file part as an image only when
// getTopLevelMediaType(mediaType) is exactly "image"; a media type that only
// starts with "image" (here "imagery/plain") is a document.
func TestR80provaCohereTopLevelImageMediaType(t *testing.T) {
	model := &LanguageModel{modelID: "command-r-plus"}
	for _, tc := range []struct {
		mediaType string
		image     bool
	}{
		{"imagery/plain", false},
		{"image/png", true},
		{"image", true},
	} {
		body, err := model.buildRequestBody(&provider.GenerateOptions{
			Prompt: types.Prompt{Messages: []types.Message{{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.FileContent{
						FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte("\x89PNG\r\n\x1a\nxxxx"), MediaType: tc.mediaType},
					},
				},
			}}},
		})
		if err != nil {
			t.Fatalf("%s: %v", tc.mediaType, err)
		}
		_, hasDocs := body["documents"]
		_, isParts := body["messages"].([]map[string]interface{})[0]["content"].([]map[string]interface{})
		if hasDocs == tc.image || isParts != tc.image {
			t.Fatalf("%s: documents=%v image parts=%v, want image=%v", tc.mediaType, hasDocs, isParts, tc.image)
		}
	}
}

// TS cohereChatChunkSchema declares index only on the content members; on
// any other member an index that is not a number is stripped, so the chunk
// parses (no error part, and the finish reason is not "error").
func TestR80provaCohereStreamForeignIndexIgnored(t *testing.T) {
	body := "data: {\"type\":\"message-start\",\"id\":\"g1\",\"index\":\"x\"}\n\n" +
		"data: {\"type\":\"citation-start\",\"index\":{}}\n\n" +
		"data: {\"type\":\"content-start\",\"index\":0,\"delta\":{\"message\":{\"content\":{\"type\":\"text\",\"text\":\"\"}}}}\n\n" +
		"data: {\"type\":\"content-delta\",\"index\":0,\"delta\":{\"message\":{\"content\":{\"text\":\"hi\"}}}}\n\n" +
		"data: {\"type\":\"content-end\",\"index\":0}\n\n" +
		"data: {\"type\":\"message-end\",\"index\":\"y\",\"delta\":{\"finish_reason\":\"COMPLETE\",\"usage\":{\"tokens\":{\"input_tokens\":1,\"output_tokens\":2}}}}\n\n"
	s := newCohereV2Stream(io.NopCloser(strings.NewReader(body)))
	var got []string
	var finish *provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			if err != io.EOF {
				t.Fatal(err)
			}
			break
		}
		if c.Type == provider.ChunkTypeError {
			t.Fatalf("unexpected error part: %s", c.Text)
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
		got = append(got, string(c.Type))
	}
	if finish == nil || finish.FinishReason != types.FinishReasonStop {
		t.Fatalf("parts %v, finish %+v", got, finish)
	}
}
