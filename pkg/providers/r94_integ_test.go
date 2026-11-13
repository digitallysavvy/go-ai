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
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// r94integDirectDecodes are the provider-package functions that still decode
// into a struct with encoding/json directly (case-insensitive keys), with the
// number of such calls in each. None of them decodes a provider response:
// review round 94 (cr-r94-carry-to-{provb,provg,prova,provc,tools}) moved
// every response decode to jsonexact, the way zod matches object keys.
// Option-typed targets (a type named *Option* / *Setting* / *Config* /
// *Param*) are user input and are not listed (known difference "Option keys
// match case-insensitively", S6).
var r94integDirectDecodes = map[string]struct {
	count  int
	reason string
}{
	"alibaba/video_model.go|*VideoModel.DoStatus":                             {1, "operation reference the SDK built"},
	"anthropic/call_options.go|decodeAnthropicCallOptions":                    {1, "caller option (fallbacks)"},
	"anthropic/context_management_json.go|*ContextManagement.UnmarshalJSON":   {6, "caller option providerOptions.anthropic.contextManagement"},
	"anthropic/tools/code_execution_20260120.go|UnmarshalCodeExecutionInput":  {4, "public helper on the SDK's tool-call input"},
	"anthropic/tools/code_execution_20260120.go|UnmarshalCodeExecutionResult": {9, "public helper on the SDK's tool-result value"},
	"anthropic/tools/web_fetch_20250910.go|ParseWebFetch20250910Result":       {1, "public helper on the SDK-built tool result"},
	"anthropic/tools/web_fetch_20260209.go|ParseWebFetchResult":               {1, "public helper on the SDK-built tool result"},
	"anthropic/tools/web_fetch_20260318.go|ParseWebFetch20260318Result":       {1, "public helper on the SDK-built tool result"},
	"anthropic/tools/web_search_20250305.go|ParseWebSearch20250305Results":    {1, "public helper on the SDK-built tool result"},
	"anthropic/tools/web_search_20260209.go|ParseWebSearchResults":            {1, "public helper on the SDK-built tool result"},
	"anthropic/tools/web_search_20260318.go|ParseWebSearch20260318Results":    {1, "public helper on the SDK-built tool result"},
	"bfl/video_model.go|*VideoModel.DoGenerate":                               {1, "operation reference the SDK built"},
	"bfl/video_model.go|*VideoModel.DoStatus":                                 {1, "operation reference the SDK built"},
	"bytedance/video_model.go|*VideoModel.DoStatus":                           {1, "operation reference the SDK built"},
	"deepgram/speech_model.go|extractSpeechModelOptions":                      {1, "caller option sampleRate"},
	"fal/video_model.go|*VideoModel.DoStatus":                                 {1, "operation reference the SDK built"},
	"gateway/decision_fallback.go|*GatewayModelFallback.UnmarshalJSON":        {1, "caller option (model fallback)"},
	"google/video_model.go|*VideoModel.DoStatus":                              {1, "operation reference the SDK built"},
	"googlevertex/video_model.go|*VideoModel.DoStatus":                        {1, "operation reference the SDK built"},
	"heygen/video_model.go|*VideoModel.DoGenerate":                            {1, "operation reference the SDK built"},
	"heygen/video_model.go|*VideoModel.DoStatus":                              {1, "operation reference the SDK built"},
	"klingai/video_model.go|*VideoModel.DoStatus":                             {1, "operation reference the SDK built"},
	"minimax/video_model.go|*VideoModel.DoGenerate":                           {1, "operation reference the SDK built"},
	"minimax/video_model.go|*VideoModel.getStatus":                            {1, "operation reference the SDK built"},
	"mistral/conversation_inputs.go|convertToMistralConversationInputs":       {1, "prompt tool-call input"},
	"openai/responses/convert.go|appendAdditionalTools":                       {1, "caller option (additional tool definitions)"},
	"openai/responses/provider_tool_args.go|openAICustomToolConfig":           {1, "caller tool args (custom tool definition, through toolConfig)"},
	"replicate/video_model.go|*VideoModel.DoStatus":                           {1, "operation reference the SDK built"},
	"tencent/video_model.go|*VideoModel.DoStatus":                             {1, "operation reference the SDK built"},
	"topaz/video_model.go|*VideoModel.DoGenerate":                             {1, "operation reference the SDK built"},
	"topaz/video_model.go|*VideoModel.DoStatus":                               {1, "operation reference the SDK built"},
	"xai/video_model.go|*VideoModel.DoStatus":                                 {1, "operation reference the SDK built"},
}

var r94integUserInputType = regexp.MustCompile(`(?i)(option|setting|config|param)`)

