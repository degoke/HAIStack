#!/usr/bin/env bash
# Optional local check: required section headings in pkg/*/README.md.
set -euo pipefail

fail=0

for readme in pkg/*/README.md; do
  pkg=$(basename "$(dirname "$readme")")
  missing=()

  grep -q '^## What it does' "$readme" || missing+=("What it does")
  grep -qE '^## How it fits in the ecosystem|^## Where it fits' "$readme" || missing+=("How it fits in the ecosystem")
  grep -q '^## When to use it' "$readme" || missing+=("When to use it")
  grep -q '^## Usage modes' "$readme" || missing+=("Usage modes")
  grep -qE '^## (Examples|Usage|Quick start|Go Usage|Basic usage)' "$readme" || missing+=("Examples/Usage")
  grep -qE '^## Limits' "$readme" || missing+=("Limits")
  grep -q '^## Related docs' "$readme" || missing+=("Related docs")

  if ((${#missing[@]} > 0)); then
    printf 'FAIL pkg/%s/README.md missing: %s\n' "$pkg" "${missing[*]}"
    fail=1
  fi

  # At least four ### subsections under Usage modes
  modes_block=$(awk '/^## Usage modes/{flag=1;next}/^## [^#]/{if(flag) exit} flag' "$readme")
  subcount=$(printf '%s\n' "$modes_block" | grep -c '^### ' || true)
  if ((subcount < 4)); then
    printf 'FAIL pkg/%s/README.md Usage modes has %s ### subsections (need >=4)\n' "$pkg" "$subcount"
    fail=1
  fi
done

if ((${fail} != 0)); then
  exit 1
fi

echo "OK: all pkg/*/README.md pass package README structure checks"
