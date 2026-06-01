package gemini

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// --- convertResponse ---------------------------------------------------------

func TestConvertResponse_SkipsThoughtParts(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro")

	resp := Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{
				{Text: "I am thinking...", Thought: true},
				{Text: "The answer is 42."},
			}},
			FinishReason: "STOP",
		}},
	}

	result := m.convertResponse(resp)
	if result.Text != "The answer is 42." {
		t.Errorf("Text: got %q, want %q", result.Text, "The answer is 42.")
	}
}

func TestConvertResponse_AllThoughtPartsProducesEmptyText(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro")

	resp := Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{
				{Text: "Thinking step 1", Thought: true},
				{Text: "Thinking step 2", Thought: true},
			}},
			FinishReason: "STOP",
		}},
	}

	result := m.convertResponse(resp)
	if result.Text != "" {
		t.Errorf("Text: got %q, want empty (all thought parts)", result.Text)
	}
}

func TestConvertResponse_ThoughtPartDoesNotBlockFunctionCall(t *testing.T) {
	m := makeTestModel("gemini-2.0-flash")

	resp := Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{
				{Text: "thinking", Thought: true},
				{FunctionCall: &struct {
					ID   string                 `json:"id,omitempty"`
					Name string                 `json:"name"`
					Args map[string]interface{} `json:"args"`
				}{Name: "get_weather", Args: map[string]interface{}{"city": "SF"}}},
			}},
			FinishReason: "STOP",
		}},
	}

	result := m.convertResponse(resp)
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].ToolName != "get_weather" {
		t.Errorf("ToolName: got %q, want 'get_weather'", result.ToolCalls[0].ToolName)
	}
}

func TestConvertResponse_MetadataKeyUsed(t *testing.T) {
	mGoogle := makeTestModel("gemini-2.5-pro")
	mVertex := makeVertexTestModel("gemini-2.5-pro")

	resp := Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{{Text: "hi"}}},
			FinishReason:  "STOP",
			SafetyRatings: []byte(`[{"category":"HARM_CATEGORY_HATE_SPEECH"}]`),
		}},
	}

	googleResult := mGoogle.convertResponse(resp)
	if _, ok := googleResult.ProviderMetadata["google"]; !ok {
		t.Errorf("google result: expected 'google' key in ProviderMetadata")
	}

	vertexResult := mVertex.convertResponse(resp)
	if _, ok := vertexResult.ProviderMetadata["vertex"]; !ok {
		t.Errorf("vertex result: expected 'vertex' key in ProviderMetadata")
	}
}

// --- buildRequestBody -------------------------------------------------------

func TestBuildRequestBody_EmptyOptsDoesNotPanic(t *testing.T) {
	m := makeTestModel("gemini-2.0-flash")
	body := m.buildRequestBody(&provider.GenerateOptions{}, false)
	if body == nil {
		t.Fatal("body should not be nil")
	}
}

func TestBuildRequestBody_GemmaModelSkipsSystemInstruction(t *testing.T) {
	m := makeTestModel("gemma-7b")
	body := m.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{System: "You are helpful."},
	}, false)
	if _, has := body["systemInstruction"]; has {
		t.Error("Gemma model should not have systemInstruction")
	}
}

func TestBuildRequestBody_NonGemmaModelIncludesSystemInstruction(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro")
	body := m.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{System: "You are helpful."},
	}, false)
	if _, has := body["systemInstruction"]; !has {
		t.Error("non-Gemma model should have systemInstruction")
	}
}

func TestBuildRequestBody_StrictToolsUsesValidatedMode(t *testing.T) {
	m := makeTestModel("gemini-2.0-flash")
	body := m.buildRequestBody(&provider.GenerateOptions{
		Tools: []types.Tool{{Name: "search", Strict: true, Parameters: map[string]interface{}{"type": "object"}}},
	}, false)
	toolConfig, ok := body["toolConfig"].(map[string]interface{})
	if !ok {
		t.Fatal("toolConfig must be present when any tool has Strict:true")
	}
	fcc := toolConfig["functionCallingConfig"].(map[string]interface{})
	if fcc["mode"] != "VALIDATED" {
		t.Errorf("mode = %v, want VALIDATED", fcc["mode"])
	}
}

