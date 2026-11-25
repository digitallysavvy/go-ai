package gemini

import (
	"regexp"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// LanguageModelCapabilities is the part of the TS Google language model
// catalog entry (google-model-catalog.ts, 1145cd58eb) that changes the
// request or the warnings: a row per listed language model ID, and one
// fallback derived from the ID for every other ID (prefixed IDs, "models/..",
// Vertex paths, case variants, future Gemini versions).
type LanguageModelCapabilities struct {
	// ProviderTools: google_search, url_context, code execution, maps and
	// the other provider-defined tools (TS tools.providerTools).
	ProviderTools bool
	// FileSearch: the file_search tool (TS tools.fileSearch).
	FileSearch bool
	// FunctionTools: the model accepts function tools at all.
	FunctionTools bool
	// Mixed: function and provider-defined tools in one request.
	Mixed bool
	// FunctionResponseParts: images and files in function responses.
	FunctionResponseParts bool
	// ThoughtSignatures: replayed function calls need a thought signature.
	ThoughtSignatures bool
	// WithResponseSchema: a JSON response schema next to function tools.
	WithResponseSchema bool
	// JSONResponseTool: the JSON response schema may be sent as a function
	// tool when it cannot be combined with the request's function tools.
	JSONResponseTool bool
	// ThinkingLevels are the thinkingLevel values the model accepts, lowest
	// first. nil: thinking is configured with a budget.
	ThinkingLevels []string
	// MinThinkingBudget and MaxThinkingBudget bound a thinkingBudget.
	MinThinkingBudget, MaxThinkingBudget int
	// PenaltiesVertexOnly: presence and frequency penalties are supported
	// on Vertex AI only (Gemini 2.5 models).
	PenaltiesVertexOnly bool
	// SystemInstructionAsUser: the system prompt goes into the first user
	// message instead of systemInstruction (Gemma).
	SystemInstructionAsUser bool
	// ExternalFileURLs: public HTTPS URLs are accepted for file inputs.
	ExternalFileURLs bool
}

var (
	gemini4XhighPattern        = regexp.MustCompile(`(?i)(^|[/.])gemini-4-(?:hydrogen|neon)(?:-|$)`)
	geminiSlashPrefixPattern   = regexp.MustCompile(`(^|/)gemini-`)
	geminiSlashPrefixPatternCI = regexp.MustCompile(`(?i)(^|/)gemini-`)
	gemini20SlashPattern       = regexp.MustCompile(`(^|/)gemini-2\.0`)
)

// LanguageModelCapabilitiesFor returns the capabilities of modelID: the row of
// a listed language model ID (exact match), else the fallback derived from the
// ID (TS getGoogleLanguageModelEntry).
func LanguageModelCapabilitiesFor(modelID string) LanguageModelCapabilities {
	if row, ok := listedLanguageModelRows[modelID]; ok {
		return row
	}
	return unlistedLanguageModelCapabilities(modelID)
}

// unlistedLanguageModelCapabilities is TS resolveUnlistedLanguageModelRow, the
// only place that derives capabilities from the model ID.
func unlistedLanguageModelCapabilities(modelID string) LanguageModelCapabilities {
	runtime := GetModelCapabilities(modelID)
	id := providerutils.JSToLower(modelID)
	// The original case-sensitive exception in the thinking resolver.
	levelFormat := runtime.UsesGemini3Features && !strings.Contains(modelID, "gemini-3-pro-image")
	// Hydrogen and Neon add xhigh and have no minimal thinking level.
	xhigh := gemini4XhighPattern.MatchString(modelID)
	minLevel := minimumThinkingLevelForGemini3Model(modelID)
	if xhigh {
		minLevel = "low"
	}

	caps := LanguageModelCapabilities{
		ProviderTools:           runtime.SupportsGemini2Tools,
		FileSearch:              runtime.SupportsGemini2Tools && runtime.SupportsFileSearch,
		FunctionTools:           true,
		Mixed:                   runtime.UsesGemini3Features,
		FunctionResponseParts:   runtime.UsesGemini3Features,
		ThoughtSignatures:       runtime.UsesGemini3Features,
		WithResponseSchema:      runtime.UsesGemini3Features,
		JSONResponseTool:        geminiSlashPrefixPatternCI.MatchString(modelID),
		MinThinkingBudget:       minThinkingTokensForGemini25Model(modelID),
		MaxThinkingBudget:       maxThinkingTokensForModel(modelID),
		PenaltiesVertexOnly:     gemini25ModelPattern.MatchString(modelID),
		SystemInstructionAsUser: strings.HasPrefix(id, "gemma-"),
		ExternalFileURLs:        geminiSlashPrefixPattern.MatchString(modelID) && !gemini20SlashPattern.MatchString(modelID),
	}
	if levelFormat {
		if minLevel == "minimal" {
			caps.ThinkingLevels = append(caps.ThinkingLevels, "minimal")
		}
		caps.ThinkingLevels = append(caps.ThinkingLevels, "low", "medium", "high")
		if xhigh {
			caps.ThinkingLevels = append(caps.ThinkingLevels, "xhigh")
		}
	}
	return caps
}

// listedLanguageModelRows are the rows of listed language model IDs that
// differ from the fallback; every other listed ID resolves to the fallback.
// TestPortprovgLanguageCatalogMatchesTS compares every TS row.
var listedLanguageModelRows = map[string]LanguageModelCapabilities{
	// gemini-nano-banana-2.1 takes no function tools (TS functionTools:
	// false), so none of the function tool combinations apply.
	"gemini-nano-banana-2.1": {
		ProviderTools:     true,
		FileSearch:        true,
		ThinkingLevels:    []string{"minimal", "low", "medium", "high"},
		MaxThinkingBudget: 24576,
		ExternalFileURLs:  true,
	},
}
