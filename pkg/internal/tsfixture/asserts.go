package tsfixture

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Assertion is one plain vitest expectation (expect(expr).toEqual(arg) and
// friends) of a TS test. Expr is TS source over the call result.
type Assertion struct {
	Expr    string      `json:"expr"`
	Matcher string      `json:"matcher"`
	Not     bool        `json:"not"`
	Arg     interface{} `json:"arg"`
	// CallArgs are the arguments of the call Expr itself makes (the assertion
	// is over fn(CallArgs...) rather than over the test's first call).
	CallArgs []interface{} `json:"callArgs"`
	// CallSuffix is what follows that call in Expr (for example ".inputTokens").
	CallSuffix string `json:"callSuffix"`
}

// request is one request the test server received.
type request struct {
	body    interface{}
	headers map[string]interface{}
	ua      string
	url     string
	method  string
}

// ignoredRequestHeaders are added by Go's HTTP client and absent from the
// request TS fetch builds (and from the test server's requestHeaders).
var ignoredRequestHeaders = map[string]bool{"accept-encoding": true, "content-length": true, "user-agent": true, "connection": true, "accept": true}

func newRequest(body []byte, h map[string][]string, url, method string) request {
	r := request{url: url, method: method, headers: map[string]interface{}{}}
	_ = json.Unmarshal(body, &r.body)
	for k, v := range h {
		lk := strings.ToLower(k)
		if lk == "user-agent" && len(v) > 0 {
			r.ua = v[0]
		}
		if ignoredRequestHeaders[lk] || len(v) == 0 {
			continue
		}
		r.headers[lk] = strings.Join(v, ", ")
	}
	return r
}

func (r request) value(field string) (interface{}, bool) {
	switch field {
	case "requestBodyJson":
		return r.body, true
	case "requestHeaders":
		return r.headers, true
	case "requestUserAgent":
		return r.ua, true
	case "requestUrl":
		return r.url, true
	case "requestMethod":
		return r.method, true
	}
	return nil, false
}

var pathToken = regexp.MustCompile(`^(?:\??\.([A-Za-z_$][\w$]*)|\[(\d+)\]|\['([^']*)'\]|\["([^"]*)"\])`)

// pathTokenExt is pathToken plus the optional index `?.[n]` (extended
// assertions only).
var pathTokenExt = regexp.MustCompile(`^(?:\??\.([A-Za-z_$][\w$]*)|(?:\?\.)?\[(\d+)\]|\['([^']*)'\]|\["([^"]*)"\])`)

// extendedAsserts enables the array calls of parseArrayCall, .slice,
// JSON.parse, `(await x)` / `(x as T)` and `?.[n]` in evalPath; see
// EvalExtendedAsserts.
var extendedAsserts bool

// EvalExtendedAsserts makes the assertions and path snapshots of the test's
// own run evaluate the extended expression forms (predicates joined by &&,
// !==, .startsWith, .every, .flatMap, `p.a ?? x` maps, .slice, JSON.parse,
// `(await x)` and `(x as T)` around a path, `?.[n]`). Suites opt in so
// the assertions they could not evaluate before do not change their result
// unseen; an opted-in suite triages what they find.
func EvalExtendedAsserts(t *testing.T) {
	t.Helper()
	prev := extendedAsserts
	extendedAsserts = true
	t.Cleanup(func() { extendedAsserts = prev }) // keep a binary-wide EnableExtendedAsserts
}

// EnableExtendedAsserts is EvalExtendedAsserts for every suite of a test
// binary (call it from an init function of a _test.go file).
func EnableExtendedAsserts() { extendedAsserts = true }

// arrayCall matches `.filter(p => p.f === 'v')`, `.some(p => p.f === 'v')`,
// `.find(p => p.f === 'v')` and `.map(p => p.f)` over an array (the
// stream-part filters TS tests write). arrayCallStart and parseArrayCall
// extend it to predicates joined by && (===, !==, a bare path for
// truthiness, .startsWith('s')), `.every`, `.flatMap(p => p.f ?? [])` and
// `.map(p => p.f ?? x)`.
var arrayCall = regexp.MustCompile(`^\.(filter|some|find|map)\(\s*\(?([A-Za-z_$][\w$]*)\)?\s*=>\s*([A-Za-z_$][\w$]*)\.([A-Za-z_$][\w$]*)(?:\s*===\s*'([^']*)')?\s*\)`)

// arrayCallStart is the head of an array call with a one-parameter arrow
// (its TS type annotation, `(t: any) =>`, ignored).
var arrayCallStart = regexp.MustCompile(`^\??\.(filter|some|every|find|map|flatMap)\(\s*\(?\s*([A-Za-z_$][\w$]*)\s*(?::\s*(?:\{[^{}()]*\}|[\w$.<>\[\]| ]+?)\s*)?\)?\s*=>\s*`)

// sliceCall matches `.slice(a)` and `.slice(a, b)`.
var sliceCall = regexp.MustCompile(`^\.slice\(\s*(-?\d+)\s*(?:,\s*(-?\d+)\s*)?\)`)

// predTerm is one && term of an arrow body: `p.a.b === lit`, `p.a !== lit`,
// `p.a.startsWith('s')` or a bare `p.a` (truthy).
var predTerm = regexp.MustCompile(`^([A-Za-z_$][\w$]*)((?:\??\.[A-Za-z_$][\w$]*)*)(?:\s*(===|!==)\s*('[^']*'|"[^"]*"|true|false|null|undefined|-?\d+(?:\.\d+)?)|\.startsWith\(\s*('[^']*'|"[^"]*")\s*\))?$`)

// mapBody is a map/flatMap arrow body: `p.a.b` with an optional `?? lit`
// (`[]`, a string, a number, true, false, null).
var mapBody = regexp.MustCompile(`^([A-Za-z_$][\w$]*)((?:\??\.[A-Za-z_$][\w$]*)*)(?:\s*\?\?\s*(\[\]|'[^']*'|"[^"]*"|true|false|null|-?\d+(?:\.\d+)?|[A-Za-z_$][\w$]*(?:\??\.[A-Za-z_$][\w$]*)+))?$`)

// tsLiteral evaluates a predicate or map literal.
func tsLiteral(lit string) interface{} {
	switch lit {
	case "true":
		return true
	case "false":
		return false
	case "null", "undefined":
		return nil
	case "[]":
		return []interface{}{}
	}
	if strings.HasPrefix(lit, "'") || strings.HasPrefix(lit, `"`) {
		return lit[1 : len(lit)-1]
	}
	f, _ := strconv.ParseFloat(lit, 64)
	return f
}

// memberPath reads a `.a?.b` path (empty: the value itself).
func memberPath(v interface{}, path string) interface{} {
	for _, seg := range strings.Split(strings.ReplaceAll(path, "?.", "."), ".") {
		if seg == "" {
			continue
		}
		v = member(v, seg)
	}
	return v
}

// jsTruthy is JavaScript truthiness of a JSON value.
func jsTruthy(v interface{}) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0
	}
	return !isUndef(v)
}

