package harnessutil

import (
	"encoding/json"
	"strings"
)

// Disk-log replay classifications. Mirrors TS `classifyDiskLog`'s return
// union ('replay' | 'rerun').
const (
	DiskLogReplay = "replay"
	DiskLogRerun  = "rerun"
)

// ClassifyDiskLog decides whether a respawned bridge should reload its
// on-disk event log (`event-log.ndjson`) for replay, or start a fresh turn
// ("rerun"). raw is the file's content, or nil when the file does not exist
// or could not be read. The log is replayable only when it is non-empty and
// its LAST line parses as JSON with `type === "finish"` (a fully completed
// turn); anything else — no file, an empty file, a truncated/malformed last
// line, or a last line that is not a finish event — falls back to rerun.
// Mirrors TS `classifyDiskLog` (packages/harness/src/utils/classify-disk-log.ts).
func ClassifyDiskLog(raw *string) string {
	if raw == nil {
		return DiskLogRerun
	}
	text := strings.TrimSpace(*raw)
	if text == "" {
		return DiskLogRerun
	}
	lines := strings.Split(text, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if last == "" {
		return DiskLogRerun
	}
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(last), &probe); err != nil {
		return DiskLogRerun
	}
	if probe.Type == "finish" {
		return DiskLogReplay
	}
	return DiskLogRerun
}
