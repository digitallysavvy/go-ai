package providerutils

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports TS provider-utils map-reasoning-to-provider.test.ts (61a315e333).

func r85prepLevel(l types.ReasoningLevel) *types.ReasoningLevel { return &l }

func r85prepCompat(details string) []types.Warning {
	return []types.Warning{{Type: "compatibility", Feature: "reasoning", Details: details}}
}

var r85prepEffortMap = map[types.ReasoningLevel]string{
	types.ReasoningMinimal: "low",
	types.ReasoningLow:     "low",
	types.ReasoningMedium:  "medium",
	types.ReasoningHigh:    "high",
	types.ReasoningXHigh:   "max",
	types.ReasoningMax:     "max",
}

func TestMapReasoningToProviderEffort(t *testing.T) {
	turbo := "turbo"
	for _, tc := range []struct {
		name          string
		reasoning     *types.ReasoningLevel
		effortMap     map[types.ReasoningLevel]string
		providerValue *string
		want          string
		wantOK        bool
		wantWarnings  []types.Warning
	}{
		{"direct match", r85prepLevel(types.ReasoningMedium), r85prepEffortMap, nil, "medium", true, nil},
		{"renamed match", r85prepLevel(types.ReasoningMinimal), r85prepEffortMap, nil, "low", true,
			r85prepCompat(`reasoning "minimal" is not directly supported by this model. mapped to effort "low".`)},
		{"xhigh renamed", r85prepLevel(types.ReasoningXHigh), r85prepEffortMap, nil, "max", true,
			r85prepCompat(`reasoning "xhigh" is not directly supported by this model. mapped to effort "max".`)},
		{"rounds down", r85prepLevel(types.ReasoningXHigh), map[types.ReasoningLevel]string{
			types.ReasoningLow: "low", types.ReasoningMedium: "medium", types.ReasoningHigh: "high", types.ReasoningMax: "max",
		}, nil, "high", true,
			r85prepCompat(`reasoning "xhigh" is not directly supported by this model. mapped to effort "high".`)},
		{"rounds up when no lower level", r85prepLevel(types.ReasoningMinimal), map[types.ReasoningLevel]string{
			types.ReasoningLow: "low", types.ReasoningHigh: "high",
		}, nil, "low", true,
			r85prepCompat(`reasoning "minimal" is not directly supported by this model. mapped to effort "low".`)},
		{"never rounds down to none", r85prepLevel(types.ReasoningMinimal), map[types.ReasoningLevel]string{
			types.ReasoningNone: "none", types.ReasoningLow: "low", types.ReasoningHigh: "high",
		}, nil, "low", true,
			r85prepCompat(`reasoning "minimal" is not directly supported by this model. mapped to effort "low".`)},
		{"none supported", r85prepLevel(types.ReasoningNone), map[types.ReasoningLevel]string{
			types.ReasoningNone: "none", types.ReasoningLow: "low",
		}, nil, "none", true, nil},
		{"unsupported none to lowest", r85prepLevel(types.ReasoningNone), map[types.ReasoningLevel]string{
			types.ReasoningLow: "low", types.ReasoningMedium: "medium", types.ReasoningHigh: "high",
		}, nil, "low", true,
			r85prepCompat(`reasoning "none" is not directly supported by this model. mapped to effort "low".`)},
		{"empty map", r85prepLevel(types.ReasoningHigh), map[types.ReasoningLevel]string{}, nil, "", false,
			[]types.Warning{{Type: "unsupported", Feature: "reasoning", Details: `reasoning "high" is not supported by this model.`}}},
		{"provider options effort wins", r85prepLevel(types.ReasoningXHigh), map[types.ReasoningLevel]string{
			types.ReasoningLow: "low", types.ReasoningHigh: "high",
		}, &turbo, "turbo", true, nil},
		{"provider options effort wins without reasoning", nil, r85prepEffortMap, &turbo, "turbo", true, nil},
		{"nil provider options effort maps", r85prepLevel(types.ReasoningHigh), r85prepEffortMap, nil, "high", true, nil},
		{"undefined", nil, r85prepEffortMap, nil, "", false, nil},
		{"provider-default", r85prepLevel(types.ReasoningDefault), r85prepEffortMap, nil, "", false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var warnings []types.Warning
			got, ok := MapReasoningToProviderEffort(tc.reasoning, tc.effortMap, tc.providerValue, &warnings)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("got (%q, %v), want (%q, %v)", got, ok, tc.want, tc.wantOK)
			}
			if !reflect.DeepEqual(warnings, tc.wantWarnings) {
				t.Fatalf("warnings = %#v, want %#v", warnings, tc.wantWarnings)
			}
		})
	}
}

