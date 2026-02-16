# Computer Use Example

This example demonstrates how to use the Anthropic Computer Use tool with Claude Opus 4.5.

## Features

- Screenshot capture
- Mouse control (move, click, drag)
- Keyboard input (type, key press)
- Screen region zoom
- Cursor position tracking

## Prerequisites

- Claude Opus 4.5 API access
- `ANTHROPIC_API_KEY` environment variable

## Usage

```bash
export ANTHROPIC_API_KEY=your_api_key_here
go run main.go
```

## Tool Capabilities

The computer use tool enables Claude to:

### Mouse Actions
- `mouse_move` - Move cursor to coordinates
- `left_click` - Click at coordinates
- `right_click` - Right-click at coordinates
- `middle_click` - Middle-click at coordinates
- `double_click` - Double-click at coordinates
- `triple_click` - Triple-click at coordinates
- `left_click_drag` - Drag from start to end coordinates
- `scroll` - Scroll in a direction

### Keyboard Actions
- `type` - Type text
- `key` - Press key combinations (xdotool syntax)
- `hold_key` - Hold keys for duration

### Screen Actions
- `screenshot` - Capture screen
- `zoom` - View screen region at full resolution (requires `EnableZoom: true`)
- `cursor_position` - Get current cursor position
- `wait` - Wait for duration

## Configuration

```go
computerTool := tools.Computer20251124(tools.Computer20251124Args{
    DisplayWidthPx:  1920,  // Display width in pixels
    DisplayHeightPx: 1080,  // Display height in pixels
    DisplayNumber:   nil,   // Optional: X11 display number
    EnableZoom:      true,  // Enable zoom action
})
```

## Important Notes

- This tool is executed by the Anthropic API, not locally
- Requires Claude Opus 4.5 or compatible model
- Image results are automatically handled by the API
- Use `MaxSteps` to allow multiple tool calls in sequence

## Example Prompts

```text
"Take a screenshot and describe what you see"
"Move the mouse to coordinates (500, 300) and click"
"Type 'Hello World' and press Enter"
"Zoom into the region [100, 100, 500, 500] to inspect details"
"Find the search button and click it"
```
