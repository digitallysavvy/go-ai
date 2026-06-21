package gateway

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

func newTestGatewayLanguageModel(modelID string) *LanguageModel {
	return NewLanguageModel(&Provider{config: Config{}}, modelID)
}

func TestGatewayTextStreamMapsV4PartsAndFiltersRaw(t *testing.T) {
	stream := &gatewayTextStream{
		parser: streamingParser(strings.Join([]string{
			`data: {"type":"stream-start","warnings":[{"type":"unsupported-setting","feature":"seed","details":"ignored"}]}`,
			"",
			`data: {"type":"raw","rawValue":{"provider":"chunk"}}`,
			"",
			`data: {"type":"response-metadata","id":"resp_1","modelId":"openai/gpt-5","timestamp":"2026-06-19T17:59:31Z"}`,
			"",
			`data: {"type":"text-start","id":"text_1","providerMetadata":{"gateway":{"a":1}}}`,
			"",
			`data: {"type":"text-delta","id":"text_1","delta":"hello"}`,
			"",
			`data: {"type":"text-end","id":"text_1"}`,
			"",
			`data: {"type":"reasoning-start","id":"reason_1"}`,
			"",
			`data: {"type":"reasoning-delta","id":"reason_1","delta":"thinking"}`,
			"",
			`data: {"type":"reasoning-end","id":"reason_1"}`,
			"",
			`data: {"type":"source","id":"src_1","sourceType":"url","url":"https://example.com","title":"Example"}`,
			"",
			`data: {"type":"file","mediaType":"text/plain","data":"SGk=","providerMetadata":{"gateway":{"fileId":"f1"}}}`,
			"",
			`data: {"type":"custom","kind":"gateway-extra","providerMetadata":{"gateway":{"x":true}}}`,
			"",
			`data: {"type":"finish","finishReason":"stop"}`,
			"",
		}, "\n")),
		body: io.NopCloser(strings.NewReader("")),
	}

	chunk, err := stream.Next()
	if err != nil || chunk.Type != provider.ChunkTypeStreamStart || len(chunk.Warnings) != 1 {
		t.Fatalf("stream-start mismatch chunk=%#v err=%v", chunk, err)
	}
	chunk, err = stream.Next()
	if err != nil || chunk.Type != provider.ChunkTypeResponseMetadata || chunk.ResponseMetadata.ID != "resp_1" || chunk.ResponseMetadata.Timestamp.IsZero() {
		t.Fatalf("response metadata mismatch chunk=%#v err=%v", chunk, err)
	}
	chunk, err = stream.Next()
	if err != nil || chunk.Type != provider.ChunkTypeTextStart || chunk.ID != "text_1" || len(chunk.ProviderMetadata) == 0 {
		t.Fatalf("text start mismatch chunk=%#v err=%v", chunk, err)
	}
	chunk, err = stream.Next()
	if err != nil || chunk.Type != provider.ChunkTypeText || chunk.Text != "hello" || chunk.ID != "text_1" {
		t.Fatalf("text delta mismatch chunk=%#v err=%v", chunk, err)
	}
	chunk, err = stream.Next()
	if err != nil || chunk.Type != provider.ChunkTypeTextEnd || chunk.ID != "text_1" {
		t.Fatalf("text end mismatch chunk=%#v err=%v", chunk, err)
	}
	chunk, err = stream.Next()
	if err != nil || chunk.Type != provider.ChunkTypeReasoningStart || chunk.ID != "reason_1" {
		t.Fatalf("reasoning start mismatch chunk=%#v err=%v", chunk, err)
	}
	chunk, err = stream.Next()
	if err != nil || chunk.Type != provider.ChunkTypeReasoning || chunk.Reasoning != "thinking" {
		t.Fatalf("reasoning delta mismatch chunk=%#v err=%v", chunk, err)
	}
	chunk, err = stream.Next()
	if err != nil || chunk.Type != provider.ChunkTypeReasoningEnd || chunk.ID != "reason_1" {
		t.Fatalf("reasoning end mismatch chunk=%#v err=%v", chunk, err)
	}
	chunk, err = stream.Next()
	if err != nil || chunk.Type != provider.ChunkTypeSource || chunk.SourceContent.URL != "https://example.com" {
		t.Fatalf("source mismatch chunk=%#v err=%v", chunk, err)
	}
	chunk, err = stream.Next()
	if err != nil || chunk.Type != provider.ChunkTypeFile || chunk.GeneratedFileContent.FileData.DataString != "SGk=" {
		t.Fatalf("file mismatch chunk=%#v err=%v", chunk, err)
	}
	chunk, err = stream.Next()
	if err != nil || chunk.Type != provider.ChunkTypeCustom || chunk.CustomContent.Kind != "gateway-extra" {
		t.Fatalf("custom mismatch chunk=%#v err=%v", chunk, err)
	}
	chunk, err = stream.Next()
	if err != nil || chunk.Type != provider.ChunkTypeFinish || chunk.FinishReason != types.FinishReasonStop {
		t.Fatalf("finish mismatch chunk=%#v err=%v", chunk, err)
	}
}

