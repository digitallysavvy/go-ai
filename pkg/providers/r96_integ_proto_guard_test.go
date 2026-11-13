package providers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// r96integPlainJSONDecodes are the functions in r95integGuardPatterns that
// decode network bytes (a response body, an SSE / event-stream frame or a
// WebSocket message) with encoding/json, which does not reject a __proto__
// or constructor.prototype key, with the number of such calls in each and
// the reason the plain decode is right there. Every other network decode
// goes through jsonexact.Unmarshal / Decode, securejson or the central
// decoders (internalhttp DoJSON*, UnmarshalChunk), which fail such a payload
// with TS's JSONParseError wherever TS runs secureJsonParse / safeParseJSON
// (round 96, known difference "JSON payloads with __proto__" retired).
var r96integPlainJSONDecodes = map[string]struct {
	count  int
	reason string
}{
	"anthropic/language_model.go|*anthropicStream.nextEvent":          {1, "error text of a frame json.Valid rejected (no object was parsed)"},
	"gateway/language_model.go|decodeGatewayLenient":                  {1, "key-by-key fallback after jsonexact.Unmarshal, which already returned the forbidden prototype error"},
	"gemini/streaming.go|*stream.Next":                                {1, "error text of a frame json.Valid rejected (no object was parsed)"},
	"gmicloud/provider.go|gmicloudIncludeUsageTransport.roundTrip":    {1, "SDK-built request body (RoundTripper), not a response"},
	"google/interactions_loose.go|looseDecodeValue":                   {2, "loose fallback after securejson.Check on the frame (interactions_stream.go)"},
	"google/interactions_stream.go|*interactionsStream.Next":          {1, "error text of a frame json.Valid rejected (no object was parsed)"},
	"googlevertex/maas_provider.go|applyMaasMaxTokensDefault":         {1, "SDK-built request body (RoundTripper), not a response"},
	"mistral/conversation_model.go|conversationRequiredFieldsPresent": {1, "runs after the frame's jsonexact.Unmarshal succeeded (handle)"},
	"openresponses/language_model.go|*openResponsesStream.Next":       {1, "runs after securejson.Check on the frame"},
	"perplexity/agent_api.go|validatePerplexityAgentResponse":         {1, "runs after securejson.Check on the body"},
	"pkg/mcp/jsonrpc.go|ValidateJSONRPCMessage":                       {1, "parseMCPMessage runs unmarshalSafeJSON (prototype check) first"},
	"pkg/mcp/mcp_event_webhook.go|*mcpEventWebhook.handle":            {2, "runs after unmarshalSafeJSON (prototype check) on the webhook body"},
	"pkg/mcp/safe_json.go|checkServerJSON":                            {1, "the depth and field limits, plus the prototype check when TS secure-parses (unmarshalSafeJSON, parseMCPMessage); without it for TS response.json() (parseMCPHTTPJSONMessages, parseMCPMessagePlain, unmarshalPlainJSON), then jsonexact.Unmarshal"},
}

