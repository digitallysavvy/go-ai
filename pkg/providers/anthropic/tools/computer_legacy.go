package tools

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Computer20241022Args represents the configuration arguments for the computer_20241022 tool.
type Computer20241022Args struct {
	// DisplayWidthPx is the width of the display being controlled in pixels.
	DisplayWidthPx int

	// DisplayHeightPx is the height of the display being controlled in pixels.
	DisplayHeightPx int

	// DisplayNumber is the display number to control (X11 environments only, optional).
	DisplayNumber *int
}

// computer20241022Opts implements anthropicAPIMapper for computer_20241022.
type computer20241022Opts struct {
	Args    Computer20241022Args
	APIType string
}

// ToAnthropicAPIMap returns the full Anthropic API representation of this computer tool.
func (o *computer20241022Opts) ToAnthropicAPIMap() map[string]interface{} {
	m := map[string]interface{}{
		"type":              o.APIType,
		"name":              "computer",
		"display_width_px":  o.Args.DisplayWidthPx,
		"display_height_px": o.Args.DisplayHeightPx,
	}
	if o.Args.DisplayNumber != nil {
		m["display_number"] = *o.Args.DisplayNumber
	}
	return m
}

// Computer20241022 creates a computer use tool that enables Claude to control computers
// through mouse, keyboard, and screenshot capabilities.
//
// Supported actions: key, type, mouse_move, left_click, left_click_drag, right_click,
// middle_click, double_click, screenshot, cursor_position.
//
// Requires beta header: computer-use-2024-10-22 (injected automatically).
//
// Supported models: Claude Sonnet 3.5
//
// Example:
//
//	tool := anthropicTools.Computer20241022(tools.Computer20241022Args{
//	    DisplayWidthPx: 1920, DisplayHeightPx: 1080,
//	})
func Computer20241022(args Computer20241022Args) types.Tool {
	return types.Tool{
		Name:            "anthropic.computer_20241022",
		Type:            types.ToolTypeProviderDefined,
		ProviderID:      "anthropic.computer_20241022",
		ProviderArgs:    computerLegacyProviderArgs(args),
		Description:     buildLegacyComputerDescription(args.DisplayWidthPx, args.DisplayHeightPx, args.DisplayNumber),
		Parameters:      buildLegacyComputerSchema(),
		ProviderOptions: &computer20241022Opts{Args: args, APIType: "computer_20241022"},
	}
}

// Computer20250124 creates a computer use tool that enables Claude to control computers
// through mouse, keyboard, and screenshot capabilities.
//
// Supported actions: key, hold_key, type, cursor_position, mouse_move, left_mouse_down,
// left_mouse_up, left_click, left_click_drag, right_click, middle_click, double_click,
// triple_click, scroll, wait, screenshot.
//
// Requires beta header: computer-use-2025-01-24 (injected automatically).
//
// Supported models: Claude Sonnet 3.7
//
// Example:
//
//	tool := anthropicTools.Computer20250124(tools.Computer20241022Args{
//	    DisplayWidthPx: 1920, DisplayHeightPx: 1080,
//	})
func Computer20250124(args Computer20241022Args) types.Tool {
	return types.Tool{
		Name:            "anthropic.computer_20250124",
		Type:            types.ToolTypeProviderDefined,
		ProviderID:      "anthropic.computer_20250124",
		ProviderArgs:    computerLegacyProviderArgs(args),
		Description:     buildLegacyComputerDescription(args.DisplayWidthPx, args.DisplayHeightPx, args.DisplayNumber),
		Parameters:      buildComputer20250124Schema(),
		ProviderOptions: &computer20241022Opts{Args: args, APIType: "computer_20250124"},
	}
}

// buildComputer20250124Schema is TS's computer_20250124 input schema: the
// 20251124 one without the zoom action and its region argument.
func buildComputer20250124Schema() map[string]interface{} {
	schema := buildComputer20251124Schema()
	props := schema["properties"].(map[string]interface{})
	delete(props, "region")
	action := props["action"].(map[string]interface{})
	enum := action["enum"].([]string)
	action["enum"] = enum[:len(enum)-1] // zoom is last
	return schema
}

func buildLegacyComputerSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type": "string",
				"enum": []string{
					"key", "type", "mouse_move", "left_click", "left_click_drag",
					"right_click", "middle_click", "double_click", "screenshot", "cursor_position",
				},
				"description": "The action to perform.",
			},
			"coordinate": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "integer"},
				"description": "(x, y) pixel coordinate. Required for mouse actions.",
			},
			"text": map[string]interface{}{
				"type":        "string",
				"description": "Text to type or key combination. Required for type and key actions.",
			},
		},
		"required": []string{"action"},
	}
}

func buildLegacyComputerDescription(width, height int, displayNumber *int) string {
	desc := fmt.Sprintf("Computer use tool for controlling a display (%d x %d pixels)", width, height)
	if displayNumber != nil {
		desc += fmt.Sprintf(" on display %d", *displayNumber)
	}
	desc += ".\n\nActions: key, type, mouse_move, left_click, left_click_drag, right_click, middle_click, double_click, screenshot, cursor_position."
	return desc
}