// parseArrayCall parses an array call at the start of rest. It returns the
// evaluator and the length consumed, or ok false when the call is not one
// of the supported shapes.
func parseArrayCall(rest string) (func(cur interface{}) (interface{}, bool), int, bool) {
	m := arrayCallStart.FindStringSubmatch(rest)
	if m == nil {
		return nil, 0, false
	}
	op, param := m[1], m[2]
	// the arrow body runs to the call's closing paren
	depth, end := 0, -1
	for i := len(m[0]); i < len(rest) && end < 0; i++ {
		switch rest[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				end = i
			}
			depth--
		case '\'', '"':
			q := rest[i]
			for i++; i < len(rest) && rest[i] != q; i++ {
			}
		}
	}
	if end < 0 {
		return nil, 0, false
	}
	body := strings.TrimSuffix(strings.TrimSpace(rest[len(m[0]):end]), ",")
	body = strings.TrimSpace(body)
	items := func(cur interface{}) ([]interface{}, bool) {
		x, ok := jsonValue(cur).([]interface{})
		return x, ok
	}
	if op == "map" || op == "flatMap" {
		bm := mapBody.FindStringSubmatch(body)
		if bm == nil || bm[1] != param {
			// `p => ('k' in p ? p.a : p.b)`, `p => (p.f === 'v' ? [p.a] : [])`
			if !isOperatorBody(body) {
				return nil, 0, false
			}
			return func(cur interface{}) (interface{}, bool) {
				list, ok := items(cur)
				if !ok {
					return nil, false
				}
				out := make([]interface{}, 0, len(list))
				for _, it := range list {
					v, ok, _ := evalOperatorExpr(map[string]interface{}{param: it}, nil, body)
					if !ok {
						return nil, false
					}
					if arr, isArr := v.([]interface{}); isArr && op == "flatMap" {
						out = append(out, arr...)
						continue
					}
					out = append(out, v)
				}
				return out, true
			}, end + 1, true
		}
		path, fallback, hasFallback := bm[2], bm[3], bm[3] != ""
		return func(cur interface{}) (interface{}, bool) {
			list, ok := items(cur)
			if !ok {
				return nil, false
			}
			out := make([]interface{}, 0, len(list))
			for _, it := range list {
				v := memberPath(it, path)
				if hasFallback && v == nil {
					if fp := mapBody.FindStringSubmatch(fallback); fp != nil && fp[1] == param && fp[3] == "" {
						v = memberPath(it, fp[2]) // `p.a ?? p.b`
					} else {
						v = tsLiteral(fallback)
					}
				}
				if op == "flatMap" {
					if arr, isArr := jsonValue(v).([]interface{}); isArr {
						out = append(out, arr...)
						continue
					}
				}
				out = append(out, v)
			}
			return out, true
		}, end + 1, true
	}
	type term struct {
		path, cmp, lit, prefix string
	}
	var terms []term
	useCond := false // a body of another shape goes through evalCond (||, in, ...)
	for _, part := range strings.Split(body, "&&") {
		tm := predTerm.FindStringSubmatch(strings.TrimSpace(part))
		if tm == nil || tm[1] != param {
			if !extendedAsserts {
				return nil, 0, false
			}
			useCond, terms = true, nil
			break
		}
		terms = append(terms, term{path: tm[2], cmp: tm[3], lit: tm[4], prefix: tm[5]})
	}
	match := func(it interface{}) (bool, bool) {
		if useCond {
			return evalCond(map[string]interface{}{param: it}, nil, body)
		}
		for _, t := range terms {
			v := memberPath(it, t.path)
			switch {
			case t.cmp == "===":
				if !reflect.DeepEqual(v, tsLiteral(t.lit)) {
					return false, true
				}
			case t.cmp == "!==":
				if reflect.DeepEqual(v, tsLiteral(t.lit)) {
					return false, true
				}
			case t.prefix != "":
				s, isStr := v.(string)
				if !isStr || !strings.HasPrefix(s, tsLiteral(t.prefix).(string)) {
					return false, true
				}
			default:
				if !jsTruthy(v) {
					return false, true
				}
			}
		}
		return true, true
	}
	return func(cur interface{}) (interface{}, bool) {
		list, ok := items(cur)
		if !ok {
			return nil, false
		}
		switch op {
		case "filter":
			out := []interface{}{}
			for _, it := range list {
				hit, ok := match(it)
				if !ok {
					return nil, false
				}
				if hit {
					out = append(out, it)
				}
			}
			return out, true
		case "find":
			for _, it := range list {
				hit, ok := match(it)
				if !ok {
					return nil, false
				}
				if hit {
					return it, true
				}
			}
			return nil, true // undefined
		case "every":
			for _, it := range list {
				hit, ok := match(it)
				if !ok {
					return nil, false
				}
				if !hit {
					return false, true
				}
			}
			return true, true
		default: // some
			for _, it := range list {
				hit, ok := match(it)
				if !ok {
					return nil, false
				}
				if hit {
					return true, true
				}
			}
			return false, true
		}
	}, end + 1, true
}

// applyArrayCall evaluates one arrayCall match over cur.
func applyArrayCall(cur interface{}, m []string) (interface{}, bool) {
	op, param, obj, field, val := m[1], m[2], m[3], m[4], m[5]
	hasCompare := strings.Contains(m[0], "===")
	if param != obj || (op == "map") == hasCompare {
		return nil, false
	}
	items, ok := jsonValue(cur).([]interface{})
	if !ok {
		return nil, false
	}
	switch op {
	case "map":
		out := make([]interface{}, 0, len(items))
		for _, it := range items {
			out = append(out, member(it, field))
		}
		return out, true
	case "filter":
		out := []interface{}{}
		for _, it := range items {
			if member(it, field) == val {
				out = append(out, it)
			}
		}
		return out, true
	case "find":
		for _, it := range items {
			if member(it, field) == val {
				return it, true
			}
		}
		return nil, true // undefined
	default: // some
		for _, it := range items {
			if member(it, field) == val {
				return true, true
			}
		}
		return false, true
	}
}