// r97integResponseJSONDecodes are the calls of the plain-JSON entry points
// (jsonexact.UnmarshalPlain, providerutils.UnmarshalChunkPlain,
// providerutils.MediaSchemaParserPlain, zodlite.ValidationMessagePlain),
// which keep a __proto__ / constructor.prototype key the way TS plain
// JSON.parse does, with the TS site that reads the value without
// secureJsonParse (round 97). Anywhere TS runs secureJsonParse /
// safeParseJSON, the checking decoders stay.
var r97integResponseJSONDecodes = map[string]struct {
	count  int
	reason string
}{
	"assemblyai/transcription_model.go|*TranscriptionModel.pollTranscript":  {2, "assemblyai-transcription-model.ts:318 await response.json(), then schema.parse"},
	"assemblyai/transcription_model.go|*TranscriptionModel.convertResponse": {1, "the in-memory transcript of the response.json() poll above, re-read"},
	"cartesia/realtime_model.go|*RealtimeModel.DoCreateClientSecret":        {1, "cartesia-realtime-model.ts:110 await response.json()"},
	"fal/transcription_model.go|*TranscriptionModel.pollForResult":          {1, "fal-transcription-model.ts:167 clone.json() (still-in-progress detail)"},
	"fal/video_model.go|*VideoModel.DoStatus":                               {1, "fal-video-model.ts:289 response.clone().json() (still-in-progress detail)"},
	"google/files_api.go|*FilesAPI.UploadFile":                              {1, "google-files.ts:126 await uploadResponse.json()"},
	"google/files_api.go|googleFilesUploadResultShape":                      {1, "google-files.ts:126 await uploadResponse.json() (uploadResult.file.state)"},
	"google/realtime_model.go|*GoogleRealtimeModel.DoCreateClientSecret":    {1, "google realtime client secret: await response.json()"},
	"openai/realtime_model.go|*OpenAIRealtimeModel.DoCreateClientSecret":    {1, "openai-realtime-model.ts:89 await response.json()"},
	"xai/realtime_model.go|*XAIRealtimeModel.DoCreateClientSecret":          {1, "xai-realtime-model.ts:76 await response.json()"},
	"youcom/tools.go|decodeYouJSON":                                         {1, "@youdotcom-oss/ai-sdk-plugin await response.json()"},
	"quiverai/language_model.go|decodeQuiverAIProviderMetadata":             {1, "TS reads part.providerMetadata.quiverai from the in-memory prompt (no parse)"},
	"pkg/providerutils/chunk_schema.go|unmarshalChunkNumbers":               {2, "the shared decode of UnmarshalChunk (prototype check before it) and UnmarshalChunkPlain"},
	"pkg/mcp/safe_json.go|unmarshalServerJSON":                              {1, "after checkServerJSON, which applies the prototype check when TS secure-parses"},
	"pkg/mcp/jsonrpc.go|parseJSONRPCErrorObject":                            {1, "error.data of a validated message object (no parse in TS; the transport checked where TS runs parseJSONRPCMessage)"},
	"pkg/mcp/oauth.go|DiscoverOAuthProtectedResourceMetadata":               {1, "oauth.ts:605 await response.json()"},
	"pkg/mcp/oauth.go|DiscoverAuthorizationServerMetadata":                  {1, "oauth.ts:754 / 759 await response.json()"},
	"pkg/mcp/oauth_schema.go|decodeOAuthFields":                             {1, "oauth.ts:1106 / 1211 / 1274 await response.json() (tokens, client registration)"},
	"pkg/harness/sandbox/vercel/auth.go|decodeOIDCClaims":                   {2, "@vercel/sandbox decode-base64-url.js:9 JSON.parse, then schema.parse"},
	"pkg/harness/sandbox/vercel/oidc_refresh.go|unverifiedTokenOwner":       {1, "@vercel/sandbox api-client.js:75 decodeUnverifiedToken JSON.parse"},
}

