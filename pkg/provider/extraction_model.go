package provider

import (
	"context"
	"encoding/json"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Extraction field types, mirroring the TypeScript
// Experimental_ExtractionModelV4Field `type` union.
const (
	ExtractionFieldText     = "text"
	ExtractionFieldNumber   = "number"
	ExtractionFieldDate     = "date"
	ExtractionFieldTime     = "time"
	ExtractionFieldDateTime = "datetime"
	ExtractionFieldDuration = "duration"
	ExtractionFieldList     = "list"
)

// ExtractionFieldTypes lists every extraction field type in the TypeScript
// declaration order.
func ExtractionFieldTypes() []string {
	return []string{
		ExtractionFieldText, ExtractionFieldNumber, ExtractionFieldDate,
		ExtractionFieldTime, ExtractionFieldDateTime, ExtractionFieldDuration,
		ExtractionFieldList,
	}
}

// Extraction duration units (TS Experimental_ExtractionModelV4DurationUnit).
const (
	ExtractionDurationSeconds = "seconds"
	ExtractionDurationMinutes = "minutes"
	ExtractionDurationHours   = "hours"
	ExtractionDurationDays    = "days"
	ExtractionDurationWeeks   = "weeks"
	ExtractionDurationMonths  = "months"
	ExtractionDurationYears   = "years"
)

// ExtractionField is a typed value to extract from the shared state. Mirrors
// TypeScript's Experimental_ExtractionModelV4Field union. Go has no
// discriminated unions, so Type selects which of the other fields apply.
type ExtractionField struct {
	// Type is "text", "number", "date", "time", "datetime", "duration" or
	// "list".
	Type string

	// Instructions is optional content for the field (a JSON-compatible
	// string, map[string]interface{} or []interface{}; nil when absent),
	// the same shape as DecisionQuestion.Instructions.
	Instructions interface{}

	// Unit applies to "number" (any unit, for example "USD" or "kg") and
	// "duration" (one of the ExtractionDuration* units; empty means
	// "seconds"). Empty means unset.
	Unit string

	// Integer requires an integer value ("number", "duration") or integer
	// items ("list" of number items). Nil means unset (TS `integer`
	// undefined); a pointer to false is an explicit false.
	Integer *bool

	// Decimals is the maximum number of decimal places, an integer from 0 to
	// 6 ("number", "duration", "list" of number items). Nil means unset.
	Decimals *int

	// Items is the item type of a "list" field: "text", "number" or "date".
	Items string
}

// Extraction source types.
const (
	ExtractionSourceText = "text"
	ExtractionSourceFile = "file"
)

// ExtractionSource says where in the state a value was found. Mirrors
// Experimental_ExtractionModelV4Source.
type ExtractionSource struct {
	// Type is "text" or "file".
	Type string `json:"type"`

	// Part is the index of the text or file part in the state.
	Part int `json:"part"`

	// Start is the inclusive start offset in UTF-16 code units ("text"
	// sources only).
	Start int `json:"start,omitempty"`

	// End is the exclusive end offset in UTF-16 code units ("text" sources
	// only).
	End int `json:"end,omitempty"`
}

// MarshalJSON emits {"type":"text","part":..,"start":..,"end":..} for text
// sources (zero offsets included) and {"type":"file","part":..} for file
// sources.
func (s ExtractionSource) MarshalJSON() ([]byte, error) {
	if s.Type == ExtractionSourceText {
		return json.Marshal(struct {
			Type  string `json:"type"`
			Part  int    `json:"part"`
			Start int    `json:"start"`
			End   int    `json:"end"`
		}{s.Type, s.Part, s.Start, s.End})
	}
	return json.Marshal(struct {
		Type string `json:"type"`
		Part int    `json:"part"`
	}{s.Type, s.Part})
}

// ExtractionListItem is the per-item entry of a "list" answer.
type ExtractionListItem struct {
	// Confidence in [0, 1] that the item is correct; nil when unreported.
	Confidence *float64 `json:"confidence,omitempty"`

	// Source is where the item was found; nil when unreported.
	Source *ExtractionSource `json:"source,omitempty"`
}

// ExtractionAnswer is one typed answer to an ExtractionField. Mirrors
// Experimental_ExtractionModelV4Answer.
type ExtractionAnswer struct {
	// Type is the field type the answer is for ("text", "number", "date",
	// "time", "datetime", "duration", "list") or "refusal". A "refusal"
	// answer means the model declined to extract the field; it is valid for
	// any field type and carries no other fields.
	Type string

	// Value is nil when the state does not contain a value for the field.
	// Otherwise: a string for text, date, time and datetime; a float64 for
	// number and duration; a []interface{} (or []string / []float64) for
	// list. Unused for "refusal".
	Value interface{}

	// Guess is the most likely value, also when Value is nil. Same Go types
	// as Value. Not used by "list" answers.
	Guess interface{}

	// PNull is the probability in [0, 1] that the state does not contain a
	// value; nil when unreported.
	PNull *float64

	// Confidence in [0, 1] that Value is correct; nil when unreported.
	Confidence *float64

	// Source is where the value was found. Not used by "list" answers (see
	// Items).
	Source *ExtractionSource

	// Items has one entry per value item, when available ("list" answers).
	Items []ExtractionListItem
}

// MarshalJSON emits the TypeScript answer shape: refusal answers carry only
// the type, every other answer always carries "value" (null when absent).
func (a ExtractionAnswer) MarshalJSON() ([]byte, error) {
	if a.Type == "refusal" {
		return json.Marshal(struct {
			Type string `json:"type"`
		}{a.Type})
	}
	return json.Marshal(struct {
		Type       string               `json:"type"`
		Value      interface{}          `json:"value"`
		Guess      interface{}          `json:"guess,omitempty"`
		PNull      *float64             `json:"pNull,omitempty"`
		Confidence *float64             `json:"confidence,omitempty"`
		Source     *ExtractionSource    `json:"source,omitempty"`
		Items      []ExtractionListItem `json:"items,omitempty"`
	}{a.Type, a.Value, a.Guess, a.PNull, a.Confidence, a.Source, a.Items})
}

// ExtractionUsage reports token usage for an extraction call.
type ExtractionUsage struct {
	InputTokens  *int `json:"inputTokens,omitempty"`
	OutputTokens *int `json:"outputTokens,omitempty"`
}

// ExtractionResponseInfo carries raw response metadata from the provider.
type ExtractionResponseInfo struct {
	ID        string
	Timestamp time.Time
	ModelID   string
	Headers   map[string]string
	Body      interface{}
}

// ExtractionCallOptions are passed to ExtractionModel.DoExtract. Mirrors
// Experimental_ExtractionModelV4CallOptions.
type ExtractionCallOptions struct {
	// State is the shared state. Core always passes ordered
	// []DecisionStatePart with file URLs already resolved to inline data;
	// answer sources index into it. Models should read it through
	// DecisionStateParts so direct calls with a string or JSON object keep
	// working.
	State interface{}

	// Fields maps field ID -> field.
	Fields map[string]ExtractionField

	// FieldOrder optionally lists field IDs in the insertion order of the
	// TypeScript fields object (Go maps are unordered). IDs it omits follow,
	// sorted. Nil keeps the sorted order.
	FieldOrder []string

	// ReferenceDate is a YYYY-MM-DD date that anchors relative dates and
	// dates without a year. Empty means unset.
	ReferenceDate string

	// MaxOutputTokens is the maximum number of output tokens for models that
	// support it (an integer >= 1; nil means unset).
	MaxOutputTokens *int

	// Reasoning is the reasoning effort for models that support it.
	Reasoning *types.ReasoningLevel

	Headers         map[string]string
	ProviderOptions map[string]interface{}
}

// FieldIDs returns the field IDs of the call in FieldOrder, then the
// remaining ones sorted (see OrderedKeys).
func (o ExtractionCallOptions) FieldIDs() []string {
	return OrderedKeys(o.Fields, o.FieldOrder)
}

// ExtractionResult is returned by ExtractionModel.DoExtract. Mirrors
// Experimental_ExtractionModelV4Result.
type ExtractionResult struct {
	// Answers has exactly one answer per field, keyed by field ID.
	Answers map[string]ExtractionAnswer

	Usage            *ExtractionUsage
	Warnings         []types.Warning
	ProviderMetadata map[string]interface{}
	Response         *ExtractionResponseInfo
}

// ExtractionModel is the experimental extraction model contract (V4). May
// change in patch releases. Mirrors TypeScript's Experimental_ExtractionModelV4.
type ExtractionModel interface {
	SpecificationVersion() string
	Provider() string
	ModelID() string

	// SupportedFieldTypes lists the field types this model accepts (a subset
	// of the ExtractionField* constants), used to reject unsupported calls
	// before any I/O.
	SupportedFieldTypes() []string

	// DoExtract extracts every field from the same state. No partial
	// results.
	DoExtract(ctx context.Context, opts ExtractionCallOptions) (*ExtractionResult, error)
}

// ExtractionModelProvider is a structural extension a Provider may implement
// to resolve extraction models by ID. Extraction is experimental and not
// part of the stable Provider contract, mirroring TypeScript's
// ExtractionProvider.extractionModel.
type ExtractionModelProvider interface {
	ExtractionModel(modelID string) (ExtractionModel, error)
}
