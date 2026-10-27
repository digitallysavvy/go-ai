package libfx

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
)

// Environment variables that override Source fields left empty.
const (
	EnvWasmPath = "GO_AI_FX_WASM_PATH"
	EnvWasmURL  = "GO_AI_FX_WASM_URL"
	EnvCacheDir = "GO_AI_FX_CACHE_DIR"
	EnvOffline  = "GO_AI_FX_OFFLINE"
)

const (
	wasmFileName    = "fx-core.wasm"
	tarballWasmPath = "package/fx-core.wasm"
	// maxWasmBytes caps every module read: file, URL, bytes or tar entry.
	maxWasmBytes = 16 << 20
	// maxTarballBytes caps the npm tarball download (21 MB for 0.0.14).
	maxTarballBytes = 64 << 20
	downloadTimeout = 3 * time.Minute
)

// maxTarballUnpackedBytes caps the decompressed tar stream (48 MB for
// 0.0.14). A URL tarball is not covered by the registry's sha512, so this
// bounds a gzip bomb ahead of the wanted entry. A var so tests can lower it.
var maxTarballUnpackedBytes int64 = 256 << 20

// ErrWasmUnavailable is matched (errors.Is) by *UnavailableError: no usable
// fx-core.wasm could be found or fetched.
var ErrWasmUnavailable = errors.New("libfx: fx-core.wasm is unavailable")

// ErrWasmIntegrity is matched (errors.Is) by *IntegrityError: a module or
// tarball did not match the pinned hash.
var ErrWasmIntegrity = errors.New("libfx: fx-core.wasm failed its integrity check")

// Source says where fx-core.wasm comes from. The zero value downloads the
// pinned module on first use and caches it. Precedence: Bytes, Path, a cache
// hit, URL, then the npm registry with the jsDelivr mirror as fallback.
// Every source is checked against the pinned sha256 unless AllowUnpinned is
// set; the import-table check always runs.
type Source struct {
	// Path is a local fx-core.wasm. Nothing is downloaded or cached.
	Path string
	// URL replaces the default download. It may serve a raw .wasm or an
	// npm-style .tgz holding package/fx-core.wasm (detected by content).
	// It goes through the SDK's SSRF-safe download client.
	URL string
	// Bytes is an in-memory module.
	Bytes []byte
	// CacheDir replaces os.UserCacheDir()/go-ai/fx as the cache root. The
	// module is cached under <CacheDir>/<Version>/.
	CacheDir string
	// Offline never touches the network: a cache miss is an
	// *UnavailableError.
	Offline bool
	// AllowUnpinned accepts a module whose sha256 differs from the pin, for
	// development builds of fx-core. Such a module is never cached.
	AllowUnpinned bool

	// download overrides the download options (tests serve from httptest).
	download func(fileutil.DownloadOptions) fileutil.DownloadOptions
	// tarballURL and mirrorURL override the pinned defaults (tests).
	tarballURL, mirrorURL string
}

// withEnv fills empty fields from the GO_AI_FX_* environment variables. A
// source set in code (Bytes, Path or URL) is never overridden by an
// environment source.
func (s Source) withEnv() Source {
	if s.Path == "" && len(s.Bytes) == 0 && s.URL == "" {
		s.Path = os.Getenv(EnvWasmPath)
		if s.Path == "" {
			s.URL = os.Getenv(EnvWasmURL)
		}
	}
	if s.CacheDir == "" {
		s.CacheDir = os.Getenv(EnvCacheDir)
	}
	if !s.Offline {
		switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvOffline))) {
		case "1", "true", "yes":
			s.Offline = true
		}
	}
	return s
}

// UnavailableError reports that no usable module was found. Its message names
// the pinned version and hash, the cache path, and how to fix it.
type UnavailableError struct {
	Version   string
	SHA256    string
	CachePath string // empty when no cache directory is usable
	Offline   bool
	Err       error // the underlying failure, when there was one
}

func (e *UnavailableError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "fx runtime: fx-core.wasm (libfx %s, sha256 %s) is unavailable", e.Version, e.SHA256)
	if e.Offline {
		b.WriteString(" and offline mode is on")
	}
	if e.Err != nil {
		fmt.Fprintf(&b, ": %v", e.Err)
	}
	b.WriteString(". To fix it: ")
	if e.CachePath != "" {
		fmt.Fprintf(&b, "place the pinned module at %s, ", e.CachePath)
	}
	fmt.Fprintf(&b, "set the wasm Path (or %s) to a local copy", EnvWasmPath)
	if e.Offline {
		fmt.Fprintf(&b, ", or turn off offline mode (%s)", EnvOffline)
	} else {
		b.WriteString(", or allow access to registry.npmjs.org or cdn.jsdelivr.net")
	}
	return b.String()
}

// Unwrap matches ErrWasmUnavailable and the underlying failure.
func (e *UnavailableError) Unwrap() []error {
	if e.Err == nil {
		return []error{ErrWasmUnavailable}
	}
	return []error{ErrWasmUnavailable, e.Err}
}

