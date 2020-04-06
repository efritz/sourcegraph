#!/usr/bin/env bash

# This script builds the symbols binary.

cd "$(dirname "${BASH_SOURCE[0]}")/../.."
set -eu

# Environment for building linux binaries
export GO111MODULE=on
export GOARCH=amd64
export GOOS=linux

# Get additional build args
. ./dev/libsqlite3-pcre/go-build-args.sh

echo "--- go build"
for pkg in github.com/sourcegraph/sourcegraph/cmd/symbols; do
    go build -trimpath -ldflags "-X github.com/sourcegraph/sourcegraph/internal/version.version=$VERSION" -buildmode exe -tags dist -o $OUTPUT/$(basename $pkg) $pkg
done
