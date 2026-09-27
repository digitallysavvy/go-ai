package opencode

import (
	"encoding/json"
	"strings"
)

// diskLogRecoveryMode is the recovery rung selected from an on-disk bridge
// event log when attach is not possible (the bridge process is gone):
// "replay" when the log holds a complete turn the host can resume from its
// cursor, "rerun" otherwise. Mirrors TS `DiskLogRecoveryMode`.
type diskLogRecoveryMode string

const (
	diskLogReplay diskLogRecoveryMode = "replay"
	diskLogRerun  diskLogRecoveryMode = "rerun"
)

// classifyDiskLog decides whether a respawned bridge can replay a turn from
// its persisted event-log.ndjson, or must rerun it from scratch. A turn is
// replayable only when its log ends in a terminal `finish` event.
//
// Mirrors TS `classifyDiskLog` (packages/harness/src/utils/
// classify-disk-log.ts, `@ai-sdk/harness/utils`). Ported locally rather than
// added to pkg/harness/harnessutil to avoid a shared-file change outside this
// adapter's scope; see the WG9 implementation report for the hand-off note
// (other bridge adapters likely need the identical helper).
func classifyDiskLog(eventLog string) diskLogRecoveryMode {
	if eventLog == "" {
		return diskLogRerun
	}
	lines := strings.Split(eventLog, "\n")
	var lastLine string
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "" {
			lastLine = trimmed
			break
		}
	}
	if lastLine == "" {
		return diskLogRerun
	}
	var parsed struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(lastLine), &parsed); err != nil {
		return diskLogRerun
	}
	if parsed.Type == "finish" {
		return diskLogReplay
	}
	return diskLogRerun
}
