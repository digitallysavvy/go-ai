package perplexity

import (
	"encoding/json"
	"fmt"
)

// This file mirrors the TS SDK's perplexity-agent-api.ts: the wire shapes for
// the Perplexity Agent API (/v1/agent) response, streaming chunk events, and
// error envelope. Fields that TS models as `.nullish()` are Go pointer/slice
// types so that both an absent key and an explicit JSON null decode to the
// same "not present" zero value -- this gives Go the same null-tolerance TS
// gained from commit 6da8aa06d6 "for free", without needing separate
// null-vs-undefined handling.

// perplexityAnnotation is one annotation on a message content part (e.g. a
// url_citation). Mirrors the annotations array in perplexityContentPartSchema.
type perplexityAnnotation struct {
	Type  string  `json:"type,omitempty"`
	URL   *string `json:"url,omitempty"`
	Title *string `json:"title,omitempty"`
}

// perplexityContentPart is one entry in a "message" output item's content
// array. Mirrors perplexityContentPartSchema.
type perplexityContentPart struct {
	Type        string                 `json:"type"`
	Text        *string                `json:"text,omitempty"`
	Annotations []perplexityAnnotation `json:"annotations,omitempty"`
}

// perplexitySearchResult is one web search result. Mirrors
// perplexitySearchResultSchema. Title and URL are required by the TS zod
// schema; validatePerplexitySearchResults enforces that at decode time.
type perplexitySearchResult struct {
	ID          *float64 `json:"id,omitempty"`
	Title       string   `json:"title"`
	URL         string   `json:"url"`
	Snippet     *string  `json:"snippet,omitempty"`
	Date        *string  `json:"date,omitempty"`
	LastUpdated *string  `json:"last_updated,omitempty"`
	Source      *string  `json:"source,omitempty"`
}

// perplexityFetchedContent is one fetched URL result. Mirrors
// perplexityFetchedContentSchema. Title and URL are required.
type perplexityFetchedContent struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Snippet *string `json:"snippet,omitempty"`
}

// perplexityOutputItem is one entry in response.output[] / a streaming
// "item". It is a discriminated union in TS (perplexityOutputItemSchema);
// here every variant's fields live on one struct and callers branch on Type.
// Unhandled types (e.g. "finance_results") are preserved for raw-chunk
// passthrough but never treated as a handled variant.
type perplexityOutputItem struct {
	Type             string                     `json:"type"`
	ID               *string                    `json:"id,omitempty"`
	Content          []perplexityContentPart    `json:"content,omitempty"`
	Results          []perplexitySearchResult   `json:"results,omitempty"`
	Contents         []perplexityFetchedContent `json:"contents,omitempty"`
	CallID           *string                    `json:"call_id,omitempty"`
	Name             *string                    `json:"name,omitempty"`
	Arguments        *string                    `json:"arguments,omitempty"`
	ThoughtSignature *string                    `json:"thought_signature,omitempty"`
}

type perplexityIncompleteDetails struct {
	Reason string `json:"reason"`
}

type perplexityResponseError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
}

type perplexityInputTokensDetails struct {
	CachedTokens             *int64 `json:"cached_tokens,omitempty"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens,omitempty"`
}

type perplexityOutputTokensDetails struct {
	ReasoningTokens *int64 `json:"reasoning_tokens,omitempty"`
}

type perplexityToolCallDetail struct {
	Invocation *int64 `json:"invocation,omitempty"`
}

type perplexityCost struct {
	Currency          *string  `json:"currency,omitempty"`
	InputCost         *float64 `json:"input_cost,omitempty"`
	OutputCost        *float64 `json:"output_cost,omitempty"`
	TotalCost         *float64 `json:"total_cost,omitempty"`
	CacheCreationCost *float64 `json:"cache_creation_cost,omitempty"`
	CacheReadCost     *float64 `json:"cache_read_cost,omitempty"`
	ToolCallsCost     *float64 `json:"tool_calls_cost,omitempty"`
}

