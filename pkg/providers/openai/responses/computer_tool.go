// Computer tool types and helpers for the OpenAI Responses API.
//
// Computer tool flow:
//  1. Include the computer tool in the request (via NewComputerTool).
//  2. The model returns a ComputerCall with a batch of UI actions and any
//     pending safety checks when it wants to interact with the computer.
//  3. Execute the actions, capture a screenshot, and send back a
//     ComputerCallOutput with the screenshot (image_url or file_id) and any
//     acknowledged safety checks.

package responses

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ─────────────────────────────────────────────────────────────────────────────
// Computer types
// ─────────────────────────────────────────────────────────────────────────────

// ComputerSafetyCheck represents a safety check that must be (or has been)
// acknowledged before continuing a computer call.
type ComputerSafetyCheck struct {
	ID      string `json:"id"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// ComputerCall represents a batch of UI actions the model wants to perform,
// plus any safety checks pending acknowledgement.
type ComputerCall struct {
	// Type is always "computer_call".
	Type string `json:"type"`

	// ID is the unique identifier for this output item.
	ID *string `json:"id,omitempty"`

	// CallID links this call to its output. A nil CallID (the API sends
	// call_id: null) means this call is fully server-executed with no
	// client round-trip; it decodes as a provider-executed "computer_use"
	// tool-call/tool-result pair instead of a client-executable "computer"
	// tool call.
	CallID *string `json:"call_id,omitempty"`

	// Status is "in_progress", "completed", or "incomplete".
	Status string `json:"status"`

	// Action is a single UI action, used instead of Actions on some
	// responses; mapComputerCallInput falls back to [Action] when Actions
	// is empty. See Actions for the map shape.
	Action map[string]interface{} `json:"action,omitempty"`

	// Actions are the ordered UI actions to execute. Each action is a
	// generic map since the shape varies by "type" (click/double_click/
	// drag/keypress/move/screenshot/scroll/type/wait); see
	// MapComputerActionToSDK / MapComputerActionToWire for the field-name
	// translation between wire (scroll_x/scroll_y) and SDK (scrollX/scrollY)
	// forms.
	Actions []map[string]interface{} `json:"actions,omitempty"`

	// PendingSafetyChecks are safety checks that must be acknowledged before
	// continuing.
	PendingSafetyChecks []ComputerSafetyCheck `json:"pending_safety_checks,omitempty"`
}

// ComputerCallOutputScreenshot is the screenshot captured after executing a
// ComputerCall's actions.
type ComputerCallOutputScreenshot struct {
	// Type is always "computer_screenshot".
	Type string `json:"type"`

	// ImageURL is the screenshot as a URL or base64 data URL. Mutually
	// exclusive with FileID isn't enforced; at least one should be set.
	ImageURL string `json:"image_url,omitempty"`

	// FileID references a previously uploaded screenshot file.
	FileID string `json:"file_id,omitempty"`

	// Detail is "auto", "low", "high", or "original".
	Detail string `json:"detail,omitempty"`
}

// ComputerCallOutput is the result sent back to the API after executing a
// ComputerCall's actions.
type ComputerCallOutput struct {
	// Type is always "computer_call_output".
	Type string `json:"type"`

	// CallID matches the ComputerCall.CallID this output is for.
	CallID string `json:"call_id"`

	// Output is the captured screenshot.
	Output ComputerCallOutputScreenshot `json:"output"`

	// AcknowledgedSafetyChecks are the safety checks the application
	// reviewed and acknowledged before sending this output.
	AcknowledgedSafetyChecks []ComputerSafetyCheck `json:"acknowledged_safety_checks,omitempty"`
}

// MapComputerActionToSDK translates a wire-format computer action (as
// received from the API in a ComputerCall) into the SDK's camelCase form,
// mirroring TS mapComputerAction: only the "scroll" action's field names
// differ (scroll_x/scroll_y -> scrollX/scrollY); every other action type's
// fields are already spelled the same in both forms.
func MapComputerActionToSDK(action map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(action))
	for k, v := range action {
		switch k {
		case "scroll_x":
			out["scrollX"] = v
		case "scroll_y":
			out["scrollY"] = v
		default:
			out[k] = v
		}
	}
	return out
}

// MapComputerActionToWire is the inverse of MapComputerActionToSDK, used
// when replaying a computer tool call's input back into a wire-format
// ComputerCall action.
func MapComputerActionToWire(action map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(action))
	for k, v := range action {
		switch k {
		case "scrollX":
			out["scroll_x"] = v
		case "scrollY":
			out["scroll_y"] = v
		default:
			out[k] = v
		}
	}
	return out
}

// NewComputerTool creates a types.Tool that enables the computer-use tool in
// the OpenAI Responses API request.
//
// When the model invokes this tool, it returns a ComputerCall with a batch
// of UI actions. Execute them, capture a screenshot, and return a
// ComputerCallOutput.
//
// Example:
//
//	tool := responses.NewComputerTool()
func NewComputerTool() types.Tool {
	return types.Tool{
		Name:             "openai.computer",
		Description:      "Control a computer's mouse, keyboard, and screen via batched UI actions",
		ProviderExecuted: true,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("computer tool is executed by the OpenAI API, not locally")
		},
	}
}