func TestBuildRequestBody_NoStrictToolsOmitsToolConfig(t *testing.T) {
	m := makeTestModel("gemini-2.0-flash")
	body := m.buildRequestBody(&provider.GenerateOptions{
		Tools: []types.Tool{{Name: "search", Strict: false, Parameters: map[string]interface{}{"type": "object"}}},
	}, false)
	if _, ok := body["toolConfig"]; ok {
		t.Error("toolConfig must NOT be present when no tool has Strict:true")
	}
}

func TestBuildRequestBody_NoArgToolUsesEmptyObjectSchema(t *testing.T) {
	m := makeTestModel("gemini-3-pro-preview")
	body := m.buildRequestBody(&provider.GenerateOptions{
		Tools: []types.Tool{{Name: "ping", Description: "No args"}},
	}, false)
	tools, ok := body["tools"].([]map[string]interface{})
	if !ok || len(tools) == 0 {
		t.Fatalf("tools missing: %#v", body["tools"])
	}
	functionDecls, ok := tools[0]["functionDeclarations"].([]map[string]interface{})
	if !ok || len(functionDecls) == 0 {
		t.Fatalf("functionDeclarations missing: %#v", tools[0]["functionDeclarations"])
	}
	params, ok := functionDecls[0]["parameters"].(map[string]interface{})
	if !ok {
		t.Fatalf("parameters type = %T", functionDecls[0]["parameters"])
	}
	if params["type"] != "object" {
		t.Errorf("parameters.type = %v, want object", params["type"])
	}
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("parameters.properties type = %T", params["properties"])
	}
	if len(props) != 0 {
		t.Errorf("parameters.properties = %#v, want empty map", props)
	}
}

func TestBuildRequestBody_Gemini3SupportsMixedNativeAndFunctionTools(t *testing.T) {
	m := makeTestModel("gemini-3-pro-preview")
	body := m.buildRequestBody(&provider.GenerateOptions{
		Tools: []types.Tool{
			{Name: "f1", Description: "function", Parameters: map[string]interface{}{"type": "object"}},
			{Type: "provider", ProviderID: "google.google_search"},
		},
	}, false)

	tools, ok := body["tools"].([]map[string]interface{})
	if !ok {
		t.Fatalf("tools type = %T", body["tools"])
	}
	if len(tools) != 2 {
		t.Fatalf("tools len = %d, want 2", len(tools))
	}
	if _, ok := tools[0]["googleSearch"]; !ok {
		t.Fatalf("tools[0] should be native googleSearch entry: %#v", tools[0])
	}
	if _, ok := tools[1]["functionDeclarations"]; !ok {
		t.Fatalf("tools[1] should contain functionDeclarations: %#v", tools[1])
	}
}

func TestBuildRequestBody_VertexStreamFunctionCallArgumentsOnlyForStream(t *testing.T) {
	m := makeVertexTestModel("gemini-3-pro-preview")
	opts := &provider.GenerateOptions{
		Tools: []types.Tool{{Name: "f1", Description: "function", Parameters: map[string]interface{}{"type": "object"}}},
		ProviderOptions: map[string]interface{}{
			"vertex": map[string]interface{}{"streamFunctionCallArguments": true},
		},
	}

	bodyNonStream := m.buildRequestBody(opts, false)
	tcNonStream, _ := bodyNonStream["toolConfig"].(map[string]interface{})
	fccNonStream, _ := tcNonStream["functionCallingConfig"].(map[string]interface{})
	if _, ok := fccNonStream["streamFunctionCallArguments"]; ok {
		t.Fatal("streamFunctionCallArguments must be omitted for non-streaming requests")
	}

	bodyStream := m.buildRequestBody(opts, true)
	tcStream, _ := bodyStream["toolConfig"].(map[string]interface{})
	fccStream, _ := tcStream["functionCallingConfig"].(map[string]interface{})
	if got, ok := fccStream["streamFunctionCallArguments"].(bool); !ok || !got {
		t.Fatalf("streamFunctionCallArguments = %v (%T), want true", fccStream["streamFunctionCallArguments"], fccStream["streamFunctionCallArguments"])
	}
}

