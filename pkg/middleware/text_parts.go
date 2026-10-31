package middleware

import (
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// mapGenerateTextParts applies fn to every text part of a generate result,
// mirroring the per-part loops of the TS wrapGenerate implementations of
// extractJsonMiddleware and extractReasoningMiddleware (non-text parts pass
// through in order). Providers that only fill GenerateResult.Text, with no
// text part in Content, are treated as having a single text part. Text is
// recomputed from the resulting text parts.
func mapGenerateTextParts(result *types.GenerateResult, fn func(types.TextContent) []types.ContentPart) {
	hasText := false
	for _, part := range result.Content {
		switch p := part.(type) {
		case types.TextContent:
			hasText = true
		case *types.TextContent:
			hasText = hasText || p != nil
		}
	}

	in := result.Content
	if !hasText {
		if result.Text == "" {
			return
		}
		in = append([]types.ContentPart{types.TextContent{Text: result.Text}}, result.Content...)
	}

	out := make([]types.ContentPart, 0, len(in))
	changedShape := false
	for _, part := range in {
		var text types.TextContent
		switch p := part.(type) {
		case types.TextContent:
			text = p
		case *types.TextContent:
			if p == nil {
				out = append(out, part)
				continue
			}
			text = *p
		default:
			out = append(out, part)
			continue
		}
		replaced := fn(text)
		if len(replaced) != 1 {
			changedShape = true
		} else if _, ok := replaced[0].(types.TextContent); !ok {
			changedShape = true
		}
		out = append(out, replaced...)
	}

	var sb strings.Builder
	for _, part := range out {
		if t, ok := part.(types.TextContent); ok {
			sb.WriteString(t.Text)
		}
	}
	result.Text = sb.String()
	if hasText || changedShape {
		result.Content = out
	}
}
