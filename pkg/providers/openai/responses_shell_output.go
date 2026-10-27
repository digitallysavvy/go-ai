package openai

import (
	"encoding/json"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// Decoding of shell-family Responses output items (local_shell_call,
// shell_call, shell_call_output) into SDK tool calls and results, mirroring
// the TS doGenerate/doStream cases of the same names.

// isOpenAIShellProviderExecuted mirrors TS isShellProviderExecuted: the
// declared openai.shell tool runs in a hosted container (containerAuto /
// containerReference environment).
func isOpenAIShellProviderExecuted(tools []types.Tool) bool {
	for _, tool := range tools {
		if tool.ProviderID != "openai.shell" {
			continue
		}
		if env, ok := tool.ProviderOptions.(*responses.ShellEnvironment); ok && env != nil {
			return env.Type == "container_auto" || env.Type == "container_reference"
		}
		if env, ok := tool.ProviderOptions.(responses.ShellEnvironment); ok {
			return env.Type == "container_auto" || env.Type == "container_reference"
		}
		return false
	}
	return false
}

type openAIShellCallItem struct {
	ID     string `json:"id"`
	CallID string `json:"call_id"`
	Action struct {
		Commands        []string `json:"commands"`
		TimeoutMs       *float64 `json:"timeout_ms"`
		MaxOutputLength *float64 `json:"max_output_length"`
	} `json:"action"`
}

type openAIShellCallOutputItem struct {
	CallID          string   `json:"call_id"`
	MaxOutputLength *float64 `json:"max_output_length"`
	Output          []struct {
		Stdout  string `json:"stdout"`
		Stderr  string `json:"stderr"`
		Outcome struct {
			Type     string `json:"type"`
			ExitCode int    `json:"exit_code"`
		} `json:"outcome"`
	} `json:"output"`
}

// openAIShellToolCall builds the tool call for a shell_call item.
func openAIShellToolCall(providerName string, tools []types.Tool, raw json.RawMessage) (types.ToolCall, bool) {
	var item openAIShellCallItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return types.ToolCall{}, false
	}
	commands := item.Action.Commands
	if commands == nil {
		commands = []string{}
	}
	// TS forwards action.timeout_ms / action.max_output_length as timeoutMs /
	// maxOutputLength; null and missing stay undefined (omitted from the JSON).
	type shellAction struct {
		Commands        []string `json:"commands"`
		TimeoutMs       *float64 `json:"timeoutMs,omitempty"`
		MaxOutputLength *float64 `json:"maxOutputLength,omitempty"`
	}
	action := shellAction{Commands: commands, TimeoutMs: item.Action.TimeoutMs, MaxOutputLength: item.Action.MaxOutputLength}
	input := marshalNoEscape(struct {
		Action shellAction `json:"action"`
	}{Action: action})
	argsAction := map[string]interface{}{"commands": commands}
	if item.Action.TimeoutMs != nil {
		argsAction["timeoutMs"] = *item.Action.TimeoutMs
	}
	if item.Action.MaxOutputLength != nil {
		argsAction["maxOutputLength"] = *item.Action.MaxOutputLength
	}
	args := map[string]interface{}{"action": argsAction}
	return types.ToolCall{
		ID:               item.CallID,
		ToolName:         openAIDeclaredToolName(tools, "openai.shell", "shell"),
		Arguments:        args,
		RawArguments:     string(input),
		ProviderExecuted: isOpenAIShellProviderExecuted(tools),
		ProviderMetadata: openAIResponsesToolCallMetadata(providerName, item.ID, "", nil, nil),
	}, true
}

// openAIShellToolResult builds the tool result for a shell_call_output item.
func openAIShellToolResult(tools []types.Tool, raw json.RawMessage) (types.ToolResultContent, bool) {
	var item openAIShellCallOutputItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return types.ToolResultContent{}, false
	}
	output := make([]interface{}, 0, len(item.Output))
	for _, o := range item.Output {
		outcome := map[string]interface{}{"type": "timeout"}
		if o.Outcome.Type == "exit" {
			outcome = map[string]interface{}{"type": "exit", "exitCode": o.Outcome.ExitCode}
		}
		output = append(output, map[string]interface{}{"stdout": o.Stdout, "stderr": o.Stderr, "outcome": outcome})
	}
	result := map[string]interface{}{"output": output}
	if item.MaxOutputLength != nil {
		result["maxOutputLength"] = *item.MaxOutputLength
	}
	return types.ToolResultContent{
		ToolCallID: item.CallID,
		ToolName:   openAIDeclaredToolName(tools, "openai.shell", "shell"),
		Result:     result,
	}, true
}

// openAILocalShellToolCall builds the tool call for a local_shell_call item.
// stream selects the TS doStream shape (camelCase action fields) over the
// doGenerate shape (the wire action verbatim).
func openAILocalShellToolCall(providerName string, tools []types.Tool, raw json.RawMessage, stream bool) (types.ToolCall, bool) {
	var item responses.LocalShellCall
	if err := json.Unmarshal(raw, &item); err != nil {
		return types.ToolCall{}, false
	}
	// env is kept when the wire sent it, even as {}.
	var env *map[string]string
	if item.Action.Env != nil {
		env = &item.Action.Env
	}
	var input []byte
	if stream {
		input = marshalNoEscape(map[string]interface{}{"action": struct {
			Type             string             `json:"type"`
			Command          []string           `json:"command"`
			TimeoutMs        *int               `json:"timeoutMs,omitempty"`
			User             *string            `json:"user,omitempty"`
			WorkingDirectory *string            `json:"workingDirectory,omitempty"`
			Env              *map[string]string `json:"env,omitempty"`
		}{"exec", item.Action.Command, item.Action.TimeoutMs, item.Action.User, item.Action.WorkingDirectory, env}})
	} else {
		input = marshalNoEscape(map[string]interface{}{"action": struct {
			Type             string             `json:"type"`
			Command          []string           `json:"command"`
			TimeoutMs        *int               `json:"timeout_ms,omitempty"`
			User             *string            `json:"user,omitempty"`
			WorkingDirectory *string            `json:"working_directory,omitempty"`
			Env              *map[string]string `json:"env,omitempty"`
		}{item.Action.Type, item.Action.Command, item.Action.TimeoutMs, item.Action.User, item.Action.WorkingDirectory, env}})
	}
	var args map[string]interface{}
	_ = json.Unmarshal(input, &args)
	return types.ToolCall{
		ID:               item.CallID,
		ToolName:         openAIDeclaredToolName(tools, "openai.local_shell", "local_shell"),
		Arguments:        args,
		RawArguments:     string(input),
		ProviderMetadata: openAIResponsesToolCallMetadata(providerName, item.ID, "", nil, nil),
	}, true
}

// marshalOrNil encodes metadata for a RawMessage field, nil when empty.
func marshalOrNil(m map[string]interface{}) json.RawMessage {
	if len(m) == 0 {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return b
}

// marshalNoEscape is jsonstr.Marshal without the error.
func marshalNoEscape(v interface{}) []byte {
	b, _ := jsonstr.Marshal(v)
	return b
}
