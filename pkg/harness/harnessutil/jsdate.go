package harnessutil

import (
	"math"
	"time"
	"unicode"
)

// ParseJSDate parses s like JavaScript's Date.parse in Node (V8): the ES
// date-time string format first (a date-only form is UTC, a date-time
// without an offset is local time), then V8's legacy fallback, which also
// reads forms such as "Tue, 27 Oct 2026 10:00:00 GMT", "Oct 27, 2026" and
// "2026/10/27 10:00". It returns the time in milliseconds since the epoch,
// or false where Date.parse returns NaN.
func ParseJSDate(s string) (int64, bool) { return parseJSDateIn(s, time.Local) }

// A port of V8's DateParser (src/date/dateparser*.{h,cc}).

const jsdNone = math.MaxInt32

type jsdTokenKind int

const (
	jsdInvalid jsdTokenKind = iota
	jsdUnknown
	jsdNumber
	jsdSymbol
	jsdWhiteSpace
	jsdKeyword
	jsdEnd
)

type jsdKeywordType int

const (
	jsdKwInvalid jsdKeywordType = iota
	jsdKwMonthName
	jsdKwTimeZoneName
	jsdKwTimeSeparator
	jsdKwAMPM
)

type jsdToken struct {
	kind   jsdTokenKind
	value  int // number value, symbol rune, keyword value
	length int
	kw     jsdKeywordType
}

func (t jsdToken) isSymbol(c rune) bool           { return t.kind == jsdSymbol && rune(t.value) == c }
func (t jsdToken) isAsciiSign() bool              { return t.isSymbol('+') || t.isSymbol('-') }
func (t jsdToken) isFixedLengthNumber(n int) bool { return t.kind == jsdNumber && t.length == n }
func (t jsdToken) isKeywordZ() bool {
	return t.kind == jsdKeyword && t.kw == jsdKwTimeZoneName && t.length == 1 && t.value == 0
}

func (t jsdToken) asciiSign() int {
	if t.isSymbol('-') {
		return -1
	}
	return 1
}

var jsdKeywords = []struct {
	prefix [3]rune
	kw     jsdKeywordType
	value  int
}{
	{[3]rune{'j', 'a', 'n'}, jsdKwMonthName, 1}, {[3]rune{'f', 'e', 'b'}, jsdKwMonthName, 2}, {[3]rune{'m', 'a', 'r'}, jsdKwMonthName, 3},
	{[3]rune{'a', 'p', 'r'}, jsdKwMonthName, 4}, {[3]rune{'m', 'a', 'y'}, jsdKwMonthName, 5}, {[3]rune{'j', 'u', 'n'}, jsdKwMonthName, 6},
	{[3]rune{'j', 'u', 'l'}, jsdKwMonthName, 7}, {[3]rune{'a', 'u', 'g'}, jsdKwMonthName, 8}, {[3]rune{'s', 'e', 'p'}, jsdKwMonthName, 9},
	{[3]rune{'o', 'c', 't'}, jsdKwMonthName, 10}, {[3]rune{'n', 'o', 'v'}, jsdKwMonthName, 11}, {[3]rune{'d', 'e', 'c'}, jsdKwMonthName, 12},
	{[3]rune{'a', 'm', 0}, jsdKwAMPM, 0}, {[3]rune{'p', 'm', 0}, jsdKwAMPM, 12},
	{[3]rune{'u', 't', 0}, jsdKwTimeZoneName, 0}, {[3]rune{'u', 't', 'c'}, jsdKwTimeZoneName, 0}, {[3]rune{'z', 0, 0}, jsdKwTimeZoneName, 0},
	{[3]rune{'g', 'm', 't'}, jsdKwTimeZoneName, 0}, {[3]rune{'c', 'd', 't'}, jsdKwTimeZoneName, -5}, {[3]rune{'c', 's', 't'}, jsdKwTimeZoneName, -6},
	{[3]rune{'e', 'd', 't'}, jsdKwTimeZoneName, -4}, {[3]rune{'e', 's', 't'}, jsdKwTimeZoneName, -5}, {[3]rune{'m', 'd', 't'}, jsdKwTimeZoneName, -6},
	{[3]rune{'m', 's', 't'}, jsdKwTimeZoneName, -7}, {[3]rune{'p', 'd', 't'}, jsdKwTimeZoneName, -7}, {[3]rune{'p', 's', 't'}, jsdKwTimeZoneName, -8},
	{[3]rune{'t', 0, 0}, jsdKwTimeSeparator, 0},
}

