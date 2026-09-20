#!/usr/bin/env python3
"""Run SDK validation without putting API keys in source files or CLI arguments.

Python 3.9+ and the Go toolchain required by the repository are needed.
Default: offline API tests (Go may still download its toolchain/dependencies).
--live: after offline checks pass, prompt securely or read TYPESAFE_API_KEY,
then run only the explicit component live suite (28 normal HTTP calls, cap 36).
"""
from __future__ import annotations

import argparse
import datetime as dt
import getpass
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time
from typing import Any

MODULE = "github.com/PinableAgents/typesafe-sdk-go"
KEY_ENV = "TYPESAFE_API_KEY"


def redact(text: str, key: str = "") -> str:
    if key:
        text = text.replace(key, "[REDACTED]")
    text = re.sub(r"apikey_[A-Za-z0-9_]+", "[REDACTED_API_KEY]", text)
    text = re.sub(r"(?i)(authorization\s*[:=]\s*)([^\r\n]+)", r"\1[REDACTED]", text)
    return text


def parse_events(text: str) -> dict[str, Any]:
    finished: dict[str, str] = {}
    for line in text.splitlines():
        try:
            event = json.loads(line)
        except (json.JSONDecodeError, TypeError):
            continue
        if isinstance(event, dict) and event.get("Test") and event.get("Action") in ("pass", "fail", "skip"):
            finished[str(event.get("Package", "")) + "/" + str(event["Test"])] = event["Action"]
    leaves = {name: action for name, action in finished.items()
              if not any(other.startswith(name + "/") for other in finished)}
    return {"leaf_pass": sum(v == "pass" for v in leaves.values()),
            "leaf_fail": sum(v == "fail" for v in leaves.values()),
            "leaf_skip": sum(v == "skip" for v in leaves.values()),
            "finished_tests": finished}


