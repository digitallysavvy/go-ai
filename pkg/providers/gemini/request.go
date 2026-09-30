package gemini

import (
	"context"
	"fmt"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

// configurableSafetySettingCategories are the categories the standalone
// `threshold` provider option expands to (TS parity).
var configurableSafetySettingCategories = []string{
	"HARM_CATEGORY_HATE_SPEECH",
	"HARM_CATEGORY_DANGEROUS_CONTENT",
	"HARM_CATEGORY_HARASSMENT",
	"HARM_CATEGORY_SEXUALLY_EXPLICIT",
}

// vertexOnlyImageConfigFields are imageConfig fields only the Vertex AI API
// accepts; the Gemini API rejects them.
var vertexOnlyImageConfigFields = []string{"personGeneration", "prominentPeople", "imageOutputOptions"}

// googleCloudStorageFunctionResponseURLs lists the media types and URL
// pattern gs:// (Google Cloud Storage) tool-result file URLs may be forwarded
// directly as functionResponse.parts[].fileData for, on Gemini 3+ models with
// Config.SupportsGoogleCloudStorageUrls (Vertex). Mirrors TS
// google-language-model.ts googleCloudStorageFunctionResponseUrls.
var googleCloudStorageFunctionResponseURLs = map[string][]string{
	"image/png":       {`^gs://.*$`},
	"image/jpeg":      {`^gs://.*$`},
	"image/webp":      {`^gs://.*$`},
	"application/pdf": {`^gs://.*$`},
	"text/plain":      {`^gs://.*$`},
}

func (m *LanguageModel) isVertex() bool {
	return m.cfg.IsVertex || m.cfg.ProviderName == "google-vertex"
}

// providerOptionsNames mirrors TS providerOptionsNames: the keys read from
// part provider options and written to provider metadata.
func (m *LanguageModel) providerOptionsNames() []string {
	if len(m.cfg.MetadataKeys) > 0 {
		return m.cfg.MetadataKeys
	}
	if m.cfg.MetadataKey != "" {
		return []string{m.cfg.MetadataKey}
	}
	return []string{"google"}
}

// getProviderOpts returns the first matching provider options map from
// GenerateOptions.ProviderOptions using the configured key precedence order.
func (m *LanguageModel) getProviderOpts(opts *provider.GenerateOptions) map[string]interface{} {
	if opts == nil || opts.ProviderOptions == nil {
		return nil
	}
	for _, key := range m.cfg.ProviderOptionsKeys {
		if v, ok := opts.ProviderOptions[key].(map[string]interface{}); ok {
			return v
		}
	}
	return nil
}

func otherWarning(message string) types.Warning {
	return types.Warning{Type: "other", Message: message, Details: message}
}

// buildRequestBody builds the Gemini API request body from GenerateOptions.
func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, isStreaming bool) map[string]interface{} {
	body, _, _, _ := m.buildRequest(context.Background(), opts, isStreaming)
	return body
}

