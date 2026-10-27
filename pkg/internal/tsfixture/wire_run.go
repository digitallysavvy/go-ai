package tsfixture

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// RunWire replays every test of one recording through the Go provider the
// suite builds. See wire.go.
//
// TSWIRE_REPORT=1 prints all differences instead of failing on them,
// TSWIRE_ERRORS=1 also compares error wording.
func RunWire(t *testing.T, file string, s WireSuite) {
	t.Helper()
	// A provider that ignores its base URL must not reach a real service.
	prev := http.DefaultTransport
	http.DefaultTransport = GuardTransport{Next: prev}
	defer func() { http.DefaultTransport = prev }()
	tests := LoadWire(t, file)
	learnWirePrefixes(tests)
	pkg := strings.TrimSuffix(filepath.Base(file), ".jsonl.gz")
	for _, w := range tests {
		w := w
		t.Run(strings.ReplaceAll(w.Test, "/", "_"), func(t *testing.T) {
			runWireTest(t, pkg, s, w)
		})
	}
}

type WireSuite struct {
	// provider builds the Go provider for a TS model; base is the replay
	// server URL plus the TS base-URL path prefix.
	Provider func(base string, m WireModel) (provider.Provider, error)
	// language builds a language model for a TS class name.
	Language func(p provider.Provider, cls, id string) (provider.LanguageModel, error)
	// LanguageFor, when set, builds the language model from the whole
	// recorded TS model (class, id and plain settings such as modelFamily).
	LanguageFor func(p provider.Provider, m WireModel) (provider.LanguageModel, error)
	Skip        map[string]string
	// rootBase passes the bare server URL as the base URL for Go providers
	// whose default base is the host root (the paths carry the API version).
	RootBase bool
	// goPrefix replaces the TS base-URL path prefix for Go providers whose
	// default base URL differs from the TS one.
	GoPrefix func(tsPrefix string) string
	// GoAdds is a path prefix the Go provider adds itself (see
	// WireServer.GoAdds).
	GoAdds string
	// LanguageResults also compares what language models return (content,
	// usage, finish reason, warnings, provider metadata); otherwise only the
	// requests they send are compared.
	LanguageResults bool
	// BatchPrefix is the Go base-URL path prefix for batch objects, whose
	// recordings carry no URL config (for example "/v1").
	BatchPrefix string
	// SkipLanguage skips language-model calls (covered by another suite).
	SkipLanguage bool
	// ProviderTool maps a TS provider tool for language-model calls.
	ProviderTool func(id, name string, args map[string]interface{}) (types.Tool, bool)
	// RawSchemas passes the responseFormat schema of language-model calls as
	// raw JSON (keeping the TS key order) instead of a Go map.
	RawSchemas bool
	// SkipStream is a reason to skip tests that stream (doStream calls) when
	// the recording carries the response as text chunks (binary frames
	// recorded as text); a response recorded as a binary body is replayed.
	SkipStream string
	// SkipModel returns a reason to skip a test that calls a model this suite
	// does not replay (for example a model another suite covers).
	SkipModel func(m WireModel) string
}

// wireClsPrefix remembers, per TS model class, the base-URL prefix of tests
// whose config.url used the path placeholder; tests with a fixed endpoint
// URL reuse it.
var wireClsPrefix = map[string]string{}

func learnWirePrefixes(tests []WireTest) {
	for _, w := range tests {
		for _, c := range w.Calls {
			if u, ok := c.Model.Config["url"].(string); ok && strings.Contains(u, "/PATH") {
				wireClsPrefix[c.Model.Cls] = WirePrefix(c.Model)
			}
		}
	}
}