// TestR96integNetworkDecodesRejectForbiddenPrototype is the class guard for
// the retired "JSON payloads with __proto__" known difference. It
// type-checks every package in r95integGuardPatterns and follows network
// bytes (io.ReadAll results, http.Response / internalhttp Response and
// HTTPStatusError bodies, streaming.SSEEvent data, Bedrock event-stream
// payloads, WebSocket messages, bufio readers and scanners over them) through
// assignments, conversions, strings / bytes helpers and module function
// parameters. It fails on an encoding/json Unmarshal or NewDecoder of such
// bytes outside r96integPlainJSONDecodes.
func TestR96integNetworkDecodesRejectForbiddenPrototype(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	const module = "github.com/digitallysavvy/go-ai/"
	cmd := exec.Command("go", append([]string{"list", "-export", "-deps", "-json"}, r95integGuardPatterns...)...)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, stderr.String())
	}
	exports := map[string]string{}
	var modulePkgs []r94integListedPackage
	isTarget := map[string]bool{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var p r94integListedPackage
		if err := dec.Decode(&p); err != nil {
			t.Fatal(err)
		}
		exports[p.ImportPath] = p.Export
		if !strings.HasPrefix(p.ImportPath, module) {
			continue
		}
		modulePkgs = append(modulePkgs, p)
		for _, pattern := range r95integGuardPatterns {
			base := module + strings.TrimSuffix(strings.TrimPrefix(pattern, "./"), "/...")
			if p.ImportPath == base || strings.HasPrefix(p.ImportPath, base+"/") {
				isTarget[p.ImportPath] = true
			}
		}
	}
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		f := exports[path]
		if f == "" {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(f)
	})
	relOf := func(filename string) string {
		if rel, err := filepath.Rel(filepath.Join(root, "pkg", "providers"), filename); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
		rel, _ := filepath.Rel(root, filename)
		return filepath.ToSlash(rel)
	}

	type fnDecl struct {
		pkg  string
		file string
		name string
		decl *ast.FuncDecl
		info *types.Info
		obj  *types.Func
	}
	var fns []fnDecl
	for _, p := range modulePkgs {
		var files []*ast.File
		for _, name := range p.GoFiles {
			f, err := parser.ParseFile(fset, filepath.Join(p.Dir, name), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, f)
		}
		info := &types.Info{
			Types:      map[ast.Expr]types.TypeAndValue{},
			Defs:       map[*ast.Ident]types.Object{},
			Uses:       map[*ast.Ident]types.Object{},
			Selections: map[*ast.SelectorExpr]*types.Selection{},
		}
		if _, err := (&types.Config{Importer: imp}).Check(p.ImportPath, fset, files, info); err != nil {
			t.Fatalf("type-check %s: %v", p.ImportPath, err)
		}
		if !isTarget[p.ImportPath] {
			continue
		}
		for _, f := range files {
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				obj, _ := info.Defs[fn.Name].(*types.Func)
				if obj == nil {
					continue
				}
				name := fn.Name.Name
				if fn.Recv != nil && len(fn.Recv.List) > 0 {
					name = types.ExprString(fn.Recv.List[0].Type) + "." + name
				}
				fns = append(fns, fnDecl{p.ImportPath, relOf(fset.Position(fn.Pos()).Filename), name, fn, info, obj})
			}
		}
	}

	// sourceField is a struct field that holds network bytes.
	sourceField := func(sel *types.Selection) bool {
		if sel == nil || sel.Kind() != types.FieldVal {
			return false
		}
		field := sel.Obj()
		recv := sel.Recv()
		for {
			p, ok := recv.(*types.Pointer)
			if !ok {
				break
			}
			recv = p.Elem()
		}
		named, ok := recv.(*types.Named)
		if !ok || named.Obj().Pkg() == nil {
			return false
		}
		key := named.Obj().Pkg().Path() + "." + named.Obj().Name() + "." + field.Name()
		switch key {
		case "net/http.Response.Body",
			module + "pkg/internal/http.Response.Body",
			module + "pkg/internal/http.HTTPStatusError.Body",
			module + "pkg/providerutils/streaming.SSEEvent.Data",
			module + "pkg/providers/bedrock/eventstream.Event.Payload":
			return true
		}
		return false
	}
	funcOf := func(info *types.Info, call *ast.CallExpr) *types.Func {
		switch fun := ast.Unparen(call.Fun).(type) {
		case *ast.SelectorExpr:
			f, _ := info.Uses[fun.Sel].(*types.Func)
			return f
		case *ast.Ident:
			f, _ := info.Uses[fun].(*types.Func)
			return f
		case *ast.IndexExpr:
			if id, ok := ast.Unparen(fun.X).(*ast.Ident); ok {
				f, _ := info.Uses[id].(*types.Func)
				return f
			}
		}
		return nil
	}
	taintedParams := map[*types.Func]map[int]bool{}
	tainted := map[types.Object]bool{}
	var isTainted func(info *types.Info, e ast.Expr) bool
	isTainted = func(info *types.Info, e ast.Expr) bool {
		switch x := ast.Unparen(e).(type) {
		case *ast.Ident:
			return tainted[info.Uses[x]]
		case *ast.SelectorExpr:
			if sourceField(info.Selections[x]) {
				return true
			}
			return false
		case *ast.SliceExpr:
			return isTainted(info, x.X)
		case *ast.IndexExpr:
			if tv, ok := info.Types[x.X]; ok && tv.Type != nil {
				if _, isSlice := tv.Type.Underlying().(*types.Slice); isSlice {
					return isTainted(info, x.X)
				}
			}
			return false
		case *ast.StarExpr:
			return isTainted(info, x.X)
		case *ast.UnaryExpr:
			if x.Op == token.AND {
				return isTainted(info, x.X)
			}
		case *ast.CallExpr:
			if tv, ok := info.Types[x.Fun]; ok && tv.IsType() && len(x.Args) == 1 {
				return isTainted(info, x.Args[0])
			}
			f := funcOf(info, x)
			if f == nil || f.Pkg() == nil {
				return false
			}
			switch path := f.Pkg().Path(); {
			case (path == "io" || path == "io/ioutil") && f.Name() == "ReadAll":
				return true
			case path == module+"pkg/providerutils/websocket" && f.Name() == "ReceiveMessage":
				return true
			case path == "bytes" || path == "strings" || path == "bufio":
				// Trim*, Split*, Fields, NewReader, NewScanner, ... and
				// methods of a reader / scanner over network bytes.
				if sel, ok := ast.Unparen(x.Fun).(*ast.SelectorExpr); ok && info.Selections[sel] != nil && isTainted(info, sel.X) {
					return true
				}
				for _, a := range x.Args {
					if isTainted(info, a) {
						return true
					}
				}
			}
		}
		return false
	}
	markLHS := func(info *types.Info, lhs ast.Expr) bool {
		id, ok := ast.Unparen(lhs).(*ast.Ident)
		if !ok || id.Name == "_" {
			return false
		}
		obj := info.Defs[id]
		if obj == nil {
			obj = info.Uses[id]
		}
		if obj == nil || tainted[obj] {
			return false
		}
		tainted[obj] = true
		return true
	}
	for changed := true; changed; {
		changed = false
		for _, fn := range fns {
			info := fn.info
			if params := taintedParams[fn.obj]; len(params) > 0 {
				idx := 0
				for _, field := range fn.decl.Type.Params.List {
					if len(field.Names) == 0 {
						idx++
						continue
					}
					for _, name := range field.Names {
						if params[idx] {
							if obj := info.Defs[name]; obj != nil && !tainted[obj] {
								tainted[obj] = true
								changed = true
							}
						}
						idx++
					}
				}
			}
			ast.Inspect(fn.decl.Body, func(n ast.Node) bool {
				switch s := n.(type) {
				case *ast.AssignStmt:
					if len(s.Rhs) == 1 && len(s.Lhs) > 1 {
						if isTainted(info, s.Rhs[0]) && markLHS(info, s.Lhs[0]) {
							changed = true
						}
						return true
					}
					for i := range s.Lhs {
						if i < len(s.Rhs) && isTainted(info, s.Rhs[i]) && markLHS(info, s.Lhs[i]) {
							changed = true
						}
					}
				case *ast.ValueSpec:
					for i, name := range s.Names {
						if i < len(s.Values) && isTainted(info, s.Values[i]) && markLHS(info, name) {
							changed = true
						}
					}
				case *ast.RangeStmt:
					if s.Value != nil && isTainted(info, s.X) && markLHS(info, s.Value) {
						changed = true
					}
				case *ast.CallExpr:
					f := funcOf(info, s)
					if f == nil {
						return true
					}
					// golang.org/x/net/websocket Message.Receive(conn, &v).
					if f.Pkg() != nil && f.Pkg().Path() == "golang.org/x/net/websocket" && f.Name() == "Receive" && len(s.Args) == 2 {
						if u, ok := ast.Unparen(s.Args[1]).(*ast.UnaryExpr); ok && u.Op == token.AND && markLHS(info, u.X) {
							changed = true
						}
					}
					if f.Pkg() == nil || !strings.HasPrefix(f.Pkg().Path(), module) {
						return true
					}
					callee := f.Origin()
					for i, a := range s.Args {
						if isTainted(info, a) {
							if taintedParams[callee] == nil {
								taintedParams[callee] = map[int]bool{}
							}
							if !taintedParams[callee][i] {
								taintedParams[callee][i] = true
								changed = true
							}
						}
					}
				}
				return true
			})
		}
	}

	found := map[string]int{}
	var problems []string
	for _, fn := range fns {
		if fn.decl.Name.Name == "UnmarshalJSON" {
			continue
		}
		info := fn.info
		ast.Inspect(fn.decl.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			f := funcOf(info, call)
			if f == nil || f.Pkg() == nil || f.Pkg().Path() != "encoding/json" {
				return true
			}
			var arg ast.Expr
			switch {
			case f.Name() == "Unmarshal" && len(call.Args) == 2:
				// A target that cannot hold a JSON object (a string,
				// number or bool, or a slice of them) fails on an object
				// anyway.
				if tv, ok := info.Types[call.Args[1]]; ok && tv.Type != nil && !r96integHoldsObject(tv.Type, map[types.Type]bool{}) {
					return true
				}
				arg = call.Args[0]
			case f.Name() == "NewDecoder" && len(call.Args) == 1:
				arg = call.Args[0]
			default:
				return true
			}
			if !isTainted(info, arg) {
				return true
			}
			key := fn.file + "|" + fn.name
			found[key]++
			if _, ok := r96integPlainJSONDecodes[key]; !ok {
				problems = append(problems, fmt.Sprintf("%s:%d (%s): encoding/json.%s of network bytes; use jsonexact.Unmarshal / Decode or securejson (they reject __proto__ like TS secureJsonParse), or list the site in r96integPlainJSONDecodes with its TS reason",
					fn.file, fset.Position(call.Pos()).Line, fn.name, f.Name()))
			}
			return true
		})
	}
	// The plain-JSON entry points: every call is listed with its TS site.
	plainFound := map[string]int{}
	for _, fn := range fns {
		// A nested decode inside an UnmarshalJSON method: the entry decoder
		// of the message applied the prototype check where TS secure-parses.
		if fn.decl.Name.Name == "UnmarshalJSON" {
			continue
		}
		info := fn.info
		ast.Inspect(fn.decl.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			f := funcOf(info, call)
			if f == nil || f.Pkg() == nil {
				return true
			}
			switch f.Pkg().Path() + "." + f.Name() {
			case module + "pkg/internal/jsonexact.UnmarshalPlain",
				module + "pkg/providerutils.UnmarshalChunkPlain",
				module + "pkg/providerutils.MediaSchemaParserPlain",
				module + "pkg/internal/zodlite.ValidationMessagePlain":
			default:
				return true
			}
			key := fn.file + "|" + fn.name
			plainFound[key]++
			if _, ok := r97integResponseJSONDecodes[key]; !ok {
				problems = append(problems, fmt.Sprintf("%s:%d (%s): %s keeps __proto__ keys (TS plain JSON.parse); list %q in r97integResponseJSONDecodes with the TS response.json() site, or use the checking decoder",
					fn.file, fset.Position(call.Pos()).Line, fn.name, f.Name(), key))
			}
			return true
		})
	}
	for key, want := range r97integResponseJSONDecodes {
		if got := plainFound[key]; got != want.count {
			problems = append(problems, fmt.Sprintf("r97integResponseJSONDecodes[%q]: %d plain decodes found, %d listed (%s)", key, got, want.count, want.reason))
		}
	}
	for key, want := range r96integPlainJSONDecodes {
		if got := found[key]; got != want.count {
			problems = append(problems, fmt.Sprintf("r96integPlainJSONDecodes[%q]: %d plain decodes found, %d listed (%s)", key, got, want.count, want.reason))
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		t.Fatalf("forbidden prototype guard (%d):\n%s", len(problems), strings.Join(problems, "\n"))
	}
}

// r96integHoldsObject reports whether a decode target of type t can hold a
// JSON object (a struct, map or interface somewhere in it, or a type with
// its own decoder).
func r96integHoldsObject(t types.Type, seen map[types.Type]bool) bool {
	if seen[t] {
		return false
	}
	seen[t] = true
	if _, ok := t.(*types.Named); ok {
		if ms := types.NewMethodSet(types.NewPointer(t)); ms.Lookup(nil, "UnmarshalJSON") != nil {
			return true
		}
		if obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(t), true, nil, "UnmarshalJSON"); obj != nil {
			return true
		}
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return false
	case *types.Pointer:
		return r96integHoldsObject(u.Elem(), seen)
	case *types.Slice:
		return r96integHoldsObject(u.Elem(), seen)
	case *types.Array:
		return r96integHoldsObject(u.Elem(), seen)
	}
	return true
}
