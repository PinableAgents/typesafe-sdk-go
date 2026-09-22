"""Exact, unfiltered Go statement gate with fresh source-denominator verification.

Coverpkg profiles may contain the same block from several package test binaries.
Counters must be SUMMED by block, not rejected independently and not rounded.
"""
import argparse
import collections
import json
from pathlib import Path
import re
import subprocess
import tempfile


def parse_profile(text):
    lines = text.splitlines()
    if not lines or lines[0] not in ("mode: atomic", "mode: count", "mode: set"):
        raise ValueError("missing or invalid coverage mode")
    blocks = {}
    for line in lines[1:]:
        if not line.strip():
            continue
        fields = line.split()
        if len(fields) != 3 or not re.fullmatch(r".+:\d+\.\d+,\d+\.\d+", fields[0]):
            raise ValueError("invalid coverage row")
        key, size, counter = fields[0], int(fields[1]), int(fields[2])
        if size < 0 or counter < 0:
            raise ValueError("negative coverage values")
        previous = blocks.get(key, (size, 0))
        if previous[0] != size:
            raise ValueError("inconsistent statement count for duplicate block")
        blocks[key] = (size, previous[1] + counter)
    if not blocks or sum(size for size, _ in blocks.values()) == 0:
        raise ValueError("empty coverage denominator")
    return blocks


def expected_blocks(root):
    """Instrument every Go source included by go list ./..., without exclusions."""
    output = subprocess.check_output(["go", "list", "-json", "./..."], cwd=root, text=True)
    decoder, packages = json.JSONDecoder(), []
    while output.strip():
        package, end = decoder.raw_decode(output.lstrip())
        packages.append(package)
        output = output.lstrip()[end:]
    expected = {}
    with tempfile.TemporaryDirectory(prefix="typesafe-cover-") as temp:
        target = Path(temp) / "instrumented.go"
        for package in packages:
            for filename in package.get("GoFiles", []) + package.get("CgoFiles", []):
                source = Path(package["Dir"]) / filename
                subprocess.run(["go", "tool", "cover", "-mode=count", "-var=CoverageAudit",
                                "-o=" + str(target), str(source)], cwd=root, check=True, capture_output=True)
                code = target.read_text(encoding="utf-8")
                position_section = re.search(r"Pos:\s*\[[^]]+\]uint32\{(.*?)\n\s*\},", code, re.S)
                count_section = re.search(r"NumStmt:\s*\[[^]]+\]uint16\{(.*?)\n\s*\},", code, re.S)
                if position_section is None or count_section is None:
                    raise ValueError("unrecognized go cover instrumentation format: " + str(source))
                positions = re.findall(r"(\d+),\s*(\d+),\s*(0x[\da-f]+|\d+),", position_section[1])
                sizes = re.findall(r"^\s*(\d+),", count_section[1], re.M)
                if len(positions) != len(sizes):
                    raise ValueError("instrumentation block count mismatch")
                for (start, end, columns), size in zip(positions, sizes):
                    packed = int(columns, 0)
                    key = f"{package['ImportPath']}/{filename}:{start}.{packed & 65535},{end}.{packed >> 16}"
                    expected[key] = int(size)
    return expected


def enforce(blocks, expected=None):
    if expected is not None and {key: size for key, (size, _) in blocks.items()} != expected:
        missing = sorted(set(expected) - set(blocks))
        extra = sorted(set(blocks) - set(expected))
        raise ValueError(f"coverage does not match ALL compiled source blocks; missing={missing}; extra={extra}")
    missed = [key for key, (size, counter) in blocks.items() if size and not counter]
    if missed:
        raise ValueError("uncovered statements (exact 100% required):\n" + "\n".join(missed))
    total = sum(size for size, _ in blocks.values())
    if total <= 0:
        raise ValueError("empty denominator")
    packages = collections.defaultdict(int)
    for key, (size, _) in blocks.items():
        packages[key.rsplit("/", 1)[0]] += size
    return {"status": "PASS", "covered": total, "statements": total, "percent": 100,
            "excluded_files": [], "package_statements": dict(sorted(packages.items()))}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("profile", type=Path)
    parser.add_argument("--root", type=Path, default=Path("."))
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    result = enforce(parse_profile(args.profile.read_text(encoding="utf-8")), expected_blocks(args.root.resolve()))
    text = json.dumps(result, indent=2) + "\n"
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(text, encoding="utf-8")
    print(text, end="")


if __name__ == "__main__":
    main()