// atCall matches `.at(n)` (n may be negative).
var atCall = regexp.MustCompile(`^\.at\((-?\d+)\)`)

// lineBreak joins an expression vitest formatting split over lines.
var lineBreak = regexp.MustCompile(`\s*\n\s*`)

// jsonParseCall matches `JSON.parse(path)` followed by a path.
var jsonParseCall = regexp.MustCompile(`^JSON\.parse\(([^()]+)\)(.*)$`)

// closingParen is the index of the paren closing expr[0] ('('), or -1.
func closingParen(expr string) int {
	depth := 0
	for i := 0; i < len(expr); i++ {
		switch expr[i] {
		case '(', '[', '{', '<':
			depth++
		case ')', ']', '}', '>':
			if expr[i] == '>' && i > 0 && expr[i-1] == '=' {
				continue // an arrow
			}
			depth--
			if depth == 0 && expr[i] == ')' {
				return i
			}
		case '\'', '"':
			q := expr[i]
			for i++; i < len(expr) && expr[i] != q; i++ {
			}
		}
	}
	return -1
}

// topLevelAs is the index of a top-level " as " in s, or -1.
func topLevelAs(s string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[', '{', '<':
			depth++
		case ')', ']', '}', '>':
			depth--
		case ' ':
			if depth == 0 && strings.HasPrefix(s[i:], " as ") {
				return i
			}
		}
	}
	return -1
}

// searchParam matches `new URL(expr).searchParams.get('key')`.
var searchParam = regexp.MustCompile(`^new URL\((.+)\)\.searchParams\.get\('([^']*)'\)$`)

// evalPath resolves a simple TS access expression (identifiers, ., ?., [n],
// ['key'], .length, the array calls of arrayCall and
// `new URL(x).searchParams.get('k')`) against roots. An alias may refer to
// another alias. ok is false for anything else.
func evalPath(roots map[string]interface{}, aliases map[string]string, expr string) (interface{}, bool) {
	expr = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(expr), "await "))
	expr = strings.TrimSuffix(lineBreak.ReplaceAllString(expr, ""), ",")
	if m := searchParam.FindStringSubmatch(expr); m != nil {
		v, ok := evalPath(roots, aliases, m[1])
		s, isStr := v.(string)
		if !ok || !isStr {
			return nil, false
		}
		u, err := url.Parse(s)
		if err != nil {
			return nil, false
		}
		q := u.Query()
		if !q.Has(m[2]) {
			return nil, true // URLSearchParams.get returns null
		}
		return q.Get(m[2]), true
	}
	if m := jsonParseCall.FindStringSubmatch(expr); extendedAsserts && m != nil {
		// JSON.parse(path) of a JSON string, then the rest of the path
		v, ok := evalPath(roots, aliases, m[1])
		str, isStr := v.(string)
		if !ok || !isStr {
			return nil, false
		}
		var parsed interface{}
		if json.Unmarshal([]byte(str), &parsed) != nil {
			return nil, false
		}
		return evalPath(map[string]interface{}{"__parsed": parsed}, nil, "__parsed"+m[2])
	}
	if extendedAsserts {
		if v, ok, handled := evalOperatorExpr(roots, aliases, expr); handled {
			return v, ok
		}
	}
	if extendedAsserts && strings.HasPrefix(expr, "(") {
		// `(await x)` and `(x as T)` around a path, then the rest of it
		if end := closingParen(expr); end > 0 {
			inner := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(expr[1:end]), "await "))
			if i := topLevelAs(inner); i >= 0 {
				inner = strings.TrimSpace(inner[:i])
			}
			if inner != "" && !strings.ContainsAny(inner, " ") {
				return evalPath(roots, aliases, inner+expr[end+1:])
			}
		}
		return nil, false
	}
	root := regexp.MustCompile(`^[A-Za-z_$][\w$]*`).FindString(expr)
	if root == "" {
		return nil, false
	}
	rest := expr[len(root):]
	if a, ok := aliases[root]; ok {
		// an alias resolves without itself, so a cycle ends
		inner := make(map[string]string, len(aliases))
		for k, v := range aliases {
			if k != root {
				inner[k] = v
			}
		}
		if rest != "" && extendedAsserts && (isOperatorBody(a) || len(splitTopLevel(a, "??")) > 1) {
			// an operator alias (`x ?? []`) is evaluated before the rest of
			// the path applies to it
			v, ok := evalPath(roots, inner, a)
			if !ok {
				return nil, false
			}
			return evalPath(map[string]interface{}{"__alias": v}, nil, "__alias"+rest)
		}
		return evalPath(roots, inner, a+rest)
	}
	cur, ok := roots[root]
	if !ok {
		return nil, false
	}
	for rest != "" {
		if m := arrayCall.FindStringSubmatch(rest); m != nil {
			v, ok := applyArrayCall(cur, m)
			if !ok {
				return nil, false
			}
			cur = v
			rest = rest[len(m[0]):]
			continue
		}
		if fn, n, ok := parseArrayCall(rest); extendedAsserts && ok {
			v, ok := fn(cur)
			if !ok {
				return nil, false
			}
			cur = v
			rest = strings.TrimSpace(rest[n:])
			continue
		}
		if m := sliceCall.FindStringSubmatch(rest); extendedAsserts && m != nil {
			// Array.prototype.slice: negative bounds count from the end
			x, isArr := jsonValue(cur).([]interface{})
			if !isArr {
				return nil, false
			}
			clamp := func(s string, def int) int {
				if s == "" {
					return def
				}
				n, _ := strconv.Atoi(s)
				if n < 0 {
					n += len(x)
				}
				return max(0, min(n, len(x)))
			}
			from, to := clamp(m[1], 0), clamp(m[2], len(x))
			out := []interface{}{}
			if from < to {
				out = append(out, x[from:to]...)
			}
			cur = out
			rest = rest[len(m[0]):]
			continue
		}
		if m := joinCall.FindStringSubmatch(rest); extendedAsserts && m != nil {
			// Array.prototype.join over strings (null / undefined join as "")
			x, isArr := jsonValue(cur).([]interface{})
			if !isArr {
				return nil, false
			}
			parts := make([]string, len(x))
			for i, e := range x {
				switch ev := e.(type) {
				case nil:
				case string:
					parts[i] = ev
				default:
					return nil, false
				}
			}
			cur = strings.Join(parts, tsLiteral(m[1]).(string))
			rest = rest[len(m[0]):]
			continue
		}
		if m := atCall.FindStringSubmatch(rest); m != nil {
			// Array.prototype.at: a negative index counts from the end
			x, isArr := cur.([]interface{})
			if !isArr {
				return nil, false
			}
			n, _ := strconv.Atoi(m[1])
			if n < 0 {
				n += len(x)
			}
			cur = nil // undefined out of range
			if n >= 0 && n < len(x) {
				cur = x[n]
			}
			rest = rest[len(m[0]):]
			continue
		}
		tok := pathToken
		if extendedAsserts {
			tok = pathTokenExt
		}
		m := tok.FindStringSubmatch(rest)
		if m == nil {
			return nil, false
		}
		rest = rest[len(m[0]):]
		switch {
		case m[1] != "":
			if items, isSet := setItems(cur); isSet && m[1] == "size" {
				cur = float64(len(items))
				continue
			}
			if m[1] == "length" {
				switch x := cur.(type) {
				case []interface{}:
					cur = float64(len(x))
					continue
				case string:
					cur = float64(len([]rune(x)))
					continue
				case []request:
					cur = float64(len(x))
					continue
				}
			}
			cur = member(cur, m[1])
		case m[2] != "":
			n, _ := strconv.Atoi(m[2])
			switch x := cur.(type) {
			case []interface{}:
				if n < len(x) {
					cur = x[n]
				} else {
					cur = nil
				}
			case []request:
				if n >= len(x) {
					return nil, false // that request never happened here
				}
				cur = x[n]
			default:
				return nil, false
			}
		default:
			key := m[3] + m[4]
			cur = member(cur, key)
		}
	}
	return cur, true
}