def execute(command: list[str], root: Path, env: dict[str, str], timeout: int) -> tuple[int, str]:
    """Capture in memory first, so logs can be redacted before writing."""
    kwargs: dict[str, Any] = {}
    if os.name == "posix":
        kwargs["start_new_session"] = True
    try:
        process = subprocess.Popen(command, cwd=root, env=env, stdout=subprocess.PIPE,
                                   stderr=subprocess.STDOUT, text=True, encoding="utf-8",
                                   errors="replace", **kwargs)
        try:
            text, _ = process.communicate(timeout=timeout)
            return process.returncode, text
        except subprocess.TimeoutExpired:
            if os.name == "posix":
                os.killpg(process.pid, signal.SIGKILL)
            else:
                subprocess.run(["taskkill", "/PID", str(process.pid), "/T", "/F"],
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
                process.kill()
            text, _ = process.communicate()
            return 124, text + "\nVALIDATION RUNNER: command deadline exceeded.\n"
    except OSError as exc:
        return 127, f"Could not start command ({type(exc).__name__}). Check PATH and permissions.\n"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    parser.add_argument("--output", type=Path)
    parser.add_argument("--live", action="store_true", help="Enable paid TypeSafe calls after local checks pass")
    parser.add_argument("--cross-build", action="store_true", help="Also compile Windows amd64 and macOS arm64 (no execution)")
    parser.add_argument("--source-label", default="working repository", help="Informational label; never substitutes for a Git SHA")
    args = parser.parse_args()
    root = args.repo.resolve()
    mod = root / "go.mod"
    suite = root / "validation" / "typesafe_components_test.go"
    if not mod.is_file() or not suite.is_file():
        parser.error("Run from the SDK root after copying the kit's validation/ and scripts/ directories.")
    module_text = mod.read_text(encoding="utf-8")
    if not re.search(r"(?m)^module\s+" + re.escape(MODULE) + r"\s*$", module_text):
        parser.error(f"Expected module {MODULE}; SDK files will not be rewritten automatically.")
    now = dt.datetime.now(dt.timezone.utc)
    out = args.output or (root / "validation-results" / now.strftime("%Y%m%dT%H%M%S.%fZ"))
    out = out.resolve()
    if out.exists() and any(out.iterdir()):
        parser.error("Output directory is not empty; choose a new path to avoid overwriting evidence.")
    out.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ)
    # Keep secrets out of subprocesses used for unit tests, builds, and metadata.
    key = env.pop(KEY_ENV, "").strip()
    env.pop("TYPESAFE_LIVE_TEST", None)
    env.pop("TYPESAFE_COMPONENTS_LIVE", None)
    env.pop("TYPESAFE_BASE_URL", None)
    env.pop("TYPESAFE_DEFAULT_MODEL", None)
    env["PYTHONIOENCODING"] = "utf-8"
    rc, sha = execute(["git", "rev-parse", "HEAD"], root, env, 10)
    rc2, dirty = execute(["git", "status", "--porcelain"], root, env, 10)
    declared = re.search(r"(?m)^go\s+(\S+)", module_text)
    results: list[dict[str, Any]] = []
    report: dict[str, Any] = {
        "started_at_utc": now.isoformat(), "source_label": args.source_label,
        "git_sha": sha.strip() if rc == 0 else None,
        "working_tree_dirty": bool(dirty.strip()) if rc2 == 0 else None,
        "module": MODULE, "declared_go_version": declared.group(1) if declared else None,
        "suite_sha256": hashlib.sha256(suite.read_bytes()).hexdigest(),
        "live_requested": args.live, "live_status": "NOT_REQUESTED", "results": results,
        "notes": ["Mock data is not model inference.", "A missing Git SHA is NOT a verified GitHub revision.",
                  "Offline means no TypeSafe calls; Go may download its toolchain/dependencies.",
                  "A small live sample is not an accuracy or security certification."],
    }

    def save() -> None:
        report["updated_at_utc"] = dt.datetime.now(dt.timezone.utc).isoformat()
        (out / "manifest.json").write_text(redact(json.dumps(report, ensure_ascii=False, indent=2), key), encoding="utf-8")

    def run(name: str, command: list[str], timeout: int = 300,
            extra_env: dict[str, str] | None = None, require_live: bool = False) -> bool:
        print(f"[{name}] {' '.join(command)}", flush=True)
        child_env = dict(env)
        if extra_env:
            child_env.update(extra_env)
        started = time.monotonic()
        code, raw = execute(command, root, child_env, timeout)
        text = redact(raw, key)
        counts = parse_events(text) if "-json" in command else None
        passed = code == 0
        if require_live:
            actions = (counts or {}).get("finished_tests", {})
            expected = MODULE + "/validation/TestTypeSafeComponentsLive"
            # Fail closed even when go test exits zero because all tests skipped.
            passed = passed and actions.get(expected) == "pass"
        logfile = name + ".log"
        (out / logfile).write_text(text, encoding="utf-8")
        item: dict[str, Any] = {"name": name, "command": command,
            "status": "PASS" if passed else "FAIL", "exit_code": code,
            "duration_seconds": round(time.monotonic() - started, 3), "log": logfile}
        if counts:
            item["tests"] = counts
        results.append(item)
        print(f"[{name}] {item['status']} -> {out / logfile}", flush=True)
        save()
        return passed

    save()
    if not run("01-go-version", ["go", "version"], 120):
        report["overall_status"] = "TOOLCHAIN_BLOCKED"
        report["live_status"] = "BLOCKED" if args.live else "NOT_REQUESTED"
        save()
        return 1
    offline = [
        ("02-unit", ["go", "test", "-count=1", "-json", "./..."], 600),
        ("03-vet", ["go", "vet", "./..."], 300),
        ("04-race", ["go", "test", "-race", "-count=1", "-json", "./..."], 900),
        ("05-coverage", ["go", "test", "-count=1", "-coverprofile=" + str(out / "coverage.out"), "./..."], 600),
        ("06-coverage-summary", ["go", "tool", "cover", "-func=" + str(out / "coverage.out")], 120),
        ("07-build", ["go", "build", "./..."], 300),
        ("08-mock-basic", ["go", "run", "./examples/basic", "-mock"], 180),
        ("09-mock-models", ["go", "run", "./examples/models", "-mock"], 180),
        ("10-mock-agent", ["go", "run", "./examples/agent", "-mock"], 180),
        ("10b-tool-definitions", ["go", "run", "./cmd/typesafe-tool", "--list"], 180),
    ]
    offline_ok = True
    for name, command, timeout in offline:
        offline_ok = run(name, command, timeout) and offline_ok
    if args.cross_build:
        for target_os, arch in (("windows", "amd64"), ("darwin", "arm64")):
            offline_ok = run("cross-" + target_os + "-" + arch, ["go", "build", "./..."], 600,
                             {"GOOS": target_os, "GOARCH": arch, "CGO_ENABLED": "0"}) and offline_ok
    live_ok = False
    if args.live and offline_ok:
        if not key:
            if sys.stdin.isatty():
                key = getpass.getpass("TypeSafe API Key (hidden, used in child environment only): ").strip()
            else:
                print("Live test blocked: set TYPESAFE_API_KEY in the environment or a CI secret.", flush=True)
        if key:
            model = os.environ.get("TYPESAFE_DEFAULT_MODEL", "jev-latest")
            live_ok = run("11-live-components", ["go", "test", "-count=1", "-json", "-timeout=10m",
                           "-run", "^TestTypeSafeComponentsLive$", "./validation"], 660,
                          {KEY_ENV: key, "TYPESAFE_COMPONENTS_LIVE": "1", "TYPESAFE_DEFAULT_MODEL": model}, True)
            report["live_status"] = "PASS" if live_ok else "FAIL"
        else:
            report["live_status"] = "BLOCKED_MISSING_KEY"
    elif args.live:
        report["live_status"] = "BLOCKED_OFFLINE_FAILURE"
    report["overall_status"] = (
        "LOCAL_AND_LIVE_CHECKS_PASS" if args.live and offline_ok and live_ok
        else "OFFLINE_CHECKS_PASS_LIVE_NOT_RUN" if not args.live and offline_ok
        else "NOT_FULLY_VALIDATED")
    save()
    summary = ["# TypeSafe validation result", "", f"Status: **{report['overall_status']}**", "",
               f"Git SHA: `{report['git_sha'] or 'UNAVAILABLE — not a pinned repository verification'}`", "",
               f"Source label: {args.source_label}", "", f"Live status: **{report['live_status']}**", "",
               "| Check | Status | Exit code |", "|---|---|---|"]
    summary += [f"| {r['name']} | {r['status']} | {r['exit_code']} |" for r in results]
    summary += ["", "See manifest.json and per-command logs. Mock results are not model accuracy.",
                "The live suite never executes Agent tools or modifies your project through the Agent."]
    (out / "summary.md").write_text(redact("\n".join(summary) + "\n", key), encoding="utf-8")
    print(report["overall_status"], flush=True)
    print(f"Report: {out / 'summary.md'}", flush=True)
    key = ""
    return 0 if offline_ok and (not args.live or live_ok) else 1


if __name__ == "__main__":
    raise SystemExit(main())
