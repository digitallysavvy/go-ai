package fx

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPrepareWasmOfflineMiss(t *testing.T) {
	for _, k := range []string{"GO_AI_FX_WASM_PATH", "GO_AI_FX_WASM_URL", "GO_AI_FX_CACHE_DIR", "GO_AI_FX_OFFLINE"} {
		t.Setenv(k, "")
	}
	err := PrepareWasm(context.Background(), &WasmSource{Offline: true, CacheDir: t.TempDir()})
	var ue *WasmUnavailableError
	if !errors.Is(err, ErrWasmUnavailable) || !errors.As(err, &ue) || !strings.Contains(err.Error(), LibfxVersion) {
		t.Fatalf("err = %v", err)
	}
}

func TestPrepareWasmRejectsUnpinnedBytes(t *testing.T) {
	err := PrepareWasm(context.Background(), &WasmSource{Bytes: []byte("\x00asm\x01\x00\x00\x00")})
	var ie *WasmIntegrityError
	if !errors.Is(err, ErrWasmIntegrity) || !errors.As(err, &ie) || ie.Want != LibfxWasmSHA256 {
		t.Fatalf("err = %v", err)
	}
	// An unpinned module still goes through the import-table check.
	err = PrepareWasm(context.Background(), &WasmSource{Bytes: []byte("\x00asm\x01\x00\x00\x00"), AllowUnpinned: true, CacheDir: t.TempDir()})
	if !errors.Is(err, ErrWasmInterface) {
		t.Fatalf("empty module: %v", err)
	}
}
