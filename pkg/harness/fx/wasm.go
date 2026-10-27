package fx

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/harness/fx/internal/libfx"
)

// LibfxVersion is the libfx release whose fx-core.wasm the libfx runtime is
// pinned to.
const LibfxVersion = libfx.Version

// LibfxWasmSHA256 is the pinned sha256 of fx-core.wasm.
const LibfxWasmSHA256 = libfx.WasmSHA256

// WasmSource says where the libfx runtime gets fx-core.wasm. The zero value
// downloads the pinned module from the npm registry on first use (falling
// back to the jsDelivr mirror), verifies the tarball's sha512 and the
// module's sha256, and caches it, with a compiled-module cache, under
// os.UserCacheDir()/go-ai/fx/<LibfxVersion>/.
//
// Precedence: Bytes, Path, a cache hit, URL, then the default download.
// Empty fields fall back to the GO_AI_FX_WASM_PATH, GO_AI_FX_WASM_URL,
// GO_AI_FX_CACHE_DIR and GO_AI_FX_OFFLINE environment variables.
//
// The libfx runtime is a preview: CreateFx does not use it yet, and the
// sandbox `fx acp` adapter stays the default.
type WasmSource struct {
	// Path is a local fx-core.wasm. Nothing is downloaded or cached.
	Path string
	// URL replaces the default download: a raw .wasm or an npm-style .tgz.
	// It is fetched through the SDK's SSRF-safe download client.
	URL string
	// Bytes is an in-memory module.
	Bytes []byte
	// CacheDir replaces os.UserCacheDir()/go-ai/fx as the cache root.
	CacheDir string
	// Offline never touches the network; a cache miss returns a
	// *WasmUnavailableError.
	Offline bool
	// AllowUnpinned accepts a module whose sha256 differs from
	// LibfxWasmSHA256 (development builds of fx-core). Such a module is never
	// cached. The import-table check still applies.
	AllowUnpinned bool
}

func (s *WasmSource) internal() libfx.Source {
	if s == nil {
		return libfx.Source{}
	}
	return libfx.Source{
		Path:          s.Path,
		URL:           s.URL,
		Bytes:         s.Bytes,
		CacheDir:      s.CacheDir,
		Offline:       s.Offline,
		AllowUnpinned: s.AllowUnpinned,
	}
}

// Errors from PrepareWasm, matched with errors.Is.
var (
	// ErrWasmUnavailable: no usable fx-core.wasm was found or fetched.
	ErrWasmUnavailable = libfx.ErrWasmUnavailable
	// ErrWasmIntegrity: a module or tarball did not match the pinned hash.
	ErrWasmIntegrity = libfx.ErrWasmIntegrity
	// ErrWasmInterface: the module's imports or exports differ from the
	// interface this SDK hosts.
	ErrWasmInterface = libfx.ErrWasmInterface
)

// WasmUnavailableError details ErrWasmUnavailable: the pinned version and
// hash, the cache path, and the underlying failure.
type WasmUnavailableError = libfx.UnavailableError

// WasmIntegrityError details ErrWasmIntegrity.
type WasmIntegrityError = libfx.IntegrityError

// WasmInterfaceError details ErrWasmInterface: added, removed and changed
// imports and missing exports.
type WasmInterfaceError = libfx.InterfaceError

// PrepareWasm resolves fx-core.wasm for the libfx runtime, downloading and
// caching it if needed, verifies it against the pin, compiles it and checks
// its import table. It never runs the module. Call it at boot, or in a
// build step, to pre-seed the cache; nothing is downloaded at import time.
func PrepareWasm(ctx context.Context, src *WasmSource) error {
	_, err := libfx.Prepare(ctx, src.internal(), 0)
	return err
}
