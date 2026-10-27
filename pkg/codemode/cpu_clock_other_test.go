//go:build !unix

package codemode

import "time"

// processCPUTime falls back to wall time where getrusage is unavailable.
func processCPUTime() time.Duration { return time.Duration(time.Now().UnixNano()) }
