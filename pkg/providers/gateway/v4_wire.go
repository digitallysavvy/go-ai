package gateway

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// The Gateway speaks LanguageModelV4 on the wire (the
// ai-language-model-specification-version header is "4"): generate results
// carry a content array and {unified, raw} finish reasons, and usage is
// {inputTokens: {...}, outputTokens: {...}, raw}. The older flat shapes
// (text / finishReason string / promptTokens) are still accepted.

// v4FinishReason decodes {unified, raw} or a bare string.
type v4FinishReason struct {
	Unified string
	Raw     string
}

func (f *v4FinishReason) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		f.Unified, f.Raw = s, s
		return nil
	}
	var o struct {
		Unified string  `json:"unified"`
		Raw     *string `json:"raw"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return err
	}
	f.Unified = o.Unified
	if o.Raw != nil {
		f.Raw = *o.Raw
	}
	return nil
}

func (f v4FinishReason) toTypes() types.FinishReason {
	switch f.Unified {
	case "stop", "length", "content-filter", "tool-calls", "error", "other":
		return types.FinishReason(f.Unified)
	case "":
		return ""
	}
	return types.FinishReasonOther
}

// v4Usage decodes the V4 usage object (or the older flat token counts).
type v4Usage struct {
	usage types.Usage
	set   bool
}

func (u *v4Usage) UnmarshalJSON(b []byte) error {
	var o struct {
		InputTokens  json.RawMessage        `json:"inputTokens"`
		OutputTokens json.RawMessage        `json:"outputTokens"`
		Raw          map[string]interface{} `json:"raw"`

		PromptTokens     *int64 `json:"promptTokens"`
		CompletionTokens *int64 `json:"completionTokens"`
		TotalTokens      *int64 `json:"totalTokens"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return err
	}
	u.set = true
	var out types.Usage
	var in struct {
		Total      *int64 `json:"total"`
		NoCache    *int64 `json:"noCache"`
		CacheRead  *int64 `json:"cacheRead"`
		CacheWrite *int64 `json:"cacheWrite"`
	}
	var outTok struct {
		Total     *int64 `json:"total"`
		Text      *int64 `json:"text"`
		Reasoning *int64 `json:"reasoning"`
	}
	inObj := len(o.InputTokens) > 0 && o.InputTokens[0] == '{' && json.Unmarshal(o.InputTokens, &in) == nil
	outObj := len(o.OutputTokens) > 0 && o.OutputTokens[0] == '{' && json.Unmarshal(o.OutputTokens, &outTok) == nil
	if inObj {
		out.InputTokens = in.Total
		out.InputDetails = &types.InputTokenDetails{NoCacheTokens: in.NoCache, CacheReadTokens: in.CacheRead, CacheWriteTokens: in.CacheWrite}
	} else {
		var n int64
		if len(o.InputTokens) > 0 && json.Unmarshal(o.InputTokens, &n) == nil {
			out.InputTokens = &n
		} else {
			out.InputTokens = o.PromptTokens
		}
	}
	if outObj {
		out.OutputTokens = outTok.Total
		out.OutputDetails = &types.OutputTokenDetails{TextTokens: outTok.Text, ReasoningTokens: outTok.Reasoning}
	} else {
		var n int64
		if len(o.OutputTokens) > 0 && json.Unmarshal(o.OutputTokens, &n) == nil {
			out.OutputTokens = &n
		} else {
			out.OutputTokens = o.CompletionTokens
		}
	}
	out.TotalTokens = o.TotalTokens
	if out.TotalTokens == nil && out.InputTokens != nil && out.OutputTokens != nil {
		t := *out.InputTokens + *out.OutputTokens
		out.TotalTokens = &t
	}
	out.Raw = o.Raw
	u.usage = out
	return nil
}

// v4Part is one LanguageModelV4Content / stream part.
type v4Part struct {
	Type             string          `json:"type"`
	ID               string          `json:"id"`
	Text             string          `json:"text"`
	Delta            string          `json:"delta"`
	ToolCallID       string          `json:"toolCallId"`
	ToolName         string          `json:"toolName"`
	Input            json.RawMessage `json:"input"`
	Result           json.RawMessage `json:"result"`
	IsError          bool            `json:"isError"`
	Preliminary      bool            `json:"preliminary"`
	ProviderExecuted bool            `json:"providerExecuted"`
	Dynamic          bool            `json:"dynamic"`
	Title            string          `json:"title"`
	ApprovalID       string          `json:"approvalId"`
	Kind             string          `json:"kind"`
	MediaType        string          `json:"mediaType"`
	Data             json.RawMessage `json:"data"`
	SourceType       string          `json:"sourceType"`
	URL              string          `json:"url"`
	Filename         string          `json:"filename"`
	ProviderMetadata json.RawMessage `json:"providerMetadata"`
	Citations        []v4Citation    `json:"citations"`
}