// IntegrityError reports a hash mismatch.
type IntegrityError struct {
	Source string // what was checked: a path, a URL or "bytes"
	Want   string
	Got    string
}

func (e *IntegrityError) Error() string {
	return fmt.Sprintf("fx runtime: %s does not match the pinned libfx %s hash: want %s, got %s", e.Source, Version, e.Want, e.Got)
}

// Unwrap matches ErrWasmIntegrity.
func (e *IntegrityError) Unwrap() error { return ErrWasmIntegrity }

// Module is a resolved, verified fx-core.wasm.
type Module struct {
	Bytes  []byte
	SHA256 string
	// Origin says where it came from: "bytes", "path", "cache", "url",
	// "registry" or "mirror".
	Origin string
	// CacheDir is the version directory used for the module and the
	// compilation cache, or "" when none is usable.
	CacheDir string
}

// Resolve finds, downloads if needed, verifies and caches fx-core.wasm. It is
// the only function in this package that touches the network, and only when
// no earlier source in the precedence order has the module.
func Resolve(ctx context.Context, src Source) (*Module, error) {
	src = src.withEnv()
	versionDir := cacheVersionDir(src.CacheDir)
	cachePath := ""
	if versionDir != "" {
		cachePath = filepath.Join(versionDir, wasmFileName)
	}

	if len(src.Bytes) > 0 {
		if len(src.Bytes) > maxWasmBytes {
			return nil, fmt.Errorf("fx runtime: wasm bytes exceed %d bytes", maxWasmBytes)
		}
		return verified(bytes.Clone(src.Bytes), "bytes", "bytes", versionDir, src.AllowUnpinned)
	}
	if src.Path != "" {
		data, err := readCapped(src.Path)
		if err != nil {
			return nil, &UnavailableError{Version: Version, SHA256: WasmSHA256, Err: err}
		}
		return verified(data, src.Path, "path", versionDir, src.AllowUnpinned)
	}
	if cachePath != "" {
		if data, err := readCapped(cachePath); err == nil {
			if sha256Hex(data) == WasmSHA256 {
				return &Module{Bytes: data, SHA256: WasmSHA256, Origin: "cache", CacheDir: versionDir}, nil
			}
			// A corrupt or swapped cache entry is removed and fetched again.
			_ = os.Remove(cachePath)
		}
	}
	if src.Offline {
		return nil, &UnavailableError{Version: Version, SHA256: WasmSHA256, CachePath: cachePath, Offline: true}
	}

	var mod *Module
	var err error
	if src.URL != "" {
		mod, err = fetchCustom(ctx, src, versionDir)
	} else {
		mod, err = fetchDefault(ctx, src, versionDir)
	}
	if err != nil {
		if errors.Is(err, ErrWasmIntegrity) {
			return nil, err
		}
		return nil, &UnavailableError{Version: Version, SHA256: WasmSHA256, CachePath: cachePath, Err: err}
	}
	if cachePath != "" && mod.SHA256 == WasmSHA256 {
		if werr := writeCache(versionDir, cachePath, mod.Bytes); werr != nil {
			// The module is still usable from memory.
			mod.CacheDir = ""
		}
	}
	return mod, nil
}