// buildRequest ports TS GoogleLanguageModel.prepareRequest.
func (m *LanguageModel) buildRequest(ctx context.Context, opts *provider.GenerateOptions, isStreaming bool) (map[string]interface{}, map[string]string, []types.Warning, error) {
	if opts == nil {
		opts = &provider.GenerateOptions{}
	}
	body := map[string]interface{}{}
	headers := map[string]string{}
	warnings := []types.Warning{}
	isVertex := m.isVertex()
	provOpts := m.getProviderOpts(opts)
	providerName := m.cfg.ProviderName

	for _, t := range opts.Tools {
		if t.Type == "provider" && t.ProviderID == "google.vertex_rag_store" && !isVertex {
			warnings = append(warnings, otherWarning(fmt.Sprintf(
				"The 'vertex_rag_store' tool is only supported with the Google Vertex provider and might not be supported or could behave unexpectedly with the current Google provider (%s).", providerName)))
			break
		}
	}
	if v, _ := provOpts["streamFunctionCallArguments"].(bool); v && !isVertex {
		warnings = append(warnings, otherWarning(fmt.Sprintf(
			"'streamFunctionCallArguments' is only supported on the Vertex AI API and will be ignored with the current Google provider (%s). See https://docs.cloud.google.com/vertex-ai/generative-ai/docs/multimodal/function-calling#streaming-fc", providerName)))
	}
	serviceTier, _ := provOpts["serviceTier"].(string)
	if serviceTier != "" && isVertex {
		warnings = append(warnings, otherWarning(
			"'serviceTier' is a Gemini API option and is not supported on Vertex AI. Use 'sharedRequestType' (and optionally 'requestType') instead. See https://docs.cloud.google.com/vertex-ai/generative-ai/docs/priority-paygo"))
	}
	sharedRequestType, _ := provOpts["sharedRequestType"].(string)
	requestType, _ := provOpts["requestType"].(string)
	if (sharedRequestType != "" || requestType != "") && !isVertex {
		warnings = append(warnings, otherWarning(fmt.Sprintf(
			"'sharedRequestType' and 'requestType' are Vertex AI options and are ignored with the current Google provider (%s).", providerName)))
	}
	if isVertex {
		if sharedRequestType != "" {
			headers["X-Vertex-AI-LLM-Shared-Request-Type"] = sharedRequestType
		}
		if requestType != "" {
			headers["X-Vertex-AI-LLM-Request-Type"] = requestType
		}
	}

	// personGeneration, prominentPeople and imageOutputOptions are Vertex-only.
	var imageConfig interface{}
	if ic, ok := provOpts["imageConfig"]; ok && ic != nil {
		imageConfig = ic
		if icMap, isMap := ic.(map[string]interface{}); isMap && !isVertex {
			var dropped []string
			filtered := make(map[string]interface{}, len(icMap))
			for k, v := range icMap {
				filtered[k] = v
			}
			for _, field := range vertexOnlyImageConfigFields {
				if v, present := icMap[field]; present && v != nil {
					dropped = append(dropped, "'imageConfig."+field+"'")
					delete(filtered, field)
				}
			}
			if len(dropped) > 0 {
				verb := "are Vertex AI options and are"
				if len(dropped) == 1 {
					verb = "is a Vertex AI option and is"
				}
				warnings = append(warnings, otherWarning(fmt.Sprintf("%s %s ignored with the current Google provider (%s).",
					strings.Join(dropped, ", "), verb, providerName)))
				imageConfig = filtered
			}
		}
	}

	gemma := isGemmaModel(m.modelID)
	isGemini25DeveloperAPI := !isVertex && isGemini25Model(m.modelID)
	if isGemini25DeveloperAPI && opts.FrequencyPenalty != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "frequencyPenalty", Message: "frequencyPenalty"})
	}
	if isGemini25DeveloperAPI && opts.PresencePenalty != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "presencePenalty", Message: "presencePenalty"})
	}

	caps := GetModelCapabilities(m.modelID)

	// Prompt conversion.
	var messages []types.Message
	if opts.Prompt.IsMessages() {
		messages = opts.Prompt.Messages
	} else if opts.Prompt.IsSimple() {
		messages = prompt.SimpleTextToMessages(opts.Prompt.Text)
	}
	var supportedFunctionResponseURLs map[string][]string
	if caps.UsesGemini3Features && m.cfg.SupportsGoogleCloudStorageUrls {
		supportedFunctionResponseURLs = googleCloudStorageFunctionResponseURLs
	}
	if m.cfg.ToolResultDownloadMaxBytes > 0 {
		// TS google-language-model.ts passes the same supportedUrls to
		// downloadToolResultFiles, so a supported gs:// URL is forwarded
		// as fileData instead of being downloaded (or rejected by the
		// download URL scheme check).
		downloaded, err := m.downloadToolResultFiles(ctx, messages, providerutils.CompileSupportedURLPatterns(supportedFunctionResponseURLs))
		if err != nil {
			return nil, nil, nil, err
		}
		messages = downloaded
	}
	converted, err := prompt.ConvertToGoogleMessages(messages, prompt.GoogleMessagesOptions{
		System:                        opts.Prompt.System,
		IsGemmaModel:                  gemma,
		IsGemini3Model:                caps.UsesGemini3Features,
		ProviderOptionsNames:          m.providerOptionsNames(),
		SupportsFunctionResponseParts: caps.UsesGemini3Features,
		IncludeFunctionCallIDs:        !isVertex,
		SupportedFunctionResponseURLs: supportedFunctionResponseURLs,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	warnings = append(warnings, converted.Warnings...)
	body["contents"] = converted.Contents
	if converted.SystemInstruction != nil && !gemma {
		body["systemInstruction"] = converted.SystemInstruction
	}

	prepared := prepareTools(opts.Tools, opts.ToolChoice, m.modelID, isVertex)

	// Thinking config: resolved call-level reasoning merged with (and
	// overridden by) providerOptions.thinkingConfig.
	var thinkingConfig map[string]interface{}
	resolvedThinking := resolveThinkingConfig(opts.Reasoning, m.modelID, &warnings)
	optThinking, _ := provOpts["thinkingConfig"].(map[string]interface{})
	if optThinking != nil || resolvedThinking != nil {
		thinkingConfig = map[string]interface{}{}
		for k, v := range resolvedThinking {
			thinkingConfig[k] = v
		}
		for k, v := range optThinking {
			thinkingConfig[k] = v
		}
	}

	genConfig := map[string]interface{}{}
	if opts.MaxTokens != nil {
		genConfig["maxOutputTokens"] = *opts.MaxTokens
	}
	if opts.Temperature != nil {
		genConfig["temperature"] = *opts.Temperature
	}
	if opts.TopK != nil {
		genConfig["topK"] = *opts.TopK
	}
	if opts.TopP != nil {
		genConfig["topP"] = *opts.TopP
	}
	if opts.FrequencyPenalty != nil && !isGemini25DeveloperAPI {
		genConfig["frequencyPenalty"] = *opts.FrequencyPenalty
	}
	if opts.PresencePenalty != nil && !isGemini25DeveloperAPI {
		genConfig["presencePenalty"] = *opts.PresencePenalty
	}
	if len(opts.StopSequences) > 0 {
		genConfig["stopSequences"] = opts.StopSequences
	}
	if opts.Seed != nil {
		genConfig["seed"] = *opts.Seed
	}
	if opts.ResponseFormat != nil && (opts.ResponseFormat.Type == "json" || opts.ResponseFormat.Type == "json_object") {
		genConfig["responseMimeType"] = "application/json"
	}
	if opts.ResponseFormat != nil && opts.ResponseFormat.Type == "json" && opts.ResponseFormat.Schema != nil {
		structuredOutputs := true
		if so, ok := provOpts["structuredOutputs"].(bool); ok {
			structuredOutputs = so
		}
		if structuredOutputs {
			genConfig["responseJsonSchema"] = sanitizeResponseJSONSchema(opts.ResponseFormat.Schema)
		}
	}
	if v, ok := provOpts["audioTimestamp"].(bool); ok && v {
		genConfig["audioTimestamp"] = v
	}
	if v, ok := provOpts["responseModalities"]; ok && v != nil {
		genConfig["responseModalities"] = v
	}
	if thinkingConfig != nil {
		genConfig["thinkingConfig"] = thinkingConfig
	}
	if v, ok := provOpts["mediaResolution"]; ok && v != nil && v != "" {
		genConfig["mediaResolution"] = v
	}
	if imageConfig != nil {
		genConfig["imageConfig"] = imageConfig
	}
	if len(genConfig) > 0 {
		body["generationConfig"] = genConfig
	}

	// Safety settings; a standalone threshold expands to the four configurable categories.
	if v, ok := provOpts["safetySettings"]; ok && v != nil {
		body["safetySettings"] = v
	} else if threshold, ok := provOpts["threshold"]; ok && threshold != nil {
		settings := make([]map[string]interface{}, 0, len(configurableSafetySettingCategories))
		for _, category := range configurableSafetySettingCategories {
			settings = append(settings, map[string]interface{}{"category": category, "threshold": threshold})
		}
		body["safetySettings"] = settings
	}

	if len(prepared.tools) > 0 {
		body["tools"] = prepared.tools
	}

	// toolConfig: prepared config + Vertex streamFunctionCallArguments + retrievalConfig.
	streamFCArgs := false
	if isStreaming && isVertex {
		streamFCArgs, _ = provOpts["streamFunctionCallArguments"].(bool)
	}
	retrievalConfig, hasRetrieval := provOpts["retrievalConfig"]
	hasRetrieval = hasRetrieval && retrievalConfig != nil
	if prepared.toolConfig != nil || streamFCArgs || hasRetrieval {
		toolConfig := map[string]interface{}{}
		for k, v := range prepared.toolConfig {
			toolConfig[k] = v
		}
		if streamFCArgs {
			fcc := map[string]interface{}{}
			if existing, ok := toolConfig["functionCallingConfig"].(map[string]interface{}); ok {
				for k, v := range existing {
					fcc[k] = v
				}
			}
			fcc["streamFunctionCallArguments"] = true
			toolConfig["functionCallingConfig"] = fcc
		}
		if hasRetrieval {
			toolConfig["retrievalConfig"] = retrievalConfig
		}
		body["toolConfig"] = toolConfig
	}

	if v, ok := provOpts["cachedContent"]; ok && v != nil {
		body["cachedContent"] = v
	}
	if v, ok := provOpts["labels"]; ok && v != nil {
		body["labels"] = v
	}
	if serviceTier != "" && !isVertex {
		body["serviceTier"] = serviceTier
	}

	warnings = append(warnings, prepared.warnings...)
	headers = internalhttp.MergeHeaders(opts.Headers, headers)
	return body, headers, warnings, nil
}

// toolNameMapping maps provider tool names (e.g. "code_execution") to the
// custom names the caller used for the provider tool (TS createToolNameMapping).
type toolNameMapping map[string]string

func newToolNameMapping(tools []types.Tool) toolNameMapping {
	mapping := toolNameMapping{}
	for _, t := range tools {
		if t.Type == "provider" && t.ProviderID == "google.code_execution" && t.Name != "" {
			mapping["code_execution"] = t.Name
		}
	}
	return mapping
}

func (m toolNameMapping) toCustomToolName(providerToolName string) string {
	if name, ok := m[providerToolName]; ok {
		return name
	}
	return providerToolName
}
