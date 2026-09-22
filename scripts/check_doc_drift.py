"""Read-only upstream/documentation drift monitor. Never updates the reviewed lock.

--snapshot writes a PROPOSED fingerprint file for manual review, not approval.
--check fails on changes, missing sources, network errors, or invalid content.
"""
import argparse
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
from pathlib import Path
import re
from urllib.request import Request, urlopen

DOCS = "https://docs.typesafe.ai/"
PAGES = ["llms.txt", "introduction.md", "api.md", "models.md", "primitives.md",
         "primitives/choice.md", "primitives/score.md", "primitives/noul.md", "primitives/advanced.md",
         "sdk/python/usage.md", "sdk/python/changelog.md", "sdk/python/api/clients/sync.md",
         "sdk/python/api/clients/async.md", "sdk/python/api/types/questions.md",
         "sdk/python/api/types/responses.md", "sdk/python/api/retries.md",
         "sdk/python/api/exceptions.md", "sdk/python/api/constants.md"]
UPSTREAM = "https://api.github.com/repos/typesafe-ai/typesafe-sdk-python/commits/main"


def fingerprint(url):
    request = Request(url, headers={"User-Agent": "typesafe-sdk-go-documentation-audit", "Accept": "text/plain, application/json"})
    with urlopen(request, timeout=30) as response:
        content = response.read(8 * 1024 * 1024 + 1)
    if not content or len(content) > 8 * 1024 * 1024:
        raise ValueError("empty or oversized source: " + url)
    text = content.decode("utf-8").replace("\r\n", "\n").strip()
    if url == UPSTREAM:
        sha = json.loads(text)["sha"]
        if not re.fullmatch(r"[0-9a-f]{40}", sha):
            raise ValueError("invalid upstream revision")
        return url, {"commit": sha}
    if re.search(r"<(?:!doctype|html)\b", text[:500], re.I) or len(text) < 80:
        raise ValueError("expected nonempty Markdown/plain text, not HTML: " + url)
    return url, {"sha256": hashlib.sha256(text.encode("utf-8")).hexdigest()}


def compare(reviewed, current):
    if not reviewed or set(reviewed) != set(current):
        raise ValueError("reviewed source inventory is missing or changed")
    changed = [url for url in current if current[url] != reviewed[url]]
    if changed:
        raise ValueError("UPSTREAM CHANGED: review implementation, tests and examples before updating the lock:\n" + "\n".join(changed))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--lock", type=Path, default=Path("docs/parity-lock.json"))
    parser.add_argument("--snapshot", type=Path, required=True)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    urls = [DOCS + page for page in PAGES] + [UPSTREAM]
    with ThreadPoolExecutor(max_workers=4) as pool:
        current = dict(pool.map(fingerprint, urls))
    args.snapshot.parent.mkdir(parents=True, exist_ok=True)
    args.snapshot.write_text(json.dumps(current, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    if args.check:
        lock = json.loads(args.lock.read_text(encoding="utf-8"))
        compare(lock.get("sources"), current)
        print(f"PASS: {len(current)} reviewed source fingerprints unchanged")
    else:
        print(f"PROPOSED snapshot of {len(current)} sources; requires human review, not automatic alignment approval")


if __name__ == "__main__":
    main()