// --- supportsFunctionResponseParts ------------------------------------------

func TestSupportsFunctionResponseParts_Gemini3(t *testing.T) {
	if !makeTestModel("gemini-3-pro-preview").supportsFunctionResponseParts() {
		t.Error("gemini-3-pro-preview should support function response parts")
	}
}

func TestSupportsFunctionResponseParts_Gemini2(t *testing.T) {
	if makeTestModel("gemini-2.0-flash").supportsFunctionResponseParts() {
		t.Error("gemini-2.0-flash should NOT support function response parts")
	}
}

// --- buildRequestBody: image tool results ------------------------------------

func TestBuildRequestBody_Gemini3ImageToolResultMapsToFunctionResponse(t *testing.T) {
	m := makeTestModel("gemini-3-pro-preview")

	imageBytes := []byte{0x89, 0x50, 0x4e, 0x47} // PNG magic bytes
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{
					types.TextContent{Text: "Describe this image"},
				}},
				{Role: types.RoleAssistant, ToolCalls: []types.ToolCall{
					{ID: "call1", ToolName: "getImage", Arguments: map[string]interface{}{}},
				}},
				{Role: types.RoleTool, Content: []types.ContentPart{
					types.ToolResultContent{
						ToolCallID: "call1",
						ToolName:   "getImage",
						Output: &types.ToolResultOutput{
							Type: types.ToolResultOutputContent,
							Content: []types.ToolResultContentBlock{
								types.TextContentBlock{Text: "Here is the image"},
								types.ImageContentBlock{Data: imageBytes, MediaType: "image/png"},
							},
						},
					},
				}},
			},
		},
	}

	body := m.buildRequestBody(opts, false)
	contents, ok := body["contents"].([]map[string]interface{})
	if !ok {
		t.Fatalf("contents type = %T", body["contents"])
	}

	// Find the tool-result message (role "user") that has a functionResponse.
	var toolMsg map[string]interface{}
	for _, c := range contents {
		if c["role"] == "user" {
			parts, _ := c["parts"].([]map[string]interface{})
			for _, pt := range parts {
				if _, hasFR := pt["functionResponse"]; hasFR {
					toolMsg = c
				}
			}
		}
	}
	if toolMsg == nil {
		t.Fatal("no tool-result message (functionResponse) found in contents")
	}

	parts := toolMsg["parts"].([]map[string]interface{})
	fr := parts[0]["functionResponse"].(map[string]interface{})

	// Gemini 3: binary parts go into functionResponse.parts[].
	frParts, ok := fr["parts"].([]map[string]interface{})
	if !ok || len(frParts) == 0 {
		t.Fatalf("functionResponse.parts missing or empty; got %v", fr["parts"])
	}
	inlineData, ok := frParts[0]["inlineData"].(map[string]interface{})
	if !ok {
		t.Fatalf("functionResponse.parts[0].inlineData missing; got %v", frParts[0])
	}
	if inlineData["mimeType"] != "image/png" {
		t.Errorf("mimeType = %v, want image/png", inlineData["mimeType"])
	}

	// Text goes into response.content.
	resp := fr["response"].(map[string]interface{})
	if resp["content"] != "Here is the image" {
		t.Errorf("response.content = %v, want %q", resp["content"], "Here is the image")
	}
}

