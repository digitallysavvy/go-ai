package libfx

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
)

// clearFxEnv keeps a developer's GO_AI_FX_* settings out of the tests.
func clearFxEnv(t *testing.T) {
	for _, k := range []string{EnvWasmPath, EnvWasmURL, EnvCacheDir, EnvOffline} {
		t.Setenv(k, "")
	}
}

// loopback lets a test Source download from httptest. The default options
// refuse loopback addresses (see TestResolveCustomURLUsesSSRFSafeClient).
func loopback(opts fileutil.DownloadOptions) fileutil.DownloadOptions {
	opts.URLValidator = nil
	opts.Transport = http.DefaultTransport
	return opts
}

func tarball(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write(data)
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func sriFor(data []byte) string {
	sum := sha512.Sum512(data)
	return "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
}

func TestResolveBytesAndPathArePinned(t *testing.T) {
	clearFxEnv(t)
	fake := []byte("\x00asm\x01\x00\x00\x00")
	_, err := Resolve(context.Background(), Source{Bytes: fake, CacheDir: t.TempDir()})
	var ie *IntegrityError
	if !errors.Is(err, ErrWasmIntegrity) || !errors.As(err, &ie) || ie.Source != "bytes" || ie.Want != WasmSHA256 {
		t.Fatalf("unpinned bytes: %v", err)
	}
	mod, err := Resolve(context.Background(), Source{Bytes: fake, AllowUnpinned: true, CacheDir: t.TempDir()})
	if err != nil || mod.Origin != "bytes" || mod.SHA256 == WasmSHA256 {
		t.Fatalf("allow unpinned: %+v %v", mod, err)
	}
	path := filepath.Join(t.TempDir(), "fx-core.wasm")
	_ = os.WriteFile(path, fake, 0o600)
	if _, err := Resolve(context.Background(), Source{Path: path, CacheDir: t.TempDir()}); !errors.Is(err, ErrWasmIntegrity) {
		t.Fatalf("unpinned path: %v", err)
	}
	t.Setenv(EnvWasmPath, path)
	if mod, err := Resolve(context.Background(), Source{AllowUnpinned: true, CacheDir: t.TempDir()}); err != nil || mod.Origin != "path" {
		t.Fatalf("env path: %+v %v", mod, err)
	}
	t.Setenv(EnvWasmPath, "")
	_, err = Resolve(context.Background(), Source{Path: filepath.Join(t.TempDir(), "missing.wasm")})
	if !errors.Is(err, ErrWasmUnavailable) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing path: %v", err)
	}
	big := filepath.Join(t.TempDir(), "big.wasm")
	_ = os.WriteFile(big, make([]byte, maxWasmBytes+1), 0o600)
	if _, err := Resolve(context.Background(), Source{Path: big, AllowUnpinned: true}); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized path: %v", err)
	}
}

func TestResolveOfflineMissIsTyped(t *testing.T) {
	clearFxEnv(t)
	dir := t.TempDir()
	_, err := Resolve(context.Background(), Source{Offline: true, CacheDir: dir})
	var ue *UnavailableError
	if !errors.Is(err, ErrWasmUnavailable) || !errors.As(err, &ue) || !ue.Offline {
		t.Fatalf("err = %v", err)
	}
	want := filepath.Join(dir, Version, "fx-core.wasm")
	for _, s := range []string{Version, WasmSHA256, want, EnvWasmPath, EnvOffline} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("message %q lacks %q", err, s)
		}
	}
	t.Setenv(EnvOffline, "1")
	t.Setenv(EnvCacheDir, dir)
	if _, err := Resolve(context.Background(), Source{}); !errors.As(err, &ue) || !ue.Offline || ue.CachePath != want {
		t.Fatalf("env offline: %v", err)
	}
}

func TestResolveCorruptCacheIsRemoved(t *testing.T) {
	clearFxEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, Version, "fx-core.wasm")
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, []byte("swapped"), 0o600)
	if _, err := Resolve(context.Background(), Source{Offline: true, CacheDir: dir}); !errors.Is(err, ErrWasmUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("corrupt cache entry was kept")
	}
}

