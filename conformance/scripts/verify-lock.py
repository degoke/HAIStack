#!/usr/bin/env python3
"""Validate conformance-lock.json tool pins and, on main, the certified git commit."""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path


def load_json(path: Path) -> dict:
    return json.loads(path.read_text())


def verify_pins(lock: dict, package_json: Path) -> None:
    pkg = load_json(package_json)
    sushi = pkg["devDependencies"]["fsh-sushi"]
    if lock.get("tools", {}).get("sushi") != sushi:
        raise SystemExit(
            f"lock sushi {lock.get('tools', {}).get('sushi')} != package.json {sushi}"
        )


def verify_main_commit(lock: dict) -> None:
    commit = (lock.get("gitCommit") or "").strip()
    if not commit:
        raise SystemExit("conformance-lock.json gitCommit is empty")
    subprocess.check_call(["git", "cat-file", "-e", commit])
    head = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
    if commit != head:
        raise SystemExit(
            f"conformance-lock.json gitCommit {commit[:12]} must match HEAD {head[:12]} "
            "(run make conformance-lock on main after merge)"
        )


def verify_pr_no_lock_commit_change(base_ref: str, lock_path: Path) -> None:
    diff = subprocess.check_output(
        ["git", "diff", f"{base_ref}...HEAD", "--", str(lock_path)],
        text=True,
    )
    if not diff:
        return
    for line in diff.splitlines():
        if line.startswith(("+", "-")) and "gitCommit" in line:
            raise SystemExit(
                "do not update conformance-lock.json gitCommit in pull requests; "
                "main CI refreshes the certified commit after merge"
            )


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--mode", choices=("pr", "main"), required=True)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument("--base-ref", default="")
    args = parser.parse_args()

    lock_path = args.root / "conformance-lock.json"
    package_json = args.root / "conformance" / "package.json"
    lock = load_json(lock_path)

    verify_pins(lock, package_json)
    if args.mode == "pr":
        if args.base_ref:
            verify_pr_no_lock_commit_change(args.base_ref, lock_path)
        return

    verify_main_commit(lock)
    print("conformance-lock.json", json.dumps(lock, indent=2))


if __name__ == "__main__":
    main()
