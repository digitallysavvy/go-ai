package anthropic

import (
	"encoding/json"
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

// restoreProviderToolOptions mirrors TS prepareTools, which builds each
// Anthropic provider tool from tool.id and tool.args. The Go constructors
// carry a typed ProviderOptions as well; a provider tool that arrives without
// it (for example one rebuilt from its serialized form by
// workflow.ResolveSerializableTools, which keeps only id and args) gets it
// back from ProviderArgs, so it is sent like the constructor's tool instead of
// being dropped as unsupported. Tools that already carry Anthropic options,
// tools named differently from their id, and unknown ids are returned as is.
func restoreProviderToolOptions(t types.Tool) (types.Tool, error) {
	if t.Type != types.ToolTypeProviderDefined || t.ProviderID == "" || t.Name != t.ProviderID {
		return t, nil
	}
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
				Message: fmt.Sprintf("invalid args for provider tool %s: %v", t.ProviderID, err),
				Cause:   err,
			}
		}
		return nil
	}
	var rebuilt types.Tool
	switch t.ProviderID {
	case "anthropic.web_search_20250305", "anthropic.web_search_20260209":
		var c tools.WebSearch20260209Config
		if err := decode(&c); err != nil {
			return t, err
		}
		if t.ProviderID == "anthropic.web_search_20250305" {
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
		if t.ProviderID == "anthropic.web_fetch_20250910" {
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
		if t.ProviderID == "anthropic.computer_20241022" {
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