// perplexityUsage is the Agent API usage object. Mirrors perplexityUsageSchema.
// This is a full replacement of the pre-migration Sonar Chat Completions usage
// shape (input_tokens/output_tokens vs. prompt_tokens/completion_tokens).
type perplexityUsage struct {
	InputTokens         int64                               `json:"input_tokens"`
	OutputTokens        int64                               `json:"output_tokens"`
	TotalTokens         int64                               `json:"total_tokens"`
	InputTokensDetails  *perplexityInputTokensDetails       `json:"input_tokens_details,omitempty"`
	OutputTokensDetails *perplexityOutputTokensDetails      `json:"output_tokens_details,omitempty"`
	ToolCallsDetails    map[string]perplexityToolCallDetail `json:"tool_calls_details,omitempty"`
	Cost                *perplexityCost                     `json:"cost,omitempty"`
}

// perplexityAgentResponse is the full Agent API response object. Mirrors
// perplexityAgentResponseSchema. id/created_at/model/object/output/status are
// required by the TS zod schema; validatePerplexityAgentResponse enforces
// that at decode time (Go's json.Unmarshal alone does not reject missing
// keys).
type perplexityAgentResponse struct {
	ID                string                       `json:"id"`
	CreatedAt         int64                        `json:"created_at"`
	Model             string                       `json:"model"`
	Object            string                       `json:"object"`
	Output            []perplexityOutputItem       `json:"output"`
	Status            string                       `json:"status"`
	IncompleteDetails *perplexityIncompleteDetails `json:"incomplete_details,omitempty"`
	Error             *perplexityResponseError     `json:"error,omitempty"`
	Usage             *perplexityUsage             `json:"usage,omitempty"`
}

// perplexityAgentChunk is a single Agent API SSE event. Mirrors
// perplexityAgentChunkSchema. Only `type` is required; every other field is
// nullish in TS and is a pointer/slice here for the same reason.
type perplexityAgentChunk struct {
	Type           string                     `json:"type"`
	SequenceNumber *int64                     `json:"sequence_number,omitempty"`
	Response       *perplexityAgentResponse   `json:"response,omitempty"`
	Item           *perplexityOutputItem      `json:"item,omitempty"`
	OutputIndex    *int64                     `json:"output_index,omitempty"`
	ItemID         *string                    `json:"item_id,omitempty"`
	ContentIndex   *int64                     `json:"content_index,omitempty"`
	Delta          *string                    `json:"delta,omitempty"`
	Text           *string                    `json:"text,omitempty"`
	Thought        *string                    `json:"thought,omitempty"`
	Queries        []string                   `json:"queries,omitempty"`
	URLs           []string                   `json:"urls,omitempty"`
	Results        []perplexitySearchResult   `json:"results,omitempty"`
	Contents       []perplexityFetchedContent `json:"contents,omitempty"`
	Error          *perplexityResponseError   `json:"error,omitempty"`
}

// perplexityErrorEnvelope mirrors perplexityErrorSchema: Perplexity's error
// responses can take several shapes (an "error" string or object, a "detail"
// string or validation-error array, or a bare top-level "message").
type perplexityErrorEnvelope struct {
	Error   json.RawMessage `json:"error,omitempty"`
	Detail  json.RawMessage `json:"detail,omitempty"`
	Message string          `json:"message,omitempty"`
}

