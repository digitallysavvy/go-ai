package tsfixture

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/internal/third_party/qjs"
)

// The JS fallback (round 87d). When evalPath cannot evaluate an assertion's
// expression and the extractor recorded a JS program for it (Assertion.JS:
// the test's statements up to the assertion, with the model or function call
// bound to its replayed result, then `return (EXPR)`), the program runs in
// QuickJS (the module's vendored qjs, no file system, no modules) over the
// replayed values in their TS form. This evaluates locals a test builds
// with loops or statements, helper calls, Object.keys, generic type
// arguments (types are stripped at extraction) and any other plain JS
// expression over the result, exactly as the TS test computes them. A
// program that throws (it needs a mock, a second model call or anything the
// replay does not have) leaves the assertion not evaluated.

var (
	jsMu      sync.Mutex
	jsRuntime *qjs.Runtime
	jsInitErr error
)

// jsUndefined is the JS fallback's value for undefined (Go nil is null
// there, so toBeDefined / toBeUndefined can tell the two apart).
var jsUndefined = map[string]interface{}{"__undef": true}

// jsPrelude are the stubs every program sees: expect is a no-op chain (the
// statements before the assertion may hold other expectations), the stream
// helpers return the replayed parts, and the encoder writes the value back
// in the extractor's JSON conventions (undefined, Uint8Array, Set, Map).
const jsPrelude = `
const __b64 = (u) => { const c = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/'; let s = ''; for (let i = 0; i < u.length; i += 3) { const n = (u[i] << 16) | ((u[i + 1] ?? 0) << 8) | (u[i + 2] ?? 0); s += c[(n >> 18) & 63] + c[(n >> 12) & 63] + (i + 1 < u.length ? c[(n >> 6) & 63] : '=') + (i + 2 < u.length ? c[n & 63] : '='); } return s; };
const __enc = (v) => {
  if (v === undefined) return { __undef: true };
  if (v === null || typeof v !== 'object') return typeof v === 'bigint' ? Number(v) : (typeof v === 'function' ? { __fn: true } : v);
  if (v instanceof Uint8Array) return { __bytes: __b64(v) };
  if (v instanceof Set) return { __set: [...v].map(__enc) };
  if (v instanceof Map) { const o = {}; for (const [k, x] of v) o[String(k)] = __enc(x); return o; }
  if (v instanceof Date) return v.toISOString();
  if (v instanceof Error) return { name: v.name, message: v.message };
  if (Array.isArray(v)) return v.map(__enc);
  const o = {}; for (const k of Object.keys(v)) { if (v[k] !== undefined) o[k] = __enc(v[k]); } return o;
};
const __stub = () => new Proxy(function () {}, { get: (t, k) => (k === 'then' ? undefined : __stub()), apply: () => __stub() });
const expect = Object.assign(() => __stub(), {
  objectContaining: (o) => ({ __objectContaining: o }),
  arrayContaining: (a) => ({ __arrayContaining: a }),
  any: () => __stub(), anything: () => __stub(), stringContaining: () => __stub(),
});
const convertReadableStreamToArray = async (s) => (Array.isArray(s) ? [...s] : s);
const convertAsyncIterableToArray = convertReadableStreamToArray;
`

var jsIdent = regexp.MustCompile(`^[A-Za-z_$][\w$]*$`)

// jsReserved are root names a program cannot rebind.
var jsReserved = map[string]bool{"expect": true, "convertReadableStreamToArray": true, "convertAsyncIterableToArray": true, "undefined": true, "NaN": true, "Infinity": true}

// jsRoot is a root value in its TS form: the server's calls become the test
// server's call objects.
func jsRoot(name string, v interface{}) interface{} {
	if name != "server" {
		return jsonValue(v)
	}
	srv, ok := v.(map[string]interface{})
	if !ok {
		return jsonValue(v)
	}
	out := map[string]interface{}{}
	for k, x := range srv {
		out[k] = jsonValue(x)
	}
	if calls, ok := srv["calls"].([]request); ok {
		list := make([]interface{}, len(calls))
		for i, r := range calls {
			list[i] = map[string]interface{}{
				"requestBodyJson":  r.body,
				"requestHeaders":   r.headers,
				"requestUserAgent": r.ua,
				"requestUrl":       r.url,
				"requestMethod":    r.method,
			}
		}
		out["calls"] = list
	}
	return out
}

// jsDebug prints why a JS program failed (TSFIXTURE_JS_DEBUG=1).
func jsDebug(err error) {
	if os.Getenv("TSFIXTURE_JS_DEBUG") != "" {
		msg, _, _ := strings.Cut(err.Error(), "\n    at ")
		fmt.Fprintln(os.Stderr, "tsfixture js:", msg)
	}
}

// evalJS runs an assertion's JS program over roots. ok is false when the
// program throws or its value cannot be read back.
// The case's aliases the Go side can evaluate (a describe-level `parts` set
// in a beforeEach, for example) are bound too; the program's own
// declarations shadow them.
func evalJS(roots map[string]interface{}, aliases map[string]string, program string) (interface{}, bool) {
	if program == "" {
		return nil, false
	}
	bound := map[string]interface{}{}
	var decls strings.Builder
	for k, v := range roots {
		if !jsIdent.MatchString(k) || jsReserved[k] || strings.HasPrefix(k, "__") {
			continue
		}
		bound[k] = jsRoot(k, v)
		fmt.Fprintf(&decls, "const %s = __roots[%q];\n", k, k)
	}
	for k, expr := range aliases {
		if _, isRoot := roots[k]; isRoot || !jsIdent.MatchString(k) || jsReserved[k] || strings.HasPrefix(k, "__") {
			continue
		}
		v, ok := evalPath(roots, aliases, expr)
		if !ok {
			continue
		}
		bound[k] = jsRoot(k, v)
		fmt.Fprintf(&decls, "const %s = __roots[%q];\n", k, k)
	}
	rootsJSON, err := json.Marshal(bound)
	if err != nil {
		return nil, false
	}
	rootsLit, _ := json.Marshal(string(rootsJSON))
	src := "(async () => {\n'use strict';\n" + jsPrelude +
		"const __roots = JSON.parse(" + string(rootsLit) + ", (k, v) => (v === null || typeof v !== 'object' || Array.isArray(v) || Object.keys(v).length !== 1 ? v : Array.isArray(v.__set) ? new Set(v.__set) : ('__returns' in v ? () => v.__returns : v)));\n" + decls.String() +
		"const __program = (" + program + ");\n" +
		"return JSON.stringify({ v: __enc(await __program(__roots.result)) });\n})()"

	jsMu.Lock()
	defer jsMu.Unlock()
	if jsRuntime == nil && jsInitErr == nil {
		jsRuntime, jsInitErr = qjs.New(qjs.Option{NoFSMount: true, DisableModules: true})
	}
	if jsInitErr != nil {
		return nil, false
	}
	out, ok := func() (s string, ok bool) {
		defer func() {
			if recover() != nil {
				ok = false
			}
		}()
		v, err := jsRuntime.Context().Eval("assertion.js", qjs.Code(src))
		if err != nil {
			jsDebug(err)
			return "", false
		}
		defer v.Free()
		if v.IsPromise() {
			w, err := v.Await()
			if err != nil {
				jsDebug(err)
				return "", false
			}
			defer w.Free()
			return w.String(), true
		}
		return v.String(), true
	}()
	if !ok {
		return nil, false
	}
	var res struct {
		V interface{} `json:"v"`
	}
	if json.Unmarshal([]byte(out), &res) != nil {
		return nil, false
	}
	return res.V, true
}