func TestResolveCustomURLUsesSSRFSafeClient(t *testing.T) {
	clearFxEnv(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("\x00asm\x01\x00\x00\x00"))
	}))
	defer srv.Close()
	_, err := Resolve(context.Background(), Source{URL: srv.URL + "/fx-core.wasm", AllowUnpinned: true, CacheDir: t.TempDir()})
	if !errors.Is(err, ErrWasmUnavailable) || hits.Load() != 0 {
		t.Fatalf("loopback URL was fetched (%d hits): %v", hits.Load(), err)
	}
}

func TestResolveCustomURLRawAndTarball(t *testing.T) {
	clearFxEnv(t)
	fake := []byte("\x00asm\x01\x00\x00\x00fake")
	tgz := tarball(t, map[string][]byte{"package/README.md": []byte("x"), tarballWasmPath: fake})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/raw.wasm":
			_, _ = w.Write(fake)
		case "/pkg.tgz":
			_, _ = w.Write(tgz)
		case "/notwasm":
			_, _ = w.Write([]byte("<html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	for _, p := range []string{"/raw.wasm", "/pkg.tgz"} {
		mod, err := Resolve(context.Background(), Source{URL: srv.URL + p, AllowUnpinned: true, CacheDir: dir, download: loopback})
		if err != nil || mod.Origin != "url" || !bytes.Equal(mod.Bytes, fake) {
			t.Fatalf("%s: %+v %v", p, mod, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, Version, "fx-core.wasm")); !os.IsNotExist(err) {
		t.Fatal("an unpinned module was cached under the pinned name")
	}
	if _, err := Resolve(context.Background(), Source{URL: srv.URL + "/raw.wasm", CacheDir: dir, download: loopback}); !errors.Is(err, ErrWasmIntegrity) {
		t.Fatalf("pinned custom URL: %v", err)
	}
	if _, err := Resolve(context.Background(), Source{URL: srv.URL + "/notwasm", AllowUnpinned: true, CacheDir: dir, download: loopback}); !errors.Is(err, ErrWasmUnavailable) || !strings.Contains(err.Error(), "neither") {
		t.Fatalf("not wasm: %v", err)
	}
	if _, err := Resolve(context.Background(), Source{URL: srv.URL + "/missing", AllowUnpinned: true, CacheDir: dir, download: loopback}); !errors.Is(err, ErrWasmUnavailable) {
		t.Fatalf("404: %v", err)
	}
}

func TestResolveRegistryIntegrityAndMirrorFallback(t *testing.T) {
	clearFxEnv(t)
	tgz := tarball(t, map[string][]byte{tarballWasmPath: []byte("\x00asm\x01\x00\x00\x00")})
	var mirrorHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/libfx.tgz":
			_, _ = w.Write(tgz)
		case "/mirror.wasm":
			mirrorHits.Add(1)
			_, _ = w.Write([]byte("\x00asm\x01\x00\x00\x00other"))
		default:
			http.Error(w, "down", http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()

	// A tarball whose sha512 is not the pinned SRI is refused before any
	// entry is read, and the mirror is not tried.
	src := Source{CacheDir: t.TempDir(), download: loopback, tarballURL: srv.URL + "/libfx.tgz", mirrorURL: srv.URL + "/mirror.wasm"}
	_, err := Resolve(context.Background(), src)
	var ie *IntegrityError
	if !errors.As(err, &ie) || ie.Want != TarballSRI || ie.Got != sriFor(tgz) || mirrorHits.Load() != 0 {
		t.Fatalf("tarball integrity: %v (mirror hits %d)", err, mirrorHits.Load())
	}

	// Registry down: the mirror is tried and refused on a sha256 mismatch.
	src.tarballURL = srv.URL + "/down.tgz"
	_, err = Resolve(context.Background(), src)
	if !errors.As(err, &ie) || ie.Want != WasmSHA256 || mirrorHits.Load() != 1 {
		t.Fatalf("mirror integrity: %v", err)
	}

	// Both down: a typed unavailable error naming both failures.
	src.mirrorURL = srv.URL + "/down.wasm"
	_, err = Resolve(context.Background(), src)
	if !errors.Is(err, ErrWasmUnavailable) || !strings.Contains(err.Error(), "registry:") || !strings.Contains(err.Error(), "mirror:") {
		t.Fatalf("both down: %v", err)
	}
}

func TestExtractWasmRejectsNonRegularAndOversized(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: tarballWasmPath, Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"})
	_ = tw.Close()
	_ = gz.Close()
	if _, err := extractWasm(bytes.NewReader(buf.Bytes())); err == nil || !strings.Contains(err.Error(), "regular") {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := extractWasm(bytes.NewReader(tarball(t, map[string][]byte{"package/other.wasm": {1}}))); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing entry: %v", err)
	}
	if _, err := extractWasm(strings.NewReader("not gzip")); err == nil {
		t.Fatal("non-gzip accepted")
	}
}

// TestResolveDownloadsAndCachesThePinnedModule serves a tarball holding the
// real pinned module (from the cache) and checks the full download path.
func TestResolveDownloadsAndCachesThePinnedModule(t *testing.T) {
	clearFxEnv(t)
	real, err := Resolve(context.Background(), Source{Offline: true})
	if err != nil {
		t.Skipf("pinned module not cached: %v", err)
	}
	tgz := tarball(t, map[string][]byte{tarballWasmPath: real.Bytes})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(tgz) }))
	defer srv.Close()
	// The served tarball is not the npm one, so check its own SRI through a
	// custom URL (raw .tgz), then the cache.
	dir := t.TempDir()
	mod, err := Resolve(context.Background(), Source{URL: srv.URL + "/libfx.tgz", CacheDir: dir, download: loopback})
	if err != nil || mod.SHA256 != WasmSHA256 {
		t.Fatalf("download: %v", err)
	}
	path := filepath.Join(dir, Version, "fx-core.wasm")
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("cache file: %v %v", st, err)
	}
	if dst, _ := os.Stat(filepath.Dir(path)); dst.Mode().Perm() != 0o700 {
		t.Fatalf("cache dir mode %v", dst.Mode())
	}
	srv.Close()
	mod, err = Resolve(context.Background(), Source{CacheDir: dir, Offline: true})
	if err != nil || mod.Origin != "cache" {
		t.Fatalf("cache hit: %+v %v", mod, err)
	}
}