func TestBuildRequestBody_Gemini2ImageToolResultFallsBackToText(t *testing.T) {
	m := makeTestModel("gemini-2.0-flash")

	imageBytes := []byte{0x89, 0x50, 0x4e, 0x47}
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{
					types.TextContent{Text: "show me"},
				}},
				{Role: types.RoleTool, Content: []types.ContentPart{
					types.ToolResultContent{
						ToolCallID: "c1",
						ToolName:   "getImage",
						Output: &types.ToolResultOutput{
							Type: types.ToolResultOutputContent,
							Content: []types.ToolResultContentBlock{
								types.ImageContentBlock{Data: imageBytes, MediaType: "image/png"},
							},
						},
					},
				}},
			},
		},
	}

	body := m.buildRequestBody(opts, false)
	contents := body["contents"].([]map[string]interface{})

	// Find the tool-result user message that has a top-level inlineData part.
	var toolMsg map[string]interface{}
	for _, c := range contents {
		if c["role"] == "user" {
			parts, _ := c["parts"].([]map[string]interface{})
			for _, pt := range parts {
				if _, hasID := pt["inlineData"]; hasID {
					toolMsg = c
				}
			}
		}
	}
	if toolMsg == nil {
		t.Fatal("no legacy inlineData part found in tool result message")
	}

	parts := toolMsg["parts"].([]map[string]interface{})
	var hasInlineData bool
	for _, pt := range parts {
		if _, ok := pt["inlineData"]; ok {
			hasInlineData = true
		}
		// Ensure functionResponse does NOT have a parts[] field.
		if fr, ok := pt["functionResponse"].(map[string]interface{}); ok {
			if _, hasParts := fr["parts"]; hasParts {
				t.Error("Gemini 2 must NOT use functionResponse.parts[] — legacy format only")
			}
		}
	}
	if !hasInlineData {
		t.Error("Gemini 2 legacy format must emit a top-level inlineData part for images")
	}
}

// --- convertResponse: detailed content types ---------------------------------

func TestConvertResponse_ThoughtSignatureOnFunctionCall(t *testing.T) {
	m := makeTestModel("gemini-2.0-flash")

	resp := Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{
				{
					FunctionCall: &struct {
						ID   string                 `json:"id,omitempty"`
						Name string                 `json:"name"`
						Args map[string]interface{} `json:"args"`
					}{Name: "search", Args: map[string]interface{}{"q": "test"}},
					ThoughtSignature: "sig-abc-123",
				},
			}},
			FinishReason: "STOP",
		}},
	}

	result := m.convertResponse(resp)

	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].ThoughtSignature != "sig-abc-123" {
		t.Errorf("ThoughtSignature = %q, want %q", result.ToolCalls[0].ThoughtSignature, "sig-abc-123")
	}
}

func TestConvertResponse_ThoughtPartsBecomesReasoningContent(t *testing.T) {
	m := makeTestModel("gemini-2.0-flash")

	resp := Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{
				{Text: "thinking...", Thought: true, ThoughtSignature: "sealed-token"},
				{Text: "Here is the answer."},
			}},
			FinishReason: "STOP",
		}},
	}

	result := m.convertResponse(resp)

	if result.Text != "Here is the answer." {
		t.Errorf("Text = %q, want %q", result.Text, "Here is the answer.")
	}

	var found *types.ReasoningContent
	for _, part := range result.Content {
		if rc, ok := part.(types.ReasoningContent); ok {
			found = &rc
			break
		}
	}
	if found == nil {
		t.Fatal("expected a ReasoningContent part in result.Content")
	}
	if found.Text != "thinking..." {
		t.Errorf("ReasoningContent.Text = %q, want %q", found.Text, "thinking...")
	}
	if found.Signature != "sealed-token" {
		t.Errorf("ReasoningContent.Signature = %q, want %q", found.Signature, "sealed-token")
	}
}

func TestConvertResponse_ReasoningFilesMarkedCorrectly(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro")

	// A part with thought=true and inlineData should be typed as ReasoningFileContent.
	resp := Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{
				{
					Thought: true,
					InlineData: &struct {
						MimeType string `json:"mimeType"`
						Data     string `json:"data"`
					}{
						MimeType: "image/png",
						Data:     "iVBORw0KGgo=", // minimal base64
					},
				},
				{Text: "The answer."},
			}},
			FinishReason: "STOP",
		}},
	}

	result := m.convertResponse(resp)

	if result.Text != "The answer." {
		t.Errorf("Text = %q, want %q", result.Text, "The answer.")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected at least one Content part for the reasoning file")
	}
	rfContent, ok := result.Content[0].(types.ReasoningFileContent)
	if !ok {
		t.Fatalf("Content[0] type = %T, want types.ReasoningFileContent", result.Content[0])
	}
	if rfContent.MediaType != "image/png" {
		t.Errorf("ReasoningFileContent.MediaType = %q, want %q", rfContent.MediaType, "image/png")
	}
}

