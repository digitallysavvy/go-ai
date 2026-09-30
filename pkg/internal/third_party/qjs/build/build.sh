#!/usr/bin/env bash
# Rebuilds qjs.wasm (the file embedded one directory up, via go:embed in
# runtime.go) from upstream fastschema/qjs's qjswasm/ C sources plus its
# pinned quickjs-ng submodule, with job-queue-quiescence.patch (this
# directory) applied on top. See ../README.vendor.md for why this patch
# exists (concurrent code-mode tool-approval batching) and the exact
# provenance of everything this script fetches.
#
# Requires: curl, tar, patch (all preinstalled on macOS/most Linux), and a
# running Docker daemon. Network access is required to fetch the pinned
# upstream sources and to pull the pinned wasi-sdk image; this script does
# not fall back to an unpinned image tag or to hand-editing the binary if
# either fetch fails -- it stops and reports the failure instead.
#
# Usage: pkg/internal/third_party/qjs/build/build.sh
# Output: pkg/internal/third_party/qjs/qjs.wasm (overwritten in place)

set -euo pipefail

QJS_COMMIT=461716f4f380f81ffd09378751f1812919cddbca
QUICKJS_NG_COMMIT=d01ca4491fb24ccfeccb4c7394e28a3b21fd5986

# ghcr.io/webassembly/wasi-sdk:wasi-sdk-24, pinned by digest so the exact
# toolchain bytes are reproducible regardless of what "wasi-sdk-24" comes
# to mean later. Re-resolve deliberately (and update this pin) if the
# toolchain ever needs to change; never swap in an unpinned tag here.
WASI_SDK_IMAGE="ghcr.io/webassembly/wasi-sdk:wasi-sdk-24@sha256:ab1595b844d67f3e2a8b5f47c9983f5165e7ce58ca376685b2d6167e9e28a663"

DOCKER_BIN="${DOCKER_BIN:-docker}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
QJS_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

echo "==> Fetching fastschema/qjs @ ${QJS_COMMIT} (qjswasm/ C sources) ..."
curl -sSL "https://codeload.github.com/fastschema/qjs/tar.gz/${QJS_COMMIT}" \
  -o "$WORK_DIR/qjs.tar.gz"

echo "==> Fetching quickjs-ng/quickjs @ ${QUICKJS_NG_COMMIT} (vendored submodule) ..."
curl -sSL "https://codeload.github.com/quickjs-ng/quickjs/tar.gz/${QUICKJS_NG_COMMIT}" \
  -o "$WORK_DIR/quickjs-ng.tar.gz"

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
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum "$OUT"
else
  shasum -a 256 "$OUT"
fi

cat <<'EOF'

NOTE: unlike upstream fastschema/qjs's own Makefile, this script does not
run `wasm-opt -O3` as a post-processing step: binaryen is not present in
the pinned wasi-sdk image, and this script intentionally does not reach
for an additional, unpinned tool just to shrink/optimize an already-working
binary. The output above is a correct, unoptimized-by-wasm-opt WASI
executable (confirmed against pkg/internal/third_party/qjs's own test
suite and pkg/codemode's tests -- see ../README.vendor.md).
EOF
