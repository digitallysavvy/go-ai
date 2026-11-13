package harnessutil

import "github.com/digitallysavvy/go-ai/pkg/internal/jsdate"

// ParseJSDate parses s like JavaScript's Date.parse in Node (V8): the ES
// date-time string format first (a date-only form is UTC, a date-time
// without an offset is local time), then V8's legacy fallback, which also
// reads forms such as "Tue, 27 Oct 2026 10:00:00 GMT", "Oct 27, 2026" and
// "2026/10/27 10:00". It returns the time in milliseconds since the epoch,
// or false where Date.parse returns NaN. It is jsdate.Parse.
func ParseJSDate(s string) (int64, bool) { return jsdate.Parse(s) }