func TestConvertResponse_GroundingMetadataInProviderMetadata(t *testing.T) {
	m := makeTestModel("gemini-2.0-flash")

	groundingJSON := json.RawMessage(`{"webSearchQueries":["golang generics"]}`)
	resp := Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{{Text: "Here is the info."}}},
			FinishReason:      "STOP",
			GroundingMetadata: groundingJSON,
		}},
	}

	result := m.convertResponse(resp)

	if result.ProviderMetadata == nil {
		t.Fatal("ProviderMetadata must be set when groundingMetadata is present")
	}
	googleMeta, ok := result.ProviderMetadata["google"].(map[string]json.RawMessage)
	if !ok {
		t.Fatalf("ProviderMetadata[google] type = %T", result.ProviderMetadata["google"])
	}
	if string(googleMeta["groundingMetadata"]) != string(groundingJSON) {
		t.Errorf("groundingMetadata = %s, want %s", googleMeta["groundingMetadata"], groundingJSON)
	}
}

// --- serviceTier tests ---

func TestBuildRequestBody_ServiceTier(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro")
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Hello"},
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"serviceTier": "SERVICE_TIER_FLEX",
			},
		},
	}
	body := m.buildRequestBody(opts, false)
	if body["serviceTier"] != "SERVICE_TIER_FLEX" {
		t.Errorf("serviceTier = %v, want %q", body["serviceTier"], "SERVICE_TIER_FLEX")
	}
}

func TestBuildRequestBody_ServiceTierAbsentWhenNotSet(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro")
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Hello"},
	}
	body := m.buildRequestBody(opts, false)
	if _, ok := body["serviceTier"]; ok {
		t.Error("serviceTier should not be present when not provided")
	}
}

func TestBuildRequest_VertexPayGoHeaders(t *testing.T) {
	m := makeVertexTestModel("gemini-2.5-pro")
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Hello"},
		ProviderOptions: map[string]interface{}{
			"vertex": map[string]interface{}{
				"sharedRequestType": "priority",
				"requestType":       "shared",
				"serviceTier":       "SERVICE_TIER_FLEX",
			},
		},
	}
	body, headers, warnings := m.buildRequest(opts, false)
	if _, ok := body["serviceTier"]; ok {
		t.Fatal("Vertex request body must not include serviceTier")
	}
	if headers["X-Vertex-AI-LLM-Shared-Request-Type"] != "priority" {
		t.Fatalf("shared request header = %q", headers["X-Vertex-AI-LLM-Shared-Request-Type"])
	}
	if headers["X-Vertex-AI-LLM-Request-Type"] != "shared" {
		t.Fatalf("request type header = %q", headers["X-Vertex-AI-LLM-Request-Type"])
	}
	if len(warnings) == 0 {
		t.Fatal("expected warning for Vertex serviceTier")
	}
}

func TestConvertResponse_ServiceTierInMetadata(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro")
	resp := Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{{Text: "Hello"}}},
			FinishReason: "STOP",
		}},
		UsageMetadata: &UsageMetadata{ServiceTier: "SERVICE_TIER_PRIORITY"},
	}
	result := m.convertResponse(resp)
	if result.ProviderMetadata == nil {
		t.Fatal("ProviderMetadata is nil")
	}
	googleMeta, ok := result.ProviderMetadata["google"].(map[string]json.RawMessage)
	if !ok {
		t.Fatalf("ProviderMetadata[google] type = %T", result.ProviderMetadata["google"])
	}
	var serviceTier string
	if err := json.Unmarshal(googleMeta["serviceTier"], &serviceTier); err != nil {
		t.Fatalf("unmarshal serviceTier: %v", err)
	}
	if serviceTier != "SERVICE_TIER_PRIORITY" {
		t.Errorf("serviceTier = %q, want %q", serviceTier, "SERVICE_TIER_PRIORITY")
	}
}

