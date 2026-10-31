package tsfixture

// Wire replay: record/ runs the TypeScript provider tests under vitest with a
// hook on the test server and on every model `do*` call, so each TS test is
// captured as: the calls made (arguments and results), the HTTP requests the
// TS provider sent, and the responses the test server served. This file
// replays such a recording through a Go model against a local server and
// diffs what the Go model sent (method, query, body, headers) and returned.

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
)

// WireTest is one recorded TS test.
type WireTest struct {
	File  string     `json:"file"`
	Test  string     `json:"test"`
	Calls []WireCall `json:"calls"`
	HTTP  []WireHTTP `json:"http"`
}

// WireModel identifies the TS model a call was made on.
type WireModel struct {
	ModelID  string                 `json:"modelId"`
	Provider string                 `json:"provider"`
	Cls      string                 `json:"cls"`
	Config   map[string]interface{} `json:"config"`
	// Inst numbers the object a stateful call (an event mapper) ran on.
	Inst int `json:"inst,omitempty"`

	// RequestURLs is set by the runner to the URLs of the HTTP requests the
	// TS test made, for providers whose settings show only in the URLs.
	RequestURLs []string `json:"-"`
	// RequestAuth is set by the runner to the Authorization header of each
	// recorded HTTP request ("" when absent), in order. A suite uses it to
	// stand in for a TS token provider (code the recording cannot carry).
	RequestAuth []string `json:"-"`
	// CallHeaders is set by the runner to the headers the first call passed.
	CallHeaders map[string]string `json:"-"`
}

// WireCall is one model.do*() invocation.
type WireCall struct {
	Method   string        `json:"method"`
	Args     []interface{} `json:"args"`
	Model    WireModel     `json:"model"`
	Result   interface{}   `json:"result"`
	Error    interface{}   `json:"error"`
	Streamed bool          `json:"streamed"`

	// argsRaw is the call arguments as recorded, keeping the key order of
	// the TS objects (Args holds Go maps, which sort keys).
	argsRaw json.RawMessage
}

// UnmarshalJSON decodes the call and keeps the raw arguments text.
func (c *WireCall) UnmarshalJSON(b []byte) error {
	type plain WireCall
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	var aux struct {
		Args json.RawMessage `json:"args"`
	}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	*c = WireCall(p)
	c.argsRaw = aux.Args
	return nil
}

// RawResponseFormatSchema returns the responseFormat schema of the first call
// argument as recorded (TS key order), or nil.
func (c WireCall) RawResponseFormatSchema() json.RawMessage {
	if len(c.argsRaw) == 0 {
		return nil
	}
	tree, ok := jsonorder.Parse(c.argsRaw)
	if !ok {
		return nil
	}
	args, _ := tree.([]interface{})
	if len(args) == 0 {
		return nil
	}
	rf, ok := jsonorder.Get(args[0], "responseFormat", "schema")
	if !ok || rf == nil {
		return nil
	}
	return json.RawMessage(jsonorder.Stringify(rf))
}

// RawQuestionOrder returns the decision questions' IDs, and each question's
// criteria keys, in the order the TS object had them.
func (c WireCall) RawQuestionOrder() (ids []string, criteria map[string][]string) {
	if len(c.argsRaw) == 0 {
		return nil, nil
	}
	tree, ok := jsonorder.Parse(c.argsRaw)
	if !ok {
		return nil, nil
	}
	args, _ := tree.([]interface{})
	if len(args) == 0 {
		return nil, nil
	}
	qs, ok := jsonorder.Get(args[0], "questions")
	if !ok {
		return nil, nil
	}
	qo, ok := qs.(*jsonorder.Object)
	if !ok {
		return nil, nil
	}
	ids = qo.Keys()
	criteria = map[string][]string{}
	for _, id := range ids {
		if cr, ok := jsonorder.Get(qs, id, "criteria"); ok {
			if co, ok := cr.(*jsonorder.Object); ok {
				criteria[id] = co.Keys()
			}
		}
	}
	return ids, criteria
}

