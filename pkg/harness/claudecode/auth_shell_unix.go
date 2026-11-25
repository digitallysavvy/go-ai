//go:build !windows

package claudecode

import "os/exec"

// systemShellCommand runs command through the platform shell, as Node's
// execSync does on Unix (`/bin/sh -c command`).
func systemShellCommand(command string) *exec.Cmd {
	return exec.Command("/bin/sh", "-c", command)
}
