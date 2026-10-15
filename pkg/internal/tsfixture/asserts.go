package tsfixture

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Assertion is one plain vitest expectation (expect(expr).toEqual(arg) and
// friends) of a TS test. Expr is TS source over the call result.
type Assertion struct {
	Expr    string      `json:"expr"`
	Matcher string      `json:"matcher"`
	Not     bool        `json:"not"`
	Arg     interface{} `json:"arg"`
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

// evalPath resolves a simple TS access expression (identifiers, ., ?., [n],
// ['key'], .length) against roots. ok is false for anything else.
func evalPath(roots map[string]interface{}, aliases map[string]string, expr string) (interface{}, bool) {
	expr = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(expr), "await "))
	root := regexp.MustCompile(`^[A-Za-z_$][\w$]*`).FindString(expr)
	if root == "" {
		return nil, false
	}
	rest := expr[len(root):]
	if a, ok := aliases[root]; ok {
		return evalPath(roots, nil, a+rest)
	}
	cur, ok := roots[root]
	if !ok {
		return nil, false
	}
	for rest != "" {
		m := pathToken.FindStringSubmatch(rest)
		if m == nil {
			return nil, false
		}
		rest = rest[len(m[0]):]
		switch {
		case m[1] != "":
			if m[1] == "length" {
				switch x := cur.(type) {
				case []interface{}:
					cur = float64(len(x))
					continue
				case string:
					cur = float64(len([]rune(x)))
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
	var pass bool
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
	if pass == a.Not {
		neg := ""
		if a.Not {
			neg = ".not"
		}
		return fmt.Sprintf("expect(%s)%s.%s(%s)\n  got:  %s", a.Expr, neg, a.Matcher, trunc(jsonString(a.Arg)), trunc(jsonString(got))), false
	}
	return "", false
}

func jsonString(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func runAsserts(t *testing.T, c Case, roots map[string]interface{}) {
	t.Helper()
	skipped := 0
	for _, a := range c.Asserts {
		msg, skip := a.check(roots, c.Aliases)
		if skip {
			skipped++
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
// can be evaluated. Cases listed in skip (by test name) are logged only.
func RunFn(t *testing.T, cases []Case, call func(args []interface{}) (interface{}, error), skip map[string]string) {
	t.Helper()
	for _, c := range cases {
		c := c
		if c.FnArgs == nil || len(c.Asserts) == 0 {
			continue
		}
		t.Run(c.Test, func(t *testing.T) {
			if why, ok := skip[c.Test]; ok {
				t.Skip(why)
			}
			ret, err := call(c.FnArgs)
			if errors.Is(err, ErrUnsupported) {
				t.Skipf("unsupported input: %v", err)
			}
			expectsThrow := false
			for _, a := range c.Asserts {
				if strings.HasPrefix(a.Matcher, "rejects.") || strings.HasPrefix(a.Matcher, "toThrow") {
					expectsThrow = true
					if (err == nil) != a.Not {
						t.Errorf("%s(...): error = %v, TS expects it to throw (not=%v)", a.Expr[:min(len(a.Expr), 40)], err, a.Not)
					}
					if msg, ok := a.Arg.(string); ok && msg != "" && err != nil && !strings.Contains(err.Error(), msg) {
						t.Errorf("error %q does not contain the TS message %q", err.Error(), msg)
					}
				}
			}
			if err != nil && !expectsThrow {
				t.Fatalf("unexpected error: %v", err)
			}
			if err != nil {
				return
			}
			roots := map[string]interface{}{"result": jsonValue(ret)}
			if m, ok := jsonValue(ret).(map[string]interface{}); ok {
				for k, v := range m {
					if _, taken := roots[k]; !taken {
						roots[k] = v
					}
				}
			}
			var plain []Assertion
			for _, a := range c.Asserts {
				if !strings.HasPrefix(a.Matcher, "rejects.") && !strings.HasPrefix(a.Matcher, "toThrow") {
					plain = append(plain, a)
				}
			}
			c.Asserts = plain
			runAsserts(t, c, roots)
		})
	}
}

// ErrUnsupported is returned by a RunFn call function for an input the Go side
// cannot express (the case is skipped).
var ErrUnsupported = errors.New("unsupported")
