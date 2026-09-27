package alibaba

import "strings"

// jsonSchemaModelPrefixes lists the model ID prefixes that support Alibaba's
// native JSON Schema structured output mode.
// https://www.alibabacloud.com/help/en/model-studio/qwen-structured-output#supported-models
var jsonSchemaModelPrefixes = []string{
	"qwen3.7-plus",
	"qwen3.7-flash",
	"qwen3.7-max",
	"qwen3.8-max",
	"qwen3.8-flash",
}

// supportsJsonSchemaOutput reports whether modelID supports Alibaba's native
// JSON Schema response_format mode, mirroring the TypeScript SDK's
// supportsJsonSchemaOutput.
func supportsJsonSchemaOutput(modelID string) bool {
	for _, prefix := range jsonSchemaModelPrefixes {
		if modelID == prefix || strings.HasPrefix(modelID, prefix+"-") {
			return true
		}
	}
	return false
}