func TestConvertResponse_ServiceTierAbsentInMetadataWhenNotSet(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro")
	resp := Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{{Text: "Hello"}}},
			FinishReason: "STOP",
		}},
	}
	result := m.convertResponse(resp)
	// serviceTier is always emitted (as null) per TS SDK parity.
	googleMeta, ok := result.ProviderMetadata["google"].(map[string]json.RawMessage)
	if !ok {
		t.Fatal("expected google providerMetadata")
	}
	raw, has := googleMeta["serviceTier"]
	if !has {
		t.Fatal("serviceTier should always be present in metadata (as null when absent from response)")
	}
	if string(raw) != "null" {
		t.Errorf("serviceTier = %s, want null", raw)
	}
}

func TestBuildRequest_Gemini3InjectsThoughtSignatureSentinel(t *testing.T) {
	model := makeTestModel("gemini-3-pro-preview")
	body, _, warnings := model.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleAssistant,
				ToolCalls: []types.ToolCall{{
					ID:        "tc_1",
					ToolName:  "weather",
					Arguments: map[string]interface{}{"location": "SF"},
				}},
			},
		}},
	}, false)

	contents := body["contents"].([]map[string]interface{})
	parts := contents[0]["parts"].([]map[string]interface{})
	if parts[0]["thoughtSignature"] != skipThoughtSignatureValidator {
		t.Fatalf("thoughtSignature = %#v", parts[0]["thoughtSignature"])
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Details, "skip_thought_signature_validator") {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestConvertResponse_ModalityTokenCountsInMetadata(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro")
	resp := Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{{Text: "Hello"}}},
			FinishReason: "STOP",
		}},
		UsageMetadata: &UsageMetadata{
			PromptTokenCount:     14,
			CandidatesTokenCount: 9,
			PromptTokensDetails: []struct {
				Modality   string `json:"modality,omitempty"`
				TokenCount int    `json:"tokenCount,omitempty"`
			}{
				{Modality: "TEXT", TokenCount: 5},
				{Modality: "IMAGE", TokenCount: 7},
				{Modality: "AUDIO", TokenCount: 2},
			},
			CandidatesTokensDetails: []struct {
				Modality   string `json:"modality,omitempty"`
				TokenCount int    `json:"tokenCount,omitempty"`
			}{
				{Modality: "TEXT", TokenCount: 6},
				{Modality: "VIDEO", TokenCount: 3},
			},
		},
	}

	result := m.convertResponse(resp)
	googleMeta, ok := result.ProviderMetadata["google"].(map[string]json.RawMessage)
	if !ok {
		t.Fatal("expected google providerMetadata")
	}
	var counts ModalityTokenCounts
	if err := json.Unmarshal(googleMeta["modalityTokenCounts"], &counts); err != nil {
		t.Fatalf("unmarshal modalityTokenCounts: %v", err)
	}
	if counts.TextTokens != 11 || counts.ImageTokens != 7 || counts.AudioTokens != 2 || counts.VideoTokens != 3 {
		t.Errorf("modalityTokenCounts = %+v", counts)
	}
}

func TestConvertResponse_NoArgsToolCallPreservesThoughtSignatureMetadata(t *testing.T) {
	m := makeTestModel("gemini-3-pro-preview")
	resp := Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{{
				FunctionCall: &struct {
					ID   string                 `json:"id,omitempty"`
					Name string                 `json:"name"`
					Args map[string]interface{} `json:"args"`
				}{Name: "read_screen"},
				ThoughtSignature: "sig-no-args",
			}}},
			FinishReason: "STOP",
		}},
	}

	result := m.convertResponse(resp)
	if len(result.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(result.ToolCalls))
	}
	call := result.ToolCalls[0]
	if len(call.Arguments) != 0 {
		t.Fatalf("Arguments = %#v, want empty map", call.Arguments)
	}
	if call.ThoughtSignature != "sig-no-args" {
		t.Fatalf("ThoughtSignature = %q, want sig-no-args", call.ThoughtSignature)
	}
	googleMeta, ok := call.ProviderMetadata["google"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing google provider metadata: %#v", call.ProviderMetadata)
	}
	if googleMeta["thoughtSignature"] != "sig-no-args" {
		t.Fatalf("thoughtSignature metadata = %v", googleMeta["thoughtSignature"])
	}
}