// WirePrefix returns the path prefix of the TS model's base URL.
func WirePrefix(m WireModel) string {
	var raw string
	if u, ok := m.Config["url"].(string); ok {
		raw = strings.SplitN(u, "/PATH", 2)[0]
		if !strings.Contains(u, "/PATH") { // fixed endpoint URL: drop the endpoint
			found := false
			for _, e := range []string{"/audio/transcriptions", "/audio/speech", "/audio/translations", "/images/generations", "/images/edits", "/embeddings", "/chat/completions", "/completions", "/responses", "/rerank", "/embed", "/chat"} {
				if i := strings.Index(raw, e); i > 0 {
					raw = raw[:i]
					found = true
					break
				}
			}
			if !found {
				if p, ok := wireClsPrefix[m.Cls]; ok {
					return p
				}
			}
		}
	} else if b, ok := m.Config["baseURL"].(string); ok {
		raw = b
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimRight(u.Path, "/")
}

func runWireTest(t *testing.T, pkg string, s WireSuite, w WireTest) {
	t.Helper()
	status, reason := "replayed", ""
	defer func() { reportWire(pkg, w, status, reason) }()
	skip := func(why string) {
		status, reason = "skipped", why
		t.Skip(why)
	}
	if why, ok := s.Skip[w.Test]; ok {
		skip(why)
	}
	for k, why := range s.Skip {
		switch {
		case strings.HasSuffix(k, "*") && strings.HasPrefix(w.Test, strings.TrimSuffix(k, "*")):
			skip(why)
		case strings.HasPrefix(k, "~") && strings.Contains(w.Test, k[1:]):
			skip(why)
		}
	}
	if len(w.Calls) == 0 || len(w.HTTP) == 0 {
		skip("no replayable call or HTTP exchange in the TS test")
	}
	for _, h := range w.HTTP {
		if h.Response == nil || h.Response.Type == "controlled-stream" {
			skip("the TS test serves a response the recording cannot carry (controlled stream)")
		}
	}
	wireVideoOps = map[string]json.RawMessage{}
	srv := NewWireServer(w)
	srv.GoAdds = s.GoAdds
	defer srv.Close()

	var diffs []string
	prov := map[string]provider.Provider{}
	for i, c := range w.Calls {
		kind := WireKind(c)
		if kind == "" {
			skip(fmt.Sprintf("unsupported TS call %s on %s", c.Method, c.Model.Cls))
		}
		if s.SkipStream != "" && c.Method == "doStream" && wireServesChunks(w) {
			skip(s.SkipStream)
		}
		if s.SkipModel != nil {
			if why := s.SkipModel(c.Model); why != "" {
				skip(why)
			}
		}
		if hasFn(c.Args) {
			skip("the TS call passes a function (for example a webhook factory)")
		}
		for _, h := range w.HTTP {
			c.Model.RequestURLs = append(c.Model.RequestURLs, h.URL)
		}
		key := c.Model.Cls + "/" + c.Model.ModelID
		p, ok := prov[key]
		if !ok {
			var err error
			base := srv.URL + WirePrefix(c.Model)
			if s.RootBase {
				base = srv.URL
			} else if s.GoPrefix != nil {
				base = srv.URL + s.GoPrefix(WirePrefix(c.Model))
			}
			if s.BatchPrefix != "" && strings.Contains(c.Model.Cls, "Batch") && WirePrefix(c.Model) == "" {
				base = srv.URL + s.BatchPrefix
			}
			p, err = s.Provider(base, c.Model)
			if err != nil || p == nil {
				skip(fmt.Sprintf("provider not constructible: %v", err))
			}
			prov[key] = p
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		got, err := RunWireCall(ctx, srv, s, p, kind, c)
		cancel()
		if err != nil && strings.Contains(err.Error(), "illegal base64 data") {
			skip("TS test data is not valid base64; TS passes the string through")
		}
		if err == errWireSkip {
			skip(fmt.Sprint(got["skip"]))
		}
		if os.Getenv("TSWIRE_DEBUG") != "" {
			b, _ := json.Marshal(got)
			t.Logf("call %d (%s) err=%v got=%s", i, c.Method, err, trim(string(b)))
		}
		if ds := CompareWireResult(srv, s, c, got, err); len(ds) > 0 {
			for _, d := range ds {
				diffs = append(diffs, fmt.Sprintf("call %d (%s): %s", i, c.Method, d))
			}
		}
	}
	diffs = append(diffs, srv.DiffRequests()...)
	if len(diffs) == 0 {
		return
	}
	status = "differs"
	sort.Strings(diffs)
	msg := strings.Join(diffs, "\n")
	if os.Getenv("TSWIRE_REPORT") != "" {
		t.Logf("DIFFS:\n%s", msg)
		return
	}
	t.Errorf("Go differs from TS:\n%s", msg)
}

var errWireSkip = errors.New("skip")

// wireServesChunks reports whether a recorded test serves a response as text
// chunks (the test server records them as strings, which cannot carry binary
// frames); responses recorded as binary bodies replay faithfully.
func wireServesChunks(w WireTest) bool {
	for _, h := range w.HTTP {
		if h.Response != nil && h.Response.Type == "stream-chunks" {
			return true
		}
	}
	return false
}

// WireKind classifies a recorded call.
func WireKind(c WireCall) string {
	cls := c.Model.Cls
	switch {
	case c.Method == "doEmbed":
		return "embed"
	case c.Method == "doRerank":
		return "rerank"
	case c.Method == "doDecide":
		return "decide"
	case c.Method == "doStartBatch" || c.Method == "doGetBatchStatus" || c.Method == "doGetBatchResults" || c.Method == "doCancelBatch":
		return "batch"
	case (c.Method == "doStart" || c.Method == "doStatus") && strings.Contains(cls, "Video"):
		return "video"
	case c.Method == "doGenerate" || c.Method == "doStream":
		switch {
		case strings.Contains(cls, "Image"):
			return "image"
		case strings.Contains(cls, "Speech") && !strings.Contains(cls, "Translation"):
			return "speech"
		case strings.Contains(cls, "Transcription") && c.Method == "doGenerate":
			return "transcription"
		case strings.Contains(cls, "Video"):
			return "video"
		case strings.Contains(cls, "Language") || strings.Contains(cls, "Chat") || strings.Contains(cls, "Responses"):
			return "language"
		}
	}
	return ""
}

func RunWireCall(ctx context.Context, srv *WireServer, s WireSuite, p provider.Provider, kind string, c WireCall) (map[string]interface{}, error) {
	arg := c.Arg()
	switch kind {
	case "embed":
		m, err := p.EmbeddingModel(c.Model.ModelID)
		if err != nil {
			return nil, err
		}
		return wireEmbed(ctx, m, arg)
	case "rerank":
		m, err := p.RerankingModel(c.Model.ModelID)
		if err != nil {
			return nil, err
		}
		return wireRerank(ctx, m, arg)
	case "image":
		m, err := p.ImageModel(c.Model.ModelID)
		if err != nil {
			return nil, err
		}
		return wireImage(ctx, m, arg)
	case "speech":
		m, err := p.SpeechModel(c.Model.ModelID)
		if err != nil {
			return nil, err
		}
		return wireSpeech(ctx, m, arg)
	case "transcription":
		m, err := p.TranscriptionModel(c.Model.ModelID)
		if err != nil {
			return nil, err
		}
		return wireTranscription(ctx, m, arg)
	case "batch":
		bp, ok := p.(provider.BatchProvider)
		if !ok {
			return map[string]interface{}{"skip": "provider has no ExperimentalBatch"}, errWireSkip
		}
		return wireBatch(ctx, s, bp.ExperimentalBatch(), c)
	case "decide":
		dp, ok := p.(provider.DecisionModelProvider)
		if !ok {
			return map[string]interface{}{"skip": "provider has no DecisionModel"}, errWireSkip
		}
		dm, err := dp.DecisionModel(c.Model.ModelID)
		if err != nil {
			return nil, err
		}
		return wireDecide(ctx, dm, arg)
	case "video":
		m, ok := p.(interface {
			VideoModel(string) (provider.VideoModelV3, error)
		})
		if !ok {
			return map[string]interface{}{"skip": "provider has no VideoModel"}, errWireSkip
		}
		vm, err := m.VideoModel(c.Model.ModelID)
		if err != nil {
			return nil, err
		}
		return wireVideo(ctx, srv, vm, c)
	case "language":
		if s.SkipLanguage {
			return map[string]interface{}{"skip": "language models are covered by another suite"}, errWireSkip
		}
		var m provider.LanguageModel
		var err error
		if s.LanguageFor != nil {
			m, err = s.LanguageFor(p, c.Model)
		} else if s.Language != nil {
			m, err = s.Language(p, c.Model.Cls, c.Model.ModelID)
		} else {
			m, err = p.LanguageModel(c.Model.ModelID)
		}
		if err != nil {
			return nil, err
		}
		return wireLanguage(ctx, s, m, c)
	}
	return map[string]interface{}{"skip": "kind " + kind}, errWireSkip
}

// CompareWireResult diffs a Go result against the recorded TS result.
func CompareWireResult(srv *WireServer, s WireSuite, c WireCall, got map[string]interface{}, err error) []string {
	if c.Error != nil {
		if err == nil {
			return []string{fmt.Sprintf("TS threw %v but Go returned a result", wireErrSummary(c.Error))}
		}
		return wireErrorDiff(c.Error, err)
	}
	if err != nil {
		return []string{"Go returned an error, TS did not: " + err.Error()}
	}
	if c.Method == "doGetBatchResults" {
		return nil
	}
	if WireKind(c) == "language" && ((!s.LanguageResults && os.Getenv("TSWIRE_LMRESULTS") == "") || c.Method == "doStream") {
		return nil
	}
	want, _ := c.Result.(map[string]interface{})
	want = wirePruneWant(want)
	g, _ := jsonRound(got).(map[string]interface{})
	// Operations are opaque references only the same implementation reads.
	delete(want, "operation")
	// Go reports response headers the TS result may leave out (a superset).
	if gr, ok := g["response"].(map[string]interface{}); ok {
		if wr, _ := want["response"].(map[string]interface{}); wr["headers"] == nil {
			delete(gr, "headers")
		}
	}
	// Go keeps the raw response body where TS exposes none (a superset).
	if gr, ok := g["response"].(map[string]interface{}); ok {
		if wr, _ := want["response"].(map[string]interface{}); wr["body"] == nil {
			delete(gr, "body")
		}
	}
	// Go always knows the model id; TS only reports it when the provider does.
	if gr, ok := g["response"].(map[string]interface{}); ok {
		if wr, _ := want["response"].(map[string]interface{}); wr["modelId"] == nil {
			delete(gr, "modelId")
		}
	}
	if WireKind(c) == "language" {
		// TS exposes the zod-parsed response body, Go the typed one.
		for _, m := range []map[string]interface{}{want, g} {
			if r, ok := m["response"].(map[string]interface{}); ok {
				delete(r, "body")
			}
		}
	}
	wireAlignInvalidBase64(want, g)
	wantN, _ := srv.NormalizeOrigins(wireNormalizeParts(want)).(map[string]interface{})
	gotN, _ := srv.NormalizeOrigins(wireNormalizeParts(g)).(map[string]interface{})
	if d := DiffValues(wantN, gotN); d != "" {
		return []string{"result (- TS, + Go):\n" + d}
	}
	return nil
}

// wireNormalizeParts applies the conventions the case replay already uses to
// language-model content: provider tools are named by id in Go (ToolNameAlias),
// generated source ids differ, and an explicit isError false is unset.
func wireNormalizeParts(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		o := make(map[string]interface{}, len(x))
		for k, e := range x {
			switch {
			case k == "id" && x["type"] == "source":
				continue
			case k == "isError" && e == false:
				continue
			case k == "data" && x["type"] == "reasoning-file":
				if ds, isStr := e.(string); isStr { // Go renders the bytes; TS wraps them as {type:"data"}
					e = map[string]interface{}{"type": "data", "data": ds}
				}
			case k == "outputTokens" && e == float64(0):
				continue // an absent count is zero (image usage)
			case k == "thoughtSignature" && x["type"] == "tool-call":
				continue // Go-only field; TS carries it in providerMetadata
			case k == "toolCallId":
				if id, _ := e.(string); generatedWireID.MatchString(id) {
					e = "<generated>"
				}
			case k == "toolName":
				if name, _ := e.(string); ToolNameAlias[name] != "" {
					e = ToolNameAlias[name]
				}
			}
			o[k] = wireNormalizeParts(e)
		}
		return o
	case []interface{}:
		o := make([]interface{}, 0, len(x))
		for _, e := range x {
			// the Go rendering of content leaves out empty text parts
			if m, ok := e.(map[string]interface{}); ok && m["type"] == "text" && (m["text"] == nil || m["text"] == "") && len(m) <= 2 {
				continue
			}
			o = append(o, wireNormalizeParts(e))
		}
		return o
	}
	return v
}

// wireAlignInvalidBase64 makes the Go value equal the TS value where the TS
// test used a string that is not valid base64 as mock file data: TS passes
// such a string through, Go decodes what it can.
func wireAlignInvalidBase64(want, got interface{}) {
	switch w := want.(type) {
	case map[string]interface{}:
		g, _ := got.(map[string]interface{})
		for k, wv := range w {
			if gv, ok := g[k]; ok {
				if ws, isStr := wv.(string); isStr {
					if gs, ok := gv.(string); ok && gs != ws {
						if _, err := base64.StdEncoding.DecodeString(ws); err != nil && (k == "data" || k == "images" || k == "audio") {
							g[k] = ws
						}
					}
					continue
				}
				wireAlignInvalidBase64(wv, gv)
			}
		}
	case []interface{}:
		g, _ := got.([]interface{})
		for i, wv := range w {
			if i >= len(g) {
				break
			}
			if ws, isStr := wv.(string); isStr {
				if gs, ok := g[i].(string); ok && gs != ws {
					if _, err := base64.StdEncoding.DecodeString(ws); err != nil {
						g[i] = ws
					}
				}
				continue
			}
			wireAlignInvalidBase64(wv, g[i])
		}
	}
}

// generatedWireID matches ids the TS tests mock ("test-id", "id-1") and ids
// Go generates (call_...).
var generatedWireID = regexp.MustCompile(`^(test-id|id-\d+|call_[0-9a-f]{8,}|code-exec-\d+)$`)

func wireErrSummary(e interface{}) string {
	m, _ := e.(map[string]interface{})
	em, _ := m["$error"].(map[string]interface{})
	return fmt.Sprintf("%v: %v", em["name"], em["message"])
}

// wireErrorDiff compares error messages: Go wraps errors in different ways,
// so the TS message must appear in the Go error text.
func wireErrorDiff(tsErr interface{}, goErr error) []string {
	m, _ := tsErr.(map[string]interface{})
	em, _ := m["$error"].(map[string]interface{})
	msg, _ := em["message"].(string)
	if msg == "" || strings.Contains(goErr.Error(), msg) {
		return nil
	}
	// Response-schema failures are worded by the TS validator; Go reports
	// its own decode error for the same condition.
	if msg == "Invalid JSON response" || strings.HasPrefix(msg, "Type validation failed") || strings.HasPrefix(msg, "Invalid JSON") {
		return nil
	}
	if os.Getenv("TSWIRE_ERRORS") == "" {
		return nil // error wording differs across providers; see TSWIRE_ERRORS
	}
	return []string{fmt.Sprintf("error text TS=%q Go=%q", trim(msg), trim(goErr.Error()))}
}

func trim(s string) string {
	if len(s) > 220 {
		return s[:220] + "..."
	}
	return s
}

// wirePruneWant removes TS result parts that are not comparable.
func wirePruneWant(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	o := map[string]interface{}{}
	for k, v := range m {
		o[k] = v
	}
	if rq, ok := o["request"].(map[string]interface{}); ok {
		if bs, ok := rq["body"].(string); ok {
			var parsed interface{}
			if json.Unmarshal([]byte(bs), &parsed) == nil {
				o["request"] = map[string]interface{}{"body": parsed}
			}
		}
	}
	if r, ok := o["response"].(map[string]interface{}); ok {
		r2 := map[string]interface{}{}
		for k, v := range r {
			if k == "timestamp" {
				continue
			}
			r2[k] = v
		}
		if h, ok := r2["headers"].(map[string]interface{}); ok {
			h2 := map[string]interface{}{}
			for k, v := range h {
				switch k {
				case "content-length", "connection", "date", "transfer-encoding":
				default:
					h2[k] = v
				}
			}
			r2["headers"] = h2
		}
		o["response"] = r2
	}
	return o
}

// wirePruneGot drops Go response header keys TS did not record.
func wirePruneHeaders(h map[string]string) map[string]interface{} {
	o := map[string]interface{}{}
	for k, v := range h {
		lk := strings.ToLower(k)
		switch lk {
		case "content-length", "date", "connection", "transfer-encoding":
			continue
		}
		o[lk] = v
	}
	return o
}

func jsonRound(v interface{}) interface{} {
	b, _ := json.Marshal(v)
	var r interface{}
	_ = json.Unmarshal(b, &r)
	return r
}

func WireWarnings(ws []types.Warning) interface{} {
	var out []interface{}
	for _, w := range ws {
		m := map[string]interface{}{"type": w.Type}
		if w.Feature != "" && w.Type != "deprecated" {
			m["feature"] = w.Feature
		}
		if w.Setting != "" {
			m["setting"] = w.Setting
		}
		switch w.Type {
		case "other", "deprecated":
			msg := w.Message
			if msg == "" {
				msg = w.Details
			}
			m["message"] = msg
		default:
			// Message is a legacy Go field; the TS warning shape has details.
			if d := w.Details; d != "" {
				m["details"] = d
			}
		}
		out = append(out, m)
	}
	return out
}

func strSlice(v interface{}) []string {
	var out []string
	for _, e := range v.([]interface{}) {
		s, _ := e.(string)
		out = append(out, s)
	}
	return out
}

func strMap(v interface{}) map[string]string {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	out := map[string]string{}
	for k, e := range m {
		if s, ok := e.(string); ok {
			out[k] = s
		}
	}
	return out
}

func objMap(v interface{}) map[string]interface{} {
	m, _ := v.(map[string]interface{})
	if m == nil {
		return nil
	}
	return WireUnwrap(m).(map[string]interface{})
}

func intPtr(v interface{}) *int {
	if f, ok := v.(float64); ok {
		n := int(f)
		return &n
	}
	return nil
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// wireData returns bytes for a TS binary value (Uint8Array marker or base64).
func wireData(v interface{}) []byte {
	if b, ok := WireBytes(v); ok {
		return b
	}
	if s, ok := v.(string); ok {
		if b, err := base64.StdEncoding.DecodeString(s); err == nil {
			return b
		}
		return []byte(s)
	}
	return nil
}

func wireEmbed(ctx context.Context, m provider.EmbeddingModel, arg map[string]interface{}) (map[string]interface{}, error) {
	opts := &provider.EmbedModelOptions{
		ProviderOptions: objMap(arg["providerOptions"]),
		Headers:         strMap(arg["headers"]),
		Dimensions:      intPtr(arg["dimensions"]),
	}
	res, err := m.DoEmbedMany(ctx, strSlice(arg["values"]), opts)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{
		"embeddings":       res.Embeddings,
		"warnings":         WireWarnings(res.Warnings),
		"providerMetadata": res.ProviderMetadata,
	}
	if res.Usage.Tokens != 0 {
		out["usage"] = map[string]interface{}{"tokens": res.Usage.Tokens}
	}
	if len(res.Responses) > 0 {
		out["response"] = map[string]interface{}{"headers": wirePruneHeaders(res.Responses[0].Headers), "body": res.Responses[0].Body}
	}
	return out, nil
}

func wireRerank(ctx context.Context, m provider.RerankingModel, arg map[string]interface{}) (map[string]interface{}, error) {
	opts := &provider.RerankOptions{
		Query:           fmt.Sprint(arg["query"]),
		TopN:            intPtr(arg["topN"]),
		Headers:         strMap(arg["headers"]),
		ProviderOptions: objMap(arg["providerOptions"]),
	}
	if d, ok := arg["documents"].(map[string]interface{}); ok {
		vals, _ := d["values"].([]interface{})
		if d["type"] == "object" {
			var docs []map[string]interface{}
			for _, v := range vals {
				mm, _ := v.(map[string]interface{})
				docs = append(docs, mm)
			}
			opts.Documents = docs
		} else {
			opts.Documents = strSlice(vals)
		}
	}
	res, err := m.DoRerank(ctx, opts)
	if err != nil {
		return nil, err
	}
	var ranking []interface{}
	for _, r := range res.Ranking {
		ranking = append(ranking, map[string]interface{}{"index": r.Index, "relevanceScore": r.RelevanceScore})
	}
	hdr := map[string]string{}
	for k, v := range res.Response.Headers {
		hdr[k] = strings.Join(v, ", ")
	}
	resp := map[string]interface{}{"id": res.Response.ID, "modelId": res.Response.ModelID, "headers": wirePruneHeaders(hdr), "body": res.Response.Body}
	return map[string]interface{}{
		"ranking":          ranking,
		"warnings":         WireWarnings(res.Warnings),
		"providerMetadata": res.ProviderMetadata,
		"response":         resp,
	}, nil
}

func wireImageFile(v interface{}) *provider.ImageFile {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	m = WireUnwrap(m).(map[string]interface{})
	f := &provider.ImageFile{MediaType: fmt.Sprint(orEmpty(m["mediaType"])), ProviderOptions: objMap(m["providerOptions"])}
	if m["type"] == "url" {
		f.Type = "url"
		f.URL, _ = m["url"].(string)
		return f
	}
	f.Type = "file"
	f.Data = wireData(m["data"])
	return f
}

func orEmpty(v interface{}) interface{} {
	if v == nil {
		return ""
	}
	return v
}

// mockBase64 reports whether a TS binary payload is a string that is not valid
// base64 (mock data the TS provider passes through but Go decodes).
func mockBase64(v interface{}) bool {
	str, ok := v.(string)
	if !ok {
		return false
	}
	_, err := base64.StdEncoding.DecodeString(str)
	return err != nil
}

func filesHaveMockBase64(arg map[string]interface{}) bool {
	if fs, ok := arg["files"].([]interface{}); ok {
		for _, f := range fs {
			if fm, ok := f.(map[string]interface{}); ok && mockBase64(fm["data"]) {
				return true
			}
		}
	}
	for _, k := range []string{"mask", "image"} {
		if fm, ok := arg[k].(map[string]interface{}); ok && mockBase64(fm["data"]) {
			return true
		}
	}
	return false
}

func wireImage(ctx context.Context, m provider.ImageModel, arg map[string]interface{}) (map[string]interface{}, error) {
	if filesHaveMockBase64(arg) {
		return map[string]interface{}{"skip": "file data string is not valid base64 (mock data)"}, errWireSkip
	}
	opts := &provider.ImageGenerateOptions{
		N:               intPtr(arg["n"]),
		Seed:            intPtr(arg["seed"]),
		ProviderOptions: objMap(arg["providerOptions"]),
		Headers:         strMap(arg["headers"]),
	}
	opts.Size, _ = arg["size"].(string)
	opts.AspectRatio, _ = arg["aspectRatio"].(string)
	switch p := arg["prompt"].(type) {
	case string:
		opts.Prompt = p
	case map[string]interface{}:
		opts.Prompt, _ = p["text"].(string)
	}
	if fs, ok := arg["files"].([]interface{}); ok {
		for _, f := range fs {
			if x := wireImageFile(f); x != nil {
				opts.Files = append(opts.Files, *x)
			}
		}
	}
	opts.Mask = wireImageFile(arg["mask"])
	res, err := m.DoGenerate(ctx, opts)
	if err != nil {
		return nil, err
	}
	var images []interface{}
	switch {
	case len(res.Base64Images) > 0:
		for _, s := range res.Base64Images {
			images = append(images, s)
		}
	case len(res.Images) > 0:
		for _, b := range res.Images {
			images = append(images, b64(b))
		}
	case len(res.Image) > 0:
		images = append(images, b64(res.Image))
	}
	pm := res.ProviderMetadata
	out := map[string]interface{}{
		"images":   images,
		"warnings": WireWarnings(res.Warnings),
	}
	if res.IsRetryable != nil {
		out["isRetryable"] = *res.IsRetryable
		// v0.5.2 mirrors isRetryable under providerMetadata.gateway.
		if jm, ok := jsonRound(pm).(map[string]interface{}); ok {
			if gm, ok := jm["gateway"].(map[string]interface{}); ok {
				delete(gm, "isRetryable")
			}
			pm = jm
		}
	}
	out["providerMetadata"] = pm
	// Go ints cannot tell "absent" from 0, so zeros are left out.
	usage := map[string]interface{}{}
	for k, v := range map[string]int{"inputTokens": res.Usage.InputTokens, "outputTokens": res.Usage.OutputTokens, "totalTokens": res.Usage.TotalTokens} {
		if v != 0 {
			usage[k] = v
		}
	}
	out["usage"] = usage
	if r := res.Response; r != nil {
		out["response"] = map[string]interface{}{"modelId": r.ModelID, "headers": wirePruneHeaders(r.Headers)}
	}
	return out, nil
}

func wireSpeech(ctx context.Context, m provider.SpeechModel, arg map[string]interface{}) (map[string]interface{}, error) {
	opts := &provider.SpeechGenerateOptions{ProviderOptions: objMap(arg["providerOptions"]), Headers: strMap(arg["headers"])}
	opts.Text, _ = arg["text"].(string)
	opts.Voice, _ = arg["voice"].(string)
	opts.OutputFormat, _ = arg["outputFormat"].(string)
	opts.Instructions, _ = arg["instructions"].(string)
	opts.Language, _ = arg["language"].(string)
	if f, ok := arg["speed"].(float64); ok {
		opts.Speed = &f
	}
	res, err := m.DoGenerate(ctx, opts)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{
		"audio":            b64(res.Audio),
		"warnings":         WireWarnings(res.Warnings),
		"providerMetadata": res.ProviderMetadata,
	}
	if res.Request != nil {
		body := jsonRound(res.Request.Body)
		if bs, ok := body.(string); ok {
			var parsed interface{}
			if json.Unmarshal([]byte(bs), &parsed) == nil {
				body = parsed
			}
		}
		out["request"] = map[string]interface{}{"body": body}
	}
	if r := res.Response; r != nil {
		// TS speech responses carry no body unless the API answers with JSON;
		// binary audio bodies are not compared.
		resp := map[string]interface{}{"modelId": r.ModelID, "headers": wirePruneHeaders(r.Headers)}
		if bm, ok := jsonRound(r.Body).(map[string]interface{}); ok {
			resp["body"] = bm
		}
		out["response"] = resp
	}
	if len(res.Usage) > 0 {
		out["usage"] = res.Usage
	}
	return out, nil
}

func wireTranscription(ctx context.Context, m provider.TranscriptionModel, arg map[string]interface{}) (map[string]interface{}, error) {
	opts := &provider.TranscriptionOptions{ProviderOptions: objMap(arg["providerOptions"]), Headers: strMap(arg["headers"])}
	opts.MimeType, _ = arg["mediaType"].(string)
	if s, ok := arg["audio"].(string); ok {
		opts.AudioBase64 = s
		if b, err := base64.StdEncoding.DecodeString(s); err == nil {
			opts.Audio = b
		}
	} else {
		opts.Audio = wireData(arg["audio"])
	}
	res, err := m.DoTranscribe(ctx, opts)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{
		"text":             res.Text,
		"language":         res.Language,
		"warnings":         WireWarnings(res.Warnings),
		"providerMetadata": res.ProviderMetadata,
	}
	if res.DurationInSeconds != nil {
		out["durationInSeconds"] = *res.DurationInSeconds
	}
	var segs []interface{}
	for _, s := range res.Segments {
		segs = append(segs, map[string]interface{}{"text": s.Text, "startSecond": s.Start, "endSecond": s.End})
	}
	out["segments"] = segs
	if len(res.ProviderUsage) > 0 {
		out["usage"] = res.ProviderUsage
	}
	if r := res.Response; r != nil {
		out["response"] = map[string]interface{}{"modelId": r.ModelID, "headers": wirePruneHeaders(r.Headers), "body": r.Body}
	}
	return out, nil
}

// wireLanguage replays a language model call. Only the request the model
// sends is compared for streams; generate results are compared too.
func wireLanguage(ctx context.Context, s WireSuite, m provider.LanguageModel, c WireCall) (map[string]interface{}, error) {
	prev := ProviderToolHook
	ProviderToolHook = s.ProviderTool
	opts, why := BuildOptions(c.Arg())
	ProviderToolHook = prev
	if opts == nil {
		return map[string]interface{}{"skip": "unsupported call options: " + why}, errWireSkip
	}
	if s.RawSchemas && opts.ResponseFormat != nil && opts.ResponseFormat.Schema != nil {
		if raw := c.RawResponseFormatSchema(); raw != nil {
			opts.ResponseFormat.Schema = raw
		}
	}
	if c.Method == "doStream" {
		st, err := m.DoStream(ctx, opts)
		if err != nil {
			return nil, err
		}
		// TS doStream throws when the first frames carry an error before any
		// output; Go surfaces that from the stream before the first output.
		defer st.Close() //nolint:errcheck
		output := false
		for {
			ch, err := st.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				if !output {
					return nil, err
				}
				break
			}
			switch ch.Type {
			case provider.ChunkTypeStreamStart, provider.ChunkTypeResponseMetadata, provider.ChunkTypeRaw:
			case provider.ChunkTypeError:
				// TS emits stream errors as parts; only a returned error
				// (the pre-output API error) corresponds to doStream throwing.
			default:
				output = true
			}
		}
		return map[string]interface{}{}, nil
	}
	res, err := m.DoGenerate(ctx, opts)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{
		"content":          GenerateContentToTS(res),
		"usage":            UsageToTS(res.Usage),
		"finishReason":     map[string]interface{}{"raw": nilIfEmpty(res.RawFinishReason), "unified": string(res.FinishReason)},
		"warnings":         WireWarnings(res.Warnings),
		"providerMetadata": res.ProviderMetadata,
	}
	if res.RawRequest != nil {
		out["request"] = map[string]interface{}{"body": res.RawRequest}
	}
	resp := map[string]interface{}{}
	if r := res.ResponseMetadata; r != nil {
		resp["id"], resp["modelId"] = r.ID, r.ModelID
		resp["headers"] = wirePruneHeaders(r.Headers)
	}
	if res.RawResponse != nil {
		resp["body"] = res.RawResponse
	}
	out["response"] = resp
	return out, nil
}

func wireVideoFile(v interface{}) *provider.VideoModelV3File {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	m = WireUnwrap(m).(map[string]interface{})
	f := &provider.VideoModelV3File{}
	f.MediaType, _ = m["mediaType"].(string)
	f.ProviderOptions = objMap(m["providerOptions"])
	if m["type"] == "url" {
		f.Type = "url"
		f.URL, _ = m["url"].(string)
		return f
	}
	f.Type = "file"
	f.Data = wireData(m["data"])
	return f
}

func wireVideoData(vs []provider.VideoModelV3VideoData) []interface{} {
	var out []interface{}
	for _, v := range vs {
		m := map[string]interface{}{"type": v.Type, "mediaType": v.MediaType}
		switch v.Type {
		case "url":
			m["url"] = v.URL
		case "base64":
			m["data"] = v.Data
		case "binary":
			m["data"] = b64(v.Binary)
		}
		out = append(out, m)
	}
	return out
}

func wireVideoCallOptions(arg map[string]interface{}) provider.VideoModelV3CallOptions {
	o := provider.VideoModelV3CallOptions{
		ProviderOptions: objMap(arg["providerOptions"]),
		Headers:         strMap(arg["headers"]),
		N:               1,
	}
	if p, ok := arg["prompt"].(string); ok {
		o.Prompt, o.PromptSet = p, true
	}
	if n := intPtr(arg["n"]); n != nil {
		o.N = *n
	}
	o.AspectRatio, _ = arg["aspectRatio"].(string)
	o.Resolution, _ = arg["resolution"].(string)
	if d, ok := arg["duration"].(float64); ok {
		o.Duration = &d
	}
	o.FPS = intPtr(arg["fps"])
	o.Seed = intPtr(arg["seed"])
	o.Image = wireVideoFile(arg["image"])
	if b, ok := arg["generateAudio"].(bool); ok {
		o.GenerateAudio = &b
	}
	if fs, ok := arg["frameImages"].([]interface{}); ok {
		for _, f := range fs {
			fm, _ := f.(map[string]interface{})
			if img := wireVideoFile(fm["image"]); img != nil {
				ft, _ := fm["frameType"].(string)
				o.FrameImages = append(o.FrameImages, provider.VideoFrameImage{Image: *img, FrameType: ft})
			}
		}
	}
	if rs, ok := arg["inputReferences"].([]interface{}); ok {
		for _, r := range rs {
			if f := wireVideoFile(r); f != nil {
				o.InputReferences = append(o.InputReferences, *f)
			}
		}
	}
	return o
}

// wireVideoOps remembers the Go operation returned by DoStart so a following
// DoStatus in the same test can use it (TS operations have a different shape).
var wireVideoOps = map[string]json.RawMessage{}

func wireVideoResponse(r provider.VideoModelV3ResponseInfo) map[string]interface{} {
	return map[string]interface{}{"modelId": r.ModelID, "headers": wirePruneHeaders(r.Headers)}
}

func wireVideo(ctx context.Context, srv *WireServer, m provider.VideoModelV3, c WireCall) (map[string]interface{}, error) {
	arg := c.Arg()
	if filesHaveMockBase64(arg) {
		return map[string]interface{}{"skip": "file data string is not valid base64 (mock data)"}, errWireSkip
	}
	switch c.Method {
	case "doGenerate":
		opts := wireVideoCallOptions(arg)
		res, err := m.DoGenerate(ctx, &opts)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"videos":           wireVideoData(res.Videos),
			"warnings":         WireWarnings(res.Warnings),
			"providerMetadata": res.ProviderMetadata,
			"response":         wireVideoResponse(res.Response),
		}, nil
	case "doStart":
		st, ok := m.(provider.VideoModelStarter)
		if !ok {
			return map[string]interface{}{"skip": "model has no DoStart"}, errWireSkip
		}
		so := provider.VideoModelV3StartOptions{VideoModelV3CallOptions: wireVideoCallOptions(arg)}
		so.WebhookURL, _ = arg["webhookUrl"].(string)
		res, err := st.DoStart(ctx, &so)
		if err != nil {
			return nil, err
		}
		wireVideoOps[m.ModelID()] = res.Operation
		return map[string]interface{}{
			"warnings":         WireWarnings(res.Warnings),
			"providerMetadata": res.ProviderMetadata,
			"response":         wireVideoResponse(res.Response),
		}, nil
	case "doStatus":
		sc, ok := m.(provider.VideoModelStatusChecker)
		if !ok {
			return map[string]interface{}{"skip": "model has no DoStatus"}, errWireSkip
		}
		opJSON, _ := json.Marshal(WireUnwrap(arg["operation"]))
		// URLs inside the TS operation point at the recorded origin.
		op := json.RawMessage(srv.rewrite(string(opJSON)))
		if prev, ok := wireVideoOps[m.ModelID()]; ok && len(prev) > 0 {
			op = prev
		}
		res, err := sc.DoStatus(ctx, &provider.VideoModelV3StatusOptions{Operation: op, Headers: strMap(arg["headers"])})
		if err != nil {
			return nil, err
		}
		out := map[string]interface{}{
			"status":           res.Status,
			"warnings":         WireWarnings(res.Warnings),
			"providerMetadata": res.ProviderMetadata,
			"response":         wireVideoResponse(res.Response),
		}
		if res.Status == provider.VideoOperationStatusCompleted {
			out["videos"] = wireVideoData(res.Videos)
		}
		if res.Error != "" {
			out["error"] = res.Error
		}
		return out, nil
	}
	return map[string]interface{}{"skip": "video method " + c.Method}, errWireSkip
}

