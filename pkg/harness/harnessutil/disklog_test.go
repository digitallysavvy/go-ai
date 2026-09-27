package harnessutil

import "testing"

// TS `classify-disk-log.test.ts` (packages/harness/src/utils/classify-disk-log.ts).
func TestClassifyDiskLog(t *testing.T) {
	tests := []struct {
		name string
		raw  *string
		want string
	}{
		{"nil (file missing)", nil, DiskLogRerun},
		{"empty file", strPtr(""), DiskLogRerun},
		{"whitespace only", strPtr("   \n\t\n"), DiskLogRerun},
		{"last line malformed JSON", strPtr(`{"seq":1,"type":"text-delta"}` + "\n{not json"), DiskLogRerun},
		{"last line is not a finish event", strPtr(`{"seq":1,"type":"finish"}` + "\n" + `{"seq":2,"type":"text-delta"}`), DiskLogRerun},
		{"single finish line", strPtr(`{"seq":1,"type":"finish"}`), DiskLogReplay},
		{"multi-line log ending in finish", strPtr(`{"seq":1,"type":"text-start"}` + "\n" + `{"seq":2,"type":"text-delta"}` + "\n" + `{"seq":3,"type":"finish"}`), DiskLogReplay},
		{"trailing newline after finish", strPtr(`{"seq":1,"type":"finish"}` + "\n"), DiskLogReplay},
		{"finish with extra fields", strPtr(`{"seq":3,"type":"finish","finishReason":{"unified":"stop"}}`), DiskLogReplay},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyDiskLog(tt.raw); got != tt.want {
				t.Errorf("ClassifyDiskLog(%v) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
