// Package intsafe provides overflow-checked integer arithmetic helpers for
// sizing allocations (e.g. make([]T, 0, intsafe.AddCap(len(a), len(b)))).
//
// In practice the lengths involved (slice/map lengths, counts derived from
// in-memory data) can never come close to math.MaxInt, so a plain a+b never
// actually overflows. These helpers exist so the addition that sizes an
// allocation is explicitly bounds-checked rather than a bare AddExpr feeding
// directly into make(), which static analysis (CodeQL's
// go/allocation-size-overflow) flags as a potential overflow.
package intsafe

import "math"

// AddCap returns a+b, saturating at math.MaxInt instead of wrapping if the
// sum would overflow. a and b are expected to be non-negative (e.g. slice or
// map lengths); a negative argument is treated as 0 for the purposes of the
// overflow check.
func AddCap(a, b int) int {
	if a < 0 {
		a = 0
	}
	if b < 0 {
		b = 0
	}
	if a > math.MaxInt-b {
		return math.MaxInt
	}
	return a + b
}
