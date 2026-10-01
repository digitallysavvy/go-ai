#!/usr/bin/env bash
# Rebuilds qjs.wasm (the file embedded one directory up, via go:embed in
# runtime.go) from upstream fastschema/qjs's qjswasm/ C sources plus its
# pinned quickjs-ng submodule, with job-queue-quiescence.patch and then
# disable-modules.patch (both in this directory) applied on top, in that
# order. See ../README.vendor.md for why these patches exist (concurrent
# code-mode tool-approval batching; rejecting every module import for
# code-mode's sandbox) and the exact provenance of everything this script
# fetches.
#
# Requires: curl, tar, patch, shasum or sha256sum (all preinstalled on
# macOS/most Linux), and a running Docker daemon. Network access is
# required to fetch the pinned upstream sources, the pinned wasi-sdk image,
# and the pinned binaryen release; this script does not fall back to an
# unpinned image tag/version or to hand-editing the binary if any fetch
# fails, or if a downloaded archive's sha256 doesn't match its pin -- it
# stops and reports the failure instead.
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

# Pinned binaryen release used to run `wasm-opt -O3` on the built binary
# (see "qjs.wasm rebuild" in ../README.vendor.md for why this step exists:
# without it, this vendor copy's binary is ~40% bigger than the
# wasm-opt'd binary upstream's own Makefile produces). Each platform
# asset's sha256 below is the release's own published
# binaryen-<version>-<platform>.tar.gz.sha256 sidecar, copied here as a
# pin rather than fetched at build time (so a compromised/rotated sidecar
# can't silently change what this script trusts).
BINARYEN_VERSION="version_133"
sha256_for_binaryen_asset() {
  case "$1" in
    arm64-macos) echo "ad66da82ac13f163e424b1643f16c6dfcccc98b5966296b43e52d3cab04f84a8" ;;
    x86_64-macos) echo "13a9b90be775c6389ce3d1f879cb8627bea56708ba8c122983941d53a8199b95" ;;
    x86_64-linux) echo "2dc9c7813f5375db93d96ead4b78222fcc3e2677bbb832297af4797782a37489" ;;
    aarch64-linux) echo "89c07ea56faf38d0fbecf36ca8ec0721756716185f265b568e133d427f299bf8" ;;
    *) return 1 ;;
  esac
}

# Maps `uname -s`/`uname -m` to a binaryen release asset name above.
# wasm-opt runs on the host (not inside the wasi-sdk container -- it's not
# a WASI toolchain component, it's a native binary that transforms a .wasm
# file), so it must match the host this script runs on, not the container.
binaryen_asset_for_host() {
  local os arch
  os="$(uname -s)"
  arch="$(uname -m)"
  case "$os-$arch" in
    Darwin-arm64) echo "arm64-macos" ;;
    Darwin-x86_64) echo "x86_64-macos" ;;
    Linux-x86_64) echo "x86_64-linux" ;;
    Linux-aarch64) echo "aarch64-linux" ;;
    *)
      echo "error: no pinned binaryen release for host '$os-$arch' -- add one to build.sh (sha256_for_binaryen_asset/binaryen_asset_for_host) rather than falling back to an unpinned download" >&2
      return 1
      ;;
  esac
}

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

DOCKER_BIN="${DOCKER_BIN:-docker}"

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

echo "==> Applying ${SCRIPT_DIR}/disable-modules.patch ..."
patch -p1 -d "$BUILD_ROOT" < "$SCRIPT_DIR/disable-modules.patch"

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

UNOPT="$WORK_DIR/qjswasm.unopt.wasm"
cp "$BUILD_ROOT/qjswasm/quickjs/build/qjswasm" "$UNOPT"

echo "==> Running wasm-opt -O3 (binaryen ${BINARYEN_VERSION}) ..."
BINARYEN_ASSET="$(binaryen_asset_for_host)"
BINARYEN_SHA256="$(sha256_for_binaryen_asset "$BINARYEN_ASSET")"
curl -sSL \
  "https://github.com/WebAssembly/binaryen/releases/download/${BINARYEN_VERSION}/binaryen-${BINARYEN_VERSION}-${BINARYEN_ASSET}.tar.gz" \
  -o "$WORK_DIR/binaryen.tar.gz"
verify_sha256 "$WORK_DIR/binaryen.tar.gz" "$BINARYEN_SHA256"
mkdir -p "$WORK_DIR/binaryen"
tar -xzf "$WORK_DIR/binaryen.tar.gz" -C "$WORK_DIR/binaryen" --strip-components=1

OUT="$QJS_DIR/qjs.wasm"
"$WORK_DIR/binaryen/bin/wasm-opt" -O3 -o "$OUT" "$UNOPT"

echo "==> Wrote ${OUT}"
sha256_file "$OUT" | { read -r h; echo "$h  $OUT"; }

cat <<'EOF'

NOTE: the binary above has been run through `wasm-opt -O3` (pinned
binaryen release, sha256-verified -- see BINARYEN_VERSION above), matching
what upstream fastschema/qjs's own Makefile does as a post-processing
step. This keeps qjs.wasm's size close to upstream's own build despite
this vendor copy's additional exports (see "qjs.wasm rebuild" in
../README.vendor.md for size/perf numbers).
EOF
