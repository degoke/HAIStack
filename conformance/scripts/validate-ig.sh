#!/usr/bin/env bash
# Validate conformance IG examples with the built-in Go validator.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
exec go run "$ROOT/cmd/validate-ig"
