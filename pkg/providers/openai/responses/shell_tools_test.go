package responses

import (
	"encoding/json"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// LocalShellCall round-trip
// ─────────────────────────────────────────────────────────────────────────────

func TestLocalShellCall_RoundTrip(t *testing.T) {
	timeoutMs := 5000
	user := "runner"
	workDir := "/tmp"

	original := LocalShellCall{
		Type:   "local_shell_call",
		ID:     "item_abc",
		CallID: "call_xyz",
		Action: LocalShellAction{
			Type:             "exec",
			Command:          []string{"python", "script.py", "--arg"},
			TimeoutMs:        &timeoutMs,
			User:             &user,
			WorkingDirectory: &workDir,
			Env:              map[string]string{"DEBUG": "1"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got LocalShellCall
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.Type != "local_shell_call" {
		t.Errorf("Type: got %q, want %q", got.Type, "local_shell_call")
	}
	if got.ID != "item_abc" {
		t.Errorf("ID: got %q, want %q", got.ID, "item_abc")
	}
	if got.CallID != "call_xyz" {
		t.Errorf("CallID: got %q, want %q", got.CallID, "call_xyz")
	}
	if len(got.Action.Command) != 3 || got.Action.Command[0] != "python" {
		t.Errorf("Action.Command mismatch: %v", got.Action.Command)
	}
	if got.Action.TimeoutMs == nil || *got.Action.TimeoutMs != 5000 {
		t.Error("Action.TimeoutMs mismatch")
	}
	if got.Action.User == nil || *got.Action.User != "runner" {
		t.Error("Action.User mismatch")
	}
	if got.Action.WorkingDirectory == nil || *got.Action.WorkingDirectory != "/tmp" {
		t.Error("Action.WorkingDirectory mismatch")
	}
	if got.Action.Env["DEBUG"] != "1" {
		t.Error("Action.Env mismatch")
	}
}

func TestLocalShellCall_MinimalFields(t *testing.T) {
	original := LocalShellCall{
		Type:   "local_shell_call",
		ID:     "item_1",
		CallID: "call_1",
		Action: LocalShellAction{
			Type:    "exec",
			Command: []string{"ls"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal map failed: %v", err)
	}

	action := m["action"].(map[string]interface{})
	if _, ok := action["timeout_ms"]; ok {
		t.Error("timeout_ms should be omitted when nil")
	}
	if _, ok := action["user"]; ok {
		t.Error("user should be omitted when nil")
	}
	if _, ok := action["env"]; ok {
		t.Error("env should be omitted when nil")
	}
}

func TestLocalShellCallOutput_RoundTrip(t *testing.T) {
	original := LocalShellCallOutput{
		Type:   "local_shell_call_output",
		CallID: "call_xyz",
		Output: "hello world\n",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got LocalShellCallOutput
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.Type != "local_shell_call_output" {
		t.Errorf("Type: got %q, want %q", got.Type, "local_shell_call_output")
	}
	if got.CallID != "call_xyz" {
		t.Errorf("CallID: got %q, want %q", got.CallID, "call_xyz")
	}
	if got.Output != "hello world\n" {
		t.Errorf("Output mismatch: got %q", got.Output)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ShellCall round-trip
// ─────────────────────────────────────────────────────────────────────────────

func TestShellCall_RoundTrip(t *testing.T) {
	timeoutMs := 30000
	maxOutput := 10000

	original := ShellCall{
		Type:   "shell_call",
		ID:     "item_shell",
		CallID: "call_shell",
		Status: "in_progress",
		Action: ShellCallAction{
			Commands:        []string{"echo hello", "ls -la"},
			TimeoutMs:       &timeoutMs,
			MaxOutputLength: &maxOutput,
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got ShellCall
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.Type != "shell_call" {
		t.Errorf("Type: got %q, want %q", got.Type, "shell_call")
	}
	if got.Status != "in_progress" {
		t.Errorf("Status: got %q, want %q", got.Status, "in_progress")
	}
	if len(got.Action.Commands) != 2 {
		t.Errorf("Commands count: got %d, want 2", len(got.Action.Commands))
	}
	if got.Action.TimeoutMs == nil || *got.Action.TimeoutMs != 30000 {
		t.Error("TimeoutMs mismatch")
	}
}

func TestShellCallOutput_WithExitOutcome(t *testing.T) {
	exitCode := 0
	status := "completed"
	original := ShellCallOutput{
		Type:   "shell_call_output",
		CallID: "call_shell",
		Status: &status,
		Output: []ShellCallOutputEntry{
			{
				Stdout: "hello\n",
				Stderr: "",
				Outcome: ShellOutcome{
					Type:     "exit",
					ExitCode: &exitCode,
				},
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got ShellCallOutput
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.Type != "shell_call_output" {
		t.Errorf("Type: got %q, want %q", got.Type, "shell_call_output")
	}
	if len(got.Output) != 1 {
		t.Fatalf("Output count: got %d, want 1", len(got.Output))
	}
	entry := got.Output[0]
	if entry.Stdout != "hello\n" {
		t.Errorf("Stdout mismatch: got %q", entry.Stdout)
	}
	if entry.Outcome.Type != "exit" {
		t.Errorf("Outcome.Type: got %q, want %q", entry.Outcome.Type, "exit")
	}
	if entry.Outcome.ExitCode == nil || *entry.Outcome.ExitCode != 0 {
		t.Error("ExitCode mismatch")
	}
}

func TestShellCallOutput_WithTimeoutOutcome(t *testing.T) {
	original := ShellCallOutput{
		Type:   "shell_call_output",
		CallID: "call_timeout",
		Output: []ShellCallOutputEntry{
			{
				Stdout: "",
				Stderr: "killed by timeout",
				Outcome: ShellOutcome{
					Type: "timeout",
				},
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got ShellCallOutput
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.Output[0].Outcome.Type != "timeout" {
		t.Errorf("Outcome.Type: got %q, want %q", got.Output[0].Outcome.Type, "timeout")
	}
	if got.Output[0].Outcome.ExitCode != nil {
		t.Error("ExitCode should be nil for timeout")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ApplyPatchCall round-trip
// ─────────────────────────────────────────────────────────────────────────────

func TestApplyPatchCall_CreateFile(t *testing.T) {
	diff := "content of new file"
	original := ApplyPatchCall{
		Type:   "apply_patch_call",
		CallID: "call_patch",
		Status: "completed",
		Operation: ApplyPatchOperation{
			Type: "create_file",
			Path: "/tmp/new_file.txt",
			Diff: &diff,
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got ApplyPatchCall
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.Type != "apply_patch_call" {
		t.Errorf("Type: got %q, want %q", got.Type, "apply_patch_call")
	}
	if got.Operation.Type != "create_file" {
		t.Errorf("Operation.Type: got %q, want %q", got.Operation.Type, "create_file")
	}
	if got.Operation.Path != "/tmp/new_file.txt" {
		t.Errorf("Operation.Path mismatch: got %q", got.Operation.Path)
	}
	if got.Operation.Diff == nil || *got.Operation.Diff != "content of new file" {
		t.Error("Operation.Diff mismatch")
	}
}

func TestApplyPatchCall_DeleteFile(t *testing.T) {
	original := ApplyPatchCall{
		Type:   "apply_patch_call",
		CallID: "call_del",
		Status: "completed",
		Operation: ApplyPatchOperation{
			Type: "delete_file",
			Path: "/tmp/old_file.txt",
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got ApplyPatchCall
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.Operation.Type != "delete_file" {
		t.Errorf("Operation.Type: got %q, want %q", got.Operation.Type, "delete_file")
	}
	if got.Operation.Diff != nil {
		t.Error("Operation.Diff should be nil for delete_file")
	}
}

func TestApplyPatchCall_UpdateFile(t *testing.T) {
	diff := "--- a/file.txt\n+++ b/file.txt\n@@ -1 +1 @@\n-old\n+new\n"
	original := ApplyPatchCall{
		Type:   "apply_patch_call",
		CallID: "call_upd",
		Status: "in_progress",
		Operation: ApplyPatchOperation{
			Type: "update_file",
			Path: "/tmp/file.txt",
			Diff: &diff,
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got ApplyPatchCall
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.Operation.Type != "update_file" {
		t.Errorf("Operation.Type: got %q", got.Operation.Type)
	}
}

func TestApplyPatchCallOutput_RoundTrip(t *testing.T) {
	out := "patched successfully"
	original := ApplyPatchCallOutput{
		Type:   "apply_patch_call_output",
		CallID: "call_patch",
		Status: "completed",
		Output: &out,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got ApplyPatchCallOutput
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.Status != "completed" {
		t.Errorf("Status: got %q", got.Status)
	}
	if got.Output == nil || *got.Output != "patched successfully" {
		t.Error("Output mismatch")
	}
}

func TestApplyPatchCallOutput_Failed(t *testing.T) {
	original := ApplyPatchCallOutput{
		Type:   "apply_patch_call_output",
		CallID: "call_err",
		Status: "failed",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal map failed: %v", err)
	}
	if _, ok := m["output"]; ok {
		t.Error("output should be omitted when nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// MCPApprovalResponse round-trip
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPApprovalResponse_Approve(t *testing.T) {
	original := MCPApprovalResponse{
		Type:              "mcp_approval_response",
		ApprovalRequestID: "req_123",
		Approve:           true,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got MCPApprovalResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.Type != "mcp_approval_response" {
		t.Errorf("Type: got %q, want %q", got.Type, "mcp_approval_response")
	}
	if got.ApprovalRequestID != "req_123" {
		t.Errorf("ApprovalRequestID: got %q, want %q", got.ApprovalRequestID, "req_123")
	}
	if !got.Approve {
		t.Error("expected Approve to be true")
	}
}

func TestMCPApprovalResponse_Reject(t *testing.T) {
	original := MCPApprovalResponse{
		Type:              "mcp_approval_response",
		ApprovalRequestID: "req_456",
		Approve:           false,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got MCPApprovalResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.Approve {
		t.Error("expected Approve to be false")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ShellSkill serialization
// ─────────────────────────────────────────────────────────────────────────────

func TestShellSkill_SkillReference(t *testing.T) {
	skillType := "skill_reference"
	skillID := "skill_abc"
	version := "1.0"
	skill := ShellSkill{
		Type:    &skillType,
		SkillID: &skillID,
		Version: &version,
	}

	data, err := json.Marshal(skill)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal map failed: %v", err)
	}
	if m["type"] != "skill_reference" {
		t.Errorf("type: got %v, want skill_reference", m["type"])
	}
	if m["skill_id"] != "skill_abc" {
		t.Errorf("skill_id: got %v, want skill_abc", m["skill_id"])
	}
	if m["version"] != "1.0" {
		t.Errorf("version: got %v, want 1.0", m["version"])
	}
}

func TestShellSkill_Inline(t *testing.T) {
	skillType := "inline"
	name := "my-skill"
	desc := "does something"
	skill := ShellSkill{
		Type:        &skillType,
		Name:        &name,
		Description: &desc,
		Source: &ShellSkillSource{
			Type:      "base64",
			MediaType: "application/zip",
			Data:      "base64encodeddata",
		},
	}

	data, err := json.Marshal(skill)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal map failed: %v", err)
	}
	if m["type"] != "inline" {
		t.Errorf("type: got %v, want inline", m["type"])
	}
	src := m["source"].(map[string]interface{})
	if src["media_type"] != "application/zip" {
		t.Errorf("source.media_type: got %v", src["media_type"])
	}
}

// TestShellSkill_LocalEnv verifies that local environment skills have NO "type"
// field in the wire format, matching the TS OpenAIResponsesTool shell local env spec.
func TestShellSkill_LocalEnv(t *testing.T) {
	name := "my-local-skill"
	desc := "runs a local binary"
	path := "/usr/local/bin/my-skill"
	skill := ShellSkill{
		// Type is intentionally nil — local skills have no type discriminator
		Name:        &name,
		Description: &desc,
		Path:        &path,
	}

	data, err := json.Marshal(skill)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal map failed: %v", err)
	}

	// The "type" field MUST be absent for local skills
	if _, ok := m["type"]; ok {
		t.Errorf("type field must be absent for local skills, got: %v", m["type"])
	}
	if m["name"] != "my-local-skill" {
		t.Errorf("name: got %v, want my-local-skill", m["name"])
	}
	if m["path"] != "/usr/local/bin/my-skill" {
		t.Errorf("path: got %v, want /usr/local/bin/my-skill", m["path"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tool factory function tests
// ─────────────────────────────────────────────────────────────────────────────

func TestNewLocalShellTool(t *testing.T) {
	tool := NewLocalShellTool()
	if tool.Name != "openai.local_shell" {
		t.Errorf("Name: got %q, want %q", tool.Name, "openai.local_shell")
	}
	if !tool.ProviderExecuted {
		t.Error("expected ProviderExecuted to be true")
	}
}

func TestNewShellTool_NoEnvironment(t *testing.T) {
	tool := NewShellTool()
	if tool.Name != "openai.shell" {
		t.Errorf("Name: got %q, want %q", tool.Name, "openai.shell")
	}
	if tool.ProviderOptions != nil {
		t.Error("expected nil ProviderOptions when no environment set")
	}
}

func TestNewShellTool_WithEnvironment(t *testing.T) {
	tool := NewShellTool(WithShellEnvironment(ShellEnvironment{
		Type: "container_auto",
	}))
	if tool.ProviderOptions == nil {
		t.Fatal("expected non-nil ProviderOptions")
	}
	env, ok := tool.ProviderOptions.(*ShellEnvironment)
	if !ok {
		t.Fatalf("ProviderOptions should be *ShellEnvironment, got %T", tool.ProviderOptions)
	}
	if env.Type != "container_auto" {
		t.Errorf("Environment.Type: got %q, want %q", env.Type, "container_auto")
	}
}

func TestNewApplyPatchTool(t *testing.T) {
	tool := NewApplyPatchTool()
	if tool.Name != "openai.apply_patch" {
		t.Errorf("Name: got %q, want %q", tool.Name, "openai.apply_patch")
	}
	if !tool.ProviderExecuted {
		t.Error("expected ProviderExecuted to be true")
	}
}
