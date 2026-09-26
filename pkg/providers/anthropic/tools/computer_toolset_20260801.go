package tools

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ComputerToolset20260801Member is a member tool of the computer toolset. Each
// member is returned by the API as its own tool_use block; the Go SDK exposes
// it as the "action" of a single computer tool call.
type ComputerToolset20260801Member string

// Member tool names for the computer toolset (TS computerToolset_20260801Members).
const (
	ComputerToolsetScreenshot     ComputerToolset20260801Member = "screenshot"
	ComputerToolsetZoom           ComputerToolset20260801Member = "zoom"
	ComputerToolsetLeftClick      ComputerToolset20260801Member = "left_click"
	ComputerToolsetRightClick     ComputerToolset20260801Member = "right_click"
	ComputerToolsetMiddleClick    ComputerToolset20260801Member = "middle_click"
	ComputerToolsetDoubleClick    ComputerToolset20260801Member = "double_click"
	ComputerToolsetTripleClick    ComputerToolset20260801Member = "triple_click"
	ComputerToolsetLeftClickDrag  ComputerToolset20260801Member = "left_click_drag"
	ComputerToolsetMouseMove      ComputerToolset20260801Member = "mouse_move"
	ComputerToolsetLeftMouseDown  ComputerToolset20260801Member = "left_mouse_down"
	ComputerToolsetLeftMouseUp    ComputerToolset20260801Member = "left_mouse_up"
	ComputerToolsetCursorPosition ComputerToolset20260801Member = "cursor_position"
	ComputerToolsetScroll         ComputerToolset20260801Member = "scroll"
	ComputerToolsetType           ComputerToolset20260801Member = "type"
	ComputerToolsetKey            ComputerToolset20260801Member = "key"
	ComputerToolsetHoldKey        ComputerToolset20260801Member = "hold_key"
	ComputerToolsetWait           ComputerToolset20260801Member = "wait"
)

// ComputerToolset20260801MemberConfig configures a single member tool: whether
// Claude can use it (default true) and whether it is only loaded once
// discovered through tool search (default false).
type ComputerToolset20260801MemberConfig struct {
	// Enabled controls whether Claude can use this member tool. Default: true.
	Enabled *bool

	// DeferLoading controls whether this member tool is only loaded once
	// discovered through tool search. Default: false.
	DeferLoading *bool
}

// ComputerToolset20260801Config configures the computer_toolset_20260801
// provider tool.
type ComputerToolset20260801Config struct {
	// Configs holds per-member configuration. Members left out keep their
	// defaults (all members enabled, including zoom).
	Configs map[ComputerToolset20260801Member]ComputerToolset20260801MemberConfig
}

// computerToolset20260801Opts stores the tool configuration and implements
// ToAnthropicAPIMap for the Anthropic tool converter.
type computerToolset20260801Opts struct {
	Config ComputerToolset20260801Config
}

// ToAnthropicAPIMap returns the Anthropic API representation of this tool.
// Unlike other computer tools, computer_toolset_20260801 has no "name" field
// (TS anthropic-prepare-tools.ts): {type:'computer_toolset_20260801', configs?}.
func (o *computerToolset20260801Opts) ToAnthropicAPIMap() map[string]interface{} {
	m := map[string]interface{}{
		"type": "computer_toolset_20260801",
	}
	if len(o.Config.Configs) > 0 {
		configs := make(map[string]interface{}, len(o.Config.Configs))
		for member, cfg := range o.Config.Configs {
			entry := map[string]interface{}{}
			if cfg.Enabled != nil {
				entry["enabled"] = *cfg.Enabled
			}
			if cfg.DeferLoading != nil {
				entry["defer_loading"] = *cfg.DeferLoading
			}
			configs[string(member)] = entry
		}
		m["configs"] = configs
	}
	return m
}

// ComputerToolset20260801 creates an Anthropic computer toolset provider tool
// (version 2026-08-01). Each member action Claude invokes is returned by the
// API as its own tool_use block with toolset_name "computer"; the Go SDK
// surfaces it as a single "computer" tool call whose input carries
// {action: <member>, ...}.
//
// Tool ID: "anthropic.computer_toolset_20260801"
//
// Example:
//
//	disabled := false
//	toolsetTool := tools.ComputerToolset20260801(tools.ComputerToolset20260801Config{
//	    Configs: map[tools.ComputerToolset20260801Member]tools.ComputerToolset20260801MemberConfig{
//	        tools.ComputerToolsetZoom: {Enabled: &disabled},
//	    },
//	})
func ComputerToolset20260801(config ComputerToolset20260801Config) types.Tool {
	return types.Tool{
		Name:        "anthropic.computer_toolset_20260801",
		Description: "Computer use toolset (Anthropic, version 2026-08-01). Each member action is executed by Anthropic's servers.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type":        "string",
					"description": "The member tool that Claude invoked.",
				},
			},
			"required": []string{"action"},
		},
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("computer_toolset_20260801 is executed by the Anthropic provider, not locally")
		},
		ProviderExecuted: true,
		ProviderOptions:  &computerToolset20260801Opts{Config: config},
	}
}
