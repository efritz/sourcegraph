#!/usr/bin/env bash

# This script runs the codeintel-qa tests against a running server.
# This script is invoked by ./dev/ci/integration/run-integration.sh after running an instance.

cd "$(dirname "${BASH_SOURCE[0]}")/../../../.."
root_dir=$(pwd)
set -e

export SOURCEGRAPH_BASE_URL="${1:-"http://localhost:7080"}"

echo '--- initializing Sourcegraph instance'

pushd internal/cmd/init-sg
go build -o "${root_dir}/init-sg"
popd

pushd dev/ci/integration/code-intel
"${root_dir}/init-sg" initSG
# Disable `-x` to avoid printing secrets
set +x
# shellcheck disable=SC1091
source /root/.sg_envrc
"${root_dir}/init-sg" addRepos -config repos.json
popd

pushd dev/codeintel-qa

echo "--- :brain: Running the test suite"
echo '--- :zero: downloading test data from GCS'
go run ./cmd/download
echo '--- :one: clearing existing state'
go run ./cmd/clear
echo '--- :two: Disabling LSIF -> SCIP migration'
# Disable migration #19 (LSIF -> SCIP)
"${root_dir}/init-sg" oobmigration -id T3V0T2ZCYW5kTWlncmF0aW9uOjE5 -down
echo '--- :three: integration test ./dev/codeintel-qa/cmd/upload'
go run ./cmd/upload --timeout=5m
echo '--- :four: integration test ./dev/codeintel-qa/cmd/query'
go run ./cmd/query
echo '--- :five: Running LSIF -> SCIP migration'
# Enable migration #19 (LSIF -> SCIP) and wait for it to complete
"${root_dir}/init-sg" oobmigration -id T3V0T2ZCYW5kTWlncmF0aW9uOjE5
echo '--- :six: integration test ./dev/codeintel-qa/cmd/query'
go run ./cmd/query
popd