func TestMapReasoningToProviderEffortNilWarnings(t *testing.T) {
	if got, ok := MapReasoningToProviderEffort(r85prepLevel(types.ReasoningNone), r85prepEffortMap, nil, nil); got != "low" || !ok {
		t.Fatalf("got (%q, %v)", got, ok)
	}
}

func TestIsCustomReasoning(t *testing.T) {
	if IsCustomReasoning(nil) || IsCustomReasoning(r85prepLevel(types.ReasoningDefault)) || IsCustomReasoning(r85prepLevel("")) {
		t.Fatal("nil, provider-default and empty must not be custom")
	}
	for _, l := range []types.ReasoningLevel{types.ReasoningNone, types.ReasoningMinimal, types.ReasoningLow,
		types.ReasoningMedium, types.ReasoningHigh, types.ReasoningXHigh, types.ReasoningMax} {
		if !IsCustomReasoning(r85prepLevel(l)) {
			t.Fatalf("%q must be custom", l)
		}
	}
}

func TestMapReasoningToProviderBudget(t *testing.T) {
	i := func(v int) *int { return &v }
	for _, tc := range []struct {
		name         string
		reasoning    types.ReasoningLevel
		opts         ReasoningBudgetOptions
		want         int
		wantWarnings []types.Warning
	}{
		{"known key", types.ReasoningMedium, ReasoningBudgetOptions{MaxOutputTokens: 64000, MaxReasoningBudget: 64000}, 19200, nil},
		{"capped", types.ReasoningXHigh, ReasoningBudgetOptions{MaxOutputTokens: 64000, MaxReasoningBudget: 50000}, 50000, nil},
		{"max is 95 percent", types.ReasoningMax, ReasoningBudgetOptions{MaxOutputTokens: 64000, MaxReasoningBudget: 64000}, 60800, nil},
		{"default floor 1024", types.ReasoningMinimal, ReasoningBudgetOptions{MaxOutputTokens: 10000, MaxReasoningBudget: 10000}, 1024, nil},
		{"custom floor", types.ReasoningMinimal, ReasoningBudgetOptions{MaxOutputTokens: 10000, MaxReasoningBudget: 10000, MinReasoningBudget: i(512)}, 512, nil},
		{"explicit zero floor", types.ReasoningMinimal, ReasoningBudgetOptions{MaxOutputTokens: 10000, MaxReasoningBudget: 10000, MinReasoningBudget: i(0)}, 200, nil},
		{"custom percentages", types.ReasoningMedium, ReasoningBudgetOptions{MaxOutputTokens: 10000, MaxReasoningBudget: 10000,
			BudgetPercentages: map[types.ReasoningLevel]float64{types.ReasoningMedium: 0.5}}, 5000, nil},
		{"default for missing custom level", types.ReasoningHigh, ReasoningBudgetOptions{MaxOutputTokens: 64000, MaxReasoningBudget: 64000,
			BudgetPercentages: map[types.ReasoningLevel]float64{types.ReasoningMedium: 0.5}}, 38400, nil},
		{"rounds half up", types.ReasoningMinimal, ReasoningBudgetOptions{MaxOutputTokens: 64025, MaxReasoningBudget: 64025, MinReasoningBudget: i(0)}, 1281, nil},
		{"none to minimum", types.ReasoningNone, ReasoningBudgetOptions{MaxOutputTokens: 64000, MaxReasoningBudget: 64000, MinReasoningBudget: i(128)}, 128,
			r85prepCompat(`reasoning "none" is not supported by this model. mapped to the minimum reasoning budget of 128 tokens.`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var warnings []types.Warning
			if got := MapReasoningToProviderBudget(tc.reasoning, tc.opts, &warnings); got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
			if !reflect.DeepEqual(warnings, tc.wantWarnings) {
				t.Fatalf("warnings = %#v, want %#v", warnings, tc.wantWarnings)
			}
		})
	}
}