// jsdIsWhiteSpace is the white space the date parser skips in Node: the
// Zs characters, tab, vertical tab, form feed, NBSP, BOM, LF and CR. U+2028
// and U+2029 are not white space here; they read as word characters
// (Date.parse("1\u2028Jan 2000") is NaN in Node).
func jsdIsWhiteSpace(c rune) bool {
	switch c {
	case '\t', '\v', '\f', ' ', 0xA0, 0xFEFF, '\n', '\r':
		return true
	}
	return unicode.Is(unicode.Zs, c)
}

// jsdTokenizer is V8's DateStringTokenizer over UTF-16 code units.
type jsdTokenizer struct {
	in   []rune
	pos  int
	next jsdToken
}

func newJSDTokenizer(s string) *jsdTokenizer {
	var in []rune
	for _, r := range s {
		if r == 0 {
			// V8's InputReader ends the input at the first NUL code unit
			// (IsEnd is ch_ == 0), so nothing after it is read.
			break
		}
		if r > 0xFFFF {
			// Two code units, neither of which V8 reads as anything but a
			// word character.
			in = append(in, 0xD800, 0xDC00)
			continue
		}
		in = append(in, r)
	}
	t := &jsdTokenizer{in: in}
	t.next = t.scan()
	return t
}

func (t *jsdTokenizer) ch() rune {
	if t.pos < len(t.in) {
		return t.in[t.pos]
	}
	return 0
}

func (t *jsdTokenizer) peek() jsdToken { return t.next }

func (t *jsdTokenizer) nextToken() jsdToken {
	out := t.next
	t.next = t.scan()
	return out
}

func (t *jsdTokenizer) skipSymbol(c rune) bool {
	if t.next.isSymbol(c) {
		t.nextToken()
		return true
	}
	return false
}

func (t *jsdTokenizer) scan() jsdToken {
	pre := t.pos
	if t.pos >= len(t.in) {
		return jsdToken{kind: jsdEnd}
	}
	c := t.ch()
	if c >= '0' && c <= '9' {
		// ReadUnsignedNumeral: leading zeros skipped, at most 9 significant
		// digits kept.
		n, i := 0, 0
		for t.ch() == '0' && t.pos < len(t.in) {
			t.pos++
		}
		for t.pos < len(t.in) && t.ch() >= '0' && t.ch() <= '9' {
			if i < 9 {
				n = n*10 + int(t.ch()-'0')
			}
			i++
			t.pos++
		}
		return jsdToken{kind: jsdNumber, value: n, length: t.pos - pre}
	}
	switch c {
	case ':', '-', '+', '.', ')':
		t.pos++
		return jsdToken{kind: jsdSymbol, value: int(c), length: 1}
	}
	if c >= 'A' && !jsdIsWhiteSpace(c) {
		var prefix [3]rune
		n := 0
		for t.pos < len(t.in) && t.ch() >= 'A' && !jsdIsWhiteSpace(t.ch()) {
			if n < 3 {
				prefix[n] = t.ch() | 0x20
			}
			n++
			t.pos++
		}
		for _, k := range jsdKeywords {
			if prefix == k.prefix && (n <= 3 || k.kw == jsdKwMonthName) {
				return jsdToken{kind: jsdKeyword, kw: k.kw, value: k.value, length: n}
			}
		}
		return jsdToken{kind: jsdKeyword, kw: jsdKwInvalid, length: n}
	}
	if jsdIsWhiteSpace(c) {
		for t.pos < len(t.in) && jsdIsWhiteSpace(t.ch()) {
			t.pos++
		}
		return jsdToken{kind: jsdWhiteSpace, length: t.pos - pre}
	}
	if c == '(' {
		balance := 0
		for {
			switch t.ch() {
			case ')':
				balance--
			case '(':
				balance++
			}
			t.pos++
			if balance <= 0 || t.pos >= len(t.in) {
				break
			}
		}
		return jsdToken{kind: jsdUnknown}
	}
	t.pos++
	return jsdToken{kind: jsdUnknown}
}

type jsdTime struct {
	comp       [4]int
	index      int
	hourOffset int
}

func jsdBetween(x, lo, hi int) bool { return x >= lo && x <= hi }
func jsdIsMinute(x int) bool        { return jsdBetween(x, 0, 59) }
func jsdIsHour(x int) bool          { return jsdBetween(x, 0, 23) }