type v4Citation struct {
	Source     json.RawMessage `json:"source"`
	StartIndex *int            `json:"startIndex"`
	EndIndex   *int            `json:"endIndex"`
	CitedText  string          `json:"citedText"`
}

func (p v4Part) sourceContent() types.SourceContent {
	return types.SourceContent{
		SourceType:       p.SourceType,
		ID:               p.ID,
		URL:              p.URL,
		MediaType:        p.MediaType,
		Title:            p.Title,
		Filename:         p.Filename,
		ProviderMetadata: p.ProviderMetadata,
	}
}

func (p v4Part) citations() []types.Citation {
	var out []types.Citation
	for _, c := range p.Citations {
		var src v4Part
		if json.Unmarshal(c.Source, &src) != nil {
			continue
		}
		out = append(out, types.Citation{Source: src.sourceContent(), StartIndex: c.StartIndex, EndIndex: c.EndIndex, CitedText: c.CitedText})
	}
	return out
}

// fileData converts a V4 file data union ({type: data|url, ...}) or a bare
// base64 string.
func decodeV4FileData(raw json.RawMessage, mediaType string) types.FileData {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return types.FileData{Type: types.FileDataTypeData, DataString: s, MediaType: mediaType}
	}
	var fd struct {
		Type string `json:"type"`
		Data string `json:"data"`
		URL  string `json:"url"`
	}
	if json.Unmarshal(raw, &fd) != nil {
		return types.FileData{}
	}
	if fd.Type == "url" {
		return types.FileData{Type: types.FileDataTypeURL, URL: fd.URL, MediaType: mediaType}
	}
	return types.FileData{Type: types.FileDataTypeData, DataString: fd.Data, MediaType: mediaType}
}

func (p v4Part) generatedFile() *types.GeneratedFileContent {
	fd := decodeV4FileData(p.Data, p.MediaType)
	f := &types.GeneratedFileContent{MediaType: p.MediaType, FileData: fd, ProviderMetadata: p.ProviderMetadata}
	if fd.Type == types.FileDataTypeURL {
		f.URL = fd.URL
	}
	return f
}

func (p v4Part) toolCall() types.ToolCall {
	input := ""
	if len(p.Input) > 0 {
		var s string
		if json.Unmarshal(p.Input, &s) == nil {
			input = s
		} else {
			input = string(p.Input)
		}
	}
	var args map[string]interface{}
	_ = json.Unmarshal([]byte(input), &args)
	tc := types.ToolCall{
		ID:               p.ToolCallID,
		ToolName:         p.ToolName,
		Arguments:        args,
		RawArguments:     input,
		ProviderExecuted: p.ProviderExecuted,
		Dynamic:          p.Dynamic,
	}
	if len(p.ProviderMetadata) > 0 {
		var md map[string]interface{}
		if json.Unmarshal(p.ProviderMetadata, &md) == nil {
			tc.ProviderMetadata = md
		}
	}
	return tc
}

func (p v4Part) toolResult() types.ToolResult {
	var result interface{}
	_ = json.Unmarshal(p.Result, &result)
	tr := types.ToolResult{
		ToolCallID: p.ToolCallID, ToolName: p.ToolName, Result: result, Dynamic: p.Dynamic,
		Preliminary: p.Preliminary,
		// A tool-result part of a language model stream is provider-executed.
		ProviderExecuted: true,
	}
	if len(p.ProviderMetadata) > 0 {
		var md map[string]interface{}
		if json.Unmarshal(p.ProviderMetadata, &md) == nil {
			tr.ProviderMetadata = md
		}
	}
	if p.IsError {
		if s, ok := result.(string); ok {
			tr.Error = errors.New(s)
		} else if b, err := json.Marshal(result); err == nil {
			tr.Error = errors.New(string(b))
		}
	}
	return tr
}

