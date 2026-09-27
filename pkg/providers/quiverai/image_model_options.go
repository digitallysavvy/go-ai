package quiverai

import (
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// QuiverAIViewBox mirrors the TS SDK's attributes.viewBox object.
type QuiverAIViewBox struct {
	MinX   float64
	MinY   float64
	Width  float64
	Height float64
}

// QuiverAIAttributes mirrors the TS SDK's providerOptions.quiverai.attributes
// object (SVG root attributes requested for generation or vectorization).
type QuiverAIAttributes struct {
	ViewBox *QuiverAIViewBox
}

// QuiverAIReferenceImageOption mirrors one entry of
// providerOptions.quiverai.referenceImages: either {url} or {base64}.
type QuiverAIReferenceImageOption struct {
	URL    string
	Base64 string
}

// QuiverAIImageModelOptions mirrors the TS SDK's
// quiveraiImageModelOptionsSchema (quiverai-image-model-options.ts).
type QuiverAIImageModelOptions struct {
	// Operation to perform. One of: "generate" (default), "vectorize",
	// "animate", "edit".
	Operation string

	// Instructions holds extra style guidance for prompt-based generation.
	Instructions string

	// ReasoningEffort applied to generation, vectorization, editing, or
	// animation. One of: "low", "medium", "high", "xhigh".
	ReasoningEffort string

	// ReferenceImages holds optional reference images for SVG editing
	// (at most 4).
	ReferenceImages []QuiverAIReferenceImageOption

	// MaxReviewSteps is the maximum number of edit review/redo steps (0-5).
	MaxReviewSteps *int

	// Attributes holds SVG root attributes requested for generation or
	// vectorization.
	Attributes *QuiverAIAttributes

	// Temperature is the sampling temperature (0-2).
	Temperature *float64

	// TopP is nucleus sampling top-p (0-1).
	TopP *float64

	// PresencePenalty is the presence penalty (-2 to 2).
	PresencePenalty *float64

	// MaxOutputTokens is the maximum number of output tokens
	// (1-131072; Arrow 2 models are further limited to 65536, enforced
	// separately in buildRequestBody).
	MaxOutputTokens *int

	// OrchestratorMaxOutputTokens is the provider orchestrator token budget
	// for SVG editing (1-65536).
	OrchestratorMaxOutputTokens *int

	// ShallowMaxOutputTokens is the provider shallow-edit token budget for
	// SVG editing (1-65536).
	ShallowMaxOutputTokens *int

	// AutoCrop controls whether to auto-crop the input image before
	// vectorization. Only used when operation is "vectorize".
	AutoCrop *bool

	// TargetSize is the target canvas size in pixels for vectorization
	// (128-4096). Only used when operation is "vectorize".
	TargetSize *int
}

var quiverOperations = map[string]bool{"generate": true, "vectorize": true, "animate": true, "edit": true}
var quiverReasoningEfforts = map[string]bool{"low": true, "medium": true, "high": true, "xhigh": true}

func invalidQuiverArgument(argument, message string) error {
	return &providererrors.InvalidArgumentError{Field: argument, Message: message}
}

// parseQuiverAIImageModelOptions parses+validates
// providerOptions.quiverai into a typed struct, mirroring TS
// parseProviderOptions with quiveraiImageModelOptionsSchema.
func parseQuiverAIImageModelOptions(raw map[string]interface{}) (*QuiverAIImageModelOptions, error) {
	if raw == nil {
		return &QuiverAIImageModelOptions{}, nil
	}
	opts := &QuiverAIImageModelOptions{}

	if v, ok := raw["operation"]; ok && v != nil {
		s, ok := v.(string)
		if !ok || !quiverOperations[s] {
			return nil, invalidQuiverArgument("providerOptions.quiverai.operation", fmt.Sprintf("invalid operation %v", v))
		}
		opts.Operation = s
	}

	if v, ok := raw["instructions"]; ok && v != nil {
		s, ok := v.(string)
		if !ok || s == "" {
			return nil, invalidQuiverArgument("providerOptions.quiverai.instructions", "instructions must be a non-empty string")
		}
		opts.Instructions = s
	}

	if v, ok := raw["reasoningEffort"]; ok && v != nil {
		s, ok := v.(string)
		if !ok || !quiverReasoningEfforts[s] {
			return nil, invalidQuiverArgument("providerOptions.quiverai.reasoningEffort", fmt.Sprintf("invalid reasoningEffort %v", v))
		}
		opts.ReasoningEffort = s
	}

	if v, ok := raw["referenceImages"]; ok && v != nil {
		arr, ok := v.([]interface{})
		if !ok {
			return nil, invalidQuiverArgument("providerOptions.quiverai.referenceImages", "must be an array")
		}
		if len(arr) > 4 {
			return nil, invalidQuiverArgument("providerOptions.quiverai.referenceImages", "must contain at most 4 entries")
		}
		refs := make([]QuiverAIReferenceImageOption, 0, len(arr))
		for i, item := range arr {
			m, ok := item.(map[string]interface{})
			if !ok {
				return nil, invalidQuiverArgument(fmt.Sprintf("providerOptions.quiverai.referenceImages[%d]", i), "must be an object")
			}
			urlVal, hasURL := m["url"].(string)
			b64Val, hasB64 := m["base64"].(string)
			switch {
			case hasURL && !hasB64:
				if urlVal == "" {
					return nil, invalidQuiverArgument(fmt.Sprintf("providerOptions.quiverai.referenceImages[%d]", i), "url must be non-empty")
				}
				refs = append(refs, QuiverAIReferenceImageOption{URL: urlVal})
			case hasB64 && !hasURL:
				if b64Val == "" || len(b64Val) > 16_777_216 {
					return nil, invalidQuiverArgument(fmt.Sprintf("providerOptions.quiverai.referenceImages[%d]", i), "base64 must contain 1-16777216 characters")
				}
				refs = append(refs, QuiverAIReferenceImageOption{Base64: b64Val})
			default:
				return nil, invalidQuiverArgument(fmt.Sprintf("providerOptions.quiverai.referenceImages[%d]", i), "must specify exactly one of url or base64")
			}
		}
		opts.ReferenceImages = refs
	}

	if v, ok := raw["maxReviewSteps"]; ok && v != nil {
		n, ok := quiverInt(v)
		if !ok || n < 0 || n > 5 {
			return nil, invalidQuiverArgument("providerOptions.quiverai.maxReviewSteps", "must be an integer between 0 and 5")
		}
		opts.MaxReviewSteps = &n
	}

	if v, ok := raw["attributes"]; ok && v != nil {
		m, ok := v.(map[string]interface{})
		if !ok {
			return nil, invalidQuiverArgument("providerOptions.quiverai.attributes", "must be an object")
		}
		attrs := &QuiverAIAttributes{}
		if vb, ok := m["viewBox"]; ok && vb != nil {
			vbMap, ok := vb.(map[string]interface{})
			if !ok {
				return nil, invalidQuiverArgument("providerOptions.quiverai.attributes.viewBox", "must be an object")
			}
			minX, okX := quiverFloat(vbMap["minX"])
			minY, okY := quiverFloat(vbMap["minY"])
			width, okW := quiverFloat(vbMap["width"])
			height, okH := quiverFloat(vbMap["height"])
			if !okX || !okY || !okW || !okH || width <= 0 || height <= 0 {
				return nil, invalidQuiverArgument("providerOptions.quiverai.attributes.viewBox", "must specify numeric minX, minY, and positive width/height")
			}
			attrs.ViewBox = &QuiverAIViewBox{MinX: minX, MinY: minY, Width: width, Height: height}
		}
		opts.Attributes = attrs
	}

	if v, ok := raw["temperature"]; ok && v != nil {
		f, ok := quiverFloat(v)
		if !ok || f < 0 || f > 2 {
			return nil, invalidQuiverArgument("providerOptions.quiverai.temperature", "must be a number between 0 and 2")
		}
		opts.Temperature = &f
	}

	if v, ok := raw["topP"]; ok && v != nil {
		f, ok := quiverFloat(v)
		if !ok || f < 0 || f > 1 {
			return nil, invalidQuiverArgument("providerOptions.quiverai.topP", "must be a number between 0 and 1")
		}
		opts.TopP = &f
	}

	if v, ok := raw["presencePenalty"]; ok && v != nil {
		f, ok := quiverFloat(v)
		if !ok || f < -2 || f > 2 {
			return nil, invalidQuiverArgument("providerOptions.quiverai.presencePenalty", "must be a number between -2 and 2")
		}
		opts.PresencePenalty = &f
	}

	if v, ok := raw["maxOutputTokens"]; ok && v != nil {
		n, ok := quiverInt(v)
		if !ok || n < 1 || n > 131072 {
			return nil, invalidQuiverArgument("providerOptions.quiverai.maxOutputTokens", "must be an integer between 1 and 131072")
		}
		opts.MaxOutputTokens = &n
	}

	if v, ok := raw["orchestratorMaxOutputTokens"]; ok && v != nil {
		n, ok := quiverInt(v)
		if !ok || n < 1 || n > 65536 {
			return nil, invalidQuiverArgument("providerOptions.quiverai.orchestratorMaxOutputTokens", "must be an integer between 1 and 65536")
		}
		opts.OrchestratorMaxOutputTokens = &n
	}

	if v, ok := raw["shallowMaxOutputTokens"]; ok && v != nil {
		n, ok := quiverInt(v)
		if !ok || n < 1 || n > 65536 {
			return nil, invalidQuiverArgument("providerOptions.quiverai.shallowMaxOutputTokens", "must be an integer between 1 and 65536")
		}
		opts.ShallowMaxOutputTokens = &n
	}

	if v, ok := raw["autoCrop"]; ok && v != nil {
		b, ok := v.(bool)
		if !ok {
			return nil, invalidQuiverArgument("providerOptions.quiverai.autoCrop", "must be a boolean")
		}
		opts.AutoCrop = &b
	}

	if v, ok := raw["targetSize"]; ok && v != nil {
		n, ok := quiverInt(v)
		if !ok || n < 128 || n > 4096 {
			return nil, invalidQuiverArgument("providerOptions.quiverai.targetSize", "must be an integer between 128 and 4096")
		}
		opts.TargetSize = &n
	}

	return opts, nil
}

func quiverInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}

func quiverFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