func member(v interface{}, key string) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		return x[key]
	case request:
		val, _ := x.value(key)
		return val
	case nil:
		return nil
	}
	// a Go value (struct, typed map, json.RawMessage) is read in its JSON form
	if m, ok := jsonValue(v).(map[string]interface{}); ok {
		return m[key]
	}
	return nil
}

// jsonValue round-trips v through JSON so Go structs compare with the JSON
// the TS side produced.
func jsonValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out interface{}
	_ = json.Unmarshal(b, &out)
	return out
}

func isUndef(v interface{}) bool {
	if m, ok := v.(map[string]interface{}); ok && len(m) == 1 && m["__undef"] != nil {
		return true
	}
	return v == nil
}

// subset reports whether want is contained in got the way toMatchObject
// means it: objects match by the keys of want, arrays by length and element.
func subset(want, got interface{}) bool {
	if isUndef(want) {
		return true
	}
	switch w := want.(type) {
	case map[string]interface{}:
		g, ok := got.(map[string]interface{})
		if !ok {
			return false
		}
		for k, wv := range w {
			if isUndef(wv) {
				if gv, has := g[k]; has && !isUndef(gv) {
					return false
				}
				continue
			}
			if !subset(wv, g[k]) {
				return false
			}
		}
		return true
	case []interface{}:
		g, ok := got.([]interface{})
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if !subset(w[i], g[i]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(want, got)
}

// check evaluates one assertion. It returns "" on success or when the
// expression is not one the harness can evaluate (skipped reports that).
func (a Assertion) check(roots map[string]interface{}, aliases map[string]string) (msg string, skipped bool) {
	got, ok := evalPath(roots, aliases, a.Expr)
	if !ok {
		return "", true
	}
	got = jsonValue(got)
	// a TS Set arrives as {__set: [...]}; length and membership look inside it
	if m, ok := got.(map[string]interface{}); ok && len(m) == 1 && (a.Matcher == "toContain" || a.Matcher == "toHaveLength") {
		if items, ok := m["__set"]; ok {
			got = items
		}
	}
	var pass bool
	if want, isSet := setItems(a.Arg); isSet && (a.Matcher == "toEqual" || a.Matcher == "toStrictEqual") {
		// vitest equals compares two Sets by membership, in any order
		have, gotSet := setItems(got)
		pass = gotSet && setsEqual(want, have)
		return a.failure(pass, got), false
	}
	if hasAsymmetric(a.Arg) {
		switch a.Matcher {
		case "toEqual", "toStrictEqual":
			pass = asymmetricMatch(Normalize(a.Arg), Normalize(got), false)
		case "toMatchObject":
			pass = asymmetricMatch(Normalize(a.Arg), Normalize(got), true)
		default:
			return "", true
		}
		return a.failure(pass, got), false
	}
	switch a.Matcher {
	case "toEqual", "toStrictEqual":
		if w, ok := a.Arg.([]interface{}); ok && len(w) == 0 && got == nil {
			got = []interface{}{} // a nil Go slice is an empty JS array
		}
		pass = Diff("", Normalize(a.Arg), Normalize(got)) == ""
	case "toMatchObject":
		pass = subset(Normalize(a.Arg), Normalize(got))
	case "toBe":
		switch a.Arg.(type) {
		case map[string]interface{}, []interface{}:
			return "", true
		}
		pass = reflect.DeepEqual(a.Arg, got) || (isUndef(a.Arg) && isUndef(got))
	case "toHaveLength":
		n, _ := a.Arg.(float64)
		switch x := got.(type) {
		case []interface{}:
			pass = float64(len(x)) == n
		case string:
			pass = float64(len([]rune(x))) == n
		}
	case "toBeUndefined", "toBeNull":
		pass = isUndef(got)
	case "toBeDefined":
		if isUndef(got) {
			return "", true // cannot tell undefined from null
		}
		pass = true
	case "toContain":
		switch x := got.(type) {
		case string:
			s, _ := a.Arg.(string)
			pass = strings.Contains(x, s)
		case []interface{}:
			for _, e := range x {
				if reflect.DeepEqual(e, a.Arg) {
					pass = true
				}
			}
		}
	case "toBeGreaterThan":
		g, ok1 := got.(float64)
		w, ok2 := a.Arg.(float64)
		pass = ok1 && ok2 && g > w
	case "toBeTruthy":
		pass = got != nil && got != false && got != "" && got != float64(0)
	case "toBeFalsy":
		pass = got == nil || got == false || got == "" || got == float64(0)
	default:
		return "", true
	}
	return a.failure(pass, got), false
}

// failure is the message of an assertion that evaluated to pass, "" when it
// holds.
func (a Assertion) failure(pass bool, got interface{}) string {
	if pass != a.Not {
		return ""
	}
	neg := ""
	if a.Not {
		neg = ".not"
	}
	return fmt.Sprintf("expect(%s)%s.%s(%s)\n  got:  %s", a.Expr, neg, a.Matcher, trunc(jsonString(a.Arg)), trunc(jsonString(got)))
}

// asymmetric matcher markers the extractor writes for expect.objectContaining
// and expect.arrayContaining
const (
	objectContaining = "__objectContaining"
	arrayContaining  = "__arrayContaining"
)

func asymmetricMarker(v interface{}) (string, interface{}, bool) {
	m, ok := v.(map[string]interface{})
	if !ok || len(m) != 1 {
		return "", nil, false
	}
	for _, k := range []string{objectContaining, arrayContaining} {
		if inner, ok := m[k]; ok {
			return k, inner, true
		}
	}
	return "", nil, false
}

func hasAsymmetric(v interface{}) bool {
	if _, _, ok := asymmetricMarker(v); ok {
		return true
	}
	switch x := v.(type) {
	case map[string]interface{}:
		for _, e := range x {
			if hasAsymmetric(e) {
				return true
			}
		}
	case []interface{}:
		for _, e := range x {
			if hasAsymmetric(e) {
				return true
			}
		}
	}
	return false
}

// asymmetricMatch compares normalized want and got as vitest equals does
// with asymmetric matchers: expect.objectContaining(o) matches an object
// that has every property of o (each compared fully),
// expect.arrayContaining(a) an array holding an element equal to each
// element of a. partial is toMatchObject: objects match by want's keys.
func asymmetricMatch(want, got interface{}, partial bool) bool {
	if kind, inner, ok := asymmetricMarker(want); ok {
		if kind == objectContaining {
			w, _ := inner.(map[string]interface{})
			g, isMap := got.(map[string]interface{})
			if !isMap {
				return false
			}
			for k, wv := range w {
				if !asymmetricMatch(wv, g[k], false) {
					return false
				}
			}
			return true
		}
		ws, _ := inner.([]interface{})
		gs, isArr := got.([]interface{})
		if !isArr {
			return false
		}
		for _, we := range ws {
			found := false
			for _, ge := range gs {
				if asymmetricMatch(we, ge, false) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	switch w := want.(type) {
	case map[string]interface{}:
		g, ok := got.(map[string]interface{})
		if !ok {
			return false
		}
		for k, wv := range w {
			if !asymmetricMatch(wv, g[k], partial) {
				return false
			}
		}
		if !partial {
			for k, gv := range g {
				if _, has := w[k]; !has && gv != nil {
					return false
				}
			}
		}
		return true
	case []interface{}:
		g, ok := got.([]interface{})
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if !asymmetricMatch(w[i], g[i], partial) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(want, got)
}

// aliasRootToolNames gives the assertions the TS tool names, as Normalize
// does for snapshots: every "toolName" string with a ToolNameAlias entry (Go
// names provider tools by their id) is renamed, in predicates and paths
// alike. Without aliases the roots are returned as they are.
func aliasRootToolNames(roots map[string]interface{}) map[string]interface{} {
	if len(ToolNameAlias) == 0 {
		return roots
	}
	// rename reports whether it renamed anything in a JSON value (in place).
	var rename func(v interface{}) bool
	rename = func(v interface{}) bool {
		changed := false
		switch x := v.(type) {
		case map[string]interface{}:
			for k, e := range x {
				if name, ok := e.(string); ok && k == "toolName" && ToolNameAlias[name] != "" {
					x[k] = ToolNameAlias[name]
					changed = true
				} else if rename(e) {
					changed = true
				}
			}
		case []interface{}:
			for _, e := range x {
				if rename(e) {
					changed = true
				}
			}
		}
		return changed
	}
	out := make(map[string]interface{}, len(roots))
	for k, v := range roots {
		out[k] = v
		// Only roots with a renamed tool name are replaced by their JSON
		// form; the others keep their Go values (request bodies, calls).
		if j := jsonValue(v); rename(j) {
			out[k] = j
		}
	}
	return out
}

func jsonString(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// Hand-ported assertions (round 87d closure rule): a TS plain assertion the
// evaluator cannot evaluate, reproduced by a named Go test with the same input
// and expected value. Marked assertions are neither counted nor logged as not
// evaluated; TSFIXTURE_SKIPPED=1 logs them as hand-ported with the Go test.
var (
	portedMu      sync.Mutex
	portedAsserts = map[string]string{}
	// portedSeen records, per key, whether a replayed assertion reached the
	// mark (false: it evaluated, so the mark is not needed).
	portedSeen = map[string]bool{}
)

// PortedKey is the RegisterPorted key of one TS plain assertion: the case's
// TS test title (no `#01` suffix, so a mark covers every case with that
// title), the assertion expression and its matcher, exactly as the case file
// holds them (TSFIXTURE_SKIPPED=1 prints them).
func PortedKey(test, expr, matcher string) string {
	return test + "\x00" + expr + "\x00" + matcher
}

// RegisterPorted marks TS plain assertions (PortedKey -> the name of the Go
// test that reproduces each) as hand-ported. Call it from an init function of
// a _test.go file, or at the start of a suite. A marked assertion the
// evaluator can evaluate is still evaluated (and CheckPortedUsed reports its
// mark as unneeded).
func RegisterPorted(m map[string]string) {
	portedMu.Lock()
	defer portedMu.Unlock()
	for k, v := range m {
		portedAsserts[k] = v
	}
}

// CheckPortedUsed fails t for every entry of m that is stale: no replayed
// assertion of this test binary matched its key, or the assertion it marks
// now evaluates (remove the mark). Call it after the suites that replay the
// marked cases have run, for example from a t.Cleanup registered by the
// first of them, or from TestMain after m.Run (with a nil t it returns the
// stale keys' descriptions only).
func CheckPortedUsed(t testing.TB, m map[string]string) []string {
	portedMu.Lock()
	var stale []string
	for k, goTest := range m {
		parts := strings.SplitN(k, "\x00", 3)
		desc := strings.Join(parts, " | ")
		reached, seen := portedSeen[k]
		switch {
		case !seen:
			stale = append(stale, fmt.Sprintf("hand-port mark %q (%s) matches no replayed assertion", desc, goTest))
		case !reached:
			stale = append(stale, fmt.Sprintf("hand-port mark %q (%s) is not needed: the assertion evaluates", desc, goTest))
		}
	}
	portedMu.Unlock()
	sort.Strings(stale)
	if t != nil {
		t.Helper()
		for _, s := range stale {
			t.Error(s)
		}
	}
	return stale
}

// portedFor returns the Go test of a marked assertion and records that the
// mark was reached (evaluated reports whether the evaluator handled it).
func portedFor(test string, a Assertion, evaluated bool) (string, bool) {
	k := PortedKey(test, a.Expr, a.Matcher)
	portedMu.Lock()
	defer portedMu.Unlock()
	goTest, ok := portedAsserts[k]
	if !ok {
		return "", false
	}
	if !evaluated {
		portedSeen[k] = true
	} else if _, seen := portedSeen[k]; !seen {
		portedSeen[k] = false
	}
	return goTest, true
}

func runAsserts(t testing.TB, c Case, roots map[string]interface{}) {
	t.Helper()
	roots = aliasRootToolNames(roots)
	skipped := 0
	for _, a := range c.Asserts {
		msg, skip := a.check(roots, c.Aliases)
		if goTest, ok := portedFor(c.Test, a, !skip); ok && skip {
			if os.Getenv("TSFIXTURE_SKIPPED") != "" {
				t.Logf("hand-ported: expect(%s).%s -> %s", a.Expr, a.Matcher, goTest)
			}
			continue
		}
		if skip {
			skipped++
			if os.Getenv("TSFIXTURE_SKIPPED") != "" {
				t.Logf("not evaluated: expect(%s).%s", a.Expr, a.Matcher)
			}
			continue
		}
		if msg != "" {
			t.Errorf("assertion differs from TS: %s", msg)
		}
	}
	if skipped > 0 {
		t.Logf("%d of %d plain assertions not evaluated", skipped, len(c.Asserts))
	}
}

// RunFn replays function-level TS tests (extractor --fn=NAME): call receives
// the evaluated TS arguments and returns the Go counterpart of the function's
// result in the TS return shape, so the assertions (expressions over result,
// its destructured members and any returned out-parameters such as warnings)
// can be evaluated; extra holds out-parameters (such as a warnings array the
// TS function appends to). An assertion over a call of its own
// (expect(fn(a, b)).toEqual(...)) runs call with that call's arguments. Cases
// listed in skip (by test name) are logged only.
func RunFn(t *testing.T, cases []Case, call func(args []interface{}) (interface{}, map[string]interface{}, error), skip map[string]string) {
	t.Helper()
	isThrow := func(a Assertion) bool {
		return strings.HasPrefix(a.Matcher, "rejects.") || strings.HasPrefix(a.Matcher, "toThrow")
	}
	for _, c := range cases {
		c := c
		if len(c.Asserts) == 0 {
			continue
		}
		t.Run(c.Test, func(t *testing.T) {
			if why, ok := skip[c.Test]; ok {
				t.Skip(why)
			}
			// group: assertions over the test's first call, and one group
			// per assertion that makes its own call
			type group struct {
				args    []interface{}
				asserts []Assertion
			}
			main := group{args: c.FnArgs}
			var own []group
			for _, a := range c.Asserts {
				if a.CallArgs != nil {
					own = append(own, group{args: a.CallArgs, asserts: []Assertion{a}})
				} else {
					main.asserts = append(main.asserts, a)
				}
			}
			groups := own
			if len(main.asserts) > 0 {
				groups = append([]group{main}, own...)
			}
			for _, g := range groups {
				ret, extra, err := call(g.args)
				if errors.Is(err, ErrUnsupported) {
					t.Skipf("unsupported input: %v", err)
				}
				expectsThrow := false
				for _, a := range g.asserts {
					if !isThrow(a) {
						continue
					}
					expectsThrow = true
					if (err == nil) != a.Not {
						t.Errorf("%s: error = %v, TS expects it to throw (not=%v)", firstLine(a.Expr), err, a.Not)
					}
					if msg, ok := a.Arg.(string); ok && msg != "" && err != nil && !strings.Contains(err.Error(), msg) {
						t.Errorf("error %q does not contain the TS message %q", err.Error(), msg)
					}
				}
				if err != nil && !expectsThrow {
					t.Fatalf("unexpected error: %v", err)
				}
				if err != nil {
					continue
				}
				roots := map[string]interface{}{"result": jsonValue(ret)}
				for k, v := range extra {
					roots[k] = jsonValue(v)
				}
				if m, ok := jsonValue(ret).(map[string]interface{}); ok {
					for k, v := range m {
						if _, taken := roots[k]; !taken {
							roots[k] = v
						}
					}
				}
				var plain []Assertion
				for _, a := range g.asserts {
					if isThrow(a) {
						continue
					}
					if a.CallArgs != nil {
						a.Expr = "result" + a.CallSuffix // the call itself is the result
					}
					plain = append(plain, a)
				}
				cc := c
				cc.Asserts = plain
				runAsserts(t, cc, roots)
			}
		})
	}
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

// ErrUnsupported is returned by a RunFn call function for an input the Go side
// cannot express (the case is skipped).
var ErrUnsupported = errors.New("unsupported")

// WarningsToTS renders warnings the way TS reports them: Go also mirrors
// details into message for old callers, which TS does not have.
func WarningsToTS(ws []types.Warning) []interface{} {
	out := make([]interface{}, 0, len(ws))
	for _, w := range ws {
		v := jsonValue(w)
		if m, ok := v.(map[string]interface{}); ok {
			if m["type"] != "other" {
				delete(m, "message")
			} else if m["details"] == m["message"] {
				delete(m, "details")
			}
		}
		out = append(out, v)
	}
	return out
}

// joinCall matches `.join('s')` (extended assertions only).
var joinCall = regexp.MustCompile(`^\.join\(\s*('[^']*'|"[^"]*")\s*\)`)

// newSetCall is the head of `new Set(x)`.
var newSetCall = regexp.MustCompile(`^new Set\(`)

// inCheck is the JS `in` operator: `'k' in path`.
var inCheck = regexp.MustCompile(`^('[^']*'|"[^"]*")\s+in\s+(.+)$`)

// compareCond is `path === lit` or `path !== lit`.
var compareCond = regexp.MustCompile(`^(.+?)\s*(===|!==)\s*('[^']*'|"[^"]*"|true|false|null|undefined|-?\d+(?:\.\d+)?)$`)

// setItems returns the elements of a TS Set (`{__set: [...]}`).
func setItems(v interface{}) ([]interface{}, bool) {
	m, ok := v.(map[string]interface{})
	if !ok || len(m) != 1 {
		return nil, false
	}
	items, ok := m["__set"].([]interface{})
	return items, ok
}

// setsEqual is vitest equality of two Sets: the same size and every element
// of one equal to an element of the other, in any order.
func setsEqual(want, got []interface{}) bool {
	if len(want) != len(got) {
		return false
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			if Diff("", Normalize(w), Normalize(g)) == "" {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// unwrapParens drops parens that enclose all of s.
func unwrapParens(s string) string {
	for {
		s = strings.TrimSpace(s)
		if !strings.HasPrefix(s, "(") || closingParen(s) != len(s)-1 {
			return s
		}
		s = s[1 : len(s)-1]
	}
}

// topLevelTernary splits `cond ? a : b` at its top level (outside brackets
// and strings; `?.` and `??` are not the operator).
func topLevelTernary(s string) (cond, a, b string, ok bool) {
	depth, q := 0, -1
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '\'', '"', '`':
			for i++; i < len(s) && s[i] != c; i++ {
			}
		case '?':
			if depth != 0 {
				continue
			}
			if i+1 < len(s) && (s[i+1] == '.' || s[i+1] == '?') {
				i++
				continue
			}
			if q < 0 {
				q = i
			}
		case ':':
			if depth == 0 && q >= 0 {
				return strings.TrimSpace(s[:q]), strings.TrimSpace(s[q+1 : i]), strings.TrimSpace(s[i+1:]), true
			}
		}
	}
	return "", "", "", false
}

// isOperatorBody reports whether an arrow body is one of the operator forms
// evalOperatorExpr evaluates.
func isOperatorBody(body string) bool {
	inner := unwrapParens(body)
	_, _, _, isTernary := topLevelTernary(inner)
	return isTernary || inCheck.MatchString(inner) || newSetCall.MatchString(inner) || len(splitTopLevel(inner, "??")) > 1
}

// evalOperatorExpr evaluates the operator forms of an extended assertion:
// `new Set(x)` (then a path such as `.size`), `'k' in x` and
// `cond ? a : b` (cond an `in` check, a `===` / `!==` comparison or a
// path's truthiness; a branch a literal, `[path, ...]` or a path). handled
// is false when expr is none of them.
func evalOperatorExpr(roots map[string]interface{}, aliases map[string]string, expr string) (v interface{}, ok, handled bool) {
	inner := unwrapParens(strings.TrimSuffix(strings.TrimSpace(lineBreak.ReplaceAllString(expr, " ")), ","))
	if cond, a, b, isTernary := topLevelTernary(inner); isTernary {
		c, ok := evalCond(roots, aliases, cond)
		if !ok {
			return nil, false, true
		}
		branch := b
		if c {
			branch = a
		}
		v, ok := evalOperand(roots, aliases, branch)
		return v, ok, true
	}
	if terms := splitTopLevel(inner, "??"); len(terms) > 1 {
		// `a ?? b`: the first operand that is neither null nor undefined
		var v interface{}
		for _, term := range terms {
			var ok bool
			if v, ok = evalOperand(roots, aliases, term); !ok {
				return nil, false, true
			}
			if !isUndef(jsonValue(v)) {
				break
			}
		}
		return v, true, true
	}
	if strings.HasPrefix(inner, "{") && closingBracket(inner) == len(inner)-1 {
		v, ok := evalObjectLiteral(roots, aliases, inner)
		return v, ok, true
	}
	if inCheck.MatchString(inner) || isArrayCond.MatchString(inner) || includesCond.MatchString(inner) {
		c, ok := evalCond(roots, aliases, inner)
		return c, ok, true
	}
	if newSetCall.MatchString(inner) {
		open := len("new Set")
		end := closingParen(inner[open:])
		if end < 0 {
			return nil, false, true
		}
		end += open
		src, ok := evalPath(roots, aliases, inner[open+1:end])
		list, isArr := jsonValue(src).([]interface{})
		if !ok || !isArr {
			return nil, false, true
		}
		// a Set keeps the first of equal (SameValueZero) primitives; objects
		// are distinct by identity
		items := []interface{}{}
		for _, e := range list {
			dup := false
			for _, have := range items {
				if isPrimitive(e) && reflect.DeepEqual(e, have) {
					dup = true
					break
				}
			}
			if !dup {
				items = append(items, e)
			}
		}
		set := map[string]interface{}{"__set": items}
		rest := strings.TrimSpace(inner[end+1:])
		if rest == "" {
			return set, true, true
		}
		v, ok := evalPath(map[string]interface{}{"__setValue": set}, nil, "__setValue"+rest)
		return v, ok, true
	}
	return nil, false, false
}

func isPrimitive(v interface{}) bool {
	switch v.(type) {
	case nil, bool, string, float64:
		return true
	}
	return false
}

// evalCond evaluates a ternary condition or a bare `in` check.
func evalCond(roots map[string]interface{}, aliases map[string]string, cond string) (bool, bool) {
	cond = unwrapParens(cond)
	for _, op := range []string{"||", "&&"} {
		terms := splitTopLevel(cond, op)
		if len(terms) < 2 {
			continue
		}
		for _, term := range terms {
			c, ok := evalCond(roots, aliases, term)
			if !ok {
				return false, false
			}
			if c == (op == "||") {
				return c, true // short circuit
			}
		}
		return op == "&&", true
	}
	if strings.HasPrefix(cond, "!") && !strings.HasPrefix(cond, "!=") {
		c, ok := evalCond(roots, aliases, cond[1:])
		return !c, ok
	}
	if m := startsWithCond.FindStringSubmatch(cond); m != nil {
		v, ok := evalPath(roots, aliases, m[1])
		if !ok {
			return false, false
		}
		str, isStr := jsonValue(v).(string)
		if !isStr {
			return false, false // a TypeError in TS
		}
		return strings.HasPrefix(str, tsLiteral(m[2]).(string)), true
	}
	if m := isArrayCond.FindStringSubmatch(cond); m != nil {
		v, ok := evalOperand(roots, aliases, m[1])
		_, isArr := jsonValue(v).([]interface{})
		return isArr, ok
	}
	if m := includesCond.FindStringSubmatch(cond); m != nil {
		// Array.prototype.includes (SameValueZero) or String.prototype.includes
		hay, ok1 := evalOperand(roots, aliases, m[1])
		needle, ok2 := evalOperand(roots, aliases, m[2])
		if !ok1 || !ok2 {
			return false, false
		}
		switch h := jsonValue(hay).(type) {
		case string:
			n, isStr := jsonValue(needle).(string)
			return isStr && strings.Contains(h, n), isStr
		case []interface{}:
			for _, e := range h {
				if isPrimitive(e) && reflect.DeepEqual(e, jsonValue(needle)) {
					return true, true
				}
			}
			return false, true
		}
		return false, false
	}
	if m := inCheck.FindStringSubmatch(cond); m != nil {
		v, ok := evalPath(roots, aliases, m[2])
		if !ok {
			return false, false
		}
		obj, isObj := jsonValue(v).(map[string]interface{})
		if !isObj {
			return false, false // `in` throws on a primitive
		}
		_, has := obj[tsLiteral(m[1]).(string)]
		return has, true
	}
	if m := compareCond.FindStringSubmatch(cond); m != nil {
		v, ok := evalPath(roots, aliases, m[1])
		if !ok {
			return false, false
		}
		eq := reflect.DeepEqual(jsonValue(v), tsLiteral(m[3]))
		return eq == (m[2] == "==="), true
	}
	v, ok := evalPath(roots, aliases, cond)
	return ok && jsTruthy(jsonValue(v)), ok
}

// operandLiteral matches the literals a ternary branch can be.
var operandLiteral = regexp.MustCompile(`^(?:'[^']*'|"[^"]*"|true|false|null|undefined|-?\d+(?:\.\d+)?|\[\])$`)

// evalOperand evaluates a ternary branch: a literal, `[path, ...]` or a path.
func evalOperand(roots map[string]interface{}, aliases map[string]string, s string) (interface{}, bool) {
	s = unwrapParens(s)
	if i := trailingCast(s); i >= 0 {
		s = unwrapParens(s[:i]) // `(x as T)`: the cast is type-only
	}
	if strings.HasPrefix(s, "{") || splitTopLevel(s, "??")[0] != s {
		v, ok, _ := evalOperatorExpr(roots, aliases, s)
		return v, ok
	}
	if operandLiteral.MatchString(s) {
		return tsLiteral(s), true
	}
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		out := []interface{}{}
		for _, e := range strings.Split(s[1:len(s)-1], ",") {
			if e = strings.TrimSpace(e); e == "" {
				continue
			}
			v, ok := evalOperand(roots, aliases, e)
			if !ok {
				return nil, false
			}
			out = append(out, v)
		}
		return out, true
	}
	return evalPath(roots, aliases, s)
}

// startsWithCond is `path.startsWith('s')`.
var startsWithCond = regexp.MustCompile(`^(.+)\.startsWith\(\s*('[^']*'|"[^"]*")\s*\)$`)

// splitTopLevel splits s at each top-level op (outside brackets and
// strings).
func splitTopLevel(s, op string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '\'', '"', '`':
			for i++; i < len(s) && s[i] != c; i++ {
			}
		default:
			if depth == 0 && strings.HasPrefix(s[i:], op) {
				out = append(out, strings.TrimSpace(s[start:i]))
				i += len(op) - 1
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}

// isArrayCond is `Array.isArray(x)`.
var isArrayCond = regexp.MustCompile(`^Array\.isArray\((.+)\)$`)

// includesCond is `x.includes(y)` (x an array literal or a path).
var includesCond = regexp.MustCompile(`^(.+)\.includes\(\s*([^()]+?)\s*,?\s*\)$`)

// trailingCast is the index of a top-level TS `as` cast in s (`x as T`, also
// with the space before T lost to a joined line break: `x as| A | B`), or
// -1.
func trailingCast(s string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '\'', '"', '`':
			for i++; i < len(s) && s[i] != c; i++ {
			}
		case ' ':
			if depth == 0 && strings.HasPrefix(s[i:], " as") && len(s) > i+3 && !isIdentByte(s[i+3]) {
				return i
			}
		}
	}
	return -1
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// evalObjectLiteral evaluates `{ key: expr, short, ... }`.
func evalObjectLiteral(roots map[string]interface{}, aliases map[string]string, s string) (interface{}, bool) {
	body := strings.TrimSpace(s[1 : len(s)-1])
	out := map[string]interface{}{}
	for _, entry := range splitTopLevel(body, ",") {
		if entry == "" {
			continue
		}
		key, val, hasColon := strings.Cut(entry, ":")
		key = strings.Trim(strings.TrimSpace(key), `'"`)
		if !hasColon {
			val = key // shorthand property
		}
		if !regexp.MustCompile(`^[A-Za-z_$][\w$]*$`).MatchString(key) {
			return nil, false
		}
		v, ok := evalOperand(roots, aliases, strings.TrimSpace(val))
		if !ok {
			return nil, false
		}
		if v = jsonValue(v); v != nil {
			out[key] = v
		} else {
			out[key] = map[string]interface{}{"__undef": true}
		}
	}
	return out, true
}

// closingBracket is the index of the bracket closing s[0] ('(', '[' or
// '{'), or -1.
func closingBracket(s string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth--; depth == 0 {
				return i
			}
		case '\'', '"', '`':
			for i++; i < len(s) && s[i] != c; i++ {
			}
		}
	}
	return -1
}
