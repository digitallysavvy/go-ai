package openresponses

import "testing"

// TS doGenerate computes noCache as (inputTokens ?? 0) - ... and text as
// (outputTokens ?? 0) - ..., so a response without usage still reports 0 for
// both while the totals stay undefined.
func TestR46_GenerateWithoutUsageReportsZeroNoCacheAndText(t *testing.T) {
	m := &LanguageModel{}
	res, err := m.convertResponse(OpenResponsesResponse{Output: []OutputItem{}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage.InputDetails == nil || res.Usage.InputDetails.NoCacheTokens == nil || *res.Usage.InputDetails.NoCacheTokens != 0 {
		t.Fatalf("input details = %+v", res.Usage.InputDetails)
	}
	if res.Usage.OutputDetails == nil || res.Usage.OutputDetails.TextTokens == nil || *res.Usage.OutputDetails.TextTokens != 0 {
		t.Fatalf("output details = %+v", res.Usage.OutputDetails)
	}
	if res.Usage.InputTokens != nil || res.Usage.OutputTokens != nil || res.Usage.TotalTokens != nil {
		t.Fatalf("totals must stay unset: %+v", res.Usage)
	}
}
