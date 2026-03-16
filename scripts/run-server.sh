#!/usr/bin/env bash
set -euo pipefail

mkdir -p /tmp/go-cache /tmp/go-modcache

export GOCACHE="${GOCACHE:-/tmp/go-cache}"
export GOMODCACHE="${GOMODCACHE:-/tmp/go-modcache}"

# If GNAT's gcc is early in PATH, Go's cgo link step can pick it up and fail
# on newer distros. Prefer the system toolchain when available.
if [[ -z "${CC:-}" && -x /usr/bin/gcc ]]; then
  export CC=/usr/bin/gcc
fi
if [[ -z "${CXX:-}" && -x /usr/bin/g++ ]]; then
  export CXX=/usr/bin/g++
fi

exec go run ./cmd/server
