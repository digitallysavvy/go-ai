// Command genpin rewrites pin_gen.go, the libfx release the fx runtime host
// is pinned to: the version, the download URLs, the tarball's npm sha512
// integrity, the fx-core.wasm sha256 and size, and the wasm import table the
// host implements.
//
// Usage, from the repository root:
//
//	go run ./pkg/harness/fx/internal/libfx/genpin -version 0.0.15
//
// or, equivalently:
//
//	LIBFX_VERSION=0.0.15 go generate ./pkg/harness/fx/internal/libfx
//
// Without a version it regenerates the current pin. It reads the npm
// registry metadata, downloads the tarball through the SDK's SSRF-safe
// download client, checks the tarball against the registry's sha512
// integrity, extracts package/fx-core.wasm, compares it with the jsDelivr
// mirror, and compiles the module with wazero to list its imports. The
// module is compiled only; it is never instantiated or run.
//
// -tarball uses a local libfx-<version>.tgz (for example from
// `npm pack libfx@<version> --ignore-scripts`) instead of downloading it. It
// is still checked against the registry's integrity value.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
)

const (
	maxTarballBytes = 128 << 20
	maxWasmBytes    = 16 << 20
	wasmEntry       = "package/fx-core.wasm"
)

var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)

func main() {
	version := flag.String("version", os.Getenv("LIBFX_VERSION"), "libfx version to pin (default: the current pin)")
	out := flag.String("out", "", "file to write (default: pin_gen.go beside this command's package)")
	tarball := flag.String("tarball", "", "local libfx-<version>.tgz to use instead of downloading")
	flag.Parse()
	if err := run(*version, *out, *tarball); err != nil {
		fmt.Fprintln(os.Stderr, "genpin:", err)
		os.Exit(1)
	}
}

