package providerutils

import "github.com/digitallysavvy/go-ai/pkg/provider"

// NormalizeBatchRequestCounts mirrors TypeScript's
// normalizeBatchRequestCounts (packages/provider-utils/src/normalize-batch-request-counts.ts):
// returns nil unless every count is present (non-nil), non-negative, and
// pending+completed+failed equals total. Go's int has no separate "safe
// integer" range concern the way a JS float64-backed number does, so only
// presence and non-negativity need checking here.
func NormalizeBatchRequestCounts(total, pending, completed, failed *int) *provider.BatchRequestCounts {
	if total == nil || pending == nil || completed == nil || failed == nil {
		return nil
	}
	if *total < 0 || *pending < 0 || *completed < 0 || *failed < 0 {
		return nil
	}
	if *pending+*completed+*failed != *total {
		return nil
	}
	return &provider.BatchRequestCounts{
		Total:     *total,
		Pending:   *pending,
		Completed: *completed,
		Failed:    *failed,
	}
}
