// Package responses provides types and utilities for the OpenAI Responses API.
//
// This package includes shell container tool types (LocalShell, Shell, ApplyPatch)
// and the MCP approval response type. These are used with the OpenAI Responses API
// to interact with sandboxed shell environments.
//
// Shell tool flow:
//  1. Include a shell tool in the request (via NewLocalShellTool, NewShellTool, or NewApplyPatchTool)
//  2. The model returns a LocalShellCall, ShellCall, or ApplyPatchCall when it invokes the tool
//  3. Execute the requested action and send back LocalShellCallOutput, ShellCallOutput, or ApplyPatchCallOutput
package responses

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ─────────────────────────────────────────────────────────────────────────────
// LocalShell types
// ─────────────────────────────────────────────────────────────────────────────

// LocalShellCall represents a local shell execution request from the model.
// The model emits this when it wants to run a command in the local environment.
type LocalShellCall struct {
	// Type is always "local_shell_call".
	Type string `json:"type"`

	// ID is the unique identifier for this output item.
	ID string `json:"id"`

	// CallID links this call to its output.
	CallID string `json:"call_id"`

	// Action describes the command to execute.
	Action LocalShellAction `json:"action"`
}

// LocalShellAction describes the command to run in the local shell.
type LocalShellAction struct {
	// Type is always "exec".
	Type string `json:"type"`

	// Command is the list of command parts (e.g. ["python", "script.py"]).
	Command []string `json:"command"`

	// TimeoutMs is an optional execution timeout in milliseconds.
	TimeoutMs *int `json:"timeout_ms,omitempty"`

	// User is an optional user to run the command as.
	User *string `json:"user,omitempty"`

	// WorkingDirectory is an optional working directory for the command.
	WorkingDirectory *string `json:"working_directory,omitempty"`

	// Env is an optional map of environment variables.
	Env map[string]string `json:"env,omitempty"`
}

