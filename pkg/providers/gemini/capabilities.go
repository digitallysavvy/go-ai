package gemini

import (
	"regexp"
	"strconv"
	"strings"
)

// ModelCapabilities classifies a Gemini model ID. Mirrors the TS SDK
// GoogleModelCapabilities type (google-model-capabilities.ts).
type ModelCapabilities struct {
	SupportsGemini2Tools bool
	SupportsFileSearch   bool
	UsesGemini3Features  bool
}

var (
	gemini1ModelPattern       = regexp.MustCompile(`(?i)(^|/)gemini-1(?:[.-]|$)`)
	gemini2ModelPattern       = regexp.MustCompile(`(?i)(^|/)gemini-2(?:[.-]|$)`)
	gemini25ModelPattern      = regexp.MustCompile(`(?i)(^|/)gemini-2\.5(?:[.-]|$)`)
	geminiModelPattern        = regexp.MustCompile(`(?i)(^|/)gemini-`)
	geminiProLegacyPattern    = regexp.MustCompile(`(?i)(^|/)gemini-pro(?:-vision)?$`)
	geminiRoboticsER15Pattern = regexp.MustCompile(`(?i)(^|/)gemini-robotics-er-1\.5(?:[.-]|$)`)
	// Go regexp has no negative lookahead; the "-lite" exclusion is handled in code.
	flashVersionPattern = regexp.MustCompile(`^gemini-(\d+)\.(\d+)-flash($|-.*)`)
)

func isKnownPreGemini2Model(modelID string) bool {
	return gemini1ModelPattern.MatchString(modelID) ||
		geminiProLegacyPattern.MatchString(modelID) ||
		geminiRoboticsER15Pattern.MatchString(modelID)
}

// GetModelCapabilities classifies Gemini capabilities by excluding known older
// generations. Google model IDs are open-ended, so unrecognized Gemini IDs and
// aliases intentionally inherit the newest supported behavior.
func GetModelCapabilities(modelID string) ModelCapabilities {
	isGemini := geminiModelPattern.MatchString(modelID)
	isGemini2 := gemini2ModelPattern.MatchString(modelID)
	isKnownPreGemini2 := isKnownPreGemini2Model(modelID)
	isKnownOlder := isKnownPreGemini2 || isGemini2
	usesGemini3Features := isGemini && !isKnownOlder

	return ModelCapabilities{
		SupportsGemini2Tools: (isGemini && !isKnownPreGemini2) ||
			strings.Contains(strings.ToLower(modelID), "nano-banana"),
		SupportsFileSearch:  gemini25ModelPattern.MatchString(modelID) || usesGemini3Features,
		UsesGemini3Features: usesGemini3Features,
	}
}

// isGemini25Model reports whether the model is a Gemini 2.5 model
// (TS gemini25ModelPattern).
func isGemini25Model(modelID string) bool {
	return gemini25ModelPattern.MatchString(modelID)
}

// minimumThinkingLevelForGemini3Model returns the lowest thinkingLevel a
// Gemini 3+ model accepts. Flash models from 3.7 on (and gemini-flash-latest)
// no longer accept "minimal". Mirrors TS getMinimumThinkingLevelForGemini3Model.
func minimumThinkingLevelForGemini3Model(modelID string) string {
	segments := strings.Split(modelID, "/")
	modelName := strings.ToLower(segments[len(segments)-1])

	if modelName == "gemini-flash-latest" {
		return "low"
	}

	match := flashVersionPattern.FindStringSubmatch(modelName)
	if match == nil {
		return "minimal"
	}
	// TS: -flash(?:$|-(?!lite(?:-|$))) — reject "-flash-lite" and "-flash-lite-*".
	if rest := match[3]; rest == "-lite" || strings.HasPrefix(rest, "-lite-") {
		return "minimal"
	}

	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	if major > 3 || (major == 3 && minor >= 7) {
		return "low"
	}
	return "minimal"
}