// Arg returns the first argument as an object (the call options).
func (c WireCall) Arg() map[string]interface{} {
	if len(c.Args) == 0 {
		return nil
	}
	m, _ := c.Args[0].(map[string]interface{})
	return m
}

// WireHTTP is one HTTP request the TS provider made and the response the
// test server served.
type WireHTTP struct {
	RouteURL string            `json:"routeUrl"`
	Method   string            `json:"method"`
	URL      string            `json:"url"`
	Headers  map[string]string `json:"headers"`
	BodyB64  string            `json:"bodyB64"`
	Response *WireResponse     `json:"response"`
}

// WireResponse is the served response.
type WireResponse struct {
	Type    string            `json:"type"`
	Headers map[string]string `json:"headers"`
	Body    interface{}       `json:"body"`
	Chunks  []string          `json:"chunks"`
	BodyB64 string            `json:"bodyB64"`
	Status  int               `json:"status"`

	// rawBody is the body as recorded, with the JSON key order of the TS
	// object (Body is a Go map, which sorts keys); json-value responses are
	// served from it so the model sees the same text as in TS.
	rawBody json.RawMessage
}

// UnmarshalJSON decodes the response and keeps the raw body text.
func (r *WireResponse) UnmarshalJSON(b []byte) error {
	type plain WireResponse
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	var aux struct {
		Body json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	*r = WireResponse(p)
	r.rawBody = aux.Body
	return nil
}

// Path is the URL path of the request.
func (h WireHTTP) Path() string {
	u, err := url.Parse(h.URL)
	if err != nil {
		return h.URL
	}
	return u.Path
}

// Body returns the request body.
func (h WireHTTP) Body() []byte {
	b, _ := base64.StdEncoding.DecodeString(h.BodyB64)
	return b
}

// LoadWire reads a gzipped JSON-lines recording.
func LoadWire(t testing.TB, path string) []WireTest {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck
	var r io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		z, err := gzip.NewReader(f)
		if err != nil {
			t.Fatal(err)
		}
		defer z.Close() //nolint:errcheck
		r = z
	}
	dec := json.NewDecoder(r)
	var out []WireTest
	for {
		var w WireTest
		if err := dec.Decode(&w); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		w.HTTP = dedupeHTTP(w.HTTP)
		out = append(out, w)
	}
	return out
}

// dedupeHTTP drops a multipart request recorded twice in a row with the same
// URL, boundary and body (the TS fetch layer replays an upload).
func dedupeHTTP(in []WireHTTP) []WireHTTP {
	var out []WireHTTP
	for _, h := range in {
		if n := len(out); n > 0 {
			p := out[n-1]
			// Only multipart uploads (same boundary): polling repeats the same
			// GET legitimately.
			if h.Method == "POST" && p.Method == h.Method && p.URL == h.URL && p.BodyB64 == h.BodyB64 &&
				strings.HasPrefix(h.Headers["content-type"], "multipart/") && p.Headers["content-type"] == h.Headers["content-type"] {
				continue
			}
		}
		out = append(out, h)
	}
	return out
}

// WireBytes decodes a recorded Uint8Array ({"$bytes": base64}).
func WireBytes(v interface{}) ([]byte, bool) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil, false
	}
	s, ok := m["$bytes"].(string)
	if !ok {
		// the case extractor (extract.mjs) marks Uint8Array values __bytes
		s, ok = m["__bytes"].(string)
	}
	if !ok {
		return nil, false
	}
	b, err := base64.StdEncoding.DecodeString(s)
	return b, err == nil
}

// WireUnwrap converts the recorder's markers to plain values: $url becomes
// its string, $date its ISO string, $bytes stays a marker.
func WireUnwrap(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		if u, ok := x["$url"].(string); ok && len(x) == 1 {
			return u
		}
		if d, ok := x["$date"].(string); ok && len(x) == 1 {
			return d
		}
		if b, ok := x["$bytes"].(string); ok && len(x) == 1 {
			return b
		}
		o := make(map[string]interface{}, len(x))
		for k, e := range x {
			o[k] = WireUnwrap(e)
		}
		return o
	case []interface{}:
		o := make([]interface{}, len(x))
		for i, e := range x {
			o[i] = WireUnwrap(e)
		}
		return o
	}
	return v
}