// LocalShellCallOutput is the result sent back to the API after executing a LocalShellCall.
type LocalShellCallOutput struct {
	// Type is always "local_shell_call_output".
	Type string `json:"type"`

	// CallID matches the LocalShellCall.CallID this output is for.
	CallID string `json:"call_id"`

	// Output is the combined stdout/stderr output from the command.
	Output string `json:"output"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Shell (container) types
// ─────────────────────────────────────────────────────────────────────────────

// ShellCall represents a shell command request from the model in a container environment.
type ShellCall struct {
	// Type is always "shell_call".
	Type string `json:"type"`

	// ID is the unique identifier for this output item.
	ID string `json:"id"`

	// CallID links this call to its output.
	CallID string `json:"call_id"`

	// Status indicates the current execution state.
	Status string `json:"status"` // "in_progress" | "completed" | "incomplete"

	// Action describes the commands to execute.
	Action ShellCallAction `json:"action"`
}

// ShellCallAction describes the commands to run in the shell container.
type ShellCallAction struct {
	// Commands is the list of shell commands to execute.
	Commands []string `json:"commands"`

	// TimeoutMs is an optional per-command timeout in milliseconds.
	TimeoutMs *int `json:"timeout_ms,omitempty"`

	// MaxOutputLength is an optional character limit for command output.
	MaxOutputLength *int `json:"max_output_length,omitempty"`
}

// ShellCallOutputEntry is a single command's output within a ShellCallOutput.
type ShellCallOutputEntry struct {
	// Stdout is the standard output from the command.
	Stdout string `json:"stdout"`

	// Stderr is the standard error from the command.
	Stderr string `json:"stderr"`

	// Outcome describes how the command terminated.
	Outcome ShellOutcome `json:"outcome"`
}

// ShellOutcome describes how a shell command terminated.
// It is either a timeout or an exit with a code.
type ShellOutcome struct {
	// Type is "timeout" or "exit".
	Type string `json:"type"`

	// ExitCode is the exit code when Type is "exit".
	ExitCode *int `json:"exit_code,omitempty"`
}

// ShellCallOutput is the result sent back to the API after executing a ShellCall.
type ShellCallOutput struct {
	// Type is always "shell_call_output".
	Type string `json:"type"`

	// ID is the optional identifier for this output item.
	ID *string `json:"id,omitempty"`

	// CallID matches the ShellCall.CallID this output is for.
	CallID string `json:"call_id"`

	// Status indicates the current state.
	Status *string `json:"status,omitempty"` // "in_progress" | "completed" | "incomplete"

	// MaxOutputLength is an optional character limit that was applied.
	MaxOutputLength *int `json:"max_output_length,omitempty"`

	// Output contains per-command results.
	Output []ShellCallOutputEntry `json:"output"`
}

// ─────────────────────────────────────────────────────────────────────────────
// ApplyPatch types
// ─────────────────────────────────────────────────────────────────────────────

// ApplyPatchOperation is the specific file operation in an ApplyPatchCall.
// It is one of CreateFile, DeleteFile, or UpdateFile.
type ApplyPatchOperation struct {
	// Type is "create_file", "delete_file", or "update_file".
	Type string `json:"type"`

	// Path is the file path to operate on.
	Path string `json:"path"`

	// Diff is the file content (for create_file) or unified diff (for update_file).
	// Empty for delete_file.
	Diff *string `json:"diff,omitempty"`
}

// ApplyPatchCall represents a patch application request from the model.
type ApplyPatchCall struct {
	// Type is always "apply_patch_call".
	Type string `json:"type"`

	// ID is the optional unique identifier for this output item.
	ID *string `json:"id,omitempty"`

	// CallID links this call to its output.
	CallID string `json:"call_id"`

	// Status indicates the current execution state.
	Status string `json:"status"` // "in_progress" | "completed"

	// Operation describes the file operation to perform.
	Operation ApplyPatchOperation `json:"operation"`
}

// ApplyPatchCallOutput is the result sent back to the API after executing an ApplyPatchCall.
type ApplyPatchCallOutput struct {
	// Type is always "apply_patch_call_output".
	Type string `json:"type"`

	// CallID matches the ApplyPatchCall.CallID this output is for.
	CallID string `json:"call_id"`

	// Status indicates success or failure.
	Status string `json:"status"` // "completed" | "failed"

	// Output is optional human-readable result text.
	Output *string `json:"output,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// MCP Approval
// ─────────────────────────────────────────────────────────────────────────────

// MCPApprovalResponse is sent to approve or reject an MCP tool execution request.
// Use this when a tool has require_approval set and you receive an mcp_approval_request.
type MCPApprovalResponse struct {
	// Type is always "mcp_approval_response".
	Type string `json:"type"`

	// ApprovalRequestID is the ID of the approval request being responded to.
	ApprovalRequestID string `json:"approval_request_id"`

	// Approve indicates whether the tool execution is approved.
	Approve bool `json:"approve"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Shell environment types (for tool definitions sent in requests)
// ─────────────────────────────────────────────────────────────────────────────

// ShellEnvironment describes the container environment for a shell tool.
// Used when creating a shell tool with NewShellTool.
type ShellEnvironment struct {
	// Type is "container_auto", "container_reference", or "local".
	Type string `json:"type"`

	// FileIDs lists file IDs to mount (container_auto only).
	FileIDs []string `json:"file_ids,omitempty"`

	// MemoryLimit sets the memory limit (container_auto only).
	// Values: "1g", "4g", "16g", "64g"
	MemoryLimit *string `json:"memory_limit,omitempty"`

	// NetworkPolicy controls network access (container_auto only).
	NetworkPolicy *ShellNetworkPolicy `json:"network_policy,omitempty"`

	// Skills lists executable skills available in the container.
	Skills []ShellSkill `json:"skills,omitempty"`

	// ContainerID references an existing container (container_reference only).
	ContainerID *string `json:"container_id,omitempty"`
}

// ShellNetworkPolicy controls network access for a shell container.
type ShellNetworkPolicy struct {
	// Type is "disabled" or "allowlist".
	Type string `json:"type"`

	// AllowedDomains lists domains allowed through (allowlist only).
	AllowedDomains []string `json:"allowed_domains,omitempty"`

	// DomainSecrets provides credentials for allowed domains.
	DomainSecrets []ShellDomainSecret `json:"domain_secrets,omitempty"`
}

// ShellDomainSecret provides credentials for a specific domain.
type ShellDomainSecret struct {
	Domain string `json:"domain"`
	Name   string `json:"name"`
	Value  string `json:"value"`
}

// ShellSkill represents an executable skill available in the container.
//
// For container_auto environments it is a discriminated union:
//   - Type "skill_reference": set SkillID (and optionally Version)
//   - Type "inline": set Name, Description, Source
//
// For local environments, skills have no Type field — set Name, Description,
// and Path, leaving Type nil so the field is omitted from JSON.
type ShellSkill struct {
	// Type is "skill_reference" or "inline" for container environments.
	// Nil for local environment skills (field is omitted from JSON).
	Type *string `json:"type,omitempty"`

	// SkillID is the referenced skill ID (skill_reference only).
	SkillID *string `json:"skill_id,omitempty"`

	// Version is the optional skill version (skill_reference only).
	Version *string `json:"version,omitempty"`

	// Name is the inline skill name (inline only).
	Name *string `json:"name,omitempty"`

	// Description is the inline skill description (inline only).
	Description *string `json:"description,omitempty"`

	// Source is the base64-encoded zip of the inline skill (inline only).
	Source *ShellSkillSource `json:"source,omitempty"`

	// Path is the local path to the skill executable (local environment only).
	Path *string `json:"path,omitempty"`
}

// ShellSkillSource holds the binary source for an inline skill.
type ShellSkillSource struct {
	// Type is always "base64".
	Type string `json:"type"`

	// MediaType is always "application/zip".
	MediaType string `json:"media_type"`

	// Data is the base64-encoded zip content.
	Data string `json:"data"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Memory limit constants
// ─────────────────────────────────────────────────────────────────────────────

// Memory limit values accepted by the container_auto environment.
// The OpenAI API rejects any other string value.
const (
	MemoryLimit1G  = "1g"
	MemoryLimit4G  = "4g"
	MemoryLimit16G = "16g"
	MemoryLimit64G = "64g"
)

// ─────────────────────────────────────────────────────────────────────────────
// Tool factory functions
// ─────────────────────────────────────────────────────────────────────────────

// NewLocalShellTool creates a types.Tool that enables the local shell tool
// in the OpenAI Responses API request.
//
// When the model invokes this tool, it returns a LocalShellCall. Execute
// the requested command and return a LocalShellCallOutput.
//
// Example:
//
//	tool := responses.NewLocalShellTool()
//	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
//	    Model: model,
//	    Tools: []types.Tool{tool},
//	})
func NewLocalShellTool() types.Tool {
	return types.Tool{
		Name:             "openai.local_shell",
		Description:      "Run commands in the local shell environment",
		ProviderExecuted: true,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("local_shell tool is executed by the OpenAI API, not locally")
		},
	}
}

// ShellToolOption is a functional option for configuring a shell tool.
type ShellToolOption func(*shellToolConfig)

type shellToolConfig struct {
	environment *ShellEnvironment
}

// WithShellEnvironment sets the container environment for a shell tool.
func WithShellEnvironment(env ShellEnvironment) ShellToolOption {
	return func(c *shellToolConfig) {
		c.environment = &env
	}
}

// NewShellTool creates a types.Tool that enables the shell container tool
// in the OpenAI Responses API request.
//
// When the model invokes this tool, it returns a ShellCall. Execute the
// requested commands and return a ShellCallOutput.
//
// Example:
//
//	tool := responses.NewShellTool()
//	// With custom environment:
//	tool := responses.NewShellTool(responses.WithShellEnvironment(responses.ShellEnvironment{
//	    Type: "container_auto",
//	}))
func NewShellTool(opts ...ShellToolOption) types.Tool {
	cfg := &shellToolConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	t := types.Tool{
		Name:             "openai.shell",
		Description:      "Run commands in a sandboxed shell container",
		ProviderExecuted: true,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("shell tool is executed by the OpenAI API, not locally")
		},
	}

	if cfg.environment != nil {
		t.ProviderOptions = cfg.environment
	}

	return t
}

// NewApplyPatchTool creates a types.Tool that enables the apply_patch tool
// in the OpenAI Responses API request.
//
// When the model invokes this tool, it returns an ApplyPatchCall. Apply the
// patch to the relevant files and return an ApplyPatchCallOutput.
//
// Example:
//
//	tool := responses.NewApplyPatchTool()
func NewApplyPatchTool() types.Tool {
	return types.Tool{
		Name:             "openai.apply_patch",
		Description:      "Apply unified diff patches to files",
		ProviderExecuted: true,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("apply_patch tool is executed by the OpenAI API, not locally")
		},
	}
}
