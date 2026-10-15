// Package anthropictools builds Go Anthropic provider tools from the JSON form
// of TS provider tool definitions (type "provider", id, args) for the TS
// replay tests of the providers that forward Anthropic tools.
package anthropictools

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

// ProviderTool maps a TS Anthropic provider tool id and args to the Go tool.
// Go identifies provider tools by their TS id in Name. ok is false for an id
// or args shape the Go constructors cannot express.
func ProviderTool(id string, args map[string]interface{}) (types.Tool, bool) {
	raw, _ := json.Marshal(args)
	decode := func(v interface{}) bool { return len(args) == 0 || json.Unmarshal(raw, v) == nil }
	switch id {
	case "anthropic.bash_20241022":
		return tools.Bash20241022(), len(args) == 0
	case "anthropic.bash_20250124":
		return tools.Bash20250124(), len(args) == 0
	case "anthropic.text_editor_20241022":
		return tools.TextEditor20241022(), len(args) == 0
	case "anthropic.text_editor_20250124":
		return tools.TextEditor20250124(), len(args) == 0
	case "anthropic.text_editor_20250429":
		return tools.TextEditor20250429(), len(args) == 0 //nolint:staticcheck // the TS id is still supported
	case "anthropic.memory_20250818":
		return tools.Memory20250818(), len(args) == 0
	case "anthropic.code_execution_20250522":
		return tools.CodeExecution20250522(), len(args) == 0
	case "anthropic.code_execution_20250825":
		return tools.CodeExecution20250825(), len(args) == 0
	case "anthropic.code_execution_20260120":
		return tools.CodeExecution20260120(), len(args) == 0
	case "anthropic.tool_search_regex_20251119":
		return tools.ToolSearchRegex20251119(), len(args) == 0
	case "anthropic.tool_search_bm25_20251119":
		return tools.ToolSearchBm2520251119(), len(args) == 0
	case "anthropic.web_search_20250305":
		var c tools.WebSearch20260209Config
		ok := decode(&c)
		return tools.WebSearch20250305(c), ok
	case "anthropic.web_search_20260209":
		var c tools.WebSearch20260209Config
		ok := decode(&c)
		return tools.WebSearch20260209(c), ok
	case "anthropic.web_search_20260318":
		var c tools.WebSearch20260318Config
		ok := decode(&c)
		return tools.WebSearch20260318(c), ok
	case "anthropic.web_fetch_20250910":
		var c tools.WebFetch20260209Config
		ok := decode(&c)
		return tools.WebFetch20250910(c), ok
	case "anthropic.web_fetch_20260209":
		var c tools.WebFetch20260209Config
		ok := decode(&c)
		return tools.WebFetch20260209(c), ok
	case "anthropic.web_fetch_20260318":
		var c tools.WebFetch20260318Config
		ok := decode(&c)
		return tools.WebFetch20260318(c), ok
	case "anthropic.advisor_20260301":
		var c tools.Advisor20260301Args
		ok := decode(&c)
		return tools.Advisor20260301(c), ok
	case "anthropic.computer_20241022", "anthropic.computer_20250124":
		var c tools.Computer20241022Args
		ok := decode(&c)
		if id == "anthropic.computer_20241022" {
			return tools.Computer20241022(c), ok
		}
		return tools.Computer20250124(c), ok
	case "anthropic.computer_20251124":
		var c tools.Computer20251124Args
		ok := decode(&c)
		return tools.Computer20251124(c), ok
	case "anthropic.text_editor_20250728":
		var c tools.TextEditor20250728Args
		ok := decode(&c)
		return tools.TextEditor20250728(c), ok
	case "anthropic.computer_toolset_20260801":
		return tools.ComputerToolset20260801(tools.ComputerToolset20260801Config{}), len(args) == 0
	}
	return types.Tool{}, false
}