type r94integListedPackage struct {
	ImportPath string
	Dir        string
	Export     string
	GoFiles    []string
}

// r95integGuardPatterns are the packages TestR94integProviderResponsesDecodeCaseExact
// checks: every provider and the shared provider code, the MCP client
// (server messages) and the sandbox clients (@vercel/sandbox API
// responses), review round 95 (provg #3, tools #7).
var r95integGuardPatterns = []string{"./pkg/providers/...", "./pkg/providerutils/...", "./pkg/mcp/...", "./pkg/harness/sandbox/..."}

// r95integDecodeHelper is a function parameter of type interface{} (or a
// type parameter) that reaches a JSON decode: lenient when it reaches
// encoding/json without jsonexact.Filter, exact when it reaches jsonexact.
type r95integDecodeHelper struct{ lenient, exact bool }

// r95integFlow is one use of a parameter as a decode target: a direct
// encoding/json decode (lenient), a jsonexact decode (exact), or an argument
// of another function (callee, index), resolved to a fixed point.
type r95integFlow struct {
	fn, callee     string
	param, index   int
	lenient, exact bool
}

// TestR94integProviderResponsesDecodeCaseExact is the class guard for
// case-exact response decoding (review rounds 94 and 95). It type-checks
// every package in r95integGuardPatterns and fails on:
//   - a direct encoding/json Unmarshal or Decoder.Decode into a value that
//     reaches a struct, outside r94integDirectDecodes (decode provider and
//     server responses with jsonexact.Unmarshal or jsonexact.Filter instead);
//   - the same through a decode helper: a struct passed as the interface{}
//     (or type parameter) argument of any module function that hands it to
//     encoding/json without jsonexact.Filter, directly or through further
//     helpers (round 95: decodeGatewayLenient, MCP's unmarshalSafeJSON),
//     and an encoding/json decode into a reflect.Value.Interface() target
//     (a reflect-based helper such as looseDecode);
//   - a jsonexact.Unmarshal / Filter target, direct or through an exact
//     helper, with an exported field that has no json tag, at any depth (it
//     would match only a key spelled like the Go field name).
func TestR94integProviderResponsesDecodeCaseExact(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	const module = "github.com/digitallysavvy/go-ai/"
	// go test puts GOROOT/bin first on the test binary's PATH.
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
	jsonPkg, err := imp.Import("encoding/json")
	if err != nil {
		t.Fatal(err)
	}
	encodingPkg, err := imp.Import("encoding")
	if err != nil {
		t.Fatal(err)
	}
	unmarshaler := jsonPkg.Scope().Lookup("Unmarshaler").Type().Underlying().(*types.Interface)
	textUnmarshaler := encodingPkg.Scope().Lookup("TextUnmarshaler").Type().Underlying().(*types.Interface)
	custom := func(t types.Type) bool {
		return types.Implements(t, unmarshaler) || types.Implements(types.NewPointer(t), unmarshaler) ||
			types.Implements(t, textUnmarshaler) || types.Implements(types.NewPointer(t), textUnmarshaler)
	}
	deref := func(t types.Type) types.Type {
		for {
			p, ok := t.Underlying().(*types.Pointer)
			if !ok {
				return t
			}
			t = p.Elem()
		}
	}
	var reachesStruct func(t types.Type, seen map[types.Type]bool) bool
	reachesStruct = func(t types.Type, seen map[types.Type]bool) bool {
		t = deref(t)
		if seen[t] || custom(t) {
			return false
		}
		seen[t] = true
		switch u := t.Underlying().(type) {
		case *types.Struct:
			return true
		case *types.Slice:
			return reachesStruct(u.Elem(), seen)
		case *types.Array:
			return reachesStruct(u.Elem(), seen)
		case *types.Map:
			return reachesStruct(u.Elem(), seen)
		}
		return false
	}
	var untagged func(t types.Type, seen map[types.Type]bool, out *[]string)
	untagged = func(t types.Type, seen map[types.Type]bool, out *[]string) {
		t = deref(t)
		if seen[t] || custom(t) {
			return
		}
		seen[t] = true
		switch u := t.Underlying().(type) {
		case *types.Struct:
			for i := 0; i < u.NumFields(); i++ {
				f := u.Field(i)
				tag := reflect.StructTag(u.Tag(i)).Get("json")
				if tag == "-" {
					continue
				}
				name, _, _ := strings.Cut(tag, ",")
				if f.Embedded() && name == "" {
					if _, ok := deref(f.Type()).Underlying().(*types.Struct); ok {
						untagged(f.Type(), seen, out)
						continue
					}
				}
				if !f.Exported() {
					continue
				}
				if name == "" {
					*out = append(*out, types.TypeString(t, func(p *types.Package) string { return p.Name() })+"."+f.Name())
				}
				untagged(f.Type(), seen, out)
			}
		case *types.Slice:
			untagged(u.Elem(), seen, out)
		case *types.Array:
			untagged(u.Elem(), seen, out)
		case *types.Map:
			untagged(u.Elem(), seen, out)
		}
	}
	// decodeCall classifies a call: a direct encoding/json decode (target
	// argument index), a jsonexact decode, or a call of a module function.
	type callKind int
	const (
		callOther callKind = iota
		callJSON
		callExact
		callFunc
	)
	classify := func(info *types.Info, call *ast.CallExpr) (callKind, int, *types.Func) {
		var obj *types.Func
		switch fun := ast.Unparen(call.Fun).(type) {
		case *ast.SelectorExpr:
			obj, _ = info.Uses[fun.Sel].(*types.Func)
			if obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == "encoding/json" && obj.Name() == "Decode" && len(call.Args) == 1 && info.Selections[fun] != nil {
				return callJSON, 0, obj
			}
		case *ast.Ident:
			obj, _ = info.Uses[fun].(*types.Func)
		case *ast.IndexExpr:
			if id, ok := ast.Unparen(fun.X).(*ast.Ident); ok {
				obj, _ = info.Uses[id].(*types.Func)
			}
		case *ast.IndexListExpr:
			if id, ok := ast.Unparen(fun.X).(*ast.Ident); ok {
				obj, _ = info.Uses[id].(*types.Func)
			}
		}
		if obj == nil || obj.Pkg() == nil {
			return callOther, 0, nil
		}
		switch {
		case obj.Pkg().Path() == "encoding/json" && obj.Name() == "Unmarshal" && len(call.Args) == 2:
			return callJSON, 1, obj
		case strings.HasSuffix(obj.Pkg().Path(), "/internal/jsonexact") && obj.Name() == "Decode" && len(call.Args) == 2:
			// json.Unmarshal with JSON.parse's number range: lenient keys
			// unless the bytes came from Filter for the same target.
			return callJSON, 1, obj
		case strings.HasSuffix(obj.Pkg().Path(), "/internal/jsonexact") && (obj.Name() == "Unmarshal" || obj.Name() == "Filter") && len(call.Args) == 2:
			return callExact, 1, obj
		case strings.HasPrefix(obj.Pkg().Path(), module):
			return callFunc, 0, obj
		}
		return callOther, 0, obj
	}
	// isReflectInterface reports a reflect.Value.Interface() call (the
	// target of a reflect-based decode helper such as looseDecode).
	isReflectInterface := func(info *types.Info, e ast.Expr) bool {
		call, ok := ast.Unparen(e).(*ast.CallExpr)
		if !ok {
			return false
		}
		sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Interface" {
			return false
		}
		tv := info.Types[sel.X].Type
		return tv != nil && types.TypeString(tv, nil) == "reflect.Value"
	}
	relOf := func(filename string) string {
		if rel, err := filepath.Rel(filepath.Join(root, "pkg", "providers"), filename); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
		rel, _ := filepath.Rel(root, filename)
		return filepath.ToSlash(rel)
	}

	type checkedPackage struct {
		p     r94integListedPackage
		files []*ast.File
		info  *types.Info
	}
	var checked []checkedPackage
	var flows []r95integFlow
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
		checked = append(checked, checkedPackage{p, files, info})
		// Decode-helper flows: an interface{} or type-parameter parameter
		// used as a decode target or passed on to another function.
		for _, f := range files {
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				fnObj, _ := info.Defs[fn.Name].(*types.Func)
				if fnObj == nil {
					continue
				}
				params := map[types.Object]int{}
				idx := 0
				for _, field := range fn.Type.Params.List {
					names := field.Names
					if len(names) == 0 {
						idx++
						continue
					}
					for _, name := range names {
						obj := info.Defs[name]
						if obj != nil {
							switch obj.Type().(type) {
							case *types.TypeParam:
								params[obj] = idx
							default:
								if _, isIface := obj.Type().Underlying().(*types.Interface); isIface {
									params[obj] = idx
								}
							}
						}
						idx++
					}
				}
				if len(params) == 0 {
					continue
				}
				paramOf := func(e ast.Expr) (int, bool) {
					id, ok := ast.Unparen(e).(*ast.Ident)
					if !ok {
						return 0, false
					}
					i, ok := params[info.Uses[id]]
					return i, ok
				}
				filtered := map[int]bool{}
				var local []r95integFlow
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					kind, argIdx, callee := classify(info, call)
					switch kind {
					case callJSON:
						if i, ok := paramOf(call.Args[argIdx]); ok {
							local = append(local, r95integFlow{fn: fnObj.FullName(), param: i, lenient: true})
						}
					case callExact:
						if i, ok := paramOf(call.Args[argIdx]); ok {
							local = append(local, r95integFlow{fn: fnObj.FullName(), param: i, exact: true})
							if callee.Name() == "Filter" {
								filtered[i] = true
							}
						}
					case callFunc:
						for ai, arg := range call.Args {
							if i, ok := paramOf(arg); ok {
								local = append(local, r95integFlow{fn: fnObj.FullName(), param: i, callee: callee.Origin().FullName(), index: ai})
							}
						}
					}
					return true
				})
				for _, fl := range local {
					// encoding/json on the bytes jsonexact.Filter returned
					// for the same target is the exact decode.
					if fl.lenient && filtered[fl.param] {
						fl.lenient, fl.exact = false, true
					}
					flows = append(flows, fl)
				}
			}
		}
	}
	helpers := map[string]map[int]r95integDecodeHelper{}
	for changed := true; changed; {
		changed = false
		for _, fl := range flows {
			h := r95integDecodeHelper{lenient: fl.lenient, exact: fl.exact}
			if fl.callee != "" {
				h = helpers[fl.callee][fl.index]
			}
			if !h.lenient && !h.exact {
				continue
			}
			cur := helpers[fl.fn][fl.param]
			next := r95integDecodeHelper{lenient: cur.lenient || h.lenient, exact: cur.exact || h.exact}
			if next != cur {
				if helpers[fl.fn] == nil {
					helpers[fl.fn] = map[int]r95integDecodeHelper{}
				}
				helpers[fl.fn][fl.param] = next
				changed = true
			}
		}
	}

	found := map[string]int{}
	var problems []string
	for _, c := range checked {
		if !isTarget[c.p.ImportPath] {
			continue
		}
		info := c.info
		for _, f := range c.files {
			for _, decl := range f.Decls {
				fnName := "<package-level>"
				if fn, ok := decl.(*ast.FuncDecl); ok {
					fnName = fn.Name.Name
					if fn.Recv != nil && len(fn.Recv.List) > 0 {
						fnName = types.ExprString(fn.Recv.List[0].Type) + "." + fnName
					}
				}
				ast.Inspect(decl, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					pos := fset.Position(call.Pos())
					rel := relOf(pos.Filename)
					kind, argIdx, callee := classify(info, call)
					var lenientTargets, exactTargets []ast.Expr
					switch kind {
					case callJSON:
						lenientTargets = append(lenientTargets, call.Args[argIdx])
					case callExact:
						exactTargets = append(exactTargets, call.Args[argIdx])
					case callFunc:
						for i, h := range helpers[callee.Origin().FullName()] {
							if i >= len(call.Args) {
								continue
							}
							if h.lenient {
								lenientTargets = append(lenientTargets, call.Args[i])
							} else if h.exact {
								exactTargets = append(exactTargets, call.Args[i])
							}
						}
					default:
						return true
					}
					for _, target := range exactTargets {
						var bad []string
						if tt := info.Types[target].Type; tt != nil {
							untagged(tt, map[types.Type]bool{}, &bad)
						}
						for _, b := range bad {
							problems = append(problems, fmt.Sprintf("%s:%d: jsonexact target field without a json tag: %s", rel, pos.Line, b))
						}
					}
					for _, target := range lenientTargets {
						tt := info.Types[target].Type
						// A reflect-built target (v.Interface()) can be any
						// type: it counts as a struct decode.
						if !isReflectInterface(info, target) && (tt == nil || !reachesStruct(tt, map[types.Type]bool{})) {
							continue
						}
						if nt, ok := deref(tt).(*types.Named); ok && r94integUserInputType.MatchString(nt.Obj().Name()) {
							continue
						}
						via := ""
						if kind == callFunc {
							via = " through " + callee.Name()
						}
						key := rel + "|" + fnName
						found[key]++
						if _, ok := r94integDirectDecodes[key]; !ok {
							problems = append(problems, fmt.Sprintf("%s:%d (%s): encoding/json decode%s into %s; decode provider and server responses with jsonexact.Unmarshal, or list a user-input site in r94integDirectDecodes with its reason",
								rel, pos.Line, fnName, via, types.TypeString(tt, func(p *types.Package) string { return p.Name() })))
						}
					}
					return true
				})
			}
		}
	}
	for key, want := range r94integDirectDecodes {
		if got := found[key]; got != want.count {
			problems = append(problems, fmt.Sprintf("r94integDirectDecodes[%q]: %d direct decodes found, %d listed (%s)", key, got, want.count, want.reason))
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		t.Fatalf("case-exact response decoding guard:\n%s", strings.Join(problems, "\n"))
	}
}
