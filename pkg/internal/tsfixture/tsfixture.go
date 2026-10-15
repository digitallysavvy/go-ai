// Package tsfixture drives Go language models with recorded and inline
// wire payloads taken from the TypeScript AI SDK's tests and compares the
// result with the TS vitest snapshots (extracted to JSON). It exists only to
// support parity tests.
package tsfixture

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Case is one TS test that feeds a canned response through a model and
// snapshots the output.
type Case struct {
	Test        string                 `json:"test"`
	Response    *Response              `json:"response"`
	CallKind    string                 `json:"callKind"`
	ModelID     string                 `json:"modelId"`
	TopDescribe string                 `json:"topDescribe"`
	Suspect     bool                   `json:"suspect"`
	CallArgs    string                 `json:"callArgs"`
	Call        map[string]interface{} `json:"call"`
	Exps        []Expectation          `json:"exps"`
	Asserts     []Assertion            `json:"asserts"`
	// FnArgs are the evaluated arguments of the function a function-level test
	// calls (extractor --fn=NAME); FnArgSrc are their TS source.
	FnArgs   []interface{}     `json:"fnArgs"`
	FnArgSrc []string          `json:"fnArgSrc"`
	Aliases  map[string]string `json:"aliases"`
}

// Response is the canned wire response.
type Response struct {
	Kind   string          `json:"kind"` // json | chunks | fixture-json | fixture-chunks
	Body   json.RawMessage `json:"body"` // raw, to keep the key order of the wire body
	Chunks []string        `json:"chunks"`
	Name   string          `json:"name"`
	Raw    string          `json:"raw"` // fixture file content
	Error  string          `json:"error"`
	// Headers the TS test server sent with the response.
	Headers map[string]string `json:"headers"`
}

// Expectation is one snapshot assertion.
type Expectation struct {
	Expr string      `json:"expr"`
	Snap interface{} `json:"snap"`
}

// LoadCases reads the extracted cases JSON.
func LoadCases(t *testing.T, path string) []Case {
	t.Helper()
	b, err := readCaseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cs []Case
	if err := json.Unmarshal(b, &cs); err != nil {
		t.Fatal(err)
	}
	return cs
}

var simpleCall = regexp.MustCompile(`^\{ prompt: [A-Z_]+,?( includeRawChunks: false,?)? \}$`)

// Simple reports whether the TS call used only the default prompt.
func (c Case) Simple() bool { return simpleCall.MatchString(c.CallArgs) }

// StreamBodyEncoder, when set, turns the canned stream lines (one JSON
// document per line) into the response body and content type. Providers whose
// stream is not SSE (Amazon Bedrock's AWS event stream) set it.
var StreamBodyEncoder func(lines []string) (contentType string, body []byte)

func (c Case) handler() http.HandlerFunc {
	r := c.Response
	return func(w http.ResponseWriter, req *http.Request) {
		for k, v := range r.Headers {
			w.Header().Set(k, v)
		}
		if StreamBodyEncoder != nil && (r.Kind == "chunks" || r.Kind == "fixture-chunks") {
			var lines []string
			src := r.Chunks
			if r.Kind == "fixture-chunks" {
				src = strings.Split(r.Raw, "\n")
			}
			for _, l := range src {
				if l = strings.TrimSpace(l); l != "" {
					lines = append(lines, l)
				}
			}
			ct, body := StreamBodyEncoder(lines)
			w.Header().Set("Content-Type", ct)
			_, _ = w.Write(body)
			return
		}
		switch r.Kind {
		case "json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(r.Body)
		case "fixture-json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, r.Raw)
		case "fixture-chunks":
			w.Header().Set("Content-Type", "text/event-stream")
			for _, l := range strings.Split(r.Raw, "\n") {
				if strings.TrimSpace(l) == "" {
					continue
				}
				_, _ = fmt.Fprintf(w, "data: %s\n\n", l)
			}
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
		case "chunks":
			w.Header().Set("Content-Type", "text/event-stream")
			for _, ch := range r.Chunks {
				_, _ = io.WriteString(w, ch)
			}
		}
	}
}