func TestGatewayTextStreamIncludesRawChunksWhenRequested(t *testing.T) {
	stream := &gatewayTextStream{
		parser:           streamingParser("data: {\"type\":\"raw\",\"provider\":\"chunk\"}\n\n"),
		body:             io.NopCloser(strings.NewReader("")),
		includeRawChunks: true,
	}
	chunk, err := stream.Next()
	if err != nil || chunk.Type != provider.ChunkTypeRaw {
		t.Fatalf("raw chunk mismatch chunk=%#v err=%v", chunk, err)
	}
	raw, ok := chunk.Raw.(map[string]interface{})
	if !ok || raw["provider"] != "chunk" {
		t.Fatalf("raw payload mismatch: %#v", chunk.Raw)
	}
}

func streamingParser(data string) *streaming.SSEParser {
	return streaming.NewSSEParser(strings.NewReader(data))
}

func TestGatewayLanguageModelMetadataAndCapabilities(t *testing.T) {
	model := newTestGatewayLanguageModel("openai/gpt-5")

	if model.SpecificationVersion() != "v4" {
		t.Fatalf("SpecificationVersion() = %q", model.SpecificationVersion())
	}
	if model.Provider() != "gateway" {
		t.Fatalf("Provider() = %q", model.Provider())
	}
	if model.ModelID() != "openai/gpt-5" {
		t.Fatalf("ModelID() = %q", model.ModelID())
	}
	if !model.SupportsTools() || !model.SupportsStructuredOutput() || !model.SupportsImageInput() {
		t.Fatal("gateway language model should report all core capabilities as supported")
	}
	if urls := model.SupportedURLs(); len(urls["*/*"]) != 1 || urls["*/*"][0] != `.*` {
		t.Fatalf("SupportedURLs() = %#v", urls)
	}
}

func TestGatewayLanguageModelGetModelConfigHeaders(t *testing.T) {
	model := newTestGatewayLanguageModel("anthropic/claude-3.7")
	headers := model.getModelConfigHeaders(true)
	if headers["ai-language-model-specification-version"] != "4" {
		t.Fatalf("spec version header = %q", headers["ai-language-model-specification-version"])
	}
	if headers["ai-language-model-id"] != "anthropic/claude-3.7" {
		t.Fatalf("model ID header = %q", headers["ai-language-model-id"])
	}
	if headers["ai-language-model-streaming"] != "true" {
		t.Fatalf("streaming header = %q", headers["ai-language-model-streaming"])
	}
}

