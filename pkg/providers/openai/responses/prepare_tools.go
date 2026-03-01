package responses

import (
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

// PrepareTools converts SDK tools to the OpenAI Responses API tool format.
// It dispatches on the tool name to determine the correct API representation:
//
//   - "openai.custom"       → CustomToolDef (name/description/format from ProviderOptions)
//   - "openai.local_shell"  → LocalShellToolDef {type: "local_shell"}
//   - "openai.shell"        → ShellToolDef {type: "shell", environment: ...}
//   - "openai.apply_patch"  → ApplyPatchToolDef {type: "apply_patch"}
//   - anything else         → FunctionToolDef {type: "function", ...}
//
// The returned slice is ready to be marshaled as the "tools" field in an
// OpenAI Responses API request body.
func PrepareTools(tools []types.Tool) []interface{} {
	if len(tools) == 0 {
		return nil
	}

	result := make([]interface{}, 0, len(tools))
	for _, t := range tools {
		result = append(result, convertTool(t))
	}
	return result
}

// convertTool converts a single types.Tool to its Responses API representation.
func convertTool(t types.Tool) interface{} {
	switch t.Name {
	case "openai.custom":
		return convertCustomTool(t)
	case "openai.local_shell":
		return LocalShellToolDef{Type: "local_shell"}
	case "openai.shell":
		return convertShellTool(t)
	case "openai.apply_patch":
		return ApplyPatchToolDef{Type: "apply_patch"}
	default:
		return convertFunctionTool(t)
	}
}

// convertCustomTool builds a CustomToolDef from a tool whose ProviderOptions
// holds an openaitool.CustomTool value.
func convertCustomTool(t types.Tool) CustomToolDef {
	def := CustomToolDef{Type: "custom"}

	ct, ok := t.ProviderOptions.(openaitool.CustomTool)
	if !ok {
		// Fallback: use the SDK tool name as the custom tool name.
		def.Name = t.Name
		return def
	}

	def.Name = ct.Name
	def.Description = ct.Description

	if ct.Format != nil {
		f := &CustomToolDefFormat{Type: ct.Format.Type}
		if ct.Format.Syntax != nil {
			f.Syntax = ct.Format.Syntax
		}
		if ct.Format.Definition != nil {
			f.Definition = ct.Format.Definition
		}
		def.Format = f
	}

	return def
}

// convertShellTool builds a ShellToolDef, including the environment config
// from ProviderOptions if present.
func convertShellTool(t types.Tool) ShellToolDef {
	def := ShellToolDef{Type: "shell"}

	if t.ProviderOptions == nil {
		return def
	}

	env, ok := t.ProviderOptions.(*ShellEnvironment)
	if !ok {
		return def
	}

	toolEnv := &ShellToolDefEnvironment{Type: env.Type}

	if len(env.FileIDs) > 0 {
		toolEnv.FileIDs = env.FileIDs
	}
	if env.MemoryLimit != nil {
		toolEnv.MemoryLimit = env.MemoryLimit
	}
	if env.NetworkPolicy != nil {
		toolEnv.NetworkPolicy = env.NetworkPolicy
	}
	if len(env.Skills) > 0 {
		toolEnv.Skills = env.Skills
	}
	if env.ContainerID != nil {
		toolEnv.ContainerID = env.ContainerID
	}

	def.Environment = toolEnv
	return def
}

// convertFunctionTool builds a FunctionToolDef for a standard function tool.
func convertFunctionTool(t types.Tool) FunctionToolDef {
	def := FunctionToolDef{
		Type:        "function",
		Name:        t.Name,
		Description: t.Description,
		Parameters:  t.Parameters,
	}

	if t.Strict {
		strict := true
		def.Strict = &strict
	}

	return def
}
