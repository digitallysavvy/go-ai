package gemini

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// GoogleSearchTool creates a Google web search grounding tool.
// Requires Gemini 2.0 or newer.
func GoogleSearchTool(args ...map[string]interface{}) types.Tool {
	var providerArgs map[string]interface{}
	if len(args) > 0 {
		providerArgs = args[0]
	}
	return types.Tool{
		Type:         "provider",
		ProviderID:   "google.google_search",
		ProviderArgs: providerArgs,
	}
}

// EnterpriseWebSearchTool creates an enterprise web search grounding tool for
// highly-regulated industries. Requires Gemini 2.0 or newer.
func EnterpriseWebSearchTool() types.Tool {
	return types.Tool{
		Type:       "provider",
		ProviderID: "google.enterprise_web_search",
	}
}

// GoogleMapsTool creates a Google Maps grounding tool.
// Requires Gemini 2.0 or newer.
func GoogleMapsTool() types.Tool {
	return types.Tool{
		Type:       "provider",
		ProviderID: "google.google_maps",
	}
}

// UrlContextTool creates a URL context fetching tool.
// Requires Gemini 2.0 or newer.
func UrlContextTool() types.Tool {
	return types.Tool{
		Type:       "provider",
		ProviderID: "google.url_context",
	}
}

// FileSearchTool creates a Gemini file search (RAG) tool.
// Requires Gemini 2.5 or newer.
func FileSearchTool(args ...map[string]interface{}) types.Tool {
	var providerArgs map[string]interface{}
	if len(args) > 0 {
		providerArgs = args[0]
	}
	return types.Tool{
		Type:         "provider",
		ProviderID:   "google.file_search",
		ProviderArgs: providerArgs,
	}
}

// CodeExecutionTool creates a code execution tool.
// Requires Gemini 2.0 or newer.
func CodeExecutionTool() types.Tool {
	return types.Tool{
		Type:       "provider",
		ProviderID: "google.code_execution",
		// TS codeExecutionInputSchema.
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"language": map[string]interface{}{"type": "string", "description": "The programming language of the code."},
				"code":     map[string]interface{}{"type": "string", "description": "The code to be executed."},
			},
			"required": []interface{}{"language", "code"},
		},
	}
}

// VertexRagStoreTool creates a Vertex RAG store retrieval tool.
// ragCorpus is the fully-qualified RAG corpus resource name.
// Requires Gemini 2.0 or newer.
func VertexRagStoreTool(ragCorpus string, topK ...int) types.Tool {
	args := map[string]interface{}{"ragCorpus": ragCorpus}
	if len(topK) > 0 {
		args["topK"] = topK[0]
	}
	return types.Tool{
		Type:         "provider",
		ProviderID:   "google.vertex_rag_store",
		ProviderArgs: args,
	}
}

// buildNativeToolEntry converts a provider Tool to the Gemini API native tool
// entry format. Returns nil for unrecognized ProviderIDs.
func buildNativeToolEntry(t types.Tool) map[string]interface{} {
	args := t.ProviderArgs
	switch t.ProviderID {
	case "google.google_search":
		if args != nil {
			return map[string]interface{}{"googleSearch": args}
		}
		return map[string]interface{}{"googleSearch": map[string]interface{}{}}
	case "google.enterprise_web_search":
		return map[string]interface{}{"enterpriseWebSearch": map[string]interface{}{}}
	case "google.google_maps":
		return map[string]interface{}{"googleMaps": map[string]interface{}{}}
	case "google.url_context":
		return map[string]interface{}{"urlContext": map[string]interface{}{}}
	case "google.file_search":
		if args != nil {
			return map[string]interface{}{"fileSearch": args}
		}
		return map[string]interface{}{"fileSearch": map[string]interface{}{}}
	case "google.code_execution":
		return map[string]interface{}{"codeExecution": map[string]interface{}{}}
	case "google.vertex_rag_store":
		// TS: rag_corpus: tool.args.ragCorpus, left out of the JSON when undefined.
		ragResources := map[string]interface{}{}
		if ragCorpus, ok := args["ragCorpus"]; ok && ragCorpus != nil {
			ragResources["rag_corpus"] = ragCorpus
		}
		ragStoreArgs := map[string]interface{}{"rag_resources": ragResources}
		if topK, ok := args["topK"]; ok && topK != nil {
			ragStoreArgs["similarity_top_k"] = topK
		}
		return map[string]interface{}{
			"retrieval": map[string]interface{}{
				"vertex_rag_store": ragStoreArgs,
			},
		}
	default:
		return nil
	}
}