// decodeV4Content converts a V4 generate content part. Tool calls are also
// returned separately so GenerateResult.ToolCalls can be populated.
func decodeV4Content(p v4Part) (types.ContentPart, *types.ToolCall) {
	switch p.Type {
	case "text":
		return types.TextContent{Text: p.Text, ProviderMetadata: p.ProviderMetadata, Citations: p.citations()}, nil
	case "reasoning":
		return types.ReasoningContent{Text: p.Text, ProviderMetadata: p.ProviderMetadata}, nil
	case "file":
		return *p.generatedFile(), nil
	case "reasoning-file":
		fd := decodeV4FileData(p.Data, p.MediaType)
		return types.ReasoningFileContent{MediaType: p.MediaType, FileData: fd, ProviderMetadata: p.ProviderMetadata}, nil
	case "source":
		return p.sourceContent(), nil
	case "custom":
		return types.CustomContent{Kind: p.Kind, ProviderMetadata: p.ProviderMetadata}, nil
	case "tool-approval-request":
		return types.ToolApprovalRequestContent{ApprovalID: p.ApprovalID, ToolCallID: p.ToolCallID}, nil
	case "tool-call":
		tc := p.toolCall()
		return types.ToolCallContent{
			ToolCallID:       tc.ID,
			ToolName:         tc.ToolName,
			Input:            tc.RawArguments,
			Arguments:        tc.Arguments,
			ProviderExecuted: tc.ProviderExecuted,
			Dynamic:          tc.Dynamic,
			ProviderMetadata: p.ProviderMetadata,
		}, &tc
	case "tool-result":
		var result interface{}
		_ = json.Unmarshal(p.Result, &result)
		tr := types.ToolResultContent{
			ToolCallID:       p.ToolCallID,
			ToolName:         p.ToolName,
			Result:           result,
			ProviderExecuted: true,
			Preliminary:      p.Preliminary,
			Dynamic:          p.Dynamic,
			ProviderMetadata: p.ProviderMetadata,
		}
		if p.IsError {
			if s, ok := result.(string); ok {
				tr.Error = s
			} else if b, err := json.Marshal(result); err == nil {
				tr.Error = string(b)
			}
		}
		return tr, nil
	}
	return nil, nil
}

// decodeV4GenerateResult builds a GenerateResult from the gateway's V4 JSON.
func decodeV4GenerateResult(body []byte) (*types.GenerateResult, error) {
	var wire struct {
		Content          rawContentList         `json:"content"`
		Text             string                 `json:"text"`
		FinishReason     v4FinishReason         `json:"finishReason"`
		Usage            v4Usage                `json:"usage"`
		Warnings         json.RawMessage        `json:"warnings"`
		ProviderMetadata map[string]interface{} `json:"providerMetadata"`
		Response         *struct {
			ID        string `json:"id"`
			ModelID   string `json:"modelId"`
			Timestamp string `json:"timestamp"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, err
	}
	result := &types.GenerateResult{
		FinishReason:     wire.FinishReason.toTypes(),
		RawFinishReason:  wire.FinishReason.Raw,
		Usage:            wire.Usage.usage,
		ProviderMetadata: wire.ProviderMetadata,
		Warnings:         parseGatewayWarnings(wire.Warnings),
	}
	for _, raw := range wire.Content {
		var p v4Part
		if json.Unmarshal(raw, &p) != nil {
			continue
		}
		part, tc := decodeV4Content(p)
		if part == nil {
			continue
		}
		result.Content = append(result.Content, part)
		if t, ok := part.(types.TextContent); ok {
			result.Text += t.Text
		}
		if tc != nil {
			result.ToolCalls = append(result.ToolCalls, *tc)
		}
	}
	if len(wire.Content) == 0 && wire.Text != "" { // older flat shape
		result.Text = wire.Text
	}
	if wire.Response != nil {
		md := &types.ResponseMetadata{ID: wire.Response.ID, ModelID: wire.Response.ModelID}
		if t, err := time.Parse(time.RFC3339, wire.Response.Timestamp); err == nil {
			md.Timestamp = t
		}
		result.ResponseMetadata = md
	}
	return result, nil
}

// rawContentList decodes the V4 content array and, like TS (which passes the
// response through untouched), tolerates a single content object.
type rawContentList []json.RawMessage

func (c *rawContentList) UnmarshalJSON(b []byte) error {
	var list []json.RawMessage
	if err := json.Unmarshal(b, &list); err == nil {
		*c = list
		return nil
	}
	var one json.RawMessage
	if err := json.Unmarshal(b, &one); err != nil {
		return err
	}
	*c = rawContentList{one}
	return nil
}