func (c *jsdTime) isEmpty() bool { return c.index == 0 }
func (c *jsdTime) add(n int) bool {
	if c.index < 4 {
		c.comp[c.index] = n
		c.index++
		return true
	}
	return false
}

func (c *jsdTime) addFinal(n int) bool {
	if !c.add(n) {
		return false
	}
	for c.index < 4 {
		c.comp[c.index] = 0
		c.index++
	}
	return true
}

func (c *jsdTime) isExpecting(n int) bool {
	return (c.index == 1 && jsdIsMinute(n)) || (c.index == 2 && jsdIsMinute(n)) || (c.index == 3 && jsdBetween(n, 0, 999))
}

func (c *jsdTime) write() (h, m, s, ms int, ok bool) {
	for c.index < 4 {
		c.comp[c.index] = 0
		c.index++
	}
	h, m, s, ms = c.comp[0], c.comp[1], c.comp[2], c.comp[3]
	if c.hourOffset != jsdNone {
		if !jsdBetween(h, 0, 12) {
			return 0, 0, 0, 0, false
		}
		h = h%12 + c.hourOffset
	}
	if !jsdIsHour(h) || !jsdIsMinute(m) || !jsdIsMinute(s) || !jsdBetween(ms, 0, 999) {
		if h != 24 || m != 0 || s != 0 || ms != 0 {
			return 0, 0, 0, 0, false
		}
	}
	return h, m, s, ms, true
}

type jsdZone struct{ sign, hour, minute int }

func (z *jsdZone) set(offsetHours int) {
	z.sign = 1
	if offsetHours < 0 {
		z.sign = -1
	}
	z.hour = offsetHours * z.sign
	z.minute = 0
}
func (z *jsdZone) isExpecting(n int) bool {
	return z.hour != jsdNone && z.minute == jsdNone && jsdIsMinute(n)
}
func (z *jsdZone) isUTC() bool   { return z.hour == 0 && z.minute == 0 }
func (z *jsdZone) isEmpty() bool { return z.hour == jsdNone }

// write returns the UTC offset in seconds, or local=true.
func (z *jsdZone) write() (offset int64, local, ok bool) {
	if z.sign == jsdNone {
		return 0, true, true
	}
	h, m := z.hour, z.minute
	if h == jsdNone {
		h = 0
	}
	if m == jsdNone {
		m = 0
	}
	total := uint64(uint32(h))*3600 + uint64(uint32(m))*60
	total &= math.MaxUint32
	// Smi::kMaxValue: Node builds V8 without pointer compression, so Smis
	// are 32 bits wide.
	if total > math.MaxInt32 {
		return 0, false, false
	}
	offset = int64(total)
	if z.sign < 0 {
		offset = -offset
	}
	return offset, false, true
}

type jsdDay struct {
	comp       [3]int
	index      int
	namedMonth int
	isoDate    bool
}

func (d *jsdDay) isEmpty() bool { return d.index == 0 }
func (d *jsdDay) add(n int) bool {
	if d.index < 3 {
		d.comp[d.index] = n
		d.index++
		return true
	}
	return false
}

func (d *jsdDay) write() (year, month, day int, ok bool) {
	if d.index < 1 {
		return 0, 0, 0, false
	}
	for d.index < 3 {
		d.comp[d.index] = 1
		d.index++
	}
	isDay := func(x int) bool { return jsdBetween(x, 1, 31) }
	if d.namedMonth == jsdNone {
		if d.isoDate || (d.index == 3 && !isDay(d.comp[0])) {
			year, month, day = d.comp[0], d.comp[1], d.comp[2]
		} else {
			month, day = d.comp[0], d.comp[1]
			if d.index == 3 {
				year = d.comp[2]
			}
		}
	} else {
		month = d.namedMonth
		if d.index == 1 {
			day = d.comp[0]
		} else if !isDay(d.comp[0]) {
			year, day = d.comp[0], d.comp[1]
		} else {
			day, year = d.comp[0], d.comp[1]
		}
	}
	if !d.isoDate {
		if jsdBetween(year, 0, 49) {
			year += 2000
		} else if jsdBetween(year, 50, 99) {
			year += 1900
		}
	}
	if year < math.MinInt32 || year > math.MaxInt32 || !jsdBetween(month, 1, 12) || !isDay(day) {
		return 0, 0, 0, false
	}
	return year, month, day, true
}

