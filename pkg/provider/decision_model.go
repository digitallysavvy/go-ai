package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// DecisionQuestion is one judgment to make about the shared decision
// state. Mirrors TypeScript's Experimental_DecisionModelV4Question union
// (choice/score/boolean/box). Fields are interpreted according to Type; Go does
// not have discriminated unions so Criteria is typed loosely, matching how
// the TypeScript validator itself treats it structurally.
type DecisionQuestion struct {
	// Type is "choice", "score", "boolean", or "box".
	Type string

	// Instructions is a JSON-compatible string, map[string]interface{}, or
	// []interface{}. It may be nil for choice, score and boolean questions
	// when the criteria define the judgment (a boolean question then needs a
	// non-nil criterion); a box question always needs it.
	Instructions interface{}

	// Criteria is interpreted according to Type:
	//   - "choice": a nonempty map[string]interface{} of option name ->
	//     description (nil description allowed, key must still be present).
	//   - "score": a []interface{} of at least one ordered level (nil entries
	//     allowed); providers may require more.
	//   - "boolean": optional; when non-nil, a map[string]interface{} with
	//     only "true"/"false" keys.
	//   - "box": unused (a box question has instructions only).
	Criteria interface{}
}

// OrderedKeys returns the keys of m in order: the entries of order that are
// keys of m (first occurrence only), then the remaining keys sorted.
func OrderedKeys[V any](m map[string]V, order []string) []string {
	out := make([]string, 0, len(m))
	seen := make(map[string]struct{}, len(m))
	for _, k := range order {
		if _, ok := m[k]; !ok {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	rest := make([]string, 0, len(m)-len(out))
	for k := range m {
		if _, ok := seen[k]; !ok {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// QuestionIDs returns the question IDs of the call in QuestionOrder, then
// the remaining ones sorted.
func (o DecisionCallOptions) QuestionIDs() []string {
	return OrderedKeys(o.Questions, o.QuestionOrder)
}

// DecisionStatePart is one ordered part of the shared decision state. Mirrors
// TypeScript's Experimental_DecisionModelV4StatePart union (text, file, or
// json). Type selects which field is meaningful.
type DecisionStatePart struct {
	// Type is "text", "file", or "json".
	Type string

	// Text is the content of a "text" part.
	Text string

	// Value is the JSON-compatible content of a "json" part.
	Value interface{}

	// File is the content of a "file" part. Core downloads URL data before
	// calling a model, so models normally see inline data (FileData.Type
	// "data").
	File *types.FileContent
}

// MarshalJSON emits the TypeScript SDK shape used in telemetry attributes:
// {"type":"text","text":...}, {"type":"json","value":...}, or
// {"type":"file","data":<base64 | url | reference>,"mediaType":...}.
func (p DecisionStatePart) MarshalJSON() ([]byte, error) {
	switch p.Type {
	case "text":
		return json.Marshal(struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{"text", p.Text})
	case "json":
		return json.Marshal(struct {
			Type  string      `json:"type"`
			Value interface{} `json:"value"`
		}{"json", p.Value})
	case "file":
		out := struct {
			Type            string                 `json:"type"`
			Data            interface{}            `json:"data,omitempty"`
			MediaType       string                 `json:"mediaType,omitempty"`
			Filename        string                 `json:"filename,omitempty"`
			ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`
		}{Type: "file"}
		if p.File != nil {
			f := p.File
			out.MediaType = f.MediaType
			if out.MediaType == "" {
				out.MediaType = f.MimeType
			}
			if out.MediaType == "" {
				out.MediaType = f.FileData.MediaType
			}
			out.Filename = f.Filename
			out.ProviderOptions = f.ProviderOptions
			switch f.FileData.Type {
			case types.FileDataTypeData:
				if f.FileData.DataString != "" {
					out.Data = f.FileData.DataString
				} else {
					out.Data = base64.StdEncoding.EncodeToString(f.FileData.Data)
				}
			case types.FileDataTypeURL:
				out.Data = f.FileData.URL
			case types.FileDataTypeReference:
				out.Data = f.FileData.Reference
			case types.FileDataTypeText:
				out.Data = f.FileData.Text
			default:
				switch {
				case len(f.Data) > 0:
					out.Data = base64.StdEncoding.EncodeToString(f.Data)
				case f.URL != "":
					out.Data = f.URL
				case f.Text != "":
					out.Data = f.Text
				}
			}
		}
		return json.Marshal(out)
	default:
		return nil, fmt.Errorf("unknown decision state part type %q", p.Type)
	}
}

// DecisionTextPart returns a "text" state part.
func DecisionTextPart(text string) DecisionStatePart {
	return DecisionStatePart{Type: "text", Text: text}
}

// DecisionJSONPart returns a "json" state part holding a JSON-compatible value.
func DecisionJSONPart(value interface{}) DecisionStatePart {
	return DecisionStatePart{Type: "json", Value: value}
}

// DecisionFilePart returns a "file" state part.
func DecisionFilePart(file types.FileContent) DecisionStatePart {
	return DecisionStatePart{Type: "file", File: &file}
}

// DecisionStateParts converts a decision state into ordered parts. It accepts
// a string (one text part), a []DecisionStatePart (returned as is), or any
// other JSON-compatible value such as map[string]interface{} (one json part).
// It performs no downloads; core resolves file URLs before calling a model.
// Models use it so that direct calls with a plain string or object state
// keep working.
func DecisionStateParts(state interface{}) []DecisionStatePart {
	switch v := state.(type) {
	case []DecisionStatePart:
		return v
	case string:
		return []DecisionStatePart{DecisionTextPart(v)}
	default:
		return []DecisionStatePart{DecisionJSONPart(v)}
	}
}

// InlineData returns the media type and raw bytes of a "file" part with
// inline data. It returns an error for URL, reference, or text file data,
// which core does not resolve for decision models.
func (p DecisionStatePart) InlineData() (mediaType string, data []byte, err error) {
	if p.Type != "file" || p.File == nil {
		return "", nil, fmt.Errorf("decision state part of type %q is not a file", p.Type)
	}
	f := p.File
	mediaType = f.MediaType
	if mediaType == "" {
		mediaType = f.MimeType
	}
	if mediaType == "" {
		mediaType = f.FileData.MediaType
	}
	switch f.FileData.Type {
	case types.FileDataTypeData:
		if len(f.FileData.Data) > 0 {
			return mediaType, f.FileData.Data, nil
		}
		if f.FileData.DataString != "" {
			decoded, derr := base64.StdEncoding.DecodeString(f.FileData.DataString)
			if derr != nil {
				return mediaType, nil, fmt.Errorf("decision file data is not valid base64: %w", derr)
			}
			return mediaType, decoded, nil
		}
		return mediaType, nil, nil
	case "":
		if len(f.Data) > 0 {
			return mediaType, f.Data, nil
		}
	}
	kind := string(f.FileData.Type)
	if kind == "" {
		kind = "data"
	}
	return mediaType, nil, fmt.Errorf("decision file data of type %q is not inline data", kind)
}

// DecisionAnswer is one typed answer to a DecisionQuestion. Mirrors
// TypeScript's Experimental_DecisionModelV4Answer union.
type DecisionAnswer struct {
	// Type is "choice", "score", "boolean", "box", or "refusal". A "refusal" answer
	// means the model declined to answer the question; it is valid for any
	// question type and carries no other fields.
	Type string `json:"type"`

	// Choice is the selected option for a "choice" answer.
	Choice string `json:"choice,omitempty"`

	// Score is the fractional position in [0, number of levels - 1] for a
	// "score" answer.
	Score *float64 `json:"score,omitempty"`

	// Probability is the model-estimated P(true) in [0, 1] for a "boolean"
	// answer.
	Probability *float64 `json:"probability,omitempty"`

	// Probabilities is the complete distribution over the question's
	// options/levels, when available, for "choice" and "score" answers.
	Probabilities map[string]float64 `json:"probabilities,omitempty"`

	// Box is the normalized bounding box for a "box" answer.
	Box *DecisionBox `json:"box,omitempty"`
}

// DecisionBox is a bounding box with edges in [0, 1], measured from the
// image's top-left corner (TS `box: { top, bottom, left, right }`).
type DecisionBox struct {
	Top    float64 `json:"top"`
	Bottom float64 `json:"bottom"`
	Left   float64 `json:"left"`
	Right  float64 `json:"right"`
}

// DecisionRounding declares the decimal precision a provider rounded its
// output to. Omit a field for full precision.
type DecisionRounding struct {
	ProbabilityDecimals *int `json:"probabilityDecimals,omitempty"`
	ScoreDecimals       *int `json:"scoreDecimals,omitempty"`
}

// DecisionUsage reports token usage for a decision call.
type DecisionUsage struct {
	InputTokens  *int `json:"inputTokens,omitempty"`
	OutputTokens *int `json:"outputTokens,omitempty"`
}

// DecisionResponseInfo carries raw response metadata from the provider.
type DecisionResponseInfo struct {
	ID        string
	Timestamp time.Time
	ModelID   string
	Headers   map[string]string
	Body      interface{}
}

// DecisionCallOptions are passed to DecisionModel.DoDecide.
type DecisionCallOptions struct {
	// State is the shared state. Core always passes ordered
	// []DecisionStatePart with file URLs already resolved to inline data;
	// models should read it through DecisionStateParts so direct calls with a
	// string or JSON object keep working.
	State interface{}

	// Questions maps question ID -> question.
	Questions map[string]DecisionQuestion

	// QuestionOrder optionally lists question IDs in the insertion order of
	// the TypeScript questions object (Go maps are unordered). Providers that
	// send the questions as a list use it; IDs it omits follow, sorted. Nil
	// keeps the sorted order.
	QuestionOrder []string

	// CriteriaOrder optionally lists, per question ID, the keys of a choice
	// Criteria map in the order the TypeScript object had them (JS objects
	// keep insertion order, Go maps do not). Keys it omits follow, sorted.
	// Kept beside the questions, not inside DecisionQuestion, so that struct
	// stays comparable.
	CriteriaOrder map[string][]string

	// MaxOutputTokens is the maximum number of output tokens for models that
	// support it (an integer >= 1; nil means unset).
	MaxOutputTokens *int

	// Reasoning is the reasoning effort for models that support it.
	// Language-model adapters default to types.ReasoningNone when nil.
	Reasoning *types.ReasoningLevel

	Headers         map[string]string
	ProviderOptions map[string]interface{}
}

// DecisionResult is returned by DecisionModel.DoDecide.
type DecisionResult struct {
	// Answers has exactly one answer per question, keyed by question ID.
	Answers map[string]DecisionAnswer

	Rounding         *DecisionRounding
	Usage            *DecisionUsage
	Warnings         []types.Warning
	ProviderMetadata map[string]interface{}
	Response         *DecisionResponseInfo
}

// DecisionModel is the experimental decision model contract (V4). May
// change in patch releases. Mirrors TypeScript's Experimental_DecisionModelV4.
type DecisionModel interface {
	SpecificationVersion() string
	Provider() string
	ModelID() string

	// SupportedQuestionTypes lists the question types this model accepts (a
	// subset of "choice", "score", "boolean", "box"), used to reject unsupported
	// calls before any I/O.
	SupportedQuestionTypes() []string

	// DoDecide decides every question against the same state. No partial
	// results.
	DoDecide(ctx context.Context, opts DecisionCallOptions) (*DecisionResult, error)
}

// DecisionModelProvider is a structural extension a Provider may implement
// to resolve decision models by ID. Decision is experimental and not
// part of the stable Provider contract, mirroring TypeScript's
// DecisionProvider.decisionModel.
type DecisionModelProvider interface {
	DecisionModel(modelID string) (DecisionModel, error)
}
