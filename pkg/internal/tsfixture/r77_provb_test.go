package tsfixture

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r77provbDo sends one request and returns "<status> <body>".
func r77provbDo(ctx context.Context, method, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck
	b, err := io.ReadAll(resp.Body)
	return fmt.Sprintf("%d %s", resp.StatusCode, b), err
}

// TestR77ProvbRunRoutesByPath: a TS test that sets the responses of two URLs
// of its test server (POST /interactions, then GET /interactions/{id} from a
// response function) gets each request's response by path, the function's
// results in order with the last repeated, a 404 for an unknown path, and
// its call counter (`let getCallCount = 0`) as the route's request count.
// Before r77 every request got the POST response, so the google
// background polling/reconnect cases could not be replayed.
func TestR77ProvbRunRoutesByPath(t *testing.T) {
	var got []string
	cases := []Case{{
		Test:     "routes",
		CallKind: "generate",
		ModelID:  "m",
		Call:     map[string]interface{}{},
		Response: &Response{Kind: "json", Body: []byte(`{"post":true}`)},
		Routes: []Route{
			{Path: "/v1beta/interactions", Response: &Response{Kind: "json", Body: []byte(`{"post":true}`)}},
			{Path: "/v1beta/interactions/v1_x", Response: &Response{Kind: "json", Body: []byte(`{"status":"in_progress"}`), Repeat: true, Next: []*Response{
				{Kind: "chunks", Chunks: []interface{}{"data: done\n\n"}},
			}}},
		},
		Aliases: map[string]string{"getCallCount": "server.hits['/v1beta/interactions/v1_x']", "calls": "server.calls"},
		Asserts: []Assertion{
			{Expr: "getCallCount", Matcher: "toBe", Arg: float64(3)},
			{Expr: "server.hits['/v1beta/interactions']", Matcher: "toBe", Arg: float64(1)},
			{Expr: "calls[0].requestMethod", Matcher: "toBe", Arg: "POST"},
			{Expr: "calls[1].requestMethod", Matcher: "toBe", Arg: "GET"},
			{Expr: "new URL(calls[2].requestUrl).searchParams.get('last_event_id')", Matcher: "toBe", Arg: "evt-3"},
			{Expr: "new URL(calls[1].requestUrl).searchParams.get('last_event_id')", Matcher: "toBeNull"},
			{Expr: "server.calls.length", Matcher: "toBe", Arg: float64(5)},
		},
	}}
	Run(t, cases, func(baseURL, _ string) provider.LanguageModel {
		return &testutil.MockLanguageModel{DoGenerateFunc: func(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			for _, rq := range []struct{ method, path string }{
				{http.MethodPost, "/interactions"},
				{http.MethodGet, "/interactions/v1_x?stream=true"},
				{http.MethodGet, "/interactions/v1_x?stream=true&last_event_id=evt-3"},
				{http.MethodGet, "/interactions/v1_x"},
				{http.MethodGet, "/other"},
			} {
				s, err := r77provbDo(ctx, rq.method, baseURL+rq.path)
				if err != nil {
					return nil, err
				}
				got = append(got, s)
			}
			return &types.GenerateResult{FinishReason: types.FinishReasonStop}, nil
		}}
	}, nil)
	want := []string{
		`200 {"post":true}`,
		`200 {"status":"in_progress"}`,
		"200 data: done\n\n",
		"200 data: done\n\n",
		`404 {"error":"Not Found"}`,
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("responses = %q\nwant        %q", got, want)
	}
}

// TestR77ProvbMatchRoute: the Go base URL replaces the TS one, so a Go path
// matches the TS route it is a suffix of (or that is a suffix of it); the
// longest match wins and "/" matches only "/".
func TestR77ProvbMatchRoute(t *testing.T) {
	routes := []Route{
		{Path: "/v1beta/interactions", Response: &Response{}},
		{Path: "/v1beta/interactions/v1_x", Response: &Response{}},
	}
	for path, want := range map[string]int{
		"/interactions":             0,
		"/v1beta/interactions":      0,
		"/interactions/v1_x":        1,
		"/v1beta/interactions/v1_x": 1,
		"/interactions/v1_x/cancel": -1,
		"/":                         -1,
		"/x":                        -1,
	} {
		if got := matchRoute(routes, path); got != want {
			t.Errorf("matchRoute(%q) = %d, want %d", path, got, want)
		}
	}
}

