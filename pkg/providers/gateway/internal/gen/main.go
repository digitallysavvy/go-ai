// Command gen regenerates pkg/providers/gateway/model_ids.go from the
// TypeScript AI SDK gateway settings files
// (packages/gateway/src/gateway-*-model-settings.ts).
//
// Usage (from pkg/providers/gateway):
//
//	AI_SDK_TS_DIR=/path/to/ai go generate .
//
// or directly:
//
//	go run ./internal/gen -ts-dir /path/to/ai -out model_ids.go
//
// -ts-dir may point at the TS repository root or directly at
// packages/gateway/src. It defaults to $AI_SDK_TS_DIR. Check out the TS repo
// at the release tag being mirrored (e.g. ai@7.0.113) before generating.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// kind describes one gateway model kind and its TS settings file.
type kind struct {
	// File is the settings file suffix: gateway-<File>-model-settings.ts.
	File string
	// TSType is the exported TS union type name.
	TSType string
	// GoName is the kind segment of Go identifiers: Gateway<GoName>ModelID.
	GoName string
	// Optional kinds may be absent in older TS releases; they are emitted
	// with an empty list when the file is missing.
	Optional bool
}

// kinds is the emit order of model_ids.go.
var kinds = []kind{
	{File: "language", TSType: "GatewayModelId", GoName: "Language"},
	{File: "embedding", TSType: "GatewayEmbeddingModelId", GoName: "Embedding"},
	{File: "image", TSType: "GatewayImageModelId", GoName: "Image"},
	{File: "video", TSType: "GatewayVideoModelId", GoName: "Video"},
	{File: "reranking", TSType: "GatewayRerankingModelId", GoName: "Reranking"},
	{File: "speech", TSType: "GatewaySpeechModelId", GoName: "Speech"},
	{File: "transcription", TSType: "GatewayTranscriptionModelId", GoName: "Transcription"},
	{File: "realtime", TSType: "GatewayRealtimeModelId", GoName: "Realtime"},
	{File: "evaluation", TSType: "GatewayEvaluationModelId", GoName: "Evaluation", Optional: true},
}

func main() {
	tsDir := flag.String("ts-dir", os.Getenv("AI_SDK_TS_DIR"), "path to the TS AI SDK repo root or its packages/gateway/src (default $AI_SDK_TS_DIR)")
	out := flag.String("out", "model_ids.go", "output Go file")
	flag.Parse()

	if *tsDir == "" {
		fatalf("no TS source directory: pass -ts-dir or set AI_SDK_TS_DIR to the TypeScript AI SDK checkout")
	}
	srcDir, err := resolveSrcDir(*tsDir)
	if err != nil {
		fatalf("%v", err)
	}

	catalog := make(map[string][]string, len(kinds))
	for _, k := range kinds {
		path := filepath.Join(srcDir, "gateway-"+k.File+"-model-settings.ts")
		data, err := os.ReadFile(path)
		if err != nil {
			if k.Optional && errors.Is(err, os.ErrNotExist) {
				continue
			}
			fatalf("read %s: %v", path, err)
		}
		ids, err := parseUnion(string(data), k.TSType)
		if err != nil {
			fatalf("%s: %v", path, err)
		}
		catalog[k.File] = ids
	}

	src, err := render(catalog)
	if err != nil {
		fatalf("render: %v", err)
	}
	if err := os.WriteFile(*out, src, 0o644); err != nil {
		fatalf("write %s: %v", *out, err)
	}
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "gateway gen: "+format+"\n", args...)
	os.Exit(1)
}