// Run executes every simple case against the model built by mk and compares
// with the TS snapshot. Cases listed in skip (by test name) are logged only.
func Run(t *testing.T, cases []Case, mk func(baseURL, modelID string) provider.LanguageModel, skip map[string]string) {
	t.Helper()
	for _, c := range cases {
		c := c
		if c.Response == nil || c.Response.Error != "" || c.Call == nil || c.Suspect {
			continue
		}
		t.Run(c.Test, func(t *testing.T) {
			if why, ok := skip[c.Test]; ok {
				t.Skip(why)
			}
			var reqBody interface{}
			var reqs []request
			h := c.handler()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				rq := newRequest(b, r.Header, "http://"+r.Host+r.URL.RequestURI(), r.Method)
				reqs = append(reqs, rq)
				if len(reqs) == 1 {
					reqBody = rq.body
				}
				h(w, r)
			}))
			defer srv.Close()
			m := mk(srv.URL, c.ModelID)
			opts, why := BuildOptions(c.Call)
			if opts == nil {
				t.Skipf("unsupported call options: %s", why)
			}
			ctx := context.Background()
			var content interface{}
			var stream []interface{}
			var res *types.GenerateResult
			resultMap := map[string]interface{}{}
			if c.CallKind == "stream" {
				s, err := m.DoStream(ctx, opts)
				if err != nil {
					t.Fatalf("DoStream: %v", err)
				}
				chunks := drain(s)
				raw := StreamToTS(chunks)
				stream = CanonStream(raw)
				resultMap["stream"] = raw
				if rb, ok := s.(provider.StreamRequestBody); ok {
					resultMap["request"] = map[string]interface{}{"body": rb.RequestBody()}
				}
				for _, ch := range chunks {
					if ch.Type == provider.ChunkTypeResponseMetadata && ch.ResponseMetadata != nil && len(ch.ResponseMetadata.Headers) > 0 {
						resultMap["response"] = map[string]interface{}{"headers": lowerHeaders(ch.ResponseMetadata.Headers)}
						break
					}
				}
			} else {
				var err error
				res, err = m.DoGenerate(ctx, opts)
				if err != nil {
					t.Fatalf("DoGenerate: %v", err)
				}
				content = GenerateContentToTS(res)
				resultMap["content"] = content
				resultMap["usage"] = UsageToTS(res.Usage)
				resultMap["finishReason"] = map[string]interface{}{"raw": nilIfEmpty(res.RawFinishReason), "unified": string(res.FinishReason)}
				warnings := make([]interface{}, 0, len(res.Warnings))
				for _, w := range res.Warnings {
					v := jsonValue(w)
					if m, ok := v.(map[string]interface{}); ok && m["type"] != "other" {
						delete(m, "message") // Go also mirrors details there for old callers
					}
					warnings = append(warnings, v)
				}
				resultMap["warnings"] = warnings
				resultMap["providerMetadata"] = res.ProviderMetadata
				resultMap["request"] = map[string]interface{}{"body": res.RawRequest}
				resp := map[string]interface{}{"headers": lowerHeaders(res.ResponseHeaders)}
				if m := res.ResponseMetadata; m != nil {
					if m.ID != "" {
						resp["id"] = m.ID
					}
					if m.ModelID != "" {
						resp["modelId"] = m.ModelID
					}
					if !m.Timestamp.IsZero() {
						resp["timestamp"] = m.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z")
					}
				}
				resultMap["response"] = resp
			}
			roots := map[string]interface{}{
				"result": resultMap,
				"server": map[string]interface{}{"calls": reqs},
			}
			for _, e := range c.Exps {
				var got interface{}
				switch {
				case e.Expr == "result.content" && res != nil:
					got = content
				case (e.Expr == "await convertReadableStreamToArray(stream)" || e.Expr == "await convertReadableStreamToArray(result.stream)") && stream != nil:
					got = stream
				case strings.HasSuffix(e.Expr, ".at(-1)") && stream != nil:
					got = stream[len(stream)-1]
					if want, ok := e.Snap.(map[string]interface{}); ok {
						e.Snap = CanonStream([]interface{}{want})[0]
					}
				case e.Expr == "await server.calls[0].requestBodyJson":
					got = reqBody
				case e.Expr == "request" && res != nil:
					got = map[string]interface{}{"body": res.RawRequest}
				case (e.Expr == "result.usage" || e.Expr == "usage") && res != nil:
					got = UsageToTS(res.Usage)
				case (e.Expr == "result.finishReason" || e.Expr == "response.finishReason") && res != nil:
					got = map[string]interface{}{"raw": nilIfEmpty(res.RawFinishReason), "unified": string(res.FinishReason)}
				default:
					if e.Expr == "result" || e.Expr == "response" {
						continue // whole-result snapshots include wire details Go does not expose
					}
					v, ok := evalPath(roots, c.Aliases, e.Expr)
					if !ok {
						continue
					}
					got = jsonValue(v)
				}
				snap := e.Snap
				if m, ok := snap.(map[string]interface{}); ok && m["__err"] != nil {
					t.Logf("%s: TS snapshot not extractable (%v)", e.Expr, m["__err"])
					continue
				}
				if l, ok := snap.([]interface{}); ok && stream != nil && !strings.HasSuffix(e.Expr, ".at(-1)") {
					snap = CanonStream(l)
				}
				if d := Diff("", Normalize(snap), Normalize(got)); d != "" {
					t.Errorf("%s: mismatch vs TS snapshot (- TS, + Go):\n%s", e.Expr, d)
				}
			}
			runAsserts(t, c, roots)
		})
	}
}

