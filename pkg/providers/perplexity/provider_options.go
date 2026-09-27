package perplexity

import (
	"encoding/json"
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// PerplexityLanguageModelOptions mirrors the TS SDK's
// perplexityLanguageModelOptions zod schema (perplexity-language-model-options.ts).
// It is a "loose object": every documented field below is validated when
// present, and unrecognized top-level keys are preserved for passthrough
// (see parsePerplexityLanguageModelOptions).
type PerplexityLanguageModelOptions struct {
	// SearchRecencyFilter filters search results to those published within the
	// specified time window. Cannot be combined with other date filters.
	// One of: "hour", "day", "week", "month", "year".
	SearchRecencyFilter string `json:"search_recency_filter,omitempty"`

	// SearchDomainFilter restricts web search results to specific domains or
	// URLs. Prefix a domain with "-" to exclude it.
	SearchDomainFilter []string `json:"search_domain_filter,omitempty"`

	// SearchLanguageFilter filters search results by language using ISO 639-1 codes.
	SearchLanguageFilter []string `json:"search_language_filter,omitempty"`

	// SearchAfterDateFilter returns search results published after this date.
	SearchAfterDateFilter string `json:"search_after_date_filter,omitempty"`

	// SearchBeforeDateFilter returns search results published before this date.
	SearchBeforeDateFilter string `json:"search_before_date_filter,omitempty"`

	// LastUpdatedAfterFilter returns search results last updated after this date.
	LastUpdatedAfterFilter string `json:"last_updated_after_filter,omitempty"`

	// LastUpdatedBeforeFilter returns search results last updated before this date.
	LastUpdatedBeforeFilter string `json:"last_updated_before_filter,omitempty"`

	// SearchMode is the source of search results. One of: "web", "academic", "sec".
	SearchMode string `json:"search_mode,omitempty"`

	// EnableSearchClassifier: if true, the model decides whether web search is needed.
	EnableSearchClassifier *bool `json:"enable_search_classifier,omitempty"`

	// DisableSearch: if true, disables web search.
	DisableSearch *bool `json:"disable_search,omitempty"`

	// ReturnRelatedQuestions: if true, a list of related questions is included
	// in the response.
	ReturnRelatedQuestions *bool `json:"return_related_questions,omitempty"`

	// ReturnImages: if true, image search results are included in the response.
	ReturnImages *bool `json:"return_images,omitempty"`

	// ImageDomainFilter restricts image results to specific domains. Prefix a
	// domain with "-" to exclude it.
	ImageDomainFilter []string `json:"image_domain_filter,omitempty"`

	// ImageFormatFilter restricts image results to specific file formats.
	ImageFormatFilter []string `json:"image_format_filter,omitempty"`

	// MediaResponse holds additional media response configuration
	// (e.g. {"overrides": {"return_videos": true}}). Kept as a loose map so
	// unrecognized nested keys pass through unchanged, matching the TS SDK's
	// nested z.looseObject schema.
	MediaResponse map[string]interface{} `json:"media_response,omitempty"`

	// StreamMode controls the format of streaming events. One of: "full", "concise".
	StreamMode string `json:"stream_mode,omitempty"`

	// ReasoningEffort controls how much effort the model spends on reasoning.
	// One of: "minimal", "low", "medium", "high".
	ReasoningEffort string `json:"reasoning_effort,omitempty"`

	// LanguagePreference is the preferred response language as an ISO 639-1
	// language code.
	LanguagePreference string `json:"language_preference,omitempty"`

	// WebSearchOptions holds additional web search configuration. Kept as a
	// loose map so unrecognized nested keys pass through unchanged, matching
	// the TS SDK's nested z.looseObject schema. Known sub-fields
	// (search_context_size, search_type) are validated.
	WebSearchOptions map[string]interface{} `json:"web_search_options,omitempty"`
}

var (
	perplexitySearchRecencyFilters = map[string]bool{"hour": true, "day": true, "week": true, "month": true, "year": true}
	perplexitySearchModes          = map[string]bool{"web": true, "academic": true, "sec": true}
	perplexityStreamModes          = map[string]bool{"full": true, "concise": true}
	perplexityReasoningEfforts     = map[string]bool{"minimal": true, "low": true, "medium": true, "high": true}
	perplexitySearchContextSizes   = map[string]bool{"low": true, "medium": true, "high": true}
	perplexitySearchTypes          = map[string]bool{"fast": true, "pro": true, "auto": true}
)

// perplexityKnownOptionKeys is the set of top-level keys modeled by
// PerplexityLanguageModelOptions. Any other key in providerOptions.perplexity
// is preserved verbatim (loose-object passthrough).
var perplexityKnownOptionKeys = map[string]bool{
	"search_recency_filter":      true,
	"search_domain_filter":       true,
	"search_language_filter":     true,
	"search_after_date_filter":   true,
	"search_before_date_filter":  true,
	"last_updated_after_filter":  true,
	"last_updated_before_filter": true,
	"search_mode":                true,
	"enable_search_classifier":   true,
	"disable_search":             true,
	"return_related_questions":   true,
	"return_images":              true,
	"image_domain_filter":        true,
	"image_format_filter":        true,
	"media_response":             true,
	"stream_mode":                true,
	"reasoning_effort":           true,
	"language_preference":        true,
	"web_search_options":         true,
}

func invalidPerplexityProviderOptions(field, message string) error {
	return &providererrors.InvalidArgumentError{
		Field:   field,
		Message: message,
	}
}

func perplexityEnumStringField(raw map[string]interface{}, field string, allowed map[string]bool) (string, error) {
	v, ok := raw[field]
	if !ok || v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok || !allowed[s] {
		return "", invalidPerplexityProviderOptions(field, fmt.Sprintf("invalid value %v", v))
	}
	return s, nil
}

func perplexityBoolField(raw map[string]interface{}, field string) (*bool, error) {
	v, ok := raw[field]
	if !ok || v == nil {
		return nil, nil
	}
	b, ok := v.(bool)
	if !ok {
		return nil, invalidPerplexityProviderOptions(field, fmt.Sprintf("must be a boolean, got %T", v))
	}
	return &b, nil
}

func perplexityStringField(raw map[string]interface{}, field string) (string, error) {
	v, ok := raw[field]
	if !ok || v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", invalidPerplexityProviderOptions(field, fmt.Sprintf("must be a string, got %T", v))
	}
	return s, nil
}

