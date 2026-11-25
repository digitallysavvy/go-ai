package bridge

import (
	"math"
	"testing"
)

// TS SandboxChannel.handleIncoming reads `seq` from any parsed object
// (`typeof value.seq === 'number'`) before the schema runs, so a frame that
// fails validation still advances the cursor, and JSON.parse reads 1e400 as
// Infinity.
func TestR98carryDecodeOutboundReadsSeqLikeTS(t *testing.T) {
	for _, tc := range []struct {
		frame string
		want  float64
	}{
		{`{"seq":5}`, 5},
		{`{"seq":6,"type":7}`, 6},
		{`{"seq":7,"type":"bridge-thread"}`, 7},
	} {
		_, seq, err := DecodeOutbound([]byte(tc.frame))
		if err == nil {
			t.Fatalf("%s: want a validation error", tc.frame)
		}
		if seq == nil || *seq != tc.want {
			t.Fatalf("%s: seq = %v, want %v", tc.frame, seq, tc.want)
		}
	}
	_, seq, err := DecodeOutbound([]byte(`{"type":"text-delta","id":"a","delta":"x","seq":1e400}`))
	if err != nil {
		t.Fatal(err)
	}
	if seq == nil || !math.IsInf(*seq, 1) {
		t.Fatalf("seq = %v, want +Inf", seq)
	}
	for _, frame := range []string{`{"seq":"5","type":"bridge-thread","threadId":"t"}`, `[1]`, `{"__proto__":{},"seq":5}`} {
		if _, seq, _ := DecodeOutbound([]byte(frame)); seq != nil {
			t.Fatalf("%s: seq = %v, want none", frame, *seq)
		}
	}
}
