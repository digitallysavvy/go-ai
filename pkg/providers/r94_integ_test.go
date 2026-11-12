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
	"runtime"
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

// TestR94integProviderResponsesDecodeCaseExact is the class guard for
// case-exact response decoding (review round 94). It type-checks every
// package under pkg/providers and fails on:
//   - a direct encoding/json Unmarshal or Decoder.Decode into a value that
//     reaches a struct, outside r94integDirectDecodes (decode provider
//     responses with jsonexact.Unmarshal or jsonexact.Filter instead);
//   - a jsonexact.Unmarshal / Filter target with an exported field that has
//     no json tag, at any depth (it would match only a key spelled like the
//     Go field name).
func TestR94integProviderResponsesDecodeCaseExact(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		goBin = "go"
	}
	cmd := exec.Command(goBin, "list", "-export", "-deps", "-json", "./pkg/providers/...")
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, stderr.String())
	}
	exports := map[string]string{}
	var targets []r94integListedPackage
	const prefix = "github.com/digitallysavvy/go-ai/pkg/providers"
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var p r94integListedPackage
		if err := dec.Decode(&p); err != nil {
			t.Fatal(err)
		}
		exports[p.ImportPath] = p.Export
		if p.ImportPath == prefix || strings.HasPrefix(p.ImportPath, prefix+"/") {
			targets = append(targets, p)
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

	found := map[string]int{}
	var problems []string
	for _, p := range targets {
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
			Uses:       map[*ast.Ident]types.Object{},
			Selections: map[*ast.SelectorExpr]*types.Selection{},
		}
		if _, err := (&types.Config{Importer: imp}).Check(p.ImportPath, fset, files, info); err != nil {
			t.Fatalf("type-check %s: %v", p.ImportPath, err)
		}
		for _, f := range files {
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
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					obj, ok := info.Uses[sel.Sel].(*types.Func)
					if !ok || obj.Pkg() == nil {
						return true
					}
					pos := fset.Position(call.Pos())
					rel, _ := filepath.Rel(filepath.Join(root, "pkg", "providers"), pos.Filename)
					rel = filepath.ToSlash(rel)
					var target ast.Expr
					switch {
					case strings.HasSuffix(obj.Pkg().Path(), "/internal/jsonexact") && (obj.Name() == "Unmarshal" || obj.Name() == "Filter") && len(call.Args) == 2:
						var bad []string
						if tt := info.Types[call.Args[1]].Type; tt != nil {
							untagged(tt, map[types.Type]bool{}, &bad)
						}
						for _, b := range bad {
							problems = append(problems, fmt.Sprintf("%s:%d: jsonexact target field without a json tag: %s", rel, pos.Line, b))
						}
						return true
					case obj.Pkg().Path() == "encoding/json" && obj.Name() == "Unmarshal" && len(call.Args) == 2:
						target = call.Args[1]
					case obj.Pkg().Path() == "encoding/json" && obj.Name() == "Decode" && len(call.Args) == 1 && info.Selections[sel] != nil:
						target = call.Args[0]
					default:
						return true
					}
					tt := info.Types[target].Type
					if tt == nil || !reachesStruct(tt, map[types.Type]bool{}) {
						return true
					}
					if nt, ok := deref(tt).(*types.Named); ok && r94integUserInputType.MatchString(nt.Obj().Name()) {
						return true
					}
					key := rel + "|" + fnName
					found[key]++
					if _, ok := r94integDirectDecodes[key]; !ok {
						problems = append(problems, fmt.Sprintf("%s:%d (%s): encoding/json decode into %s; decode provider responses with jsonexact.Unmarshal, or list a user-input site in r94integDirectDecodes with its reason",
							rel, pos.Line, fnName, types.TypeString(tt, func(p *types.Package) string { return p.Name() })))
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