func TestGatewayLanguageModelConvertContentPart(t *testing.T) {
	model := newTestGatewayLanguageModel("x")

	text, err := model.convertContentPart(types.TextContent{Text: "hello"})
	if err != nil {
		t.Fatalf("text conversion error = %v", err)
	}
	if text["type"] != "text" || text["text"] != "hello" {
		t.Fatalf("text part = %#v", text)
	}

	imageURL, err := model.convertContentPart(types.ImageContent{URL: "https://example.com/image.png"})
	if err != nil {
		t.Fatalf("image URL conversion error = %v", err)
	}
	if imageURL["type"] != "image" || imageURL["image"] != "https://example.com/image.png" {
		t.Fatalf("image URL part = %#v", imageURL)
	}

	imageBytes := []byte("img-bytes")
	imageData, err := model.convertContentPart(types.ImageContent{Image: imageBytes, MimeType: "image/png"})
	if err != nil {
		t.Fatalf("image bytes conversion error = %v", err)
	}
	encodedImage := imageData["image"].(string)
	if !strings.HasPrefix(encodedImage, "data:image/png;base64,") {
		t.Fatalf("image data prefix = %q", encodedImage)
	}
	if !strings.Contains(encodedImage, base64.StdEncoding.EncodeToString(imageBytes)) {
		t.Fatalf("image data missing encoded payload: %q", encodedImage)
	}

	fileBytes := []byte("file-bytes")
	filePart, err := model.convertContentPart(types.FileContent{Data: fileBytes, MimeType: "application/pdf"})
	if err != nil {
		t.Fatalf("file conversion error = %v", err)
	}
	dataPart, ok := filePart["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("file data type = %T", filePart["data"])
	}
	if dataPart["type"] != "data" {
		t.Fatalf("file data.type = %v", dataPart["type"])
	}
	if dataPart["data"] != base64.StdEncoding.EncodeToString(fileBytes) {
		t.Fatalf("file data.data mismatch = %v", dataPart["data"])
	}
	if filePart["mediaType"] != "application/pdf" {
		t.Fatalf("file mediaType = %#v", filePart["mediaType"])
	}

	reasoningBytes := []byte{0, 1, 2, 3}
	reasoningPart, err := model.convertContentPart(types.ReasoningFileContent{
		Data:             reasoningBytes,
		MediaType:        "application/pdf",
		ProviderOptions:  map[string]interface{}{"openai": map[string]interface{}{"foo": "bar"}},
		ProviderMetadata: json.RawMessage(`{"id":"rs_1"}`),
	})
	if err != nil {
		t.Fatalf("reasoning file conversion error = %v", err)
	}
	reasoningData, ok := reasoningPart["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("reasoning file data type = %T", reasoningPart["data"])
	}
	if reasoningPart["type"] != "reasoning-file" || reasoningPart["mediaType"] != "application/pdf" {
		t.Fatalf("reasoning file part = %#v", reasoningPart)
	}
	if reasoningData["data"] != base64.StdEncoding.EncodeToString(reasoningBytes) {
		t.Fatalf("reasoning file data.data = %#v", reasoningData["data"])
	}
	if _, ok := reasoningPart["providerOptions"]; !ok {
		t.Fatalf("reasoning file missing provider options: %#v", reasoningPart)
	}
	if _, ok := reasoningPart["providerMetadata"].(map[string]interface{}); !ok {
		t.Fatalf("reasoning file metadata type = %T", reasoningPart["providerMetadata"])
	}

	base64Data := "YWxyZWFkeQ=="
	stringFilePart, err := model.convertContentPart(types.FileContent{
		FileData:  types.FileData{Type: types.FileDataTypeData, DataString: base64Data},
		MediaType: "text/plain",
	})
	if err != nil {
		t.Fatalf("file data string conversion error = %v", err)
	}
	stringFileData := stringFilePart["data"].(map[string]interface{})
	if stringFileData["data"] != base64Data {
		t.Fatalf("base64 string was re-encoded: %#v", stringFileData["data"])
	}

	toolFileBytes := []byte("tool-file")
	toolPart, err := model.convertContentPart(types.ToolResultContent{
		ToolCallID: "call_1",
		ToolName:   "read_file",
		Output: &types.ToolResultOutput{
			Type: types.ToolResultOutputContent,
			Content: []types.ToolResultContentBlock{
				types.FileContentBlock{
					Data:      toolFileBytes,
					MediaType: "text/csv",
					Filename:  "data.csv",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("tool result conversion error = %v", err)
	}
	output := toolPart["output"].(map[string]interface{})
	content := output["value"].([]map[string]interface{})
	fileBlock := content[0]
	blockData := fileBlock["data"].(map[string]interface{})
	if blockData["type"] != "data" || blockData["data"] != base64.StdEncoding.EncodeToString(toolFileBytes) {
		t.Fatalf("tool result file data = %#v", blockData)
	}
	if fileBlock["mediaType"] != "text/csv" || fileBlock["filename"] != "data.csv" {
		t.Fatalf("tool result file block = %#v", fileBlock)
	}

	_, err = model.convertContentPart(types.CustomContent{Kind: "xai-citation"})
	if err == nil || !strings.Contains(err.Error(), "unsupported content part type") {
		t.Fatalf("unsupported part error = %v", err)
	}
}

func TestGatewayLanguageModelProviderOptionsMerge(t *testing.T) {
	model := &LanguageModel{
		provider: &Provider{
			config: Config{
				DisallowPromptTraining: true,
				HIPAACompliant:         true,
				QuotaEntityID:          "quota-123",
			},
		},
		modelID: "openai/gpt-5",
	}

	opts := &provider.GenerateOptions{
		ProviderOptions: map[string]interface{}{
			"gateway": map[string]interface{}{
				"only": []interface{}{"openai"},
			},
			"openai": map[string]interface{}{
				"parallelToolCalls": true,
			},
		},
	}

	got := model.providerOptions(opts)
	gatewayOpts, ok := got["gateway"].(map[string]interface{})
	if !ok {
		t.Fatalf("gateway options type = %T", got["gateway"])
	}
	if gatewayOpts["disallowPromptTraining"] != true || gatewayOpts["hipaaCompliant"] != true || gatewayOpts["quotaEntityId"] != "quota-123" {
		t.Fatalf("missing config-derived gateway options: %#v", gatewayOpts)
	}
	if _, ok := gatewayOpts["only"]; !ok {
		t.Fatalf("caller gateway options should be merged: %#v", gatewayOpts)
	}
	if _, ok := got["openai"]; !ok {
		t.Fatalf("non-gateway provider options should be preserved: %#v", got)
	}
}

func TestGatewayLanguageModelBuildRequestBodyUsesPrompt(t *testing.T) {
	model := newTestGatewayLanguageModel("openai/gpt-5")

	body, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{
					Role: types.RoleUser,
					Content: []types.ContentPart{
						types.FileContent{
							Data:      []byte("file"),
							MediaType: "text/plain",
						},
					},
				},
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody error = %v", err)
	}
	if _, ok := body["messages"]; ok {
		t.Fatalf("gateway body should not use messages key: %#v", body)
	}
	prompt, ok := body["prompt"].([]map[string]interface{})
	if !ok || len(prompt) != 1 {
		t.Fatalf("prompt shape = %#v", body["prompt"])
	}
	content := prompt[0]["content"].([]map[string]interface{})
	filePart := content[0]
	if filePart["mediaType"] != "text/plain" {
		t.Fatalf("file part mediaType = %#v", filePart)
	}
	data := filePart["data"].(map[string]interface{})
	if data["data"] != base64.StdEncoding.EncodeToString([]byte("file")) {
		t.Fatalf("file data = %#v", data)
	}
}
