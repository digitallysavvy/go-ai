package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

// restoreProviderToolOptions mirrors TS prepareTools, which builds each
// Anthropic provider tool from tool.id and tool.args. The Go constructors
// carry a typed ProviderOptions as well; a provider tool that arrives without
// it (for example one rebuilt from its serialized form by
// workflow.ResolveSerializableTools, which keeps only id and args) gets it
// back from ProviderArgs, so it is sent like the constructor's tool instead of
// being dropped as unsupported. Tools that already carry Anthropic options and
// unknown ids are returned as is. The tool's Name (TS: its key in the tools
// record) may differ from its id.
func restoreProviderToolOptions(t types.Tool) (types.Tool, error) {
	// A provider tool (TS shape), or a tool literal named after an Anthropic
	// tool id: Anthropic function names cannot contain ".", so prepareTools
	// treats every "anthropic.*" name as a provider tool id.
	isProviderTool := t.Type == types.ToolTypeProviderDefined && t.ProviderID != ""
	if !isProviderTool && !strings.HasPrefix(t.Name, "anthropic.") {
		return t, nil
	}
	id := ProviderToolID(t)
	if _, ok := t.ProviderOptions.(anthropicAPIMapper); ok {
		return t, nil
	}
	if _, ok := t.ProviderOptions.(*ToolOptions); ok {
		return t, nil
	}
	raw, err := json.Marshal(t.ProviderArgs)
	if err != nil {
		return t, &providererrors.TypeValidationError{Field: "args", Message: err.Error(), Cause: err}
	}
	decode := func(v interface{}) error {
		if len(t.ProviderArgs) == 0 {
			return nil
		}
		if err := json.Unmarshal(raw, v); err != nil {
			return &providererrors.TypeValidationError{
				Field:   "args",
				Message: fmt.Sprintf("invalid args for provider tool %s: %v", id, err),
				Cause:   err,
			}
		}
		return nil
	}
	var rebuilt types.Tool
	switch id {
	case "anthropic.web_search_20250305", "anthropic.web_search_20260209":
		var c tools.WebSearch20260209Config
		if err := decode(&c); err != nil {
			return t, err
		}
		if id == "anthropic.web_search_20250305" {
			rebuilt = tools.WebSearch20250305(c)
		} else {
			rebuilt = tools.WebSearch20260209(c)
		}
	case "anthropic.web_search_20260318":
		var c tools.WebSearch20260318Config
		if err := decode(&c); err != nil {
			return t, err
		}
		rebuilt = tools.WebSearch20260318(c)
	case "anthropic.web_fetch_20250910", "anthropic.web_fetch_20260209":
		var c tools.WebFetch20260209Config
		if err := decode(&c); err != nil {
			return t, err
		}
		if id == "anthropic.web_fetch_20250910" {
			rebuilt = tools.WebFetch20250910(c)
		} else {
			rebuilt = tools.WebFetch20260209(c)
		}
	case "anthropic.web_fetch_20260318":
		var c tools.WebFetch20260318Config
		if err := decode(&c); err != nil {
			return t, err
		}
		rebuilt = tools.WebFetch20260318(c)
	case "anthropic.computer_20241022", "anthropic.computer_20250124":
		var c tools.Computer20241022Args
		if err := decode(&c); err != nil {
			return t, err
		}
		if id == "anthropic.computer_20241022" {
			rebuilt = tools.Computer20241022(c)
		} else {
			rebuilt = tools.Computer20250124(c)
		}
	case "anthropic.computer_20251124":
		var c tools.Computer20251124Args
		if err := decode(&c); err != nil {
			return t, err
		}
		rebuilt = tools.Computer20251124(c)
	case "anthropic.computer_toolset_20260801":
		var c tools.ComputerToolset20260801Config
		if err := decode(&c); err != nil {
			return t, err
		}
		rebuilt = tools.ComputerToolset20260801(c)
	case "anthropic.text_editor_20250728":
		var c tools.TextEditor20250728Args
		if err := decode(&c); err != nil {
			return t, err
		}
		rebuilt = tools.TextEditor20250728(c)
	case "anthropic.advisor_20260301":
		// advisor_20260301ArgsSchema: model is a required string (the
		// constructor's default model is a Go convenience of the constructor).
		if _, ok := t.ProviderArgs["model"].(string); !ok {
			return t, &providererrors.TypeValidationError{
				Field:   "args.model",
				Message: fmt.Sprintf("invalid args for provider tool %s: model must be a string", id),
			}
		}
		var c tools.Advisor20260301Args
		if err := decode(&c); err != nil {
			return t, err
		}
		rebuilt = tools.Advisor20260301(c)
	default:
		// The argument-free tools (bash, the older text editors, code
		// execution, memory, tool search) are serialized from their name.
		return t, nil
	}
	t.ProviderOptions = rebuilt.ProviderOptions
	return t, nil
}

