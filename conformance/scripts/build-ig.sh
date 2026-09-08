#!/usr/bin/env bash
# Build the HAIStack FHIR IG with SUSHI and export compiled resources into
# modules/*/ig so pkg/modules can install from IG artefacts.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
CONF="$ROOT/conformance"
MAP="$CONF/module-map.json"

cd "$CONF"

if [[ ! -d node_modules/fsh-sushi ]]; then
  if [[ -f package-lock.json ]]; then
    npm ci
  else
    npm install
  fi
fi

# SUSHI reads FSH from input/fsh. Keep conformance/fsh as the source of truth.
mkdir -p input
ln -sfn ../fsh input/fsh

npx sushi .

RESOURCES="$CONF/fsh-generated/resources"
if [[ ! -d "$RESOURCES" ]]; then
  echo "sushi did not produce $RESOURCES" >&2
  exit 1
fi

python3 - "$MAP" "$RESOURCES" "$ROOT/modules" <<'PY'
import json, shutil, sys
from pathlib import Path

map_path, resources, modules = Path(sys.argv[1]), Path(sys.argv[2]), Path(sys.argv[3])
mapping = json.loads(map_path.read_text())
missing = []
for module, files in mapping.items():
    dest = modules / module / "ig"
    dest.mkdir(parents=True, exist_ok=True)
    for name in files:
        src = resources / name
        if not src.is_file():
            missing.append(str(src))
            continue
        shutil.copy2(src, dest / name)
    # Drop stale generated files that are no longer mapped.
    keep = set(files)
    for existing in dest.glob("*.json"):
        if existing.name not in keep:
            existing.unlink()
if missing:
    print("missing generated resources:", *missing, sep="\n  ", file=sys.stderr)
    sys.exit(1)
print("exported IG resources into modules/*/ig")
PY