func verified(data []byte, label, origin, versionDir string, allowUnpinned bool) (*Module, error) {
	sum := sha256Hex(data)
	if sum != WasmSHA256 && !allowUnpinned {
		return nil, &IntegrityError{Source: label, Want: WasmSHA256, Got: sum}
	}
	return &Module{Bytes: data, SHA256: sum, Origin: origin, CacheDir: versionDir}, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func readCapped(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	data, err := io.ReadAll(io.LimitReader(f, maxWasmBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxWasmBytes {
		return nil, fmt.Errorf("%s exceeds %d bytes", path, maxWasmBytes)
	}
	return data, nil
}

// cacheRoot returns the cache root: override, os.UserCacheDir()/go-ai/fx,
// or os.TempDir()/go-ai-fx-<uid>; "" when none can be created.
func cacheVersionDir(override string) string {
	var candidates []string
	if override != "" {
		candidates = append(candidates, override)
	} else {
		if dir, err := os.UserCacheDir(); err == nil && dir != "" {
			candidates = append(candidates, filepath.Join(dir, "go-ai", "fx"))
		}
		candidates = append(candidates, filepath.Join(os.TempDir(), fmt.Sprintf("go-ai-fx-%d", os.Getuid())))
	}
	for _, root := range candidates {
		dir := filepath.Join(root, Version)
		if err := os.MkdirAll(dir, 0o700); err == nil {
			return dir
		}
	}
	return ""
}

// writeCache writes data to path atomically: a temp file in the same
// directory, then a rename. Concurrent processes at worst both download.
func writeCache(dir, path string, data []byte) error {
	f, err := os.CreateTemp(dir, ".fx-core-*.wasm")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) //nolint:errcheck
	if err := f.Chmod(0o600); err != nil {
		f.Close() //nolint:errcheck
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close() //nolint:errcheck
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s Source) downloadOptions(maxSize int64) fileutil.DownloadOptions {
	opts := fileutil.DefaultDownloadOptions()
	opts.MaxSize = maxSize
	opts.Timeout = downloadTimeout
	if s.download != nil {
		opts = s.download(opts)
	}
	return opts
}

// fetchDefault downloads the pinned tarball, checks its sha512 integrity,
// extracts fx-core.wasm and checks its sha256. When the registry fails it
// tries the single-file mirror, accepted only on a sha256 match.
func fetchDefault(ctx context.Context, src Source, versionDir string) (*Module, error) {
	tarballURL, mirrorURL := TarballURL, MirrorURL
	if src.tarballURL != "" {
		tarballURL = src.tarballURL
	}
	if src.mirrorURL != "" {
		mirrorURL = src.mirrorURL
	}
	data, regErr := fetchTarball(ctx, src, tarballURL, versionDir, TarballSRI)
	if regErr == nil {
		return verified(data, tarballURL, "registry", versionDir, false)
	}
	if errors.Is(regErr, ErrWasmIntegrity) {
		return nil, regErr
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(regErr, err)
	}
	data, mirErr := fileutil.Download(ctx, mirrorURL, src.downloadOptions(maxWasmBytes))
	if mirErr != nil {
		return nil, fmt.Errorf("registry: %w; mirror: %w", regErr, mirErr)
	}
	return verified(data, mirrorURL, "mirror", versionDir, false)
}

// fetchCustom downloads a caller-supplied URL: a raw module or a tarball.
func fetchCustom(ctx context.Context, src Source, versionDir string) (*Module, error) {
	tmp, err := tempFile(versionDir)
	if err != nil {
		return nil, err
	}
	defer cleanupTemp(tmp)
	if err := fileutil.DownloadToWriter(ctx, src.URL, tmp, src.downloadOptions(maxTarballBytes)); err != nil {
		return nil, err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	var magic [4]byte
	n, _ := io.ReadFull(tmp, magic[:])
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	var data []byte
	switch {
	case n >= 2 && magic[0] == 0x1f && magic[1] == 0x8b:
		data, err = extractWasm(tmp)
	case n == 4 && bytes.Equal(magic[:], []byte("\x00asm")):
		data, err = io.ReadAll(io.LimitReader(tmp, maxWasmBytes+1))
		if err == nil && len(data) > maxWasmBytes {
			err = fmt.Errorf("module exceeds %d bytes", maxWasmBytes)
		}
	default:
		err = errors.New("response is neither a wasm module nor a gzip tarball")
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", src.URL, err)
	}
	return verified(data, src.URL, "url", versionDir, src.AllowUnpinned)
}

// fetchTarball downloads a tarball to a temp file while hashing it, checks
// the sha512 SRI before reading any entry, then extracts fx-core.wasm.
func fetchTarball(ctx context.Context, src Source, url, versionDir, sri string) ([]byte, error) {
	tmp, err := tempFile(versionDir)
	if err != nil {
		return nil, err
	}
	defer cleanupTemp(tmp)
	h := sha512.New()
	if err := fileutil.DownloadToWriter(ctx, url, io.MultiWriter(tmp, h), src.downloadOptions(maxTarballBytes)); err != nil {
		return nil, err
	}
	if got := sriOf(h); got != sri {
		return nil, &IntegrityError{Source: url, Want: sri, Got: got}
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return extractWasm(tmp)
}

func sriOf(h hash.Hash) string {
	return "sha512-" + base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func tempFile(dir string) (*os.File, error) {
	if dir != "" {
		if f, err := os.CreateTemp(dir, ".download-*"); err == nil {
			return f, nil
		}
	}
	return os.CreateTemp("", "go-ai-fx-download-*")
}

func cleanupTemp(f *os.File) {
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
}

// extractWasm streams a gzip tarball and returns the regular-file entry
// named exactly package/fx-core.wasm, capped at maxWasmBytes. Nothing is
// written to disk, and at most maxTarballUnpackedBytes are decompressed.
func extractWasm(r io.Reader) ([]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer gz.Close() //nolint:errcheck
	tr := tar.NewReader(io.LimitReader(gz, maxTarballUnpackedBytes))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("%s not found in tarball", tarballWasmPath)
		}
		if err != nil {
			return nil, err
		}
		if h.Name != tarballWasmPath {
			continue
		}
		if h.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("%s is not a regular file", tarballWasmPath)
		}
		if h.Size > maxWasmBytes {
			return nil, fmt.Errorf("%s exceeds %d bytes", tarballWasmPath, maxWasmBytes)
		}
		return io.ReadAll(io.LimitReader(tr, maxWasmBytes))
	}
}

// defaultFetchClient is the HTTP client model and OAuth requests use when the
// caller supplies no Fetch: SSRF-safe dialing.
var defaultFetchClient = &http.Client{Transport: fileutil.SafeTransport()}
