package responses

import (
	"encoding/json"
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
	jsonschema "github.com/digitallysavvy/go-ai/pkg/schema"
)

// validateOpenAIToolArgsSchema mirrors the validateTypes call TS
// prepareResponsesTools makes on each provider tool's args before building
// the request: invalid args (an unknown enum value, an out-of-range
// outputCompression, a grammar format without a definition, ...) fail with
// a type validation error instead of reaching the API. Tools without TS
// args (a hand-built tool with only a typed ProviderOptions config) are not
// checked.
func validateOpenAIToolArgsSchema(t types.Tool) error {
	if t.Type != types.ToolTypeProviderDefined || t.ProviderArgs == nil {
		return nil
	}
	schemaJSON, ok := openAIToolArgsSchemas[t.ProviderID]
	if !ok {
		return nil
	}
	b, err := json.Marshal(t.ProviderArgs)
	if err != nil {
		return openAIToolArgsError(t.ProviderArgs, err.Error(), err)
	}
	var args interface{}
	if err := json.Unmarshal(b, &args); err != nil {
		return openAIToolArgsError(t.ProviderArgs, err.Error(), err)
	}
	if err := jsonschema.NewSimpleJSONSchema(schemaFromJSON(schemaJSON)).Validator().Validate(args); err != nil {
		return openAIToolArgsError(args, err.Error(), err)
	}
	return nil
}

// openAIToolArgsError is the TypeValidationError TS validateTypes throws
// without a context: "Type validation failed: Value: <JSON.stringify(args)>.
// \nError message: <issues>". The issues are the Go JSON Schema validator's
// text (zod renders its issue list there).
func openAIToolArgsError(args interface{}, issues string, cause error) error {
	value := ""
	if b, err := jsonstr.Marshal(args); err == nil {
		value = string(b)
	} else {
		value = fmt.Sprintf("%v", args)
	}
	return &providererrors.TypeValidationError{
		Context: &providererrors.ValidationContext{},
		Value:   value,
		Message: issues,
		Cause:   cause,
	}
}

