package openresponses

import (
	"encoding/json"
	"testing"
)

func TestConvertOpenResponsesUsageZeroVersusAbsentDetails(t *testing.T) {
	parse := func(s string) *Usage {
		var u Usage
		if err := json.Unmarshal([]byte(s), &u); err != nil {
			t.Fatal(err)
		}
		return &u
	}
	absent := convertOpenResponsesUsage(parse(`{"input_tokens":10,"output_tokens":5,"total_tokens":15}`))
	if absent.InputDetails.CacheReadTokens != nil || absent.OutputDetails.ReasoningTokens != nil {
		t.Errorf("absent details must stay undefined: %+v %+v", absent.InputDetails, absent.OutputDetails)
	}
	if *absent.InputDetails.NoCacheTokens != 10 || *absent.OutputDetails.TextTokens != 5 {
		t.Errorf("noCache/text wrong")
	}
	zero := convertOpenResponsesUsage(parse(`{"input_tokens":10,"output_tokens":5,"total_tokens":15,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}`))
	if zero.InputDetails.CacheReadTokens == nil || *zero.InputDetails.CacheReadTokens != 0 || zero.OutputDetails.ReasoningTokens == nil || *zero.OutputDetails.ReasoningTokens != 0 {
		t.Errorf("reported zero must stay 0: %+v %+v", zero.InputDetails, zero.OutputDetails)
	}
}
