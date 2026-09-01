#!/usr/bin/env bash
# Refresh conformance-lock.json with the current git commit and tool pins.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
LOCK="$ROOT/conformance-lock.json"
PKG="$ROOT/conformance/package.json"

commit="$(git -C "$ROOT" rev-parse HEAD)"
sushi="$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['devDependencies']['fsh-sushi'])" "$PKG")"

python3 - "$LOCK" "$commit" "$sushi" <<'PY'
import json, sys
from pathlib import Path

path = Path(sys.argv[1])
commit, sushi = sys.argv[2], sys.argv[3]
data = json.loads(path.read_text()) if path.is_file() else {}
data.setdefault("igPackage", {})
data["igPackage"].update({
    "name": "haistack.fhir.r4",
    "version": "0.1.0",
    "canonical": "http://haistack.example.org/fhir",
})
data["fhirVersion"] = "4.0.1"
data.setdefault("tools", {})
data["tools"].update({
    "sushi": sushi,
    "node": "20",
})
data.setdefault("packages", {})
data["packages"]["hl7.fhir.r4.core"] = "4.0.1"
data["packages"].setdefault("hl7.terminology.r4", "7.3.0")
data["packages"].setdefault("hl7.fhir.uv.extensions.r4", "5.3.0")
data["packages"].setdefault("hl7.fhir.uv.tools.r4", "1.1.2")
data["gitCommit"] = commit
path.write_text(json.dumps(data, indent=2) + "\n")
print(f"wrote {path} gitCommit={commit}")
PY