// TestR77ProvbEvalPathFindAndAliasChains: `parts.find(p => p.type === 'v')`
// returns the first match (undefined without one), an alias may name another
// alias (`const textDelta = parts.find(...)` over `const parts = await
// convertReadableStreamToArray(stream)`), and
// `new URL(x).searchParams.get('k')` is the query value or null.
func TestR77ProvbEvalPathFindAndAliasChains(t *testing.T) {
	roots := map[string]interface{}{
		"result": map[string]interface{}{"stream": []interface{}{
			map[string]interface{}{"type": "text-start"},
			map[string]interface{}{"type": "text-delta", "delta": "a"},
			map[string]interface{}{"type": "text-delta", "delta": "b"},
		}},
		"server": map[string]interface{}{"calls": []request{{url: "http://h/i/x?stream=true"}}},
	}
	aliases := map[string]string{
		"parts":     "result.stream",
		"textDelta": "parts.find(p => p.type === 'text-delta')",
		"types":     "parts.map(p => p.type)",
		"calls":     "server.calls",
		"loop":      "loop.x",
	}
	for _, tc := range []struct {
		expr string
		want interface{}
	}{
		{"textDelta.delta", "a"},
		{"parts.find(p => p.type === 'finish')", nil},
		{"types", []interface{}{"text-start", "text-delta", "text-delta"}},
		{"new URL(calls[0].requestUrl).searchParams.get('stream')", "true"},
		{"new URL(calls[0].requestUrl).searchParams.get('last_event_id')", nil},
	} {
		got, ok := evalPath(roots, aliases, tc.expr)
		if !ok {
			t.Errorf("%q: not evaluated", tc.expr)
			continue
		}
		if d := Diff("", Normalize(tc.want), Normalize(jsonValue(got))); d != "" {
			t.Errorf("%q: %s", tc.expr, d)
		}
	}
	for _, expr := range []string{"loop", "new URL(calls[3].requestUrl).searchParams.get('k')"} {
		if _, ok := evalPath(roots, aliases, expr); ok {
			t.Errorf("%q evaluated; want not evaluated", expr)
		}
	}
}

// TestR77ProvbAsymmetricMatchers: expect.objectContaining /
// expect.arrayContaining (extracted as markers) match as vitest does:
// objectContaining ignores other properties but compares its own fully,
// arrayContaining needs each element somewhere, and toEqual still rejects
// extra properties outside them.
func TestR77ProvbAsymmetricMatchers(t *testing.T) {
	oc := func(m map[string]interface{}) interface{} { return map[string]interface{}{objectContaining: m} }
	ac := func(a ...interface{}) interface{} { return map[string]interface{}{arrayContaining: a} }
	content := []interface{}{
		map[string]interface{}{"type": "reasoning", "text": "r"},
		map[string]interface{}{"type": "text", "text": "answer", "providerMetadata": map[string]interface{}{"g": map[string]interface{}{"a": 1.0, "b": 2.0}}},
	}
	roots := map[string]interface{}{"result": map[string]interface{}{"content": content}}
	for _, tc := range []struct {
		matcher string
		arg     interface{}
		pass    bool
	}{
		{"toEqual", ac(oc(map[string]interface{}{"type": "text", "text": "answer"})), true},
		{"toEqual", ac(oc(map[string]interface{}{"type": "text", "text": "other"})), false},
		{"toEqual", []interface{}{oc(map[string]interface{}{"type": "reasoning"}), oc(map[string]interface{}{"type": "text"})}, true},
		{"toEqual", []interface{}{oc(map[string]interface{}{"type": "text"})}, false}, // length differs
		// objectContaining compares its own properties fully
		{"toEqual", ac(oc(map[string]interface{}{"providerMetadata": map[string]interface{}{"g": map[string]interface{}{"a": 1.0}}})), false},
		{"toEqual", ac(oc(map[string]interface{}{"providerMetadata": map[string]interface{}{"g": map[string]interface{}{"a": 1.0, "b": 2.0}}})), true},
		// outside the marker toEqual rejects extra keys
		{"toEqual", []interface{}{map[string]interface{}{"type": "reasoning"}, oc(map[string]interface{}{"type": "text"})}, false},
		{"toMatchObject", []interface{}{map[string]interface{}{"type": "reasoning"}, oc(map[string]interface{}{"type": "text"})}, true},
	} {
		a := Assertion{Expr: "result.content", Matcher: tc.matcher, Arg: tc.arg}
		msg, skipped := a.check(roots, nil)
		if skipped {
			t.Errorf("%s %s: skipped", tc.matcher, jsonString(tc.arg))
			continue
		}
		if (msg == "") != tc.pass {
			t.Errorf("%s %s: pass = %v, want %v (%s)", tc.matcher, jsonString(tc.arg), msg == "", tc.pass, msg)
		}
	}
}