func drain(s provider.TextStream) []*provider.StreamChunk {
	defer s.Close() //nolint:errcheck
	var out []*provider.StreamChunk
	for {
		c, err := s.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			out = append(out, &provider.StreamChunk{Type: provider.ChunkTypeError, Text: err.Error()})
			return out
		}
		out = append(out, c)
	}
}

func raw(m json.RawMessage) interface{} {
	if len(m) == 0 {
		return nil
	}
	var v interface{}
	if json.Unmarshal(m, &v) != nil {
		return nil
	}
	return v
}

func setPM(o map[string]interface{}, m json.RawMessage) {
	if v := raw(m); v != nil {
		o["providerMetadata"] = v
	}
}

func i64(p *int64) interface{} {
	if p == nil {
		return nil
	}
	return *p
}

// UsageToTS renders usage in the TS LanguageModelV4Usage shape.
func UsageToTS(u types.Usage) map[string]interface{} {
	in := map[string]interface{}{"total": i64(u.InputTokens)}
	if d := u.InputDetails; d != nil {
		in["noCache"], in["cacheRead"], in["cacheWrite"] = i64(d.NoCacheTokens), i64(d.CacheReadTokens), i64(d.CacheWriteTokens)
	}
	out := map[string]interface{}{"total": i64(u.OutputTokens)}
	if d := u.OutputDetails; d != nil {
		out["text"], out["reasoning"] = i64(d.TextTokens), i64(d.ReasoningTokens)
	}
	r := map[string]interface{}{"inputTokens": in, "outputTokens": out}
	if u.Raw != nil {
		r["raw"] = u.Raw
	}
	return r
}

// GenerateContentToTS renders GenerateResult content in TS content order.
func GenerateContentToTS(res *types.GenerateResult) interface{} {
	var parts []interface{}
	hasText := false
	for _, p := range res.Content {
		if _, ok := p.(types.TextContent); ok {
			hasText = true
		}
	}
	if !hasText && res.Text != "" {
		parts = append(parts, map[string]interface{}{"type": "text", "text": res.Text})
	}
	have := map[string]bool{}
	callPart := func(tc types.ToolCall) map[string]interface{} {
		in := tc.RawArguments
		if in == "" && tc.Arguments != nil {
			bb, _ := json.Marshal(tc.Arguments)
			in = string(bb)
		}
		part := map[string]interface{}{"type": "tool-call", "toolCallId": tc.ID, "toolName": tc.ToolName, "input": in}
		if tc.ProviderExecuted {
			part["providerExecuted"] = true
		}
		if len(tc.ProviderMetadata) > 0 {
			part["providerMetadata"] = tc.ProviderMetadata
		}
		return part
	}
	for _, p := range res.Content {
		if tc, ok := p.(types.ToolCallContent); ok {
			have[tc.ToolCallID] = true
		}
	}
	for _, p := range res.Content {
		b, _ := json.Marshal(p)
		var v interface{}
		_ = json.Unmarshal(b, &v)
		switch x := p.(type) {
		case types.ToolCallContent:
			// TS tool-call input is the raw JSON string.
			in := x.Input
			if in == "" && x.Arguments != nil {
				bb, _ := json.Marshal(x.Arguments)
				in = string(bb)
			}
			if m, ok := v.(map[string]interface{}); ok {
				m["input"] = in
			}
		case types.ReasoningContent:
			// TS carries the signature and redacted data in providerMetadata
			// only; Go also keeps its pre-v0.6 fields.
			if m, ok := v.(map[string]interface{}); ok {
				delete(m, "signature")
				delete(m, "redacted_data")
			}
		case types.ToolResultContent:
			// TS marks a failed result isError; Go carries the error text.
			if m, ok := v.(map[string]interface{}); ok {
				if x.Error != "" {
					delete(m, "error")
					m["isError"] = true
				}
				// TS tool results are always provider executed and say so
				// nowhere.
				delete(m, "providerExecuted")
			}
			// A provider-executed call that only exists in ToolCalls is
			// reported right before its result.
			if !have[x.ToolCallID] {
				for _, tc := range res.ToolCalls {
					if tc.ID == x.ToolCallID {
						parts = append(parts, callPart(tc))
						have[tc.ID] = true
					}
				}
			}
		}
		parts = append(parts, v)
	}
	for _, tc := range res.ToolCalls {
		if !have[tc.ID] {
			parts = append(parts, callPart(tc))
		}
	}
	return parts
}

