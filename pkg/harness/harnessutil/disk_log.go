package harnessutil

import (
	"encoding/json"
	"strings"
)

// DiskLogRecoveryMode is the recovery rung selected from an on-disk bridge
// event log when attach is not possible (the bridge process is gone):
// "replay" when the log holds a complete turn the host can resume from its
// cursor, "rerun" otherwise. Mirrors TS `DiskLogRecoveryMode`.
type DiskLogRecoveryMode string

const (
	DiskLogReplay DiskLogRecoveryMode = "replay"
	DiskLogRerun  DiskLogRecoveryMode = "rerun"
)

// ClassifyDiskLog decides whether a respawned bridge can replay a turn from
// its persisted event-log.ndjson, or must rerun it from scratch. A turn is
// replayable only when its log ends in a terminal `finish` event.
//
// Mirrors TS `classifyDiskLog` (packages/harness/src/utils/
// classify-disk-log.ts, `@ai-sdk/harness/utils`). Shared by every bridge
// adapter that respawns a lost bridge process (opencode, acp, and — per the
// same TS helper — claude-code/codex, which should adopt this instead of a
// local copy if/when they need it).
func ClassifyDiskLog(eventLog string) DiskLogRecoveryMode {
	if eventLog == "" {
		return DiskLogRerun
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
		return DiskLogRerun
	}
	var parsed struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(lastLine), &parsed); err != nil {
		return DiskLogRerun
	}
	if parsed.Type == "finish" {
		return DiskLogReplay
	}
	return DiskLogRerun
}
