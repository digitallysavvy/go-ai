package fx

import (
	"context"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Port of the pattern preparation Node's path.matchesGlob runs before it
// matches a path: minimatch's braceExpand (the brace-expansion package, with
// its expansion limits) and its optimizationLevel 2 preprocessing of the
// slash-split parts (Node passes optimizationLevel: 2). The fx glob tool
// compiles every resulting part list with parseGlobSegment.

const (
	// minimatch assertValidPattern: MAX_PATTERN_LENGTH.
	globMaxPatternLength = 1024 * 64
	// brace-expansion EXPANSION_MAX and EXPANSION_MAX_LENGTH (minimatch
	// passes no braceExpandMax, so the defaults apply).
	braceExpansionMax       = 100000
	braceExpansionMaxLength = 4000000
)

// brace-expansion's escape markers (it appends a random number; a pattern
// never holds NUL bytes, so fixed markers cannot collide).
const (
	braceEscSlash  = "\x00SLASH\x00"
	braceEscOpen   = "\x00OPEN\x00"
	braceEscClose  = "\x00CLOSE\x00"
	braceEscComma  = "\x00COMMA\x00"
	braceEscPeriod = "\x00PERIOD\x00"
)

var (
	braceNumericSequenceRE = regexp.MustCompile(`^-?\d+\.\.-?\d+(?:\.\.-?\d+)?$`)
	braceAlphaSequenceRE   = regexp.MustCompile(`^[a-zA-Z]\.\.[a-zA-Z](?:\.\.-?\d+)?$`)
	bracePaddedRE          = regexp.MustCompile(`^-?0\d`)
	globSlashesRE          = regexp.MustCompile(`/+`)
)

// globCancelEvery is how many steps a compile takes between context checks.
const globCancelEvery = 1024

// globCanceler stops a pattern compile when its context ends. minimatch's
// work is synchronous, so a pattern such as `{a,b}` repeated 17 times (whose
// second-phase merge is quadratic in its 100000 expansions) runs for tens of
// seconds; Go checks the context every globCancelEvery steps instead. A
// stopped compile returns partial results that the caller discards for
// err, so a compile that is not canceled is unchanged. A nil canceler never
// stops.
type globCanceler struct {
	ctx context.Context
	n   int
	err error
}

func newGlobCanceler(ctx context.Context) *globCanceler {
	if ctx == nil || ctx.Done() == nil {
		return nil
	}
	return &globCanceler{ctx: ctx}
}

// stop counts one step and reports whether the compile must stop.
func (c *globCanceler) stop() bool {
	if c == nil {
		return false
	}
	if c.err != nil {
		return true
	}
	c.n++
	if c.n%globCancelEvery == 0 {
		c.err = c.ctx.Err()
	}
	return c.err != nil
}

// jsLength is a string's JavaScript length in UTF-16 code units.
func jsLength(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// isJSLineTerminator reports the characters a JavaScript `.` does not match.
func isJSLineTerminator(r rune) bool {
	return r == '\n' || r == '\r' || r == 0x2028 || r == 0x2029
}

// minimatchBraceExpand is minimatch's braceExpand: a pattern without a
// `{...}` group (no nested `{` or line terminator inside) is kept as is.
func minimatchBraceExpand(pattern string) []string {
	return minimatchBraceExpandC(nil, pattern)
}

func minimatchBraceExpandC(c *globCanceler, pattern string) []string {
	if !hasBraceGroup(pattern) {
		return []string{pattern}
	}
	return braceExpandC(c, pattern)
}

// hasBraceGroup is /\{(?:(?!\{).)*\}/.test(pattern).
func hasBraceGroup(s string) bool {
	rs := []rune(s)
	for i, r := range rs {
		if r != '{' {
			continue
		}
		for _, c := range rs[i+1:] {
			if c == '}' {
				return true
			}
			if c == '{' || isJSLineTerminator(c) {
				break
			}
		}
	}
	return false
}

// braceExpand is brace-expansion's expand.
func braceExpandC(c *globCanceler, str string) []string {
	if str == "" {
		return nil
	}
	if strings.HasPrefix(str, "{}") {
		str = `\{\}` + str[2:]
	}
	out := braceExpandInner(c, braceEscape(str), true)
	for i := range out {
		out[i] = braceUnescape(out[i])
	}
	return out
}

func braceEscape(s string) string {
	s = strings.ReplaceAll(s, `\\`, braceEscSlash)
	s = strings.ReplaceAll(s, `\{`, braceEscOpen)
	s = strings.ReplaceAll(s, `\}`, braceEscClose)
	s = strings.ReplaceAll(s, `\,`, braceEscComma)
	return strings.ReplaceAll(s, `\.`, braceEscPeriod)
}

func braceUnescape(s string) string {
	s = strings.ReplaceAll(s, braceEscSlash, `\`)
	s = strings.ReplaceAll(s, braceEscOpen, "{")
	s = strings.ReplaceAll(s, braceEscClose, "}")
	s = strings.ReplaceAll(s, braceEscComma, ",")
	return strings.ReplaceAll(s, braceEscPeriod, ".")
}

// indexFrom is JavaScript's str.indexOf(sub, from) for from >= 0.
func indexFrom(s, sub string, from int) int {
	if from > len(s) {
		return -1
	}
	if i := strings.Index(s[from:], sub); i >= 0 {
		return i + from
	}
	return -1
}

// balancedBraces is balanced-match's balanced('{', '}', str).
func balancedBraces(c *globCanceler, str string) (pre, body, post string, ok bool) {
	ai := strings.IndexByte(str, '{')
	bi := indexFrom(str, "}", ai+1)
	i := ai
	start, end := -1, -1
	if ai >= 0 && bi > 0 {
		var begs []int
		left, right := len(str), -1
		found := false
		for i >= 0 && !found {
			if c.stop() {
				return "", "", "", false
			}
			switch {
			case i == ai:
				begs = append(begs, i)
				ai = indexFrom(str, "{", i+1)
			case len(begs) == 1:
				start, end = begs[0], bi
				begs = begs[:0]
				found = true
			default:
				if n := len(begs); n > 0 {
					beg := begs[n-1]
					begs = begs[:n-1]
					if beg < left {
						left, right = beg, bi
					}
				}
				bi = indexFrom(str, "}", i+1)
			}
			if ai < bi && ai >= 0 {
				i = ai
			} else {
				i = bi
			}
		}
		if len(begs) > 0 && right >= 0 {
			start, end, found = left, right, true
		}
		if !found {
			return "", "", "", false
		}
	} else {
		return "", "", "", false
	}
	return str[:start], str[start+1 : end], str[end+1:], true
}

func braceParseCommaParts(c *globCanceler, str string) []string {
	if str == "" || c.stop() {
		return []string{""}
	}
	pre, body, post, ok := balancedBraces(c, str)
	if !ok {
		return strings.Split(str, ",")
	}
	p := strings.Split(pre, ",")
	p[len(p)-1] += "{" + body + "}"
	postParts := braceParseCommaParts(c, post)
	if post != "" {
		p[len(p)-1] += postParts[0]
		p = append(p, postParts[1:]...)
	}
	return p
}

func braceCombine(c *globCanceler, acc []string, pre string, values []string, dropEmpties bool) []string {
	out := []string{}
	length := 0
	for _, a := range acc {
		for _, v := range values {
			if len(out) >= braceExpansionMax || c.stop() {
				return out
			}
			expansion := a + pre + v
			if dropEmpties && expansion == "" {
				continue
			}
			n := jsLength(expansion)
			if length+n > braceExpansionMaxLength {
				return out
			}
			out = append(out, expansion)
			length += n
		}
	}
	return out
}

// braceNumeric is brace-expansion's numeric: parseInt for a number, else the
// first character's code.
func braceNumeric(s string) float64 {
	// A digit string too long for a float64 is ±Infinity, as in parseInt.
	if f, err := strconv.ParseFloat(s, 64); err == nil || errors.Is(err, strconv.ErrRange) {
		return math.Trunc(f)
	}
	return float64(utf16.Encode([]rune(s))[0])
}

func braceExpandSequence(c *globCanceler, body string, alpha bool) []string {
	n := strings.Split(body, "..")
	out := []string{}
	if len(n) < 2 {
		return out
	}
	x, y := braceNumeric(n[0]), braceNumeric(n[1])
	width := max(len(n[0]), len(n[1]))
	incr := 1.0
	if len(n) == 3 {
		incr = math.Max(math.Abs(braceNumeric(n[2])), 1)
	}
	reverse := y < x
	if reverse {
		incr = -incr
	}
	pad := false
	for _, el := range n {
		if bracePaddedRE.MatchString(el) {
			pad = true
		}
	}
	length := 0
	for i := x; (reverse && i >= y || !reverse && i <= y) && len(out) < braceExpansionMax && !c.stop(); i += incr {
		var c string
		if alpha {
			c = string(utf16.Decode([]uint16{uint16(i)}))
			if c == `\` {
				c = ""
			}
		} else {
			c = jsNumber(i)
			if pad {
				if need := width - len(c); need > 0 {
					z := strings.Repeat("0", need)
					if i < 0 {
						c = "-" + z + c[1:]
					} else {
						c = z + c
					}
				}
			}
		}
		if length+jsLength(c) > braceExpansionMaxLength {
			break
		}
		out = append(out, c)
		length += jsLength(c)
	}
	return out
}

// bracePostReopens is /,(?!,).*\}/.test(post): a comma not followed by a
// comma, then a `}` on the same line.
func bracePostReopens(post string) bool {
	rs := []rune(post)
	for i, r := range rs {
		if r != ',' || (i+1 < len(rs) && rs[i+1] == ',') {
			continue
		}
		for _, c := range rs[i+1:] {
			if c == '}' {
				return true
			}
			if isJSLineTerminator(c) {
				break
			}
		}
	}
	return false
}

func braceExpandInner(c *globCanceler, str string, isTop bool) []string {
	acc := []string{""}
	dropEmpties := false
	firstGroup := true
	for {
		if c.stop() {
			return acc
		}
		pre, body, post, ok := balancedBraces(c, str)
		if !ok {
			return braceCombine(c, acc, str, []string{""}, dropEmpties)
		}
		if strings.HasSuffix(pre, "$") {
			acc = braceCombine(c, acc, pre+"{"+body+"}", []string{""}, dropEmpties && post == "")
			firstGroup = false
			if post == "" {
				break
			}
			str = post
			continue
		}
		isAlpha := braceAlphaSequenceRE.MatchString(body)
		isSequence := braceNumericSequenceRE.MatchString(body) || isAlpha
		isOptions := strings.Contains(body, ",")
		if !isSequence && !isOptions {
			if bracePostReopens(post) {
				str = pre + "{" + body + braceEscClose + post
				isTop = true
				continue
			}
			return braceCombine(c, acc, pre+"{"+body+"}"+post, []string{""}, dropEmpties)
		}
		if firstGroup {
			dropEmpties = isTop && !isSequence
			firstGroup = false
		}
		var values []string
		if isSequence {
			values = braceExpandSequence(c, body, isAlpha)
		} else {
			n := braceParseCommaParts(c, body)
			if len(n) == 1 {
				inner := braceExpandInner(c, n[0], false)
				n = make([]string, len(inner))
				for i, s := range inner {
					n[i] = "{" + s + "}"
				}
				if len(n) == 1 {
					acc = braceCombine(c, acc, pre+n[0], []string{""}, dropEmpties && post == "")
					if post == "" {
						break
					}
					str = post
					continue
				}
			}
			dropsEmpties := dropEmpties && post == "" && pre == ""
			for _, a := range acc {
				if a != "" {
					dropsEmpties = false
					break
				}
			}
			values = []string{}
			valuesLength := 0
		outer:
			for _, part := range n {
				if c.stop() {
					break
				}
				for _, v := range braceExpandInner(c, part, false) {
					if dropsEmpties && v == "" {
						continue
					}
					if len(values) >= braceExpansionMax || valuesLength+jsLength(v) > braceExpansionMaxLength {
						break outer
					}
					values = append(values, v)
					valuesLength += jsLength(v)
				}
			}
		}
		acc = braceCombine(c, acc, pre, values, dropEmpties && post == "")
		if post == "" {
			break
		}
		str = post
	}
	return acc
}

// globSlashSplit is minimatch's slashSplit (preserveMultipleSlashes false).
func globSlashSplit(p string) []string {
	return globSlashesRE.Split(p, -1)
}

func indexOfPart(parts []string, part string, from int) int {
	for i := max(from, 0); i < len(parts); i++ {
		if parts[i] == part {
			return i
		}
	}
	return -1
}

func partAt(parts []string, i int) (string, bool) {
	if i < 0 || i >= len(parts) {
		return "", false
	}
	return parts[i], true
}

func removeParts(parts []string, at, n int, insert ...string) []string {
	out := append([]string(nil), parts[:at]...)
	out = append(out, insert...)
	return append(out, parts[at+n:]...)
}

// globFirstPhasePreProcess is minimatch's firstPhasePreProcess.
func globFirstPhasePreProcess(c *globCanceler, globParts [][]string) [][]string {
	for didSomething := true; didSomething; {
		didSomething = false
		for k := 0; k < len(globParts); k++ {
			if c.stop() {
				return globParts
			}
			parts := globParts[k]
			for gs := indexOfPart(parts, "**", 0); gs != -1; gs = indexOfPart(parts, "**", gs+1) {
				gss := gs
				for gss+1 < len(parts) && parts[gss+1] == "**" {
					gss++
				}
				if gss > gs {
					parts = removeParts(parts, gs+1, gss-gs)
				}
				next, _ := partAt(parts, gs+1)
				p, _ := partAt(parts, gs+2)
				p2, _ := partAt(parts, gs+3)
				if next != ".." {
					continue
				}
				if p == "" || p == "." || p == ".." || p2 == "" || p2 == "." || p2 == ".." {
					continue
				}
				didSomething = true
				parts = removeParts(parts, gs, 1)
				other := append([]string(nil), parts...)
				other[gs] = "**"
				globParts = append(globParts, other)
				gs--
			}
			parts, changed := globLevelTwoDots(parts)
			didSomething = didSomething || changed
			for dd := indexOfPart(parts, "..", 1); dd != -1; dd = indexOfPart(parts, "..", dd+1) {
				p := parts[dd-1]
				if p != "" && p != "." && p != ".." && p != "**" {
					didSomething = true
					var splin []string
					if next, _ := partAt(parts, dd+1); dd == 1 && next == "**" {
						splin = []string{"."}
					}
					parts = removeParts(parts, dd-1, 2, splin...)
					if len(parts) == 0 {
						parts = []string{""}
					}
					dd -= 2
				}
			}
			globParts[k] = parts
		}
	}
	return globParts
}

// globLevelTwoDots drops "." and "" parts between the first and the last
// (keeping a leading "//"), and a trailing "." or "" after a leading ".".
func globLevelTwoDots(parts []string) ([]string, bool) {
	changed := false
	for i := 1; i < len(parts)-1; i++ {
		p := parts[i]
		if i == 1 && p == "" && parts[0] == "" {
			continue
		}
		if p == "." || p == "" {
			changed = true
			parts = removeParts(parts, i, 1)
			i--
		}
	}
	if len(parts) == 2 && parts[0] == "." && (parts[1] == "." || parts[1] == "") {
		changed = true
		parts = parts[:1]
	}
	return parts, changed
}

// globLevelTwoFileOptimize is minimatch's levelTwoFileOptimize, applied to the
// path being matched (posix platform).
func globLevelTwoFileOptimize(parts []string) []string {
	for didSomething := true; didSomething; {
		parts, didSomething = globLevelTwoDots(parts)
		for dd := indexOfPart(parts, "..", 1); dd != -1; dd = indexOfPart(parts, "..", dd+1) {
			p := parts[dd-1]
			if p != "" && p != "." && p != ".." && p != "**" {
				didSomething = true
				parts = removeParts(parts, dd-1, 2)
				dd -= 2
			}
		}
	}
	if len(parts) == 0 {
		return []string{""}
	}
	return parts
}

// globSecondPhasePreProcess is minimatch's secondPhasePreProcess: a part
// list that partsMatch merges with a later one is replaced by the merge.
func globSecondPhasePreProcess(c *globCanceler, globParts [][]string) [][]string {
	for i := 0; i < len(globParts)-1; i++ {
		for j := i + 1; j < len(globParts); j++ {
			if c.stop() {
				return nil
			}
			if matched, ok := globPartsMatch(globParts[i], globParts[j]); ok {
				globParts[i] = nil
				globParts[j] = matched
				break
			}
		}
	}
	out := globParts[:0]
	for _, parts := range globParts {
		if len(parts) > 0 {
			out = append(out, parts)
		}
	}
	return out
}

// globPartsMatch is minimatch's partsMatch(a, b, true) without the dot option.
func globPartsMatch(a, b []string) ([]string, bool) {
	ai, bi := 0, 0
	result := []string{}
	which := ""
	for ai < len(a) && bi < len(b) {
		nextA, hasNextA := partAt(a, ai+1)
		nextB, hasNextB := partAt(b, bi+1)
		switch {
		case a[ai] == b[bi]:
			result = append(result, a[ai])
			ai++
			bi++
		case a[ai] == "**" && hasNextA && b[bi] == nextA:
			result = append(result, a[ai])
			ai++
		case b[bi] == "**" && hasNextB && a[ai] == nextB:
			result = append(result, b[bi])
			bi++
		case a[ai] == "*" && b[bi] != "" && !strings.HasPrefix(b[bi], ".") && b[bi] != "**":
			if which == "b" {
				return nil, false
			}
			which = "a"
			result = append(result, a[ai])
			ai++
			bi++
		case b[bi] == "*" && a[ai] != "" && !strings.HasPrefix(a[ai], ".") && a[ai] != "**":
			if which == "a" {
				return nil, false
			}
			which = "b"
			result = append(result, b[bi])
			ai++
			bi++
		default:
			return nil, false
		}
	}
	if len(a) != len(b) {
		return nil, false
	}
	return result, true
}

// globPatternPartsContext is the part lists minimatch matches a path against:
// the deduplicated brace expansions, slash-split and preprocessed. It stops
// with ctx.Err() when ctx ends during the expansion or either preprocessing
// phase.
func globPatternPartsContext(ctx context.Context, pattern string) ([][]string, error) {
	c := newGlobCanceler(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var globParts [][]string
	seen := map[string]bool{}
	for _, s := range minimatchBraceExpandC(c, pattern) {
		if seen[s] {
			continue
		}
		seen[s] = true
		globParts = append(globParts, globSlashSplit(s))
	}
	if c.stop() {
		return nil, c.err
	}
	globParts = globSecondPhasePreProcess(c, globFirstPhasePreProcess(c, globParts))
	if c != nil && c.err != nil {
		return nil, c.err
	}
	return globParts, nil
}
