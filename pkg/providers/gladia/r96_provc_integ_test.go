package gladia

import "testing"

// TestR96provcPollOutOfRangeNumber: a number beyond the double range under an
// undeclared key reads as Infinity (JSON.parse); the poll result still
// converts.
func TestR96provcPollOutOfRangeNumber(t *testing.T) {
	_, res, err := r91provcRun(t, `{"status":"done","extra":1e400,"result":{"metadata":{"audio_duration":1},"transcription":{"full_transcript":"hello","languages":["en"],"utterances":[]}}}`, nil)
	if err != nil || res.Text != "hello" || res.Usage.DurationSeconds != 1 {
		t.Fatalf("err = %v, res = %+v", err, res)
	}
}
