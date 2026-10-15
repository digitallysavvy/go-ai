package streaming

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// OpenAICompatContent is a chat message `content` that is a string, null, or an
// array of typed parts (TS openAICompatibleContentSchema). Decoding keeps the
// text parts joined; use OpenAICompatContentParts for the ordered text and
// thinking parts.
type OpenAICompatContent string

// UnmarshalJSON accepts a string, null, or an array of parts; array text parts
// are joined and unknown parts are ignored.
func (c *OpenAICompatContent) UnmarshalJSON(b []byte) error {
	text, _, err := compatContentParts(b)
	if err != nil {
		return err
	}
	*c = OpenAICompatContent(text)
	return nil
}

// CompatContentPart is one text or reasoning part of a message content value.
type CompatContentPart struct {
	Reasoning bool
	Text      string
}

// OpenAICompatContentParts mirrors TS convertOpenAICompatibleContent: text
// parts and thinking parts (the text of their `type: "text"` chunks joined) in
// wire order, empty ones dropped; unknown parts are ignored.
func OpenAICompatContentParts(raw json.RawMessage) ([]CompatContentPart, error) {
	_, parts, err := compatContentParts(raw)
	return parts, err
}

func compatContentParts(raw []byte) (string, []CompatContentPart, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", nil, err
		}
		if s == "" {
			return "", nil, nil
		}
		return s, []CompatContentPart{{Text: s}}, nil
	}
	var wire []struct {
		Type     string          `json:"type"`
		Text     json.RawMessage `json:"text"`
		Thinking json.RawMessage `json:"thinking"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return "", nil, err
	}
	var text strings.Builder
	var parts []CompatContentPart
	for _, p := range wire {
		switch p.Type {
		case "text":
			var t string
			if json.Unmarshal(p.Text, &t) == nil && t != "" {
				text.WriteString(t)
				parts = append(parts, CompatContentPart{Text: t})
			}
		case "thinking":
			var chunks []json.RawMessage
			if json.Unmarshal(p.Thinking, &chunks) != nil {
				continue
			}
			var reasoning strings.Builder
			for _, c := range chunks {
				var chunk struct {
					Type string `json:"type"`
					Text string `json:"text"`
				}
				if json.Unmarshal(c, &chunk) == nil && chunk.Type == "text" {
					reasoning.WriteString(chunk.Text)
				}
			}
			if reasoning.Len() > 0 {
				parts = append(parts, CompatContentPart{Reasoning: true, Text: reasoning.String()})
			}
		}
	}
	return text.String(), parts, nil
}

// ApplyOpenAICompatibleChatResponse fills the choice-derived fields of result
// from the raw wire body the way TS OpenAICompatibleChatLanguageModel.doGenerate
// does: content parts (text and thinking), reasoning_content ?? reasoning, tool
// calls (id falls back to a generated one, extra_content.google.thought_signature
// becomes tool call metadata), providerMetadata
// {metadataKey: {acceptedPredictionTokens, rejectedPredictionTokens}} and the
// response id / model / timestamp. A body without choices is an
// InvalidResponseDataError. Usage and the finish reason are left to the caller
// (providers map their own finish reasons).
func ApplyOpenAICompatibleChatResponse(result *types.GenerateResult, body []byte, headers map[string]string, metadataKey string) error {
	var wire struct {
		ID      string `json:"id"`
		Created *int64 `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			FinishReason *string `json:"finish_reason"`
			Message      struct {
				Content          json.RawMessage `json:"content"`
				ReasoningContent *string         `json:"reasoning_content"`
				Reasoning        *string         `json:"reasoning"`
				ToolCalls        []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
					ExtraContent *struct {
						Google *struct {
							ThoughtSignature *string `json:"thought_signature"`
						} `json:"google"`
					} `json:"extra_content"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage *struct {
			CompletionTokensDetails *struct {
				AcceptedPredictionTokens *int `json:"accepted_prediction_tokens"`
				RejectedPredictionTokens *int `json:"rejected_prediction_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return providererrors.NewInvalidResponseDataError(nil, err.Error())
	}
	if len(wire.Choices) == 0 {
		var data interface{}
		_ = json.Unmarshal(body, &data) //nolint:errcheck // best-effort error data
		return providererrors.NewInvalidResponseDataError(data, "Response did not contain any choices.")
	}
	choice := wire.Choices[0]

	var text strings.Builder
	var content []types.ContentPart
	parts, err := OpenAICompatContentParts(choice.Message.Content)
	if err != nil {
		return providererrors.NewInvalidResponseDataError(nil, err.Error())
	}
	for _, p := range parts {
		if p.Reasoning {
			content = append(content, types.ReasoningContent{Text: p.Text})
		} else {
			text.WriteString(p.Text)
		}
	}
	reasoning := choice.Message.ReasoningContent
	if reasoning == nil {
		reasoning = choice.Message.Reasoning
	}
	if reasoning != nil && *reasoning != "" {
		content = append(content, types.ReasoningContent{Text: *reasoning})
	}
	result.Text = text.String()
	result.Content = content

	result.ToolCalls = nil
	for _, tc := range choice.Message.ToolCalls {
		var args map[string]interface{}
		if tc.Function.Arguments != "" {
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args) //nolint:errcheck // invalid input stays empty like before
		}
		id := tc.ID
		if id == "" {
			id = GenerateID()
		}
		call := types.ToolCall{
			ID:        id,
			ToolName:  tc.Function.Name,
			Arguments: args,
			// Wire text keeps the key order TS keeps.
			RawArguments: tc.Function.Arguments,
		}
		if tc.ExtraContent != nil && tc.ExtraContent.Google != nil &&
			tc.ExtraContent.Google.ThoughtSignature != nil && *tc.ExtraContent.Google.ThoughtSignature != "" {
			sig := *tc.ExtraContent.Google.ThoughtSignature
			call.ThoughtSignature = sig
			call.ProviderMetadata = map[string]interface{}{metadataKey: map[string]interface{}{"thoughtSignature": sig}}
		}
		result.ToolCalls = append(result.ToolCalls, call)
	}

	meta := map[string]interface{}{}
	if wire.Usage != nil && wire.Usage.CompletionTokensDetails != nil {
		d := wire.Usage.CompletionTokensDetails
		if d.AcceptedPredictionTokens != nil {
			meta["acceptedPredictionTokens"] = *d.AcceptedPredictionTokens
		}
		if d.RejectedPredictionTokens != nil {
			meta["rejectedPredictionTokens"] = *d.RejectedPredictionTokens
		}
	}
	result.ProviderMetadata = map[string]interface{}{metadataKey: meta}

	rm := &types.ResponseMetadata{ID: wire.ID, ModelID: wire.Model, Headers: headers}
	if wire.Created != nil {
		rm.Timestamp = time.Unix(*wire.Created, 0).UTC()
	}
	result.ResponseMetadata = rm
	return nil
}