func run(version, out, tarballPath string) error {
	if out == "" {
		out = defaultOut()
	}
	if version == "" {
		current, err := currentVersion(out)
		if err != nil {
			return fmt.Errorf("no -version given and the current pin is unreadable: %w", err)
		}
		version = current
	}
	if !versionPattern.MatchString(version) {
		return fmt.Errorf("invalid libfx version %q", version)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	meta, err := registryMetadata(ctx, version)
	if err != nil {
		return err
	}
	var tgz []byte
	if tarballPath != "" {
		tgz, err = os.ReadFile(tarballPath)
	} else {
		tgz, err = download(ctx, meta.Dist.Tarball, maxTarballBytes)
	}
	if err != nil {
		return fmt.Errorf("tarball: %w", err)
	}
	sum := sha512.Sum512(tgz)
	sri := "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
	if sri != meta.Dist.Integrity {
		return fmt.Errorf("tarball integrity mismatch: registry %s, downloaded %s", meta.Dist.Integrity, sri)
	}
	wasm, err := extractWasm(tgz)
	if err != nil {
		return err
	}
	wasmSum := sha256.Sum256(wasm)
	wasmSHA := hex.EncodeToString(wasmSum[:])

	mirror := fmt.Sprintf("https://cdn.jsdelivr.net/npm/libfx@%s/fx-core.wasm", version)
	if mirrored, err := download(ctx, mirror, maxWasmBytes); err != nil {
		fmt.Fprintf(os.Stderr, "genpin: WARN: jsDelivr mirror unavailable: %v\n", err)
	} else if s := sha256.Sum256(mirrored); hex.EncodeToString(s[:]) != wasmSHA {
		fmt.Fprintln(os.Stderr, "genpin: WARN: jsDelivr mirror serves a different fx-core.wasm; the fallback will refuse it")
	}

	imports, exports, err := describe(ctx, wasm)
	if err != nil {
		return err
	}
	src, err := render(pin{
		Version:     version,
		TarballURL:  meta.Dist.Tarball,
		TarballSRI:  sri,
		TarballSize: len(tgz),
		MirrorURL:   mirror,
		WasmSHA256:  wasmSHA,
		WasmSize:    len(wasm),
		Imports:     imports,
		Exports:     exports,
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, src, 0o644); err != nil {
		return err
	}
	fmt.Printf("pinned libfx %s\n  tarball %s (%d bytes)\n  integrity %s\n  fx-core.wasm sha256 %s (%d bytes)\n  %d imports written to %s\n",
		version, meta.Dist.Tarball, len(tgz), sri, wasmSHA, len(wasm), len(imports), out)
	fmt.Println("next: review the import diff, the fx-sdk.js host table diff, and run the fx wasm tests (see pin.go)")
	return nil
}

func defaultOut() string {
	if wd, err := os.Getwd(); err == nil {
		if filepath.Base(wd) == "libfx" {
			return filepath.Join(wd, "pin_gen.go")
		}
		if filepath.Base(wd) == "genpin" {
			return filepath.Join(filepath.Dir(wd), "pin_gen.go")
		}
	}
	return filepath.Join("pkg", "harness", "fx", "internal", "libfx", "pin_gen.go")
}

var currentVersionPattern = regexp.MustCompile(`(?m)^\s*Version\s*=\s*"([^"]+)"`)

func currentVersion(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	m := currentVersionPattern.FindSubmatch(data)
	if m == nil {
		return "", errors.New("no Version constant")
	}
	return string(m[1]), nil
}

type registryVersion struct {
	Dist struct {
		Tarball   string `json:"tarball"`
		Integrity string `json:"integrity"`
	} `json:"dist"`
}

func registryMetadata(ctx context.Context, version string) (*registryVersion, error) {
	data, err := download(ctx, "https://registry.npmjs.org/libfx/"+version, 4<<20)
	if err != nil {
		return nil, fmt.Errorf("registry metadata: %w", err)
	}
	var meta registryVersion
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("registry metadata: %w", err)
	}
	if !strings.HasPrefix(meta.Dist.Tarball, "https://registry.npmjs.org/") || !strings.HasPrefix(meta.Dist.Integrity, "sha512-") {
		return nil, fmt.Errorf("unexpected registry metadata: tarball %q integrity %q", meta.Dist.Tarball, meta.Dist.Integrity)
	}
	return &meta, nil
}

func download(ctx context.Context, url string, maxBytes int64) ([]byte, error) {
	opts := fileutil.DefaultDownloadOptions()
	opts.MaxSize = maxBytes
	opts.Timeout = 5 * time.Minute
	return fileutil.Download(ctx, url, opts)
}

func extractWasm(tgz []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		return nil, err
	}
	defer gz.Close() //nolint:errcheck
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("%s not found in tarball", wasmEntry)
		}
		if err != nil {
			return nil, err
		}
		if h.Name != wasmEntry {
			continue
		}
		if h.Typeflag != tar.TypeReg || h.Size > maxWasmBytes {
			return nil, fmt.Errorf("%s is not a regular file under %d bytes", wasmEntry, maxWasmBytes)
		}
		return io.ReadAll(io.LimitReader(tr, maxWasmBytes))
	}
}

type importDef struct {
	Module, Name    string
	Params, Results []string
}

func valueTypes(ts []api.ValueType) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		switch t {
		case api.ValueTypeI32:
			out[i] = "i32"
		case api.ValueTypeI64:
			out[i] = "i64"
		case api.ValueTypeF32:
			out[i] = "f32"
		case api.ValueTypeF64:
			out[i] = "f64"
		default:
			out[i] = fmt.Sprintf("0x%x", t)
		}
	}
	return out
}