// A tarball whose entries before fx-core.wasm decompress past the cap is
// refused instead of being inflated without bound.
func TestExtractWasmBoundsDecompression(t *testing.T) {
	old := maxTarballUnpackedBytes
	maxTarballUnpackedBytes = 1 << 20
	t.Cleanup(func() { maxTarballUnpackedBytes = old })
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "package/aaa-padding", Mode: 0o644, Size: 2 << 20, Typeflag: tar.TypeReg})
	_, _ = tw.Write(make([]byte, 2<<20))
	_ = tw.WriteHeader(&tar.Header{Name: tarballWasmPath, Mode: 0o644, Size: 8, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("\x00asm\x01\x00\x00\x00"))
	_ = tw.Close()
	_ = gz.Close()
	if _, err := extractWasm(bytes.NewReader(buf.Bytes())); err == nil {
		t.Fatal("tarball past the decompression cap accepted")
	}
	maxTarballUnpackedBytes = old
	if got, err := extractWasm(bytes.NewReader(buf.Bytes())); err != nil || string(got) != "\x00asm\x01\x00\x00\x00" {
		t.Fatalf("within the cap: %q, %v", got, err)
	}
}

// An explicit URL is not overridden by GO_AI_FX_WASM_PATH, which would
// otherwise win by precedence (and, with AllowUnpinned, skip the pin).
func TestResolveEnvDoesNotOverrideExplicitSource(t *testing.T) {
	clearFxEnv(t)
	envModule := filepath.Join(t.TempDir(), "env.wasm")
	if err := os.WriteFile(envModule, []byte("\x00asm\x01\x00\x00\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvWasmPath, envModule)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("\x00asm\x01\x00\x00\x00\x00"))
	}))
	defer srv.Close()
	mod, err := Resolve(context.Background(), Source{URL: srv.URL + "/x.wasm", AllowUnpinned: true, CacheDir: t.TempDir(), download: loopback})
	if err != nil || mod.Origin != "url" {
		t.Fatalf("explicit URL lost to the env path: %+v, %v", mod, err)
	}
}
