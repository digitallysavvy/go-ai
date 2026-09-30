#!/usr/bin/env bash
# Rebuilds qjs.wasm (the file embedded one directory up, via go:embed in
# runtime.go) from upstream fastschema/qjs's qjswasm/ C sources plus its
# pinned quickjs-ng submodule, with job-queue-quiescence.patch (this
# directory) applied on top. See ../README.vendor.md for why this patch
# exists (concurrent code-mode tool-approval batching) and the exact
# provenance of everything this script fetches.
#
# Requires: curl, tar, patch, shasum or sha256sum (all preinstalled on
# macOS/most Linux), and a running Docker daemon. Network access is
# required to fetch the pinned upstream sources and to pull the pinned
# wasi-sdk image; this script does not fall back to an unpinned image tag
# or to hand-editing the binary if either fetch fails, or if a downloaded
# source archive's sha256 doesn't match its pin -- it stops and reports the
# failure instead.
#
# Usage: pkg/internal/third_party/qjs/build/build.sh
# Output: pkg/internal/third_party/qjs/qjs.wasm (overwritten in place)

set -euo pipefail

QJS_COMMIT=461716f4f380f81ffd09378751f1812919cddbca
QJS_TARBALL_SHA256=d1d04dfa4a52b27f6f123138812bad9924a18bbc3268c025cbbd4169410c02b1

QUICKJS_NG_COMMIT=d01ca4491fb24ccfeccb4c7394e28a3b21fd5986
QUICKJS_NG_TARBALL_SHA256=40be727abd6f7d24911b6a33fc3d25603a3f89eee183f74ffe6ca8d5619d9922

# GitHub's codeload tarball-by-commit archives are content-addressed by the
# commit they're generated from and have been verified stable across
# repeated fetches (see ../README.vendor.md), but pin+verify their sha256
# here anyway rather than trusting TLS transport integrity alone.

# ghcr.io/webassembly/wasi-sdk:wasi-sdk-24, pinned by digest so the exact
# toolchain bytes are reproducible regardless of what "wasi-sdk-24" comes
# to mean later. Re-resolve deliberately (and update this pin) if the
# toolchain ever needs to change; never swap in an unpinned tag here.
WASI_SDK_IMAGE="ghcr.io/webassembly/wasi-sdk:wasi-sdk-24@sha256:ab1595b844d67f3e2a8b5f47c9983f5165e7ce58ca376685b2d6167e9e28a663"

DOCKER_BIN="${DOCKER_BIN:-docker}"

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

verify_sha256() {
  local file="$1" expected="$2" actual
  actual="$(sha256_file "$file")"
  if [ "$actual" != "$expected" ]; then
    echo "error: sha256 mismatch for $file: expected $expected, got $actual" >&2
    exit 1
  fi
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
QJS_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

echo "==> Fetching fastschema/qjs @ ${QJS_COMMIT} (qjswasm/ C sources) ..."
curl -sSL "https://codeload.github.com/fastschema/qjs/tar.gz/${QJS_COMMIT}" \
  -o "$WORK_DIR/qjs.tar.gz"
verify_sha256 "$WORK_DIR/qjs.tar.gz" "$QJS_TARBALL_SHA256"

echo "==> Fetching quickjs-ng/quickjs @ ${QUICKJS_NG_COMMIT} (vendored submodule) ..."
curl -sSL "https://codeload.github.com/quickjs-ng/quickjs/tar.gz/${QUICKJS_NG_COMMIT}" \
  -o "$WORK_DIR/quickjs-ng.tar.gz"
verify_sha256 "$WORK_DIR/quickjs-ng.tar.gz" "$QUICKJS_NG_TARBALL_SHA256"

mkdir -p "$WORK_DIR/extract"
tar -xzf "$WORK_DIR/qjs.tar.gz" -C "$WORK_DIR/extract"
tar -xzf "$WORK_DIR/quickjs-ng.tar.gz" -C "$WORK_DIR/extract"

BUILD_ROOT="$WORK_DIR/build-root"
mkdir -p "$BUILD_ROOT/qjswasm"
cp "$WORK_DIR"/extract/qjs-*/qjswasm/*.c \
   "$WORK_DIR"/extract/qjs-*/qjswasm/*.h \
   "$WORK_DIR"/extract/qjs-*/qjswasm/*.cmake \
   "$BUILD_ROOT/qjswasm/"
cp -R "$WORK_DIR"/extract/quickjs-* "$BUILD_ROOT/qjswasm/quickjs"

echo "==> Applying ${SCRIPT_DIR}/job-queue-quiescence.patch ..."
patch -p1 -d "$BUILD_ROOT" < "$SCRIPT_DIR/job-queue-quiescence.patch"

echo "==> Configuring (wasi-sdk image: ${WASI_SDK_IMAGE}) ..."
"$DOCKER_BIN" run --rm \
  -v "$BUILD_ROOT:/work" \
  -w /work/qjswasm/quickjs \
  "$WASI_SDK_IMAGE" \
  sh -lc '
    rm -rf build &&
    cmake -B build \
      -DQJS_BUILD_LIBC=ON \
      -DQJS_BUILD_CLI_WITH_MIMALLOC=OFF \
      -DCMAKE_TOOLCHAIN_FILE=/opt/wasi-sdk/share/cmake/wasi-sdk.cmake \
      -DCMAKE_PROJECT_INCLUDE=../qjswasm.cmake
  '

echo "==> Building the qjswasm target ..."
"$DOCKER_BIN" run --rm \
  -v "$BUILD_ROOT:/work" \
  -w /work/qjswasm/quickjs \
  "$WASI_SDK_IMAGE" \
  sh -lc 'make -C build qjswasm -j"$(nproc)"'

OUT="$QJS_DIR/qjs.wasm"
cp "$BUILD_ROOT/qjswasm/quickjs/build/qjswasm" "$OUT"

echo "==> Wrote ${OUT}"
sha256_file "$OUT" | { read -r h; echo "$h  $OUT"; }

cat <<'EOF'

NOTE: unlike upstream fastschema/qjs's own Makefile, this script does not
run `wasm-opt -O3` as a post-processing step: binaryen is not present in
the pinned wasi-sdk image, and this script intentionally does not reach
for an additional, unpinned tool just to shrink/optimize an already-working
binary. The output above is a correct, unoptimized-by-wasm-opt WASI
executable (confirmed against pkg/internal/third_party/qjs's own test
suite and pkg/codemode's tests -- see ../README.vendor.md).
EOF
