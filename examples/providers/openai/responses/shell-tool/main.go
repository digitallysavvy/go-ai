// Package main demonstrates the OpenAI Responses API shell container tools.
//
// Shell tools allow the model to execute commands in a sandboxed container
// or local environment. This example shows:
//   - Building a local_shell tool (no container)
//   - Building a shell tool with a managed container environment
//   - Building a shell tool with a referenced container
//   - Building an apply_patch tool for filesystem operations
//   - Serializing all tool types to the wire format expected by the Responses API
//
// Run with:
//
//	go run main.go
package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

func main() {
	fmt.Println("=== OpenAI Responses API: Shell Container Tool Examples ===")
	fmt.Println()

	// Example 1: Local shell tool
	fmt.Println("--- Example 1: Local Shell Tool ---")
	localShellExample()

	fmt.Println()

	// Example 2: Shell tool with auto-managed container
	fmt.Println("--- Example 2: Shell Tool with Container Auto Environment ---")
	shellContainerAutoExample()

	fmt.Println()

	// Example 3: Shell tool with container reference
	fmt.Println("--- Example 3: Shell Tool with Container Reference ---")
	shellContainerReferenceExample()

	fmt.Println()

	// Example 4: Apply patch tool
	fmt.Println("--- Example 4: Apply Patch Tool ---")
	applyPatchExample()

	fmt.Println()

	// Example 5: All shell tools combined
	fmt.Println("--- Example 5: All Shell Tools Combined ---")
	allShellToolsExample()
}

// localShellExample shows a tool for executing commands in the local (sandboxed) shell.
// This is the simplest shell tool — no container configuration needed.
func localShellExample() {
	tool := responses.NewLocalShellTool()
	printToolWireFormat("local_shell", tool)
}

// shellContainerAutoExample shows a shell tool with an auto-provisioned container.
// The container_auto environment spins up a fresh container for each session.
func shellContainerAutoExample() {
	memLimit := responses.MemoryLimit4G // valid values: 1g, 4g, 16g, 64g

	tool := responses.NewShellTool(
		responses.WithShellEnvironment(responses.ShellEnvironment{
			Type:        "container_auto",
			MemoryLimit: &memLimit,
			FileIDs:     []string{"file-abc123", "file-def456"}, // files to mount
			NetworkPolicy: &responses.ShellNetworkPolicy{
				Type:           "allowlist",
				AllowedDomains: []string{"pypi.org", "files.pythonhosted.org"},
			},
		}),
	)

	printToolWireFormat("shell (container_auto)", tool)
}

// shellContainerReferenceExample shows a shell tool pointing to an existing container.
// Use container_reference when you want the model to reuse a specific container.
func shellContainerReferenceExample() {
	containerID := "cntr_abc123xyz"

	tool := responses.NewShellTool(
		responses.WithShellEnvironment(responses.ShellEnvironment{
			Type:        "container_reference",
			ContainerID: &containerID,
		}),
	)

	printToolWireFormat("shell (container_reference)", tool)
}

// applyPatchExample shows the apply_patch tool for making filesystem changes.
// The model uses this to create, update, or delete files via unified diffs.
func applyPatchExample() {
	tool := responses.NewApplyPatchTool()
	printToolWireFormat("apply_patch", tool)
}

// allShellToolsExample shows how to combine multiple shell tools in one request.
func allShellToolsExample() {
	memLimit := responses.MemoryLimit4G

	tools := []types.Tool{
		responses.NewLocalShellTool(),
		responses.NewShellTool(
			responses.WithShellEnvironment(responses.ShellEnvironment{
				Type:        "container_auto",
				MemoryLimit: &memLimit,
			}),
		),
		responses.NewApplyPatchTool(),
	}

	prepared := responses.PrepareTools(tools)
	data, err := json.MarshalIndent(prepared, "", "  ")
	if err != nil {
		log.Fatalf("marshal failed: %v", err)
	}
	fmt.Printf("Wire format for %d shell tools:\n%s\n", len(prepared), data)
}

// printToolWireFormat shows the JSON wire format for a single tool.
func printToolWireFormat(label string, tool types.Tool) {
	prepared := responses.PrepareTools([]types.Tool{tool})
	if len(prepared) == 0 {
		fmt.Printf("%s: (no tools prepared)\n", label)
		return
	}

	data, err := json.MarshalIndent(prepared[0], "", "  ")
	if err != nil {
		log.Fatalf("marshal failed: %v", err)
	}
	fmt.Printf("Wire format for %s:\n%s\n", label, data)
}