// resolveSrcDir accepts either the TS repo root or packages/gateway/src.
func resolveSrcDir(dir string) (string, error) {
	for _, candidate := range []string{filepath.Join(dir, "packages", "gateway", "src"), dir} {
		if _, err := os.Stat(filepath.Join(candidate, "gateway-language-model-settings.ts")); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("gateway-language-model-settings.ts not found under %s or %s/packages/gateway/src", dir, dir)
}

var (
	stringLiteral = regexp.MustCompile(`'([^'\\]*)'`)
)

// parseUnion extracts the string-literal members of `export type <name> = ...;`.
// The open-ended `(string & {})` member is ignored; Go models it by using a
// string-based type.
func parseUnion(src, typeName string) ([]string, error) {
	decl := regexp.MustCompile(`export\s+type\s+` + regexp.QuoteMeta(typeName) + `\s*=([^;]*);`)
	m := decl.FindStringSubmatch(src)
	if m == nil {
		return nil, fmt.Errorf("export type %s not found", typeName)
	}
	var ids []string
	seen := map[string]bool{}
	for _, lit := range stringLiteral.FindAllStringSubmatch(m[1], -1) {
		id := lit[1]
		if seen[id] {
			return nil, fmt.Errorf("duplicate model ID %q in %s", id, typeName)
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, nil
}

// identSuffix converts a model ID into the exported Go identifier suffix used
// after Gateway<Kind>Model, e.g. "openai/gpt-5.5" -> "OpenaiGpt55" and
// "alibaba/qwen3-235b-a22b-thinking" -> "AlibabaQwen3235bA22bThinking".
// Each run of letters/digits is a word whose first rune is upper-cased; all
// other characters are dropped.
func identSuffix(id string) string {
	var b strings.Builder
	for _, word := range strings.FieldsFunc(id, func(r rune) bool {
		return r >= unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsDigit(r))
	}) {
		b.WriteString(strings.ToUpper(word[:1]))
		b.WriteString(word[1:])
	}
	return b.String()
}

func render(catalog map[string][]string) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("// Code generated by internal/gen from ai/packages/gateway/src/gateway-*-model-settings.ts; DO NOT EDIT.\n\n")
	buf.WriteString("package gateway\n\n")
	buf.WriteString("// Gateway model ID types mirror the TypeScript AI SDK Gateway*ModelId unions.\n")
	buf.WriteString("// Each TS union is open-ended (`| (string & {})`), so any string converts to\n")
	buf.WriteString("// these types; the constants below are the suggested IDs.\n")
	buf.WriteString("type (\n")
	for _, k := range kinds {
		_, _ = fmt.Fprintf(&buf, "\t// Gateway%sModelID mirrors TS %s.\n", k.GoName, k.TSType)
		_, _ = fmt.Fprintf(&buf, "\tGateway%sModelID string\n", k.GoName)
	}
	buf.WriteString(")\n")

	for _, k := range kinds {
		ids := catalog[k.File]
		if len(ids) == 0 {
			continue
		}
		typ := "Gateway" + k.GoName + "ModelID"
		_, _ = fmt.Fprintf(&buf, "\n// %s constants mirror gateway-%s-model-settings.ts.\nconst (\n", typ, k.File)
		names := map[string]string{}
		for _, id := range ids {
			name := "Gateway" + k.GoName + "Model" + identSuffix(id)
			if prev, dup := names[name]; dup {
				return nil, fmt.Errorf("identifier %s collides for %q and %q", name, prev, id)
			}
			names[name] = id
			_, _ = fmt.Fprintf(&buf, "\t%s %s = %q\n", name, typ, id)
		}
		buf.WriteString(")\n")
	}

	for _, k := range kinds {
		typ := "Gateway" + k.GoName + "ModelID"
		_, _ = fmt.Fprintf(&buf, "\n// %ss lists the suggested gateway %s model IDs from gateway-%s-model-settings.ts.\n", typ, k.File, k.File)
		_, _ = fmt.Fprintf(&buf, "var %ss = []%s{\n", typ, typ)
		for _, id := range catalog[k.File] {
			_, _ = fmt.Fprintf(&buf, "\tGateway%sModel%s,\n", k.GoName, identSuffix(id))
		}
		buf.WriteString("}\n")
	}

	return format.Source(buf.Bytes())
}