func describe(ctx context.Context, wasm []byte) ([]importDef, []string, error) {
	// The interpreter compiles quickly and reads the same import and export
	// metadata; nothing is instantiated.
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigInterpreter())
	defer rt.Close(ctx) //nolint:errcheck
	cm, err := rt.CompileModule(ctx, wasm)
	if err != nil {
		return nil, nil, fmt.Errorf("compile fx-core.wasm: %w", err)
	}
	if len(cm.ImportedMemories()) > 0 {
		return nil, nil, errors.New("fx-core.wasm imports a memory; the host does not provide one")
	}
	var imports []importDef
	for _, d := range cm.ImportedFunctions() {
		module, name, _ := d.Import()
		for _, t := range d.ParamTypes() {
			if t != api.ValueTypeI32 && t != api.ValueTypeI64 {
				return nil, nil, fmt.Errorf("%s.%s takes a %s parameter; the host supports only i32 and i64", module, name, api.ValueTypeName(t))
			}
		}
		imports = append(imports, importDef{Module: module, Name: name, Params: valueTypes(d.ParamTypes()), Results: valueTypes(d.ResultTypes())})
	}
	var exports []string
	for name := range cm.ExportedFunctions() {
		exports = append(exports, "func "+name)
	}
	for name := range cm.ExportedMemories() {
		exports = append(exports, "memory "+name)
	}
	return imports, exports, nil
}

type pin struct {
	Version     string
	TarballURL  string
	TarballSRI  string
	TarballSize int
	MirrorURL   string
	WasmSHA256  string
	WasmSize    int
	Imports     []importDef
	Exports     []string
}

func render(p pin) ([]byte, error) {
	var b bytes.Buffer
	fmt.Fprintf(&b, "// Code generated by genpin; DO NOT EDIT.\n// Regenerate with: go run ./pkg/harness/fx/internal/libfx/genpin -version <version>\n\npackage libfx\n\n")
	fmt.Fprintf(&b, "import \"github.com/tetratelabs/wazero/api\"\n\n")
	fmt.Fprintf(&b, "const (\n")
	fmt.Fprintf(&b, "\t// Version is the pinned libfx npm release.\n\tVersion = %q\n", p.Version)
	fmt.Fprintf(&b, "\t// TarballURL is the npm registry tarball fx-core.wasm is extracted from.\n\tTarballURL = %q\n", p.TarballURL)
	fmt.Fprintf(&b, "\t// TarballSRI is the registry's dist.integrity for TarballURL.\n\tTarballSRI = %q\n", p.TarballSRI)
	fmt.Fprintf(&b, "\t// TarballSize is the size of TarballURL in bytes.\n\tTarballSize = %d\n", p.TarballSize)
	fmt.Fprintf(&b, "\t// MirrorURL serves the same fx-core.wasm alone; used when the registry fails.\n\tMirrorURL = %q\n", p.MirrorURL)
	fmt.Fprintf(&b, "\t// WasmSHA256 is the hex sha256 of package/fx-core.wasm.\n\tWasmSHA256 = %q\n", p.WasmSHA256)
	fmt.Fprintf(&b, "\t// WasmSize is the size of package/fx-core.wasm in bytes.\n\tWasmSize = %d\n", p.WasmSize)
	fmt.Fprintf(&b, ")\n\n")
	fmt.Fprintf(&b, "// expectedImports is every function fx-core.wasm %s imports, in module order.\nvar expectedImports = []importSig{\n", p.Version)
	for _, imp := range p.Imports {
		fmt.Fprintf(&b, "\t{%q, %q, %s, %s},\n", imp.Module, imp.Name, typeList(imp.Params), typeList(imp.Results))
	}
	fmt.Fprintf(&b, "}\n\n// expectedExports is what the host calls or reads.\nvar expectedExports = []string{\n")
	for _, e := range sorted(p.Exports) {
		fmt.Fprintf(&b, "\t%q,\n", e)
	}
	fmt.Fprintf(&b, "}\n")
	return format.Source(b.Bytes())
}

func typeList(ts []string) string {
	if len(ts) == 0 {
		return "nil"
	}
	parts := make([]string, len(ts))
	for i, t := range ts {
		parts[i] = "api.ValueType" + strings.ToUpper(t[:1]) + t[1:]
	}
	return "[]api.ValueType{" + strings.Join(parts, ", ") + "}"
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