func hasFn(v interface{}) bool {
	switch x := v.(type) {
	case map[string]interface{}:
		if _, ok := x["$fn"]; ok {
			return true
		}
		for _, e := range x {
			if hasFn(e) {
				return true
			}
		}
	case []interface{}:
		for _, e := range x {
			if hasFn(e) {
				return true
			}
		}
	}
	return false
}

func wireDecide(ctx context.Context, m provider.DecisionModel, arg map[string]interface{}) (map[string]interface{}, error) {
	opts := provider.DecisionCallOptions{
		Headers:         strMap(arg["headers"]),
		ProviderOptions: objMap(arg["providerOptions"]),
		Questions:       map[string]provider.DecisionQuestion{},
	}
	if v, ok := arg["maxOutputTokens"].(float64); ok {
		n := int(v)
		opts.MaxOutputTokens = &n
	}
	if v, ok := arg["reasoning"].(string); ok {
		r := types.ReasoningLevel(v)
		opts.Reasoning = &r
	}
	switch st := arg["state"].(type) {
	case []interface{}:
		var parts []provider.DecisionStatePart
		for _, pv := range st {
			pm, _ := pv.(map[string]interface{})
			switch pm["type"] {
			case "text":
				txt, _ := pm["text"].(string)
				parts = append(parts, provider.DecisionTextPart(txt))
			case "json":
				parts = append(parts, provider.DecisionJSONPart(pm["value"]))
			case "file":
				fd, ok := fileDataFromTS(pm["data"])
				if !ok {
					return map[string]interface{}{"skip": "decision file part data"}, errWireSkip
				}
				mt, _ := pm["mediaType"].(string)
				fd.MediaType = mt
				fname, _ := pm["filename"].(string)
				fc := types.FileContent{FileData: fd, MediaType: mt, Filename: fname}
				fc.ProviderOptions = objMap(pm["providerOptions"])
				parts = append(parts, provider.DecisionFilePart(fc))
			}
		}
		opts.State = parts
	default:
		opts.State = st
	}
	if qs, ok := arg["questions"].(map[string]interface{}); ok {
		for id, qv := range qs {
			qm, _ := qv.(map[string]interface{})
			q := provider.DecisionQuestion{Instructions: qm["instructions"], Criteria: qm["criteria"]}
			q.Type, _ = qm["type"].(string)
			opts.Questions[id] = q
		}
	}
	res, err := m.DoDecide(ctx, opts)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{
		"answers":          res.Answers,
		"warnings":         WireWarnings(res.Warnings),
		"providerMetadata": res.ProviderMetadata,
		"rounding":         res.Rounding,
		"usage":            res.Usage,
	}
	if r := res.Response; r != nil {
		out["response"] = map[string]interface{}{"id": r.ID, "modelId": r.ModelID, "headers": wirePruneHeaders(r.Headers), "body": r.Body}
	}
	return out, nil
}