// StreamToTS renders chunks as TS stream parts.
func StreamToTS(chunks []*provider.StreamChunk) []interface{} {
	var out []interface{}
	for _, c := range chunks {
		o := map[string]interface{}{"type": string(c.Type)}
		id := c.ID
		if id == "" && c.ToolCall != nil {
			id = c.ToolCall.ID
		}
		switch c.Type {
		case provider.ChunkTypeStreamStart:
			ws := []interface{}{}
			for _, w := range c.Warnings {
				b, _ := json.Marshal(w)
				var v interface{}
				_ = json.Unmarshal(b, &v)
				ws = append(ws, v)
			}
			o["warnings"] = ws
		case provider.ChunkTypeResponseMetadata:
			if m := c.ResponseMetadata; m != nil {
				if m.ID != "" {
					o["id"] = m.ID
				}
				if m.ModelID != "" && !DropResponseModelID {
					o["modelId"] = m.ModelID
				}
				if !m.Timestamp.IsZero() && !DropResponseTimestamp {
					o["timestamp"] = m.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z")
				}
			}
		case provider.ChunkTypeText:
			o["type"], o["id"], o["delta"] = "text-delta", id, c.Text
			setPM(o, c.ProviderMetadata)
		case provider.ChunkTypeReasoning:
			o["type"], o["id"], o["delta"] = "reasoning-delta", id, c.Reasoning
			if c.Reasoning == "" {
				o["delta"] = c.Text
			}
			setPM(o, c.ProviderMetadata)
		case provider.ChunkTypeTextStart, provider.ChunkTypeTextEnd, provider.ChunkTypeReasoningStart, provider.ChunkTypeReasoningEnd:
			o["id"] = id
			setPM(o, c.ProviderMetadata)
		case provider.ChunkTypeToolInputStart:
			o["id"] = id
			setPM(o, c.ProviderMetadata)
			if c.ToolCall != nil {
				o["toolName"] = c.ToolCall.ToolName
				if c.ToolCall.ProviderExecuted {
					o["providerExecuted"] = true
				}
				if c.ToolCall.Dynamic {
					o["dynamic"] = true
				}
			}
		case provider.ChunkTypeToolInputDelta:
			o["id"], o["delta"] = id, c.Text
			setPM(o, c.ProviderMetadata)
		case provider.ChunkTypeToolInputEnd:
			o["id"] = id
			setPM(o, c.ProviderMetadata)
		case provider.ChunkTypeToolCall:
			if tc := c.ToolCall; tc != nil {
				o["toolCallId"], o["toolName"] = tc.ID, tc.ToolName
				switch {
				case tc.RawArguments != "":
					o["input"] = tc.RawArguments
				case tc.Arguments != nil:
					b, _ := json.Marshal(tc.Arguments)
					o["input"] = string(b)
				default:
					o["input"] = ""
				}
				if tc.ProviderExecuted {
					o["providerExecuted"] = true
				}
				if tc.Dynamic {
					o["dynamic"] = true
				}
				if len(tc.ProviderMetadata) > 0 {
					o["providerMetadata"] = tc.ProviderMetadata
				} else {
					setPM(o, c.ProviderMetadata)
				}
			}
		case provider.ChunkTypeToolResult:
			if tr := c.ToolResult; tr != nil {
				o["toolCallId"], o["toolName"], o["result"] = tr.ToolCallID, tr.ToolName, tr.Result
				if len(tr.ProviderMetadata) > 0 {
					o["providerMetadata"] = tr.ProviderMetadata
				}
			}
		case provider.ChunkTypeToolApprovalRequest:
			if r := c.ToolApprovalRequest; r != nil {
				o["approvalId"], o["toolCallId"] = r.ApprovalID, r.ToolCallID
			}
		case provider.ChunkTypeSource:
			if s := c.SourceContent; s != nil {
				b, _ := json.Marshal(s)
				var v map[string]interface{}
				_ = json.Unmarshal(b, &v)
				v["type"] = "source"
				o = v
			}
		case provider.ChunkTypeFile:
			if f := c.GeneratedFileContent; f != nil {
				o["mediaType"] = f.MediaType
			}
		case provider.ChunkTypeFinish:
			o["finishReason"] = map[string]interface{}{"raw": nilIfEmpty(c.RawFinishReason), "unified": string(c.FinishReason)}
			if c.Usage != nil {
				o["usage"] = UsageToTS(*c.Usage)
			}
			setPM(o, c.ProviderMetadata)
		case provider.ChunkTypeCustom:
			if cc := c.CustomContent; cc != nil {
				o["kind"] = cc.Kind
				setPM(o, cc.ProviderMetadata)
			}
		case provider.ChunkTypeError:
			o["error"] = c.Text
		case provider.ChunkTypeRaw:
			o["rawValue"] = c.Raw
		}
		out = append(out, o)
	}
	return out
}