// openAIToolArgsSchemas are the TS args schemas (packages/openai/src/tool/*.ts
// at e64ed82f27, the *ArgsSchema each prepareResponsesTools case validates
// with) as zod toJSONSchema renders them (target draft-7, io "input",
// reused "inline").
var openAIToolArgsSchemas = map[string]string{
	"openai.web_search":         `{"type":"object","properties":{"externalWebAccess":{"type":"boolean"},"filters":{"type":"object","properties":{"allowedDomains":{"type":"array","items":{"type":"string"}},"blockedDomains":{"type":"array","items":{"type":"string"}}}},"searchContextSize":{"type":"string","enum":["low","medium","high"]},"userLocation":{"type":"object","properties":{"type":{"type":"string","const":"approximate"},"country":{"type":"string"},"city":{"type":"string"},"region":{"type":"string"},"timezone":{"type":"string"}},"required":["type"]}}}`,
	"openai.web_search_preview": `{"type":"object","properties":{"searchContextSize":{"type":"string","enum":["low","medium","high"]},"userLocation":{"type":"object","properties":{"type":{"type":"string","const":"approximate"},"country":{"type":"string"},"city":{"type":"string"},"region":{"type":"string"},"timezone":{"type":"string"}},"required":["type"]}}}`,
	"openai.code_interpreter":   `{"type":"object","properties":{"container":{"anyOf":[{"type":"string"},{"type":"object","properties":{"fileIds":{"type":"array","items":{"type":"string"}}}}]}}}`,
	"openai.file_search":        `{"type":"object","properties":{"vectorStoreIds":{"type":"array","items":{"type":"string"}},"maxNumResults":{"type":"number"},"ranking":{"type":"object","properties":{"ranker":{"type":"string"},"scoreThreshold":{"type":"number"}}},"filters":{"anyOf":[{"type":"object","properties":{"key":{"type":"string"},"type":{"type":"string","enum":["eq","ne","gt","gte","lt","lte","in","nin"]},"value":{"anyOf":[{"type":"string"},{"type":"number"},{"type":"boolean"},{"type":"array","items":{"type":"string"}}]}},"required":["key","type","value"]},{"$ref":"#/definitions/__schema0"}]}},"required":["vectorStoreIds"],"definitions":{"__schema0":{"type":"object","properties":{"type":{"type":"string","enum":["and","or"]},"filters":{"type":"array","items":{"anyOf":[{"type":"object","properties":{"key":{"type":"string"},"type":{"type":"string","enum":["eq","ne","gt","gte","lt","lte","in","nin"]},"value":{"anyOf":[{"type":"string"},{"type":"number"},{"type":"boolean"},{"type":"array","items":{"type":"string"}}]}},"required":["key","type","value"]},{"allOf":[{"$ref":"#/definitions/__schema0"}]}]}}},"required":["type","filters"]}}}`,
	"openai.image_generation":   `{"type":"object","properties":{"action":{"type":"string","enum":["generate","edit","auto"]},"background":{"type":"string","enum":["auto","opaque","transparent"]},"inputFidelity":{"type":"string","enum":["low","high"]},"inputImageMask":{"type":"object","properties":{"fileId":{"type":"string"},"imageUrl":{"type":"string"}}},"model":{"type":"string"},"moderation":{"type":"string","enum":["auto","low"]},"outputCompression":{"type":"integer","minimum":0,"maximum":100},"outputFormat":{"type":"string","enum":["png","jpeg","webp"]},"partialImages":{"type":"integer","minimum":0,"maximum":3},"quality":{"type":"string","enum":["auto","low","medium","high","xhigh","max"]},"size":{"anyOf":[{"type":"string","enum":["1024x1024","1024x1536","1536x1024","auto"]},{"type":"string","pattern":"^\\d+x\\d+$"}]}},"additionalProperties":false}`,
	"openai.mcp":                `{"type":"object","properties":{"serverLabel":{"type":"string"},"allowedTools":{"anyOf":[{"type":"array","items":{"type":"string"}},{"type":"object","properties":{"readOnly":{"type":"boolean"},"toolNames":{"type":"array","items":{"type":"string"}}}}]},"authorization":{"type":"string"},"connectorId":{"type":"string"},"headers":{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{"type":"string"}},"requireApproval":{"anyOf":[{"type":"string","enum":["always","never"]},{"type":"object","properties":{"never":{"type":"object","properties":{"toolNames":{"type":"array","items":{"type":"string"}}}}}}]},"serverDescription":{"type":"string"},"serverUrl":{"type":"string"}},"required":["serverLabel"]}`,
	"openai.custom":             `{"type":"object","properties":{"description":{"type":"string"},"async":{"type":"boolean"},"format":{"anyOf":[{"type":"object","properties":{"type":{"type":"string","const":"grammar"},"syntax":{"type":"string","enum":["regex","lark"]},"definition":{"type":"string"}},"required":["type","syntax","definition"]},{"type":"object","properties":{"type":{"type":"string","const":"text"}},"required":["type"]}]}}}`,
	"openai.tool_search":        `{"type":"object","properties":{"execution":{"type":"string","enum":["server","client"]},"description":{"type":"string"},"parameters":{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{}}}}`,
	"openai.shell":              `{"type":"object","properties":{"environment":{"anyOf":[{"type":"object","properties":{"type":{"type":"string","const":"containerAuto"},"fileIds":{"type":"array","items":{"type":"string"}},"memoryLimit":{"type":"string","enum":["1g","4g","16g","64g"]},"networkPolicy":{"oneOf":[{"type":"object","properties":{"type":{"type":"string","const":"disabled"}},"required":["type"]},{"type":"object","properties":{"type":{"type":"string","const":"allowlist"},"allowedDomains":{"type":"array","items":{"type":"string"}},"domainSecrets":{"type":"array","items":{"type":"object","properties":{"domain":{"type":"string"},"name":{"type":"string"},"value":{"type":"string"}},"required":["domain","name","value"]}}},"required":["type","allowedDomains"]}]},"skills":{"type":"array","items":{"oneOf":[{"type":"object","properties":{"type":{"type":"string","const":"skillReference"},"providerReference":{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{"type":"string"}},"version":{"type":"string"}},"required":["type","providerReference"]},{"type":"object","properties":{"type":{"type":"string","const":"inline"},"name":{"type":"string"},"description":{"type":"string"},"source":{"type":"object","properties":{"type":{"type":"string","const":"base64"},"mediaType":{"type":"string","const":"application/zip"},"data":{"type":"string"}},"required":["type","mediaType","data"]}},"required":["type","name","description","source"]}]}}},"required":["type"]},{"type":"object","properties":{"type":{"type":"string","const":"containerReference"},"containerId":{"type":"string"}},"required":["type","containerId"]},{"type":"object","properties":{"type":{"type":"string","const":"local"},"skills":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},"path":{"type":"string"}},"required":["name","description","path"]}}}}]}}}`,
}