// GotRequest is a request a Go model made against the replay server.
type GotRequest struct {
	Method  string
	Path    string
	Query   url.Values
	Header  http.Header
	Body    []byte
	Matched int // index into WireTest.HTTP, -1 if none

	ServerURL string
}

// WireServer serves the recorded responses of one test.
type WireServer struct {
	*httptest.Server
	test    WireTest
	origins []string

	// GoAdds is a path prefix the Go provider adds to endpoint paths that the
	// TS base URL carries instead (for example "/v1"); requests are matched
	// to recordings with and without it.
	GoAdds string

	mu   sync.Mutex
	used []int
	Got  []GotRequest
}

// NewWireServer starts a server replaying w.HTTP.
func NewWireServer(w WireTest) *WireServer {
	s := &WireServer{test: w, used: make([]int, len(w.HTTP))}
	seen := map[string]bool{}
	for _, h := range w.HTTP {
		if u, err := url.Parse(h.URL); err == nil && u.Host != "" {
			o := u.Scheme + "://" + u.Host
			if !seen[o] {
				seen[o] = true
				s.origins = append(s.origins, o)
			}
		}
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

func (s *WireServer) rewrite(str string) string {
	for _, o := range s.origins {
		str = strings.ReplaceAll(str, o, s.URL)
	}
	return str
}

func (s *WireServer) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	idx := -1
	path := r.URL.Path
	if s.GoAdds != "" && strings.HasPrefix(path, s.GoAdds+"/") {
		trimmed := strings.TrimPrefix(path, s.GoAdds)
		for _, h := range s.test.HTTP {
			if h.Method == r.Method && h.Path() == trimmed {
				path = trimmed
				break
			}
		}
	}
	for i, h := range s.test.HTTP {
		if h.Method == r.Method && h.Path() == path && s.used[i] == 0 {
			idx = i
			break
		}
	}
	if idx < 0 { // repeated request (polling): reuse the last match
		for i, h := range s.test.HTTP {
			if h.Method == r.Method && h.Path() == path {
				idx = i
			}
		}
	}
	if idx >= 0 {
		s.used[idx]++
	}
	s.Got = append(s.Got, GotRequest{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Header: r.Header.Clone(), Body: body, Matched: idx, ServerURL: s.URL})
	s.mu.Unlock()
	if idx < 0 || s.test.HTTP[idx].Response == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"wire replay: no recorded response for `+r.Method+` `+r.URL.Path+`"}`)
		return
	}
	resp := s.test.HTTP[idx].Response
	setH := func(def map[string]string) {
		for k, v := range def {
			w.Header().Set(k, v)
		}
		for k, v := range resp.Headers {
			w.Header().Set(k, s.rewrite(v))
		}
	}
	switch resp.Type {
	case "json-value":
		setH(map[string]string{"Content-Type": "application/json"})
		b := []byte(resp.rawBody)
		if len(b) == 0 {
			b, _ = json.Marshal(resp.Body)
		}
		_, _ = io.WriteString(w, s.rewrite(string(b)))
	case "stream-chunks":
		setH(map[string]string{"Content-Type": "text/event-stream", "Cache-Control": "no-cache", "Connection": "keep-alive"})
		for _, c := range resp.Chunks {
			_, _ = io.WriteString(w, s.rewrite(c))
		}
	case "binary":
		setH(map[string]string{"Content-Type": "application/octet-stream"})
		b, _ := base64.StdEncoding.DecodeString(resp.BodyB64)
		_, _ = w.Write(b)
	case "error":
		setH(nil)
		st := resp.Status
		if st == 0 {
			st = 500
		}
		w.WriteHeader(st)
		if b, ok := resp.Body.(string); ok {
			_, _ = io.WriteString(w, s.rewrite(b))
		}
	case "empty":
		setH(nil)
		st := resp.Status
		if st == 0 {
			st = 200
		}
		w.WriteHeader(st)
	default:
		w.WriteHeader(http.StatusInternalServerError)
	}
}

// ignoredHeaders are not compared (transport noise or client identity).
var ignoredHeaders = map[string]bool{
	"user-agent": true, "content-length": true, "accept-encoding": true, "host": true,
	"connection": true, "accept-language": true, "sec-fetch-mode": true, "content-type": true,
	"transfer-encoding": true, "x-stainless-os": true,
}

// DiffRequests compares what Go sent with what TS sent and returns
// human-readable differences (empty when equal).
func (s *WireServer) DiffRequests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	seen := map[int]bool{}
	for _, g := range s.Got {
		if g.Matched < 0 {
			out = append(out, fmt.Sprintf("Go sent unexpected request %s %s", g.Method, g.Path))
			continue
		}
		if seen[g.Matched] {
			continue
		}
		seen[g.Matched] = true
		out = append(out, diffRequest(s.test.HTTP[g.Matched], g)...)
	}
	for i, h := range s.test.HTTP {
		if !seen[i] && !isSeenPollRepeat(s.test.HTTP, seen, i) {
			out = append(out, fmt.Sprintf("TS sent %s %s but Go did not", h.Method, h.Path()))
		}
	}
	return out
}

// isSeenPollRepeat reports whether recorded request i repeats an earlier
// recorded request that Go did send: same method, URL, headers and body, and
// the same served response. How many identical polls fit in a poll timeout
// depends on the machine's load, so Go sending fewer of them is not a
// difference; every distinct request (including a poll that got a different
// response) must still be sent.
func isSeenPollRepeat(recorded []WireHTTP, seen map[int]bool, i int) bool {
	h := recorded[i]
	for j := 0; j < i; j++ {
		p := recorded[j]
		if seen[j] && p.Method == h.Method && p.URL == h.URL && p.BodyB64 == h.BodyB64 &&
			reflect.DeepEqual(p.Headers, h.Headers) && reflect.DeepEqual(p.Response, h.Response) {
			return true
		}
	}
	return false
}

func diffRequest(ts WireHTTP, g GotRequest) []string {
	var out []string
	label := ts.Method + " " + ts.Path()
	if u, err := url.Parse(ts.URL); err == nil {
		if d := diffQuery(u.Query(), g.Query); d != "" {
			out = append(out, label+": query "+d)
		}
	}
	for k, v := range ts.Headers {
		lk := strings.ToLower(k)
		if ignoredHeaders[lk] {
			continue
		}
		gv := g.Header.Get(k)
		if gv == "" {
			out = append(out, fmt.Sprintf("%s: header %s missing in Go (TS %q)", label, lk, trunc(v)))
		} else if gv != v {
			out = append(out, fmt.Sprintf("%s: header %s TS=%q Go=%q", label, lk, trunc(v), trunc(gv)))
		}
	}
	tsCT := ts.Headers["content-type"]
	body := ts.Body()
	// Responses are rewritten to point at the replay server; map URLs the Go
	// provider echoed back to the recorded origin.
	if g.ServerURL != "" {
		if u, err := url.Parse(ts.URL); err == nil && u.Host != "" {
			g.Body = bytes.ReplaceAll(g.Body, []byte(g.ServerURL), []byte(u.Scheme+"://"+u.Host))
		}
	}
	switch {
	case len(body) == 0 && len(g.Body) == 0:
	case strings.HasPrefix(tsCT, "multipart/"):
		out = append(out, diffMultipart(label, tsCT, body, g.Header.Get("Content-Type"), g.Body)...)
	case strings.Contains(tsCT, "json") || (tsCT == "" && len(body) > 0 && (body[0] == '{' || body[0] == '[')):
		var a, b interface{}
		ea := json.Unmarshal(body, &a)
		eb := json.Unmarshal(g.Body, &b)
		switch {
		case ea != nil:
			out = append(out, label+": TS body is not JSON")
		case eb != nil:
			out = append(out, fmt.Sprintf("%s: Go body is not JSON (%s)", label, trunc(string(g.Body))))
		default:
			if d := Diff("", WNorm(a), WNorm(b)); d != "" {
				out = append(out, label+": body (- TS, + Go)\n"+d)
			}
		}
	default:
		if !bytes.Equal(body, g.Body) {
			out = append(out, fmt.Sprintf("%s: body TS=%q Go=%q", label, trunc(string(body)), trunc(string(g.Body))))
		}
	}
	if ct := g.Header.Get("Content-Type"); tsCT != "" {
		a, _, _ := mime.ParseMediaType(tsCT)
		b, _, _ := mime.ParseMediaType(ct)
		if a != b {
			out = append(out, fmt.Sprintf("%s: content-type TS=%q Go=%q", label, a, b))
		}
	}
	return out
}

func diffQuery(a, b url.Values) string {
	if len(a) == 0 && len(b) == 0 {
		return ""
	}
	var parts []string
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	var ks []string
	for k := range keys {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		if strings.Join(a[k], ",") != strings.Join(b[k], ",") {
			parts = append(parts, fmt.Sprintf("%s TS=%q Go=%q", k, a[k], b[k]))
		}
	}
	return strings.Join(parts, "; ")
}

type mpPart struct {
	name, filename, ctype string
	data                  []byte
}

func readMultipart(ct string, body []byte) ([]mpPart, error) {
	_, params, err := mime.ParseMediaType(ct)
	if err != nil {
		return nil, err
	}
	r := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	var parts []mpPart
	for {
		p, err := r.NextPart()
		if err == io.EOF {
			return parts, nil
		}
		if err != nil {
			return parts, err
		}
		b, _ := io.ReadAll(p)
		parts = append(parts, mpPart{p.FormName(), p.FileName(), p.Header.Get("Content-Type"), normalizeRandomAssetRefs(p.FormName(), b)})
	}
}

var randomAssetRef = regexp.MustCompile(`^[A-Za-z0-9]{16}$`)

// normalizeRandomAssetRefs replaces the random 16-character image references
// the TypeSafe decision request generates (TS and Go draw different ones) with
// a stable name derived from the asset's part, everywhere the request uses it.
func normalizeRandomAssetRefs(name string, data []byte) []byte {
	if name != "request" {
		return data
	}
	var req struct {
		Assets map[string]struct {
			Part string `json:"part"`
		} `json:"assets"`
	}
	if json.Unmarshal(data, &req) != nil || len(req.Assets) == 0 {
		return data
	}
	var doc map[string]interface{}
	if json.Unmarshal(data, &doc) != nil {
		return data
	}
	repl := map[string]string{}
	for ref, a := range req.Assets {
		if randomAssetRef.MatchString(ref) {
			repl[ref] = "<ref:" + a.Part + ">"
		}
	}
	if len(repl) == 0 {
		return data
	}
	var walk func(v interface{}) interface{}
	walk = func(v interface{}) interface{} {
		switch x := v.(type) {
		case string:
			for k, r := range repl {
				x = strings.ReplaceAll(x, k, r)
			}
			return x
		case []interface{}:
			for i := range x {
				x[i] = walk(x[i])
			}
			return x
		case map[string]interface{}:
			out := make(map[string]interface{}, len(x))
			for k, e := range x {
				if r, ok := repl[k]; ok {
					k = r
				}
				out[k] = walk(e)
			}
			return out
		}
		return v
	}
	out, err := json.Marshal(walk(doc))
	if err != nil {
		return data
	}
	return out
}

func diffMultipart(label, tsCT string, tsBody []byte, goCT string, goBody []byte) []string {
	ta, err := readMultipart(tsCT, tsBody)
	if err != nil {
		return []string{label + ": TS multipart unreadable: " + err.Error()}
	}
	ga, err := readMultipart(goCT, goBody)
	if err != nil {
		return []string{label + ": Go multipart unreadable: " + err.Error()}
	}
	var out []string
	key := func(p mpPart) string { return p.name }
	used := make([]bool, len(ga))
	for _, t := range ta {
		found := false
		for i, g := range ga {
			if used[i] || key(g) != key(t) {
				continue
			}
			used[i] = true
			found = true
			if !bytes.Equal(t.data, g.data) && !sameJSON(t.data, g.data) {
				out = append(out, fmt.Sprintf("%s: multipart %q value TS=%q Go=%q", label, t.name, trunc(string(t.data)), trunc(string(g.data))))
			}
			if t.filename != g.filename {
				out = append(out, fmt.Sprintf("%s: multipart %q filename TS=%q Go=%q", label, t.name, t.filename, g.filename))
			}
			if t.filename != "" && t.ctype != g.ctype {
				out = append(out, fmt.Sprintf("%s: multipart %q content-type TS=%q Go=%q", label, t.name, t.ctype, g.ctype))
			}
			break
		}
		if !found {
			out = append(out, fmt.Sprintf("%s: multipart field %q missing in Go (TS %q)", label, t.name, trunc(string(t.data))))
		}
	}
	for i, g := range ga {
		if !used[i] {
			out = append(out, fmt.Sprintf("%s: multipart field %q only in Go (%q)", label, g.name, trunc(string(g.data))))
		}
	}
	return out
}

// WNorm normalizes a JSON value for wire comparison: nulls, empty objects and
// empty arrays are treated as absent.
func WNorm(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		o := map[string]interface{}{}
		for k, e := range x {
			if n := WNorm(e); n != nil {
				o[k] = n
			}
		}
		if len(o) == 0 {
			return nil
		}
		return o
	case []interface{}:
		if len(x) == 0 {
			return nil
		}
		o := make([]interface{}, len(x))
		for i, e := range x {
			o[i] = WNorm(e)
		}
		return o
	}
	return v
}

// wnormLoose is WNorm that also treats empty strings and false as absent
// (TS leaves optional result fields undefined where Go has zero values).
func wnormLoose(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		o := map[string]interface{}{}
		for k, e := range x {
			if n := wnormLoose(e); n != nil {
				o[k] = n
			}
		}
		if len(o) == 0 {
			return nil
		}
		return o
	case []interface{}:
		if len(x) == 0 {
			return nil
		}
		o := make([]interface{}, len(x))
		for i, e := range x {
			o[i] = wnormLoose(e)
		}
		return o
	case string:
		if x == "" {
			return nil
		}
	}
	return v
}

// DiffValues diffs two result values after loose wire normalization.
func DiffValues(want, got interface{}) string {
	return Diff("", wnormLoose(WireUnwrap(want)), wnormLoose(got))
}

// sameJSON reports whether both values are JSON texts that decode equally
// (key order is not significant to providers).
func sameJSON(a, b []byte) bool {
	var x, y interface{}
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return Diff("", x, y) == ""
}

// NormalizeOrigins replaces the replay server URL and every recorded origin in
// strings of v with a fixed token, so URLs echoed from responses compare equal.
func (s *WireServer) NormalizeOrigins(v interface{}) interface{} {
	repl := func(str string) string {
		str = strings.ReplaceAll(str, s.URL, "ORIGIN")
		for _, o := range s.origins {
			str = strings.ReplaceAll(str, o, "ORIGIN")
		}
		return str
	}
	var walk func(interface{}) interface{}
	walk = func(x interface{}) interface{} {
		switch t := x.(type) {
		case string:
			return repl(t)
		case map[string]interface{}:
			o := make(map[string]interface{}, len(t))
			for k, e := range t {
				o[k] = walk(e)
			}
			return o
		case []interface{}:
			o := make([]interface{}, len(t))
			for i, e := range t {
				o[i] = walk(e)
			}
			return o
		}
		return x
	}
	return walk(v)
}

// GuardTransport fails every request to a non-loopback host, so a Go provider
// that ignores its configured base URL cannot reach the real service.
type GuardTransport struct{ Next http.RoundTripper }

func (g GuardTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	host := r.URL.Hostname()
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return nil, fmt.Errorf("tsfixture: blocked request to non-loopback host %s", r.URL.Host)
	}
	return g.Next.RoundTrip(r)
}