// reportWire appends one JSON line per recorded test to $TSWIRE_SUMMARY, for
// the coverage table in the review reports.
func reportWire(pkg string, w WireTest, status, reason string) {
	path := os.Getenv("TSWIRE_SUMMARY")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close() //nolint:errcheck
	b, _ := json.Marshal(map[string]string{"pkg": pkg, "file": w.File, "test": w.Test, "status": status, "reason": reason})
	_, _ = f.Write(append(b, '\n'))
}

func wireBatchStatus(st provider.BatchV4Status) map[string]interface{} {
	out := map[string]interface{}{"status": string(st.Status), "rawStatus": st.RawStatus, "providerMetadata": st.ProviderMetadata}
	if c := st.RequestCounts; c != nil {
		out["requestCounts"] = map[string]interface{}{"total": c.Total, "pending": c.Pending, "completed": c.Completed, "failed": c.Failed}
	}
	if e := st.Error; e != nil {
		out["error"] = e
	}
	out["createdAt"], out["expiresAt"] = st.CreatedAt, st.ExpiresAt
	return out
}

// wireBatch replays batch calls. Results are a stream in TS, so only the
// requests are compared for doGetBatchResults.
func wireBatch(ctx context.Context, s WireSuite, b provider.BatchV4, c WireCall) (map[string]interface{}, error) {
	arg := c.Arg()
	headers, po := strMap(arg["headers"]), objMap(arg["providerOptions"])
	switch c.Method {
	case "doStartBatch":
		opts := provider.BatchV4StartOptions{Headers: headers, ProviderOptions: po}
		opts.WebhookURL, _ = arg["webhookUrl"].(string)
		reqs, _ := arg["requests"].([]interface{})
		for _, rv := range reqs {
			rm, _ := rv.(map[string]interface{})
			id, _ := rm["id"].(string)
			model, _ := rm["modelId"].(string)
			optsMap, _ := rm["options"].(map[string]interface{})
			switch rm["type"] {
			case "text":
				prev := ProviderToolHook
				ProviderToolHook = s.ProviderTool
				g, why := BuildOptions(optsMap)
				ProviderToolHook = prev
				if g == nil {
					return map[string]interface{}{"skip": "unsupported batch text options: " + why}, errWireSkip
				}
				opts.Requests = append(opts.Requests, provider.BatchV4Request{Type: provider.BatchRequestTypeText, Text: &provider.TextBatchV4Request{ID: id, ModelID: model, Options: *g}})
			case "image":
				img := provider.ImageGenerateOptions{ProviderOptions: objMap(optsMap["providerOptions"]), N: intPtr(optsMap["n"]), Seed: intPtr(optsMap["seed"])}
				img.Prompt, _ = optsMap["prompt"].(string)
				img.Size, _ = optsMap["size"].(string)
				img.AspectRatio, _ = optsMap["aspectRatio"].(string)
				if fs, ok := optsMap["files"].([]interface{}); ok {
					for _, f := range fs {
						if x := wireImageFile(f); x != nil {
							img.Files = append(img.Files, *x)
						}
					}
				}
				img.Mask = wireImageFile(optsMap["mask"])
				opts.Requests = append(opts.Requests, provider.BatchV4Request{Type: provider.BatchRequestTypeImage, Image: &provider.ImageBatchV4Request{ID: id, ModelID: model, Options: img}})
			default:
				opts.Requests = append(opts.Requests, provider.BatchV4Request{Type: provider.BatchRequestType(fmt.Sprint(rm["type"]))})
			}
		}
		res, err := b.DoStartBatch(ctx, opts)
		if err != nil {
			return nil, err
		}
		out := wireBatchStatus(res.BatchV4Status)
		out["batchId"] = res.BatchID
		var ws []interface{}
		for _, w := range res.Warnings {
			ws = append(ws, map[string]interface{}{"requestId": w.RequestID, "warning": WireWarnings([]types.Warning{w.Warning}).([]interface{})[0]})
		}
		out["warnings"] = ws
		return out, nil
	case "doGetBatchStatus":
		id, _ := arg["batchId"].(string)
		st, err := b.DoGetBatchStatus(ctx, provider.BatchV4OperationOptions{BatchID: id, Headers: headers, ProviderOptions: po})
		if err != nil {
			return nil, err
		}
		return wireBatchStatus(*st), nil
	case "doGetBatchResults":
		id, _ := arg["batchId"].(string)
		st, err := b.DoGetBatchResults(ctx, provider.BatchV4OperationOptions{BatchID: id, Headers: headers, ProviderOptions: po})
		if err != nil {
			return nil, err
		}
		defer st.Close() //nolint:errcheck
		for {
			if _, err := st.Next(); err != nil {
				break
			}
		}
		return map[string]interface{}{}, nil
	case "doCancelBatch":
		cb, ok := b.(provider.BatchV4Canceller)
		if !ok {
			return map[string]interface{}{"skip": "batch has no DoCancelBatch"}, errWireSkip
		}
		id, _ := arg["batchId"].(string)
		res, err := cb.DoCancelBatch(ctx, provider.BatchV4OperationOptions{BatchID: id, Headers: headers, ProviderOptions: po})
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"providerMetadata": res.ProviderMetadata}, nil
	}
	return map[string]interface{}{"skip": "batch method " + c.Method}, errWireSkip
}