func perplexityStringSliceField(raw map[string]interface{}, field string) ([]string, error) {
	v, ok := raw[field]
	if !ok || v == nil {
		return nil, nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil, invalidPerplexityProviderOptions(field, fmt.Sprintf("must be an array of strings, got %T", v))
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil, invalidPerplexityProviderOptions(field, "must be an array of strings")
		}
		out = append(out, s)
	}
	return out, nil
}

func perplexityMapField(raw map[string]interface{}, field string) (map[string]interface{}, error) {
	v, ok := raw[field]
	if !ok || v == nil {
		return nil, nil
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil, invalidPerplexityProviderOptions(field, fmt.Sprintf("must be an object, got %T", v))
	}
	return m, nil
}

func validatePerplexityWebSearchOptions(m map[string]interface{}) error {
	if m == nil {
		return nil
	}
	if _, err := perplexityEnumStringField(m, "search_context_size", perplexitySearchContextSizes); err != nil {
		return err
	}
	if _, err := perplexityEnumStringField(m, "search_type", perplexitySearchTypes); err != nil {
		return err
	}
	return nil
}

// parsePerplexityLanguageModelOptions parses+validates the resolved
// providerOptions.perplexity map into a typed struct, mirroring TS
// parseProviderOptions with the perplexityLanguageModelOptions zod schema.
// Because the TS schema is a "loose object", the second return value carries
// any top-level keys not modeled by PerplexityLanguageModelOptions so they can
// still be forwarded verbatim.
func parsePerplexityLanguageModelOptions(raw map[string]interface{}) (*PerplexityLanguageModelOptions, map[string]interface{}, error) {
	if raw == nil {
		return nil, nil, nil
	}

	opts := &PerplexityLanguageModelOptions{}
	var err error

	if opts.SearchRecencyFilter, err = perplexityEnumStringField(raw, "search_recency_filter", perplexitySearchRecencyFilters); err != nil {
		return nil, nil, err
	}
	if opts.SearchDomainFilter, err = perplexityStringSliceField(raw, "search_domain_filter"); err != nil {
		return nil, nil, err
	}
	if opts.SearchLanguageFilter, err = perplexityStringSliceField(raw, "search_language_filter"); err != nil {
		return nil, nil, err
	}
	if opts.SearchAfterDateFilter, err = perplexityStringField(raw, "search_after_date_filter"); err != nil {
		return nil, nil, err
	}
	if opts.SearchBeforeDateFilter, err = perplexityStringField(raw, "search_before_date_filter"); err != nil {
		return nil, nil, err
	}
	if opts.LastUpdatedAfterFilter, err = perplexityStringField(raw, "last_updated_after_filter"); err != nil {
		return nil, nil, err
	}
	if opts.LastUpdatedBeforeFilter, err = perplexityStringField(raw, "last_updated_before_filter"); err != nil {
		return nil, nil, err
	}
	if opts.SearchMode, err = perplexityEnumStringField(raw, "search_mode", perplexitySearchModes); err != nil {
		return nil, nil, err
	}
	if opts.EnableSearchClassifier, err = perplexityBoolField(raw, "enable_search_classifier"); err != nil {
		return nil, nil, err
	}
	if opts.DisableSearch, err = perplexityBoolField(raw, "disable_search"); err != nil {
		return nil, nil, err
	}
	if opts.ReturnRelatedQuestions, err = perplexityBoolField(raw, "return_related_questions"); err != nil {
		return nil, nil, err
	}
	if opts.ReturnImages, err = perplexityBoolField(raw, "return_images"); err != nil {
		return nil, nil, err
	}
	if opts.ImageDomainFilter, err = perplexityStringSliceField(raw, "image_domain_filter"); err != nil {
		return nil, nil, err
	}
	if opts.ImageFormatFilter, err = perplexityStringSliceField(raw, "image_format_filter"); err != nil {
		return nil, nil, err
	}
	if opts.MediaResponse, err = perplexityMapField(raw, "media_response"); err != nil {
		return nil, nil, err
	}
	if opts.StreamMode, err = perplexityEnumStringField(raw, "stream_mode", perplexityStreamModes); err != nil {
		return nil, nil, err
	}
	if opts.ReasoningEffort, err = perplexityEnumStringField(raw, "reasoning_effort", perplexityReasoningEfforts); err != nil {
		return nil, nil, err
	}
	if opts.LanguagePreference, err = perplexityStringField(raw, "language_preference"); err != nil {
		return nil, nil, err
	}
	if opts.WebSearchOptions, err = perplexityMapField(raw, "web_search_options"); err != nil {
		return nil, nil, err
	}
	if err = validatePerplexityWebSearchOptions(opts.WebSearchOptions); err != nil {
		return nil, nil, err
	}

	var extras map[string]interface{}
	for k, v := range raw {
		if !perplexityKnownOptionKeys[k] {
			if extras == nil {
				extras = map[string]interface{}{}
			}
			extras[k] = v
		}
	}

	return opts, extras, nil
}

// toWireMap serializes the validated options back into a flat map of wire
// keys, ready to be spread into the chat/completions request body — mirrors
// the TS SDK's `...perplexityOptions` spread.
func (o *PerplexityLanguageModelOptions) toWireMap() map[string]interface{} {
	if o == nil {
		return nil
	}
	b, err := json.Marshal(o)
	if err != nil {
		return nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	return m
}
