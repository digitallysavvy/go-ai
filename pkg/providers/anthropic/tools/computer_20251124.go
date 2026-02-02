package tools

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Computer20251124Action represents the action type for computer use tool
type Computer20251124Action string

const (
	ActionKey               Computer20251124Action = "key"
	ActionHoldKey           Computer20251124Action = "hold_key"
	ActionType              Computer20251124Action = "type"
	ActionCursorPosition    Computer20251124Action = "cursor_position"
	ActionMouseMove         Computer20251124Action = "mouse_move"
	ActionLeftMouseDown     Computer20251124Action = "left_mouse_down"
	ActionLeftMouseUp       Computer20251124Action = "left_mouse_up"
	ActionLeftClick         Computer20251124Action = "left_click"
	ActionLeftClickDrag     Computer20251124Action = "left_click_drag"
	ActionRightClick        Computer20251124Action = "right_click"
	ActionMiddleClick       Computer20251124Action = "middle_click"
	ActionDoubleClick       Computer20251124Action = "double_click"
	ActionTripleClick       Computer20251124Action = "triple_click"
	ActionScroll            Computer20251124Action = "scroll"
	ActionWait              Computer20251124Action = "wait"
	ActionScreenshot        Computer20251124Action = "screenshot"
	ActionZoom              Computer20251124Action = "zoom"
)

// ScrollDirection represents the scroll direction
type ScrollDirection string

const (
	ScrollUp    ScrollDirection = "up"
	ScrollDown  ScrollDirection = "down"
	ScrollLeft  ScrollDirection = "left"
	ScrollRight ScrollDirection = "right"
)

// Computer20251124Input represents the input parameters for the computer tool
type Computer20251124Input struct {
	// Action to perform
	Action Computer20251124Action `json:"action"`

	// Coordinate (x, y) for mouse actions
	Coordinate []int `json:"coordinate,omitempty"`

	// Duration in seconds for hold_key and wait actions
	Duration *float64 `json:"duration,omitempty"`

	// Region [x1, y1, x2, y2] for zoom action
	Region []int `json:"region,omitempty"`

	// ScrollAmount for scroll action
	ScrollAmount *int `json:"scroll_amount,omitempty"`

	// ScrollDirection for scroll action
	ScrollDirection *ScrollDirection `json:"scroll_direction,omitempty"`

	// StartCoordinate for left_click_drag action
	StartCoordinate []int `json:"start_coordinate,omitempty"`

	// Text for type, key, and hold_key actions
	Text *string `json:"text,omitempty"`
}

// Computer20251124Args represents the configuration arguments for creating the computer tool
type Computer20251124Args struct {
	// DisplayWidthPx is the width of the display being controlled by the model in pixels
	DisplayWidthPx int

	// DisplayHeightPx is the height of the display being controlled by the model in pixels
	DisplayHeightPx int

	// DisplayNumber is the display number to control (only relevant for X11 environments)
	DisplayNumber *int

	// EnableZoom enables the zoom action. Set to true to allow Claude to zoom into specific screen regions
	EnableZoom bool
}

// Computer20251124 creates a computer use tool that enables Claude to control computers
// through mouse, keyboard, and screenshot capabilities.
//
// This is the latest version (v20251124) which includes the new zoom action for detailed
// screen region inspection.
//
// Actions supported:
//   - key: Press a key or key-combination on the keyboard (supports xdotool syntax)
//   - hold_key: Hold down a key or multiple keys for a specified duration
//   - type: Type a string of text on the keyboard
//   - cursor_position: Get the current (x, y) pixel coordinate of the cursor
//   - mouse_move: Move the cursor to a specified (x, y) pixel coordinate
//   - left_mouse_down: Press the left mouse button
//   - left_mouse_up: Release the left mouse button
//   - left_click: Click the left mouse button at the specified coordinate
//   - left_click_drag: Click and drag from start_coordinate to coordinate
//   - right_click: Click the right mouse button at the specified coordinate
//   - middle_click: Click the middle mouse button at the specified coordinate
//   - double_click: Double-click the left mouse button at the specified coordinate
//   - triple_click: Triple-click the left mouse button at the specified coordinate
//   - scroll: Scroll the screen in a specified direction by a specified amount
//   - wait: Wait for a specified duration in seconds
//   - screenshot: Take a screenshot of the screen
//   - zoom: View a specific region of the screen at full resolution (requires enableZoom: true)
//
// Supported models: Claude Opus 4.5
//
// Example:
//
//	tool := anthropicTools.Computer20251124(Computer20251124Args{
//	    DisplayWidthPx: 1920,
//	    DisplayHeightPx: 1080,
//	    EnableZoom: true,
//	})
func Computer20251124(args Computer20251124Args) types.Tool {
	// Build tool name
	toolName := "anthropic.computer_20251124"

	// Build parameters schema
	parameters := buildComputer20251124Schema(args.EnableZoom)

	tool := types.Tool{
		Name:        toolName,
		Description: buildComputerDescription(args),
		Parameters:  parameters,
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("computer tool must be executed by the provider (Anthropic). Set ProviderExecuted: true")
		},
		ProviderExecuted: true,
	}

	return tool
}