// perplexityErrorToMessage mirrors TS perplexityErrorToMessage: it resolves
// whichever shape the error envelope used into a single human-readable
// message string.
func perplexityErrorToMessage(data perplexityErrorEnvelope) string {
	if len(data.Error) > 0 && string(data.Error) != "null" {
		var asString string
		if err := json.Unmarshal(data.Error, &asString); err == nil {
			return asString
		}
		var asObject struct {
			Message *string `json:"message"`
			Type    *string `json:"type"`
		}
		if err := json.Unmarshal(data.Error, &asObject); err == nil {
			if asObject.Message != nil {
				return *asObject.Message
			}
			if asObject.Type != nil {
				return *asObject.Type
			}
		}
		return "unknown error"
	}
	if len(data.Detail) > 0 && string(data.Detail) != "null" {
		var asString string
		if err := json.Unmarshal(data.Detail, &asString); err == nil {
			return asString
		}
		var asArray []struct {
			Msg string `json:"msg"`
		}
		if err := json.Unmarshal(data.Detail, &asArray); err == nil {
			msgs := make([]string, 0, len(asArray))
			for _, d := range asArray {
				msgs = append(msgs, d.Msg)
			}
			result := ""
			for i, m := range msgs {
				if i > 0 {
					result += ", "
				}
				result += m
			}
			return result
		}
	}
	if data.Message != "" {
		return data.Message
	}
	return "unknown error"
}

// validatePerplexityAgentResponse enforces the required top-level fields of
// perplexityAgentResponseSchema (id/created_at/model/object/output/status)
// and the required fields of nested search_results/fetch_url_results items
// (title/url) against the raw response bytes. Go's json.Unmarshal silently
// zero-fills missing fields, so this presence check is what makes malformed
// responses fail the way TS's zod schema does (surfaced as "Invalid JSON
// response" for the top-level response, or a thrown validation error for a
// malformed handled output item).
func validatePerplexityAgentResponse(raw []byte) error {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return fmt.Errorf("perplexity: invalid JSON response: %w", err)
	}
	for _, key := range []string{"id", "created_at", "model", "object", "output", "status"} {
		if _, ok := probe[key]; !ok {
			return fmt.Errorf("perplexity: response missing required field %q", key)
		}
	}
	var object string
	if err := json.Unmarshal(probe["object"], &object); err != nil || object != "response" {
		return fmt.Errorf(`perplexity: response "object" field must be "response"`)
	}
	var rawOutput []json.RawMessage
	if err := json.Unmarshal(probe["output"], &rawOutput); err != nil {
		return fmt.Errorf("perplexity: invalid output array: %w", err)
	}
	for _, itemRaw := range rawOutput {
		if err := validatePerplexityOutputItem(itemRaw); err != nil {
			return err
		}
	}
	return nil
}

// validatePerplexityOutputItem enforces the required title/url fields on
// search_results and fetch_url_results items, mirroring
// perplexitySearchResultSchema / perplexityFetchedContentSchema.
func validatePerplexityOutputItem(raw json.RawMessage) error {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("perplexity: invalid output item: %w", err)
	}
	switch envelope.Type {
	case "search_results":
		var withResults struct {
			Results []map[string]json.RawMessage `json:"results"`
		}
		if err := json.Unmarshal(raw, &withResults); err != nil {
			return fmt.Errorf("perplexity: invalid search_results item: %w", err)
		}
		for _, result := range withResults.Results {
			if err := requirePerplexityFields(result, "title", "url"); err != nil {
				return fmt.Errorf("perplexity: search result %w", err)
			}
		}
	case "fetch_url_results":
		var withContents struct {
			Contents []map[string]json.RawMessage `json:"contents"`
		}
		if err := json.Unmarshal(raw, &withContents); err != nil {
			return fmt.Errorf("perplexity: invalid fetch_url_results item: %w", err)
		}
		for _, content := range withContents.Contents {
			if err := requirePerplexityFields(content, "title", "url"); err != nil {
				return fmt.Errorf("perplexity: fetched content %w", err)
			}
		}
	}
	return nil
}

func requirePerplexityFields(m map[string]json.RawMessage, fields ...string) error {
	for _, field := range fields {
		if _, ok := m[field]; !ok {
			return fmt.Errorf("missing required field %q", field)
		}
	}
	return nil
}
