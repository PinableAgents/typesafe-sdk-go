"""Offline release metadata guard and single-source pinned Python requirement.
No tag is created. Immutable released tags must never be moved to fix metadata.
"""
import argparse
import json
import os
from pathlib import Path
import re


def python_requirement(lock):
    sha = lock.get("python_commit", "")
    if not re.fullmatch(r"[0-9a-f]{40}", sha):
        raise ValueError("python_commit must be a full lowercase commit SHA")
    if not re.fullmatch(r"v\d+\.\d+\.\d+", lock.get("python_tag", "")):
        raise ValueError("python_tag must identify a reviewed release")
    return "git+https://github.com/typesafe-ai/typesafe-sdk-python.git@" + sha


def check(version_source, readme, ref=""):
    match = re.search(r'\bVersion\s*=\s*"([^"]+)"', version_source)
    if match is None or not re.fullmatch(r"\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?", match[1]):
        raise ValueError("missing or invalid SDK Version")
    version = match[1]
    if f"<!-- sdk-version: {version} -->" not in readme:
        raise ValueError("README and SDK Version disagree")
    if ref.startswith("refs/tags/") and ref != "refs/tags/v" + version:
        raise ValueError("release tag and SDK Version disagree")
    return version


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--python-requirement", action="store_true")
    parser.add_argument("--root", type=Path, default=Path("."))
    args = parser.parse_args()
    lock = json.loads((args.root / "docs/parity-lock.json").read_text())
    requirement = python_requirement(lock)
    if args.python_requirement:
        print(requirement)
    else:
        version = check((args.root / "types.go").read_text(), (args.root / "README.md").read_text(), os.getenv("GITHUB_REF", ""))
        print(f"PASS: SDK {version}; reviewed Python {lock['python_tag']} / {lock['python_commit']}")