func readMilliseconds(t jsdToken) int {
	n, length := t.value, t.length
	switch {
	case length == 1:
		n *= 100
	case length == 2:
		n *= 10
	case length > 3:
		if length > 9 {
			length = 9
		}
		factor := 1
		for ; length > 3; length-- {
			factor *= 10
		}
		n /= factor
	}
	return n
}

func parseES5DateTime(sc *jsdTokenizer, day *jsdDay, tm *jsdTime, tz *jsdZone) jsdToken {
	invalid := jsdToken{kind: jsdInvalid}
	switch {
	case sc.peek().isAsciiSign():
		sign := sc.nextToken()
		if !sc.peek().isFixedLengthNumber(6) {
			return sign
		}
		year := sc.nextToken().value
		if sign.asciiSign() < 0 && year == 0 {
			return sign
		}
		day.add(sign.asciiSign() * year)
	case sc.peek().isFixedLengthNumber(4):
		day.add(sc.nextToken().value)
	default:
		return sc.nextToken()
	}
	if sc.skipSymbol('-') {
		if !sc.peek().isFixedLengthNumber(2) || !jsdBetween(sc.peek().value, 1, 12) {
			return sc.nextToken()
		}
		day.add(sc.nextToken().value)
		if sc.skipSymbol('-') {
			if !sc.peek().isFixedLengthNumber(2) || !jsdBetween(sc.peek().value, 1, 31) {
				return sc.nextToken()
			}
			day.add(sc.nextToken().value)
		}
	}
	if sc.peek().kind != jsdKeyword || sc.peek().kw != jsdKwTimeSeparator {
		if sc.peek().kind != jsdEnd {
			return sc.nextToken()
		}
	} else {
		sc.nextToken()
		if !sc.peek().isFixedLengthNumber(2) || !jsdBetween(sc.peek().value, 0, 24) {
			return invalid
		}
		hourIs24 := sc.peek().value == 24
		tm.add(sc.nextToken().value)
		if !sc.skipSymbol(':') {
			return invalid
		}
		if !sc.peek().isFixedLengthNumber(2) || !jsdIsMinute(sc.peek().value) || (hourIs24 && sc.peek().value > 0) {
			return invalid
		}
		tm.add(sc.nextToken().value)
		if sc.skipSymbol(':') {
			if !sc.peek().isFixedLengthNumber(2) || !jsdIsMinute(sc.peek().value) || (hourIs24 && sc.peek().value > 0) {
				return invalid
			}
			tm.add(sc.nextToken().value)
			if sc.skipSymbol('.') {
				if sc.peek().kind != jsdNumber || (hourIs24 && sc.peek().value > 0) {
					return invalid
				}
				tm.add(readMilliseconds(sc.nextToken()))
			}
		}
		if sc.peek().isKeywordZ() {
			sc.nextToken()
			tz.set(0)
		} else if sc.peek().isSymbol('+') || sc.peek().isSymbol('-') {
			tz.sign = sc.nextToken().asciiSign()
			if sc.peek().isFixedLengthNumber(4) {
				hourmin := sc.nextToken().value
				hour, minute := hourmin/100, hourmin%100
				if !jsdIsHour(hour) || !jsdIsMinute(minute) {
					return invalid
				}
				tz.hour, tz.minute = hour, minute
			} else {
				if !sc.peek().isFixedLengthNumber(2) || !jsdIsHour(sc.peek().value) {
					return invalid
				}
				tz.hour = sc.nextToken().value
				if !sc.skipSymbol(':') {
					return invalid
				}
				if !sc.peek().isFixedLengthNumber(2) || !jsdIsMinute(sc.peek().value) {
					return invalid
				}
				tz.minute = sc.nextToken().value
			}
		}
		if sc.peek().kind != jsdEnd {
			return invalid
		}
	}
	if tz.isEmpty() && tm.isEmpty() {
		tz.set(0)
	}
	day.isoDate = true
	return jsdToken{kind: jsdEnd}
}