// OpenAICompatChatDecoder decodes a chat-completions response into Target. A
// response whose message content is an array of parts (TS
// openAICompatibleContentSchema) cannot decode into a string field; it is
// retried with each array flattened to its joined text parts. Pass it to
// DoJSONResponse as the result.
type OpenAICompatChatDecoder struct {
	Target interface{}
}

// UnmarshalJSON implements json.Unmarshaler.
func (d *OpenAICompatChatDecoder) UnmarshalJSON(b []byte) error {
	firstErr := json.Unmarshal(b, d.Target)
	if firstErr == nil {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var doc map[string]interface{}
	if err := dec.Decode(&doc); err != nil {
		return firstErr
	}
	choices, _ := doc["choices"].([]interface{})
	for _, c := range choices {
		choice, _ := c.(map[string]interface{})
		msg, _ := choice["message"].(map[string]interface{})
		arr, ok := msg["content"].([]interface{})
		if !ok {
			continue
		}
		raw, err := json.Marshal(arr)
		if err != nil {
			return firstErr
		}
		text, _, err := compatContentParts(raw)
		if err != nil {
			return firstErr
		}
		msg["content"] = text
	}
	flat, err := json.Marshal(doc)
	if err != nil {
		return firstErr
	}
	return json.Unmarshal(flat, d.Target)
}
