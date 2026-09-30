#!/usr/bin/env bash
# Recursively build and vet every example under examples/.
#
# Covers three cases:
#   1. Regular `package main` directories in the root module — a single
#      `go build ./...` / `go vet ./...` from the repo root already compiles
#      all of these (Go module boundaries make this automatically skip case 2,
#      and the `//go:build ignore` tag makes it automatically skip case 3).
#   2. Nested example modules (a directory under examples/ with its own
#      go.mod) — built and vetted inside that module.
#   3. Standalone files tagged `//go:build ignore`. These are meant to be run
#      individually with `go run <file>.go`, so they're excluded from the
#      module build; each one is vetted on its own instead.
#
# Fails (and prints which example failed) on any error.

set -uo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root_dir"

fail=0

echo "==> go build ./... (root module, includes all examples/*/... packages)"
if ! go build ./...; then
  echo "FAILED: root module build"
  fail=1
fi

echo "==> go vet ./... (root module)"
if ! go vet ./...; then
  echo "FAILED: root module vet"
  fail=1
fi

echo "==> Building nested example modules"
while IFS= read -r modfile; do
  dir="$(dirname "$modfile")"
  echo "--> $dir"
  if ! (cd "$dir" && go build ./... && go vet ./...); then
    echo "FAILED: nested example module $dir"
    fail=1
  fi
done < <(find examples -name go.mod | sort)

echo "==> Vetting standalone //go:build ignore example files (run individually with 'go run')"
while IFS= read -r f; do
  if ! go vet "$f"; then
    echo "FAILED: $f"
    fail=1
  fi
done < <(grep -rl "go:build ignore" examples --include="*.go" | sort)

if [ "$fail" -ne 0 ]; then
  echo
  echo "One or more examples failed to build or vet. See FAILED lines above."
  exit 1
fi

echo
echo "All examples built and vetted successfully."