var generatedID = regexp.MustCompile(`^(id-\d+|test-id(-\d+)?|call_[0-9a-f]{16})$`)

// MetadataKeyAlias renames provider-metadata keys of the TS snapshot (e.g. the
// tests' "test-provider") to the key the Go provider under test reports.
var MetadataKeyAlias map[string]string

// ToolNameAlias renames tool names reported by the Go provider (for example
// the "anthropic.code_execution_20250825" id Go uses as a provider tool's
// name) to the name the TS test registered them under.
var ToolNameAlias map[string]string

// PromptToolNameAlias maps the tool names a TS prompt uses to the names the Go
// provider knows the tools by (the reverse of ToolNameAlias).
var PromptToolNameAlias map[string]string

// DropResponseTimestamp omits the response-metadata timestamp from stream
// parts, for providers where TS reports none.
var DropResponseTimestamp bool

// DropResponseModelID omits the response-metadata model id from stream parts,
// for providers where TS reports none there.
var DropResponseModelID bool

// Normalize drops TS "undefined" markers and nil/empty noise so the two sides
// compare structurally.
func Normalize(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		if _, ok := x["__undef"]; ok && len(x) == 1 {
			return nil
		}
		if x["type"] == "tool-call" && x["input"] == "" {
			x["input"] = "{}" // empty tool input and {} are the same call
		}
		o := map[string]interface{}{}
		for k, e := range x {
			// Go-only replay fields; TS has providerMetadata for the same data.
			if k == "providerOptions" || k == "encrypted_content" {
				continue
			}
			if k == "id" && x["type"] == "source" {
				continue // generated ids differ
			}
			if k == "toolCallId" || (k == "id" && strings.HasPrefix(fmt.Sprint(x["type"]), "tool-input")) {
				if id, _ := e.(string); generatedID.MatchString(id) {
					continue // generated ids differ
				}
			}
			if k == "isError" && e == false {
				continue // an explicit false is the same as unset
			}
			if k == "toolName" {
				if name, _ := e.(string); ToolNameAlias[name] != "" {
					e = ToolNameAlias[name]
				}
			}
			n := Normalize(e)
			if n == nil {
				continue
			}
			if k == "providerMetadata" {
				if pm, ok := n.(map[string]interface{}); ok && len(MetadataKeyAlias) > 0 {
					for from, to := range MetadataKeyAlias {
						if v, ok := pm[from]; ok {
							delete(pm, from)
							pm[to] = v
						}
					}
				}
			}
			o[k] = n
		}
		if len(o) == 0 {
			return nil
		}
		return o
	case []interface{}:
		o := make([]interface{}, len(x))
		for i, e := range x {
			o[i] = Normalize(e)
		}
		return o
	case float64:
		return x
	case int64:
		return float64(x)
	case int:
		return float64(x)
	}
	// round-trip other types through JSON
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map, reflect.Slice, reflect.Struct, reflect.Pointer:
		b, _ := json.Marshal(v)
		var r interface{}
		_ = json.Unmarshal(b, &r)
		return Normalize(r)
	}
	return v
}