func parseJSDateIn(s string, loc *time.Location) (int64, bool) {
	sc := newJSDTokenizer(s)
	tz := &jsdZone{sign: jsdNone, hour: jsdNone, minute: jsdNone}
	tm := &jsdTime{hourOffset: jsdNone}
	day := &jsdDay{namedMonth: jsdNone}

	tok := parseES5DateTime(sc, day, tm, tz)
	if tok.kind == jsdInvalid {
		return 0, false
	}
	hasReadNumber := !day.isEmpty()
	for ; tok.kind != jsdEnd; tok = sc.nextToken() {
		switch {
		case tok.kind == jsdNumber:
			hasReadNumber = true
			n := tok.value
			switch {
			case sc.skipSymbol(':'):
				if sc.skipSymbol(':') {
					if !tm.isEmpty() {
						return 0, false
					}
					tm.add(n)
					tm.add(0)
				} else {
					if !tm.add(n) {
						return 0, false
					}
					if sc.peek().isSymbol('.') {
						sc.nextToken()
					}
				}
			case sc.skipSymbol('.') && tm.isExpecting(n):
				tm.add(n)
				if sc.peek().kind != jsdNumber {
					return 0, false
				}
				ms := readMilliseconds(sc.nextToken())
				if ms < 0 {
					return 0, false
				}
				tm.addFinal(ms)
			case tz.isExpecting(n):
				tz.minute = n
			case tm.isExpecting(n):
				tm.addFinal(n)
				p := sc.peek()
				if p.kind != jsdEnd && p.kind != jsdWhiteSpace && !p.isKeywordZ() && !p.isAsciiSign() {
					return 0, false
				}
			default:
				if !day.add(n) {
					return 0, false
				}
				sc.skipSymbol('-')
			}
		case tok.kind == jsdKeyword:
			switch {
			case tok.kw == jsdKwAMPM && !tm.isEmpty():
				tm.hourOffset = tok.value
			case tok.kw == jsdKwMonthName:
				day.namedMonth = tok.value
				sc.skipSymbol('-')
			case tok.kw == jsdKwTimeZoneName && hasReadNumber:
				tz.set(tok.value)
			default:
				if hasReadNumber || sc.peek().kind == jsdNumber {
					return 0, false
				}
			}
		case tok.isAsciiSign() && (tz.isUTC() || !tm.isEmpty()):
			tz.sign = tok.asciiSign()
			n, length := 0, 0
			if sc.peek().kind == jsdNumber {
				t := sc.nextToken()
				n, length = t.value, t.length
			}
			hasReadNumber = true
			switch {
			case sc.peek().isSymbol(':'):
				tz.hour, tz.minute = n, jsdNone
			case length == 1 || length == 2:
				tz.hour, tz.minute = n, 0
			case length == 3 || length == 4:
				tz.hour, tz.minute = n/100, n%100
			default:
				return 0, false
			}
		case (tok.isAsciiSign() || tok.isSymbol(')')) && hasReadNumber:
			return 0, false
		}
	}

	year, month, dd, ok := day.write()
	if !ok {
		return 0, false
	}
	h, m, sec, ms, ok := tm.write()
	if !ok {
		return 0, false
	}
	offset, local, ok := tz.write()
	if !ok {
		return 0, false
	}
	// MakeDate(MakeDay(year, month, day), MakeTime(h, m, s, ms)).
	days := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC).Unix()/86400 + int64(dd) - 1
	date := days*86400000 + int64(h)*3600000 + int64(m)*60000 + int64(sec)*1000 + int64(ms)
	const maxTime = 8_640_000_000_000_000
	if local {
		if date < -(maxTime+864_000_000) || date > maxTime+864_000_000 {
			return 0, false
		}
		date = jsLocalToUTC(date, loc)
	} else {
		date -= offset * 1000
	}
	if date < -maxTime || date > maxTime {
		return 0, false
	}
	return date, true
}

// jsLocalToUTC converts a local wall-clock time, in milliseconds as if it
// were UTC, to UTC in loc. Like V8 (ICU's UCAL_TZ_LOCAL_FORMER, and the ES
// UTC(t) disambiguation), a repeated local time takes the offset before the
// transition (the earlier instant), and a skipped one is read with the
// offset before the transition too, so 02:30 on a spring-forward night in
// New York is 03:30 EDT. Go's time.Date leaves the skipped case unspecified.
func jsLocalToUTC(localMs int64, loc *time.Location) int64 {
	const day = 86_400_000
	offsetAt := func(utcMs int64) int64 {
		_, sec := time.UnixMilli(utcMs).In(loc).Zone()
		return int64(sec) * 1000
	}
	before := offsetAt(localMs - day)
	best, found := int64(0), false
	for _, o := range [...]int64{before, offsetAt(localMs), offsetAt(localMs + day)} {
		if u := localMs - o; offsetAt(u) == o && (!found || u < best) {
			best, found = u, true
		}
	}
	if !found {
		return localMs - before
	}
	return best
}
