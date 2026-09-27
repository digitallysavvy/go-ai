package providerutils

import "testing"

// Ported from packages/provider-utils/src/normalize-batch-request-counts.test.ts.

func intPtr(v int) *int { return &v }

func TestNormalizeBatchRequestCounts_ReturnsCompleteConsistentCounts(t *testing.T) {
	got := NormalizeBatchRequestCounts(intPtr(5), intPtr(2), intPtr(2), intPtr(1))
	if got == nil || got.Total != 5 || got.Pending != 2 || got.Completed != 2 || got.Failed != 1 {
		t.Fatalf("got = %+v", got)
	}
}

func TestNormalizeBatchRequestCounts_ReturnsNilForInvalidCounts(t *testing.T) {
	cases := []struct {
		name                              string
		total, pending, completed, failed *int
	}{
		{"missing total", nil, intPtr(0), intPtr(0), intPtr(0)},
		{"negative pending", intPtr(1), intPtr(-1), intPtr(1), intPtr(1)},
		{"inconsistent sum", intPtr(2), intPtr(0), intPtr(1), intPtr(0)},
		{"missing pending", intPtr(1), nil, intPtr(1), intPtr(0)},
		{"negative total", intPtr(-1), intPtr(0), intPtr(0), intPtr(0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeBatchRequestCounts(tc.total, tc.pending, tc.completed, tc.failed); got != nil {
				t.Fatalf("got = %+v, want nil", got)
			}
		})
	}
}
