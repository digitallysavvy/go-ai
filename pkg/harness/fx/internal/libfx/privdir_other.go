//go:build !unix

package libfx

import "os"

// privateDir reports whether path is a real directory. Ownership and ACLs
// are not checked on this platform; the cache root defaults to the
// per-user os.UserCacheDir().
func privateDir(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.IsDir()
}
