package gemini

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TS addGoogleUsageMetadata merges {...a, ...b}: a key the later request sent
// (an explicit null or "") replaces the earlier value, and providerMetadata
// serviceTier is `usageMetadata?.serviceTier ?? null`. Expected values from a
// bun run of TS 3fab50aa9b convert-google-usage.ts on the same objects.
func TestR87eprovaUsageMetadataMergeNullOverrides(t *testing.T) {
	a := `{"promptTokenCount":1,"candidatesTokenCount":2,"serviceTier":"standard","trafficType":"ON_DEMAND"}`
	for _, tc := range []struct{ b, want, tier string }{
		{`{"promptTokenCount":3,"serviceTier":null,"trafficType":""}`, `{"promptTokenCount":4,"candidatesTokenCount":2,"serviceTier":null,"trafficType":""}`, `null`},
		{`{"promptTokenCount":3}`, `{"promptTokenCount":4,"candidatesTokenCount":2,"serviceTier":"standard","trafficType":"ON_DEMAND"}`, `"standard"`},
		{`{"promptTokenCount":3,"serviceTier":"flex"}`, `{"promptTokenCount":4,"candidatesTokenCount":2,"serviceTier":"flex","trafficType":"ON_DEMAND"}`, `"flex"`},
	} {
		var ua, ub UsageMetadata
		if err := json.Unmarshal([]byte(a), &ua); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(tc.b), &ub); err != nil {
			t.Fatal(err)
		}
		sum := addUsageMetadata(&ua, &ub)
		got, err := json.Marshal(sum)
		if err != nil {
			t.Fatal(err)
		}
		var gotV, wantV interface{}
		_ = json.Unmarshal(got, &gotV)
		_ = json.Unmarshal([]byte(tc.want), &wantV)
		if !reflect.DeepEqual(gotV, wantV) {
			t.Errorf("b=%s: merged %s, want %s", tc.b, got, tc.want)
		}
		if tier := string(serviceTierJSON(sum)); tier != tc.tier {
			t.Errorf("b=%s: serviceTier %s, want %s", tc.b, tier, tc.tier)
		}
	}
}

// TS `usageMetadata?.serviceTier ?? null` keeps an empty tier the API sent.
func TestR87eprovaServiceTierEmptyString(t *testing.T) {
	for in, want := range map[string]string{
		`{"serviceTier":""}`:     `""`,
		`{"serviceTier":null}`:   `null`,
		`{}`:                     `null`,
		`{"serviceTier":"flex"}`: `"flex"`,
	} {
		var u UsageMetadata
		if err := json.Unmarshal([]byte(in), &u); err != nil {
			t.Fatal(err)
		}
		if got := string(serviceTierJSON(&u)); got != want {
			t.Errorf("%s: serviceTier %s, want %s", in, got, want)
		}
	}
	if got := string(serviceTierJSON(nil)); got != "null" {
		t.Errorf("nil usage: %s", got)
	}
}
