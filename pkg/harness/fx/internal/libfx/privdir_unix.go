//go:build unix

package libfx

import (
	"os"
	"syscall"
)

// privateDir reports whether path is a real directory (not a symlink)
// owned by this user and not writable by group or others. The compilation
// cache holds native code that wazero maps and runs after only a CRC32
// check, so it is used only from a directory nobody else can write.
func privateDir(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil || !fi.IsDir() || fi.Mode().Perm()&0o022 != 0 {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}
