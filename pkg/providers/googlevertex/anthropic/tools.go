package anthropic

import (
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	anthropictools "github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

type Computer20241022Args = anthropictools.Computer20241022Args
type TextEditor20250728Args = anthropictools.TextEditor20250728Args
type WebSearch20250305Config = anthropictools.WebSearch20260209Config

// GoogleVertexAnthropicTools exposes the Anthropic tools supported by Vertex AI.
// It intentionally excludes Anthropic tools that the current TypeScript SDK does
// not expose on googleVertexAnthropic.tools, such as code execution and memory.
var GoogleVertexAnthropicTools = struct {
	Bash20241022            func() types.Tool
	Bash20250124            func() types.Tool
	TextEditor20241022      func() types.Tool
	TextEditor20250124      func() types.Tool
	TextEditor20250429      func() types.Tool
	TextEditor20250728      func(TextEditor20250728Args) types.Tool
	Computer20241022        func(Computer20241022Args) types.Tool
	WebSearch20250305       func(WebSearch20250305Config) types.Tool
	ToolSearchRegex20251119 func() types.Tool
	ToolSearchBm25_20251119 func() types.Tool
	ToolSearchBm2520251119  func() types.Tool
}{
	Bash20241022:            anthropictools.Bash20241022,
	Bash20250124:            anthropictools.Bash20250124,
	TextEditor20241022:      anthropictools.TextEditor20241022,
	TextEditor20250124:      anthropictools.TextEditor20250124,
	TextEditor20250429:      anthropictools.TextEditor20250429,
	TextEditor20250728:      anthropictools.TextEditor20250728,
	Computer20241022:        anthropictools.Computer20241022,
	WebSearch20250305:       anthropictools.WebSearch20250305,
	ToolSearchRegex20251119: anthropictools.ToolSearchRegex20251119,
	ToolSearchBm25_20251119: anthropictools.ToolSearchBm2520251119,
	ToolSearchBm2520251119:  anthropictools.ToolSearchBm2520251119,
}

func Bash20241022() types.Tool { return anthropictools.Bash20241022() }

func Bash20250124() types.Tool { return anthropictools.Bash20250124() }

func TextEditor20241022() types.Tool { return anthropictools.TextEditor20241022() }

func TextEditor20250124() types.Tool { return anthropictools.TextEditor20250124() }

func TextEditor20250429() types.Tool { return anthropictools.TextEditor20250429() }

func TextEditor20250728(args TextEditor20250728Args) types.Tool {
	return anthropictools.TextEditor20250728(args)
}

func Computer20241022(args Computer20241022Args) types.Tool {
	return anthropictools.Computer20241022(args)
}

func WebSearch20250305(config WebSearch20250305Config) types.Tool {
	return anthropictools.WebSearch20250305(config)
}

func ToolSearchRegex20251119() types.Tool {
	return anthropictools.ToolSearchRegex20251119()
}

func ToolSearchBm25_20251119() types.Tool {
	return anthropictools.ToolSearchBm2520251119()
}

func ToolSearchBm2520251119() types.Tool {
	return anthropictools.ToolSearchBm2520251119()
}