// Diff returns a readable diff (empty when equal).
func Diff(path string, want, got interface{}) string {
	if reflect.DeepEqual(want, got) {
		return ""
	}
	wm, wok := want.(map[string]interface{})
	gm, gok := got.(map[string]interface{})
	if wok && gok {
		keys := map[string]bool{}
		for k := range wm {
			keys[k] = true
		}
		for k := range gm {
			keys[k] = true
		}
		var ks []string
		for k := range keys {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		var sb strings.Builder
		for _, k := range ks {
			sb.WriteString(Diff(path+"."+k, wm[k], gm[k]))
		}
		return sb.String()
	}
	wa, wok := want.([]interface{})
	ga, gok := got.([]interface{})
	if wok && gok {
		var sb strings.Builder
		n := len(wa)
		if len(ga) > n {
			n = len(ga)
		}
		if len(wa) != len(ga) {
			fmt.Fprintf(&sb, "  %s: length TS=%d Go=%d\n", path, len(wa), len(ga))
		}
		for i := 0; i < n; i++ {
			var w, g interface{}
			if i < len(wa) {
				w = wa[i]
			}
			if i < len(ga) {
				g = ga[i]
			}
			sb.WriteString(Diff(fmt.Sprintf("%s[%d]", path, i), w, g))
		}
		return sb.String()
	}
	ws, _ := json.Marshal(want)
	gs, _ := json.Marshal(got)
	return fmt.Sprintf("  %s:\n    - %s\n    + %s\n", path, trunc(string(ws)), trunc(string(gs)))
}

func trunc(s string) string {
	if len(s) > 300 {
		return s[:300] + "..."
	}
	return s
}

// CanonStream removes representation differences that are by design: Go
// providers may omit text-start/text-end and empty text deltas, adjacent text
// deltas are merged and block ids on text are ignored, and error payloads
// are reduced to their type.
func CanonStream(parts []interface{}) []interface{} {
	var out []interface{}
	for _, p := range parts {
		m, ok := p.(map[string]interface{})
		if !ok {
			out = append(out, p)
			continue
		}
		switch m["type"] {
		case "reasoning-start", "reasoning-end":
			// generated block ids differ (mockId vs "reasoning-0")
			if id, _ := m["id"].(string); strings.HasPrefix(id, "id-") || strings.HasPrefix(id, "reasoning-") || (id != "" && strings.Trim(id, "0123456789") == "") {
				delete(m, "id")
			}
		case "text-start", "text-end":
			continue
		case "stream-start":
			// Go emits it only when there are warnings.
			if w, _ := m["warnings"].([]interface{}); len(w) == 0 {
				continue
			}
		case "response-metadata":
			// Go providers add a headers-only metadata chunk (no wire id).
			if id, ok := m["id"]; !ok || Normalize(id) == nil {
				continue
			}
		case "text-delta":
			d, _ := m["delta"].(string)
			if d == "" {
				continue
			}
			if n := len(out); n > 0 {
				if prev, ok := out[n-1].(map[string]interface{}); ok && prev["type"] == "text-delta" {
					prev["delta"] = prev["delta"].(string) + d
					continue
				}
			}
			out = append(out, map[string]interface{}{"type": "text-delta", "delta": d})
			continue
		case "reasoning-delta":
			d, _ := m["delta"].(string)
			if n := len(out); n > 0 {
				if prev, ok := out[n-1].(map[string]interface{}); ok && prev["type"] == "reasoning-delta" {
					prev["delta"] = prev["delta"].(string) + d
					continue
				}
			}
			out = append(out, map[string]interface{}{"type": "reasoning-delta", "delta": d})
			continue
		case "tool-input-delta":
			if d, _ := m["delta"].(string); d == "" {
				continue
			}
		case "error":
			out = append(out, map[string]interface{}{"type": "error"})
			continue
		}
		out = append(out, m)
	}
	return out
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// ProviderToolHook converts a TS provider tool (type "provider") to the Go
// tool for the provider under test. It returns false when the args are not
// representable, which skips the case. When nil, only argument-less provider
// tools are supported.
var ProviderToolHook func(id, name string, args map[string]interface{}) (types.Tool, bool)

// BuildOptions converts the JSON form of a TS call-options literal to Go
// options. It returns nil and a reason when it uses an unsupported key.
func BuildOptions(call map[string]interface{}) (*provider.GenerateOptions, string) {
	o := &provider.GenerateOptions{AllowSystemMessages: true}
	f := func(v interface{}) *float64 {
		if x, ok := v.(float64); ok {
			return &x
		}
		return nil
	}
	// tools first: their hook may register names the prompt refers to
	keys := make([]string, 0, len(call))
	for k := range call {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if (keys[i] == "tools") != (keys[j] == "tools") {
			return keys[i] == "tools"
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		v := call[k]
		switch k {
		case "prompt":
			msgs, why := buildPrompt(v)
			if why != "" {
				return nil, why
			}
			o.Prompt = types.Prompt{Messages: msgs}
		case "maxOutputTokens":
			if x, ok := v.(float64); ok {
				n := int(x)
				o.MaxTokens = &n
			}
		case "temperature":
			o.Temperature = f(v)
		case "topP":
			o.TopP = f(v)
		case "topK":
			if x, ok := v.(float64); ok {
				n := int(x)
				o.TopK = &n
			}
		case "presencePenalty":
			o.PresencePenalty = f(v)
		case "frequencyPenalty":
			o.FrequencyPenalty = f(v)
		case "seed":
			if x, ok := v.(float64); ok {
				n := int(x)
				o.Seed = &n
			}
		case "stopSequences":
			for _, s := range v.([]interface{}) {
				o.StopSequences = append(o.StopSequences, s.(string))
			}
		case "includeRawChunks":
			o.IncludeRawChunks, _ = v.(bool)
		case "headers":
			o.Headers = map[string]string{}
			for hk, hv := range v.(map[string]interface{}) {
				o.Headers[hk], _ = hv.(string)
			}
		case "providerOptions":
			o.ProviderOptions, _ = v.(map[string]interface{})
		case "reasoning":
			r := types.ReasoningLevel(v.(string))
			o.Reasoning = &r
		case "responseFormat":
			m := v.(map[string]interface{})
			rf := &provider.ResponseFormat{}
			rf.Type, _ = m["type"].(string)
			rf.Schema = m["schema"]
			rf.Name, _ = m["name"].(string)
			rf.Description, _ = m["description"].(string)
			o.ResponseFormat = rf
		case "toolChoice":
			m := v.(map[string]interface{})
			t, _ := m["type"].(string)
			o.ToolChoice = types.ToolChoice{Type: types.ToolChoiceType(t)}
			o.ToolChoice.ToolName, _ = m["toolName"].(string)
		case "tools":
			for _, tv := range v.([]interface{}) {
				m := tv.(map[string]interface{})
				tool := types.Tool{}
				tool.Name, _ = m["name"].(string)
				tool.Description, _ = m["description"].(string)
				if m["type"] == "provider" {
					tool.Type = "provider"
					tool.ProviderID, _ = m["id"].(string)
					a, _ := m["args"].(map[string]interface{})
					if ProviderToolHook != nil {
						ht, ok := ProviderToolHook(tool.ProviderID, tool.Name, a)
						if !ok {
							return nil, "provider tool args (typed config in Go)"
						}
						tool = ht
					} else if len(a) > 0 {
						return nil, "provider tool args (typed config in Go)"
					}
					tool.ProviderExecuted = true
				} else {
					tool.Parameters = m["inputSchema"]
					if sv, ok := m["strict"].(bool); ok {
						tool.Strict = &sv
					}
					if po, ok := m["providerOptions"].(map[string]interface{}); ok {
						tool.ProviderOptions = po
					}
				}
				o.Tools = append(o.Tools, tool)
			}
		default:
			return nil, "option " + k
		}
	}
	return o, ""
}

func buildPrompt(v interface{}) ([]types.Message, string) {
	var out []types.Message
	for _, mv := range v.([]interface{}) {
		m := mv.(map[string]interface{})
		role, _ := m["role"].(string)
		msg := types.Message{Role: types.MessageRole(role)}
		if po, ok := m["providerOptions"].(map[string]interface{}); ok {
			msg.ProviderOptions = po
		}
		if s, ok := m["content"].(string); ok {
			msg.Content = []types.ContentPart{types.TextContent{Text: s}}
			out = append(out, msg)
			continue
		}
		for _, pv := range m["content"].([]interface{}) {
			p := pv.(map[string]interface{})
			po, _ := p["providerOptions"].(map[string]interface{})
			switch p["type"] {
			case "text":
				msg.Content = append(msg.Content, types.TextContent{Text: p["text"].(string), ProviderOptions: po})
			case "reasoning":
				msg.Content = append(msg.Content, types.ReasoningContent{Text: p["text"].(string), ProviderOptions: po})
			case "tool-call":
				in := p["input"]
				tc := types.ToolCallContent{ToolCallID: p["toolCallId"].(string), ToolName: promptToolName(p["toolName"].(string)), ProviderOptions: po}
				if str, ok := in.(string); ok {
					tc.Input = str
					_ = json.Unmarshal([]byte(str), &tc.Arguments)
				} else if mm, ok := in.(map[string]interface{}); ok {
					tc.Arguments = mm
					b, _ := json.Marshal(mm)
					tc.Input = string(b)
				}
				if pe, ok := p["providerExecuted"].(bool); ok {
					tc.ProviderExecuted = pe
				}
				msg.Content = append(msg.Content, tc)
			case "file":
				fc := types.FileContent{MediaType: stringOf(p["mediaType"]), Filename: stringOf(p["filename"]), ProviderOptions: po}
				d, _ := p["data"].(map[string]interface{})
				switch d["type"] {
				case "data":
					if str, ok := d["data"].(string); ok {
						fc.FileData = types.FileData{Type: types.FileDataTypeData, DataString: str, MediaType: fc.MediaType}
					} else {
						return nil, "prompt file data bytes"
					}
				case "url":
					// TS keeps the unparsed string of a gs: URL as originalUrl; Go never
					// re-parses the string it was given.
					u := stringOf(d["url"])
					if o := stringOf(d["originalUrl"]); o != "" {
						u = o
					}
					fc.FileData = types.FileData{Type: types.FileDataTypeURL, URL: u, MediaType: fc.MediaType}
				case "reference":
					ref := types.ProviderReference{}
					if r, ok := d["reference"].(map[string]interface{}); ok {
						for k, v := range r {
							ref[k] = stringOf(v)
						}
					}
					fc.FileData = types.FileData{Type: types.FileDataTypeReference, Reference: ref, MediaType: fc.MediaType}
				default:
					return nil, fmt.Sprintf("prompt file data %v", d["type"])
				}
				msg.Content = append(msg.Content, fc)
			case "tool-result":
				tr := types.ToolResultContent{ToolCallID: p["toolCallId"].(string), ToolName: promptToolName(p["toolName"].(string)), ProviderOptions: po}
				if out, ok := p["output"].(map[string]interface{}); ok {
					b, _ := json.Marshal(out)
					var o types.ToolResultOutput
					if err := json.Unmarshal(b, &o); err != nil {
						return nil, "tool-result output " + fmt.Sprint(out["type"])
					}
					tr.Output = &o
				}
				msg.Content = append(msg.Content, tr)
			default:
				return nil, fmt.Sprintf("prompt part %v", p["type"])
			}
		}
		out = append(out, msg)
	}
	return out, ""
}

// readCaseFile reads an extracted cases file. Files ending in .gz (the
// committed form, see README.md) are gunzipped.
func readCaseFile(path string) ([]byte, error) {
	if !strings.HasSuffix(path, ".gz") {
		return os.ReadFile(path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(zr)
	if cerr := zr.Close(); err == nil {
		err = cerr
	}
	return b, err
}

func stringOf(v interface{}) string {
	s, _ := v.(string)
	return s
}

func lowerHeaders(h map[string]string) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range h {
		lk := strings.ToLower(k)
		if lk == "date" || lk == "content-length" {
			continue // added by the Go test server / transport, not by the TS mock
		}
		out[lk] = v
	}
	return out
}

func promptToolName(name string) string {
	if n, ok := PromptToolNameAlias[name]; ok {
		return n
	}
	return name
}

// BuildPromptMessages converts the JSON form of a TS prompt to Go messages. It
// returns a reason when the prompt uses a part the harness cannot build.
func BuildPromptMessages(v interface{}) ([]types.Message, string) {
	return buildPrompt(v)
}
