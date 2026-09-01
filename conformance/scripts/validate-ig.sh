#!/usr/bin/env bash
# Run the HL7 FHIR Validator against valid and invalid IG examples.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
CONF="$ROOT/conformance"
LOCK="$ROOT/conformance-lock.json"
TOOLS="$CONF/.tools"
RESOURCES="$CONF/fsh-generated/resources"
VALID="$CONF/examples/valid"
INVALID="$CONF/examples/invalid"

if [[ ! -d "$RESOURCES" ]]; then
  echo "IG resources missing; run conformance/scripts/build-ig.sh first" >&2
  exit 1
fi

VALIDATOR_VERSION="$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['tools']['fhirValidator'])" "$LOCK")"
JAR="$TOOLS/validator_cli-${VALIDATOR_VERSION}.jar"
mkdir -p "$TOOLS"

if [[ ! -f "$JAR" ]]; then
  url="https://github.com/hapifhir/org.hl7.fhir.core/releases/download/${VALIDATOR_VERSION}/validator_cli.jar"
  echo "downloading FHIR validator ${VALIDATOR_VERSION}"
  curl -fsSL -o "$JAR" "$url"
fi

python3 - "$JAR" "$RESOURCES" "$VALID" "$INVALID" <<'PY'
import json, subprocess, sys, tempfile
from pathlib import Path

jar, resources, valid_dir, invalid_dir = map(Path, sys.argv[1:])
base = [
    "java", "-jar", str(jar),
    "-version", "4.0.1",
    "-ig", str(resources),
    "-tx", "n/a",
]

def run_validator(source: Path, profile: str | None, out: Path) -> subprocess.CompletedProcess:
    cmd = base + [str(source), "-output", str(out)]
    if profile:
        cmd.extend(["-profile", profile])
    return subprocess.run(cmd, capture_output=True, text=True)

def issues_from_outcome(path: Path) -> list[dict]:
    if not path.is_file() or path.stat().st_size == 0:
        return []
    data = json.loads(path.read_text())
    return data.get("issue") or []

def issue_blob(issues: list[dict], stdout: str, stderr: str) -> str:
    texts = []
    for iss in issues:
        texts.append(str(iss.get("diagnostics") or ""))
        details = iss.get("details") or {}
        if isinstance(details, dict):
            texts.append(str(details.get("text") or ""))
        texts.extend(str(x) for x in (iss.get("expression") or []))
        texts.append(str(iss.get("code") or ""))
    parts = texts + [stdout, stderr]
    return "\n".join(parts).lower()

def error_issues(issues: list[dict]) -> list[dict]:
    return [i for i in issues if str(i.get("severity", "")).lower() in {"error", "fatal"}]

failures = []

for example in sorted(valid_dir.glob("*.json")):
    with tempfile.NamedTemporaryFile(suffix=".json", delete=False) as tmp:
        out = Path(tmp.name)
    proc = run_validator(example, None, out)
    issues = issues_from_outcome(out)
    out.unlink(missing_ok=True)
    errors = error_issues(issues)
    if errors:
        detail = json.dumps(errors, indent=2)
        failures.append(f"valid example {example.name} failed validation:\n{detail}\n{proc.stdout}\n{proc.stderr}")
        continue
    print(f"PASS valid {example.name}")

for example in sorted(invalid_dir.glob("*.json")):
    if example.name.endswith(".expected.json"):
        continue
    expected_path = example.with_name(example.stem + ".expected.json")
    if not expected_path.is_file():
        failures.append(f"missing expected sidecar for {example.name}")
        continue
    expected = json.loads(expected_path.read_text())
    with tempfile.NamedTemporaryFile(suffix=".json", delete=False) as tmp:
        out = Path(tmp.name)
    proc = run_validator(example, expected.get("profile"), out)
    issues = issues_from_outcome(out)
    blob = issue_blob(issues, proc.stdout, proc.stderr)
    out.unlink(missing_ok=True)
    errors = error_issues(issues)
    if not errors:
        failures.append(f"invalid example {example.name} unexpectedly passed")
        continue
    for needle in expected.get("expectedSubstrings") or []:
        if needle.lower() not in blob:
            failures.append(
                f"invalid example {example.name} did not mention {needle!r}:\n{proc.stdout}\n{proc.stderr}"
            )
            break
    else:
        print(f"PASS invalid {example.name} (failed as expected)")

if failures:
    print("\n".join(failures), file=sys.stderr)
    sys.exit(1)
print("all IG examples matched expected validator outcomes")
PY
