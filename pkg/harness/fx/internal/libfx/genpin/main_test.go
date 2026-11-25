package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"strings"
	"testing"
)

func tgz(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A release whose tarball has no package/fx-core.wasm is refused.
func TestExtractWasmRefusesAReleaseWithoutTheModule(t *testing.T) {
	_, err := extractWasm(tgz(t, map[string][]byte{"package/index.js": []byte("x")}))
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
}

// Only _start and memory are pinned as exports; a module without a
// () -> () _start or an exported memory is refused.
func TestDescribePinsOnlyTheHostExports(t *testing.T) {
	// (module (memory (export "memory") 1) (func (export "_start"))
	//         (func (export "extra") (param i32)))
	ok := []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x08, 0x02, 0x60, 0x00, 0x00, 0x60, 0x01, 0x7f, 0x00, // types
		0x03, 0x03, 0x02, 0x00, 0x01, // funcs
		0x05, 0x03, 0x01, 0x00, 0x01, // memory
		0x07, 0x1b, 0x03,
		0x06, 'm', 'e', 'm', 'o', 'r', 'y', 0x02, 0x00,
		0x06, '_', 's', 't', 'a', 'r', 't', 0x00, 0x00,
		0x05, 'e', 'x', 't', 'r', 'a', 0x00, 0x01,
		0x0a, 0x07, 0x02, 0x02, 0x00, 0x0b, 0x02, 0x00, 0x0b, // code
	}
	_, exports, err := describe(context.Background(), ok)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(exports, ",") != "func _start,memory memory" {
		t.Fatalf("exports = %q", exports)
	}
	// (module (memory (export "memory") 1))
	noStart := []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x05, 0x03, 0x01, 0x00, 0x01,
		0x07, 0x0a, 0x01, 0x06, 'm', 'e', 'm', 'o', 'r', 'y', 0x02, 0x00,
	}
	if _, _, err := describe(context.Background(), noStart); err == nil || !strings.Contains(err.Error(), "_start") {
		t.Fatalf("no _start: %v", err)
	}
}
