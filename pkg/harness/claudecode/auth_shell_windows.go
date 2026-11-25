//go:build windows

package claudecode

import (
	"os"
	"os/exec"
	"syscall"
)

// systemShellCommand runs command through the platform shell, as Node's
// execSync does on Windows: %ComSpec% (default cmd.exe) with the verbatim
// arguments `/d /s /c "command"` and no window.
func systemShellCommand(command string) *exec.Cmd {
	shell := os.Getenv("ComSpec")
	if shell == "" {
		shell = "cmd.exe"
	}
	cmd := exec.Command(shell)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: shell + ` /d /s /c "` + command + `"`, HideWindow: true}
	return cmd
}
