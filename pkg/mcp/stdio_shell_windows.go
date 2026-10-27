//go:build windows

package mcp

import (
	"os"
	"os/exec"
	"syscall"
)

// shellCommand runs line through shellPath, or ComSpec (cmd.exe) when empty.
// The command line is passed verbatim (as Node does for shell: true) so cmd.exe
// sees /d /s /c "line" without Go's argv quoting.
func shellCommand(shellPath, line string) *exec.Cmd {
	if shellPath == "" {
		shellPath = os.Getenv("ComSpec")
		if shellPath == "" {
			shellPath = "cmd.exe"
		}
	}
	cmd := exec.Command(shellPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `"` + shellPath + `" /d /s /c "` + line + `"`}
	return cmd
}