// ProviderToolID returns the TS tool id that selects an Anthropic provider
// tool: ProviderID for a provider tool (Type "provider"), otherwise the tool
// Name, since the constructors in pkg/providers/anthropic/tools name each tool
// after its id ("anthropic.bash_20250124"). TS switches on tool.id and lets
// the caller choose the name (the tools record key), so a provider tool
// renamed to, for example, "bash" is still that provider tool.
func ProviderToolID(t types.Tool) string {
	if t.Type == types.ToolTypeProviderDefined && t.ProviderID != "" {
		return t.ProviderID
	}
	return t.Name
}

// anthropicToolNames mirrors the TS createToolNameMapping with the Anthropic
// providerToolNames table, plus getAnthropicToolsetNames: custom tool names
// (the tool Name) of the provider tools in the request map to and from their
// Anthropic wire names, found by tool id.
type anthropicToolNames struct {
	toProvider map[string]string
	toCustom   map[string]string
	toolsets   map[string]string
}

func newAnthropicToolNames(tools []types.Tool) anthropicToolNames {
	n := anthropicToolNames{toProvider: map[string]string{}, toCustom: map[string]string{}, toolsets: map[string]string{}}
	for _, t := range tools {
		id := ProviderToolID(t)
		if id == "anthropic.computer_toolset_20260801" {
			n.toolsets[t.Name] = "computer"
		}
		if wire := prompt.AnthropicProviderToolName(id); wire != id {
			n.toProvider[t.Name] = wire
			n.toCustom[wire] = t.Name
		}
	}
	return n
}

// providerName maps a custom tool name to its Anthropic wire name. Names of
// tools not in the request fall back to the id table, so history that names
// a provider tool by its id ("anthropic.bash_20250124") still converts.
func (n anthropicToolNames) providerName(name string) string {
	if wire, ok := n.toProvider[name]; ok {
		return wire
	}
	return prompt.AnthropicProviderToolName(name)
}

// customNames is the wire name -> custom name map the response parsers use
// (nil when no provider tool is mapped).
func (n anthropicToolNames) customNames() map[string]string {
	if len(n.toCustom) == 0 {
		return nil
	}
	return n.toCustom
}

// PreparedProviderTools is the part of the Anthropic tool preparation a host
// that forwards Anthropic provider tools through another API uses (TS
// prepareTools' toolChoice, toolWarnings and betas, as
// amazon-bedrock-prepare-tools.ts reads them).
type PreparedProviderTools struct {
	// ToolChoice is the Anthropic tool_choice, nil when none is sent.
	ToolChoice map[string]interface{}
	Warnings   []types.Warning
	Betas      []string
}

// PrepareProviderTools runs the Anthropic tool preparation the way Amazon
// Bedrock calls it (TS prepareAnthropicTools with supportsStructuredOutput and
// supportsStrictTools false): it validates the provider tool args (a
// *TypeValidationError on invalid args), warns about provider tools it does
// not know and computes the betas and the tool_choice. toolChoice nil means
// none was given.
func PrepareProviderTools(tools []types.Tool, toolChoice *types.ToolChoice, disableParallelToolUse *bool, rejectsForcedToolUse bool) (PreparedProviderTools, error) {
	prepared := prepareTools(prepareToolsOptions{
		tools:                  tools,
		toolChoice:             toolChoice,
		disableParallelToolUse: disableParallelToolUse,
		rejectsForcedToolUse:   rejectsForcedToolUse,
	})
	if prepared.err != nil {
		return PreparedProviderTools{}, prepared.err
	}
	return PreparedProviderTools{ToolChoice: prepared.toolChoice, Warnings: prepared.warnings, Betas: prepared.betas}, nil
}