func buildComputer20251124Schema(enableZoom bool) map[string]interface{} {
	// Build action enum
	actions := []string{
		string(ActionKey),
		string(ActionHoldKey),
		string(ActionType),
		string(ActionCursorPosition),
		string(ActionMouseMove),
		string(ActionLeftMouseDown),
		string(ActionLeftMouseUp),
		string(ActionLeftClick),
		string(ActionLeftClickDrag),
		string(ActionRightClick),
		string(ActionMiddleClick),
		string(ActionDoubleClick),
		string(ActionTripleClick),
		string(ActionScroll),
		string(ActionWait),
		string(ActionScreenshot),
	}

	if enableZoom {
		actions = append(actions, string(ActionZoom))
	}

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type": "string",
				"enum": actions,
				"description": "The action to perform. See tool description for details on each action.",
			},
			"coordinate": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "integer",
				},
				"minItems":    2,
				"maxItems":    2,
				"description": "(x, y): The x (pixels from the left edge) and y (pixels from the top edge) coordinates. Required for mouse_move and mouse click actions.",
			},
			"duration": map[string]interface{}{
				"type":        "number",
				"description": "The duration in seconds. Required for hold_key and wait actions.",
			},
			"region": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "integer",
				},
				"minItems":    4,
				"maxItems":    4,
				"description": "[x1, y1, x2, y2]: The coordinates defining the region to zoom into. x1, y1 is the top-left corner and x2, y2 is the bottom-right corner. Required for zoom action.",
			},
			"scroll_amount": map[string]interface{}{
				"type":        "integer",
				"description": "The number of 'clicks' to scroll. Required for scroll action.",
			},
			"scroll_direction": map[string]interface{}{
				"type": "string",
				"enum": []string{
					string(ScrollUp),
					string(ScrollDown),
					string(ScrollLeft),
					string(ScrollRight),
				},
				"description": "The direction to scroll. Required for scroll action.",
			},
			"start_coordinate": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "integer",
				},
				"minItems":    2,
				"maxItems":    2,
				"description": "(x, y): The x (pixels from the left edge) and y (pixels from the top edge) coordinates to start the drag from. Required for left_click_drag action.",
			},
			"text": map[string]interface{}{
				"type":        "string",
				"description": "Text to type or key combination to press. Required for type, key, and hold_key actions. Can also be used with click or scroll actions to hold down keys while performing the action.",
			},
		},
		"required": []string{"action"},
	}

	return schema
}

func buildComputerDescription(args Computer20251124Args) string {
	baseDesc := fmt.Sprintf(`Computer use tool for controlling a display (%d x %d pixels)`, args.DisplayWidthPx, args.DisplayHeightPx)

	if args.DisplayNumber != nil {
		baseDesc += fmt.Sprintf(" on display %d", *args.DisplayNumber)
	}

	baseDesc += ".\n\n"
	baseDesc += "Actions:\n"
	baseDesc += "- key: Press a key or key-combination (xdotool syntax). Examples: 'a', 'Return', 'alt+Tab', 'ctrl+s'\n"
	baseDesc += "- hold_key: Hold down a key or keys for a specified duration (seconds)\n"
	baseDesc += "- type: Type a string of text\n"
	baseDesc += "- cursor_position: Get current cursor (x, y) coordinate\n"
	baseDesc += "- mouse_move: Move cursor to (x, y) coordinate\n"
	baseDesc += "- left_mouse_down: Press left mouse button\n"
	baseDesc += "- left_mouse_up: Release left mouse button\n"
	baseDesc += "- left_click: Click left mouse button at (x, y). Use 'text' param to hold keys while clicking\n"
	baseDesc += "- left_click_drag: Drag from start_coordinate to coordinate\n"
	baseDesc += "- right_click: Click right mouse button at (x, y)\n"
	baseDesc += "- middle_click: Click middle mouse button at (x, y)\n"
	baseDesc += "- double_click: Double-click left mouse button at (x, y)\n"
	baseDesc += "- triple_click: Triple-click left mouse button at (x, y)\n"
	baseDesc += "- scroll: Scroll in direction by scroll_amount clicks. DO NOT use PageUp/PageDown\n"
	baseDesc += "- wait: Wait for specified duration (seconds)\n"
	baseDesc += "- screenshot: Take a screenshot of the screen\n"

	if args.EnableZoom {
		baseDesc += "- zoom: View a specific region [x1, y1, x2, y2] at full resolution for detailed inspection\n"
	}

	return baseDesc
}
