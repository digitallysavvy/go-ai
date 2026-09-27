package harnessutil

import (
	"encoding/json"
	"testing"
)

// Ports TS `classify-disk-log.test.ts` (packages/harness/src/utils).
func line(t *testing.T, obj map[string]any) string {
	t.Helper()
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestClassifyDiskLogAbsentLogsReturnRerun(t *testing.T) {
	for _, eventLog := range []string{"", "   \n  \n"} {
		if got := ClassifyDiskLog(eventLog); got != DiskLogRerun {
			t.Errorf("ClassifyDiskLog(%q) = %q, want %q", eventLog, got, DiskLogRerun)
		}
	}
}

func TestClassifyDiskLogReplayWhenLastEventIsFinish(t *testing.T) {
	log := line(t, map[string]any{"type": "text-delta", "id": "m", "delta": "hi", "seq": 1}) + "\n" +
		line(t, map[string]any{"type": "finish", "seq": 2})
	if got := ClassifyDiskLog(log); got != DiskLogReplay {
		t.Errorf("ClassifyDiskLog = %q, want %q", got, DiskLogReplay)
	}
}

func TestClassifyDiskLogToleratesTrailingNewline(t *testing.T) {
	log := line(t, map[string]any{"type": "finish", "seq": 1}) + "\n"
	if got := ClassifyDiskLog(log); got != DiskLogReplay {
		t.Errorf("ClassifyDiskLog = %q, want %q", got, DiskLogReplay)
	}
}

func TestClassifyDiskLogRerunWhenTurnEndedMidStream(t *testing.T) {
	log := line(t, map[string]any{"type": "text-delta", "id": "m", "delta": "one", "seq": 1}) + "\n" +
		line(t, map[string]any{"type": "text-delta", "id": "m", "delta": "two", "seq": 2})
	if got := ClassifyDiskLog(log); got != DiskLogRerun {
		t.Errorf("ClassifyDiskLog = %q, want %q", got, DiskLogRerun)
	}
}

func TestClassifyDiskLogRerunWhenLastLineIsNonFinishEvent(t *testing.T) {
	log := line(t, map[string]any{"type": "finish", "seq": 1}) + "\n" +
		line(t, map[string]any{"type": "text-delta", "id": "m", "delta": "late", "seq": 2})
	if got := ClassifyDiskLog(log); got != DiskLogRerun {
		t.Errorf("ClassifyDiskLog = %q, want %q", got, DiskLogRerun)
	}
}

func TestClassifyDiskLogRerunWhenLastLineIsCorrupt(t *testing.T) {
	log := line(t, map[string]any{"type": "finish", "seq": 1}) + "\n{not json"
	if got := ClassifyDiskLog(log); got != DiskLogRerun {
		t.Errorf("ClassifyDiskLog = %q, want %q", got, DiskLogRerun)
	}
}
