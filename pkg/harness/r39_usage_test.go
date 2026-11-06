package harness

import "testing"

func TestR39FractionalTokenCounts(t *testing.T) {
	for _, n := range []string{"1.5", "1e3", "1000.0"} {
		p, err := DecodeStreamPart([]byte(`{"type":"finish","finishReason":{"unified":"stop"},"totalUsage":{"inputTokens":{"total":` + n + `},"outputTokens":{"text":2.5}}}`))
		if err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		u := p.(*FinishPart).TotalUsage
		if u.InputTokens.Total == nil || *u.InputTokens.Total < 1 || *u.OutputTokens.Text != 2 {
			t.Fatalf("%s: %+v", n, u)
		}
	}
	if _, err := DecodeStreamPart([]byte(`{"type":"finish","finishReason":{"unified":"stop"},"totalUsage":{"inputTokens":{"total":"x"},"outputTokens":{}}}`)); err == nil {
		t.Fatal("string count accepted")
	}
}
