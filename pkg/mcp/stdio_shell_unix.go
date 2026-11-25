//go:build !windows

package mcp

import "os/exec"

// shellCommand runs line through shellPath, or /bin/sh when empty.
func shellCommand(shellPath, line string) *exec.Cmd {
	if shellPath == "" {
		shellPath = "/bin/sh"
	}
	return exec.Command(shellPath, "-c", line)
}
