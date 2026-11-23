"""Exercise the actual inline write-token publisher with an offline fake API."""

import base64
from pathlib import Path
import textwrap
import unittest


WORKFLOW = Path(__file__).resolve().parents[1] / ".github/workflows/auto-tag-main.yml"
SOURCE = WORKFLOW.read_text().split("          python3 - <<'PY'\n", 1)[1].rsplit("          PY\n", 1)[0]
PUBLISHER = {"__name__": "workflow_under_test"}
exec(compile(textwrap.dedent(SOURCE), str(WORKFLOW), "exec"), PUBLISHER)
PUBLISH = PUBLISHER["publish"]
REPO = "PinableAgents/typesafe-sdk-go"
SHA = "a" * 40
PREVIOUS = "b" * 40
ANNOTATION = "c" * 40
OTHER = "d" * 40
ID = "12345"
RUN_PATH = "/actions/runs/" + ID
REF_MAIN = "/git/ref/heads/main"


class Fixture:
    def __init__(self):
        self.run = {"event": "push", "head_branch": "main", "status": "completed",
                    "conclusion": "success", "path": ".github/workflows/docs-parity.yml",
                    "repository": {"full_name": REPO}, "head_repository": {"full_name": REPO},
                    "head_sha": SHA}
        self.jobs = [{"name": n, "status": "completed", "conclusion": "success"} for n in
                     ("Full coverage (Go 1.23)", "Official Python SDK differential contract")]
        self.observed_job_count = 2
        self.version = "0.3.1"
        self.readme = "<!-- sdk-version: 0.3.1 -->"
        self.go_mod = f"module github.com/{REPO}\n\ngo 1.23\n"
        self.previous_releases = [{"ref": "refs/tags/v0.3.0"}, {"ref": "refs/tags/v2.0.0-pre.1"}]
        self.main = [SHA, SHA]
        self.existing = None
        self.ancestor = True
        self.writes = []
        self.calls = []

    def __call__(self, path, payload=None, missing_ok=False):
        self.calls.append(path)
        if payload is not None:
            self.writes.append((path, payload))
        if path == RUN_PATH:
            return self.run
        if path == RUN_PATH + "/jobs?filter=latest&per_page=100":
            return {"total_count": self.observed_job_count, "jobs": self.jobs}
        if path == REF_MAIN:
            return {"object": {"sha": self.main.pop(0)}}
        for filename, contents in (("types.go", f'const Version = "{self.version}"\n'),
                                   ("README.md", self.readme), ("go.mod", self.go_mod)):
            if path == f"/contents/{filename}?ref={SHA}":
                return {"encoding": "base64", "content": base64.b64encode(contents.encode())}
        if path == "/git/ref/tags/v" + self.version and missing_ok:
            return self.existing
        if path == "/git/tags/" + ANNOTATION:
            return {"object": {"type": "commit", "sha": PREVIOUS}}
        if path == f"/compare/{PREVIOUS}...{SHA}":
            return {"status": "ahead" if self.ancestor else "diverged",
                    "merge_base_commit": {"sha": PREVIOUS if self.ancestor else OTHER}}
        if path == "/git/matching-refs/tags/v":
            return self.previous_releases
        if path == "/git/tags" and payload:
            return {"sha": ANNOTATION}
        if path == "/git/refs" and payload:
            return {"ref": payload["ref"], "object": {"sha": payload["sha"]}}
        raise AssertionError(f"Unexpected API call: {path}")


class AutoTagTest(unittest.TestCase):
    def test_tags_exact_verified_new_bump(self):
        f = Fixture()
        self.assertEqual(PUBLISH(f, ID)[1:], ("v0.3.1", SHA))
        self.assertEqual([p for p, _ in f.writes], ["/git/tags", "/git/refs"])
        self.assertEqual(f.writes[0][1]["object"], SHA)
        self.assertEqual(f.writes[1][1], {"ref": "refs/tags/v0.3.1", "sha": ANNOTATION})

    def test_existing_version_on_main_is_noop_including_retry(self):
        for obj in ({"type": "tag", "sha": ANNOTATION}, {"type": "commit", "sha": SHA}):
            with self.subTest(obj=obj):
                f = Fixture()
                f.existing = {"object": obj}
                self.assertIsNone(PUBLISH(f, ID)[1])
                self.assertEqual(f.writes, [])

    def test_same_tag_outside_main_history_cannot_be_reassigned(self):
        f = Fixture()
        f.existing = {"object": {"type": "commit", "sha": PREVIOUS}}
        f.ancestor = False
        with self.assertRaisesRegex(RuntimeError, "outside verified main history"):
            PUBLISH(f, ID)
        self.assertEqual(f.writes, [])

    def test_wrong_origin_or_outdated_main_never_publishes(self):
        for field, value in (("event", "pull_request"), ("head_branch", "topic"),
                             ("path", ".github/workflows/other.yml"), ("conclusion", "failure"),
                             ("head_repository", {"full_name": "outside/typesafe-sdk-go"})):
            with self.subTest(field=field):
                f = Fixture()
                f.run[field] = value
                with self.assertRaisesRegex(RuntimeError, "trusted main"):
                    PUBLISH(f, ID)
                self.assertEqual(f.writes, [])
        f = Fixture()
        f.main[0] = OTHER
        self.assertIsNone(PUBLISH(f, ID)[1])
        self.assertEqual(f.writes, [])
        f = Fixture()
        f.main[1] = OTHER
        self.assertIsNone(PUBLISH(f, ID)[1])
        self.assertEqual([p for p, _ in f.writes], ["/git/tags"])

    def test_either_gate_or_incomplete_listing_blocks_publishing(self):
        for issue in ("missing-coverage", "missing-parity", "incomplete"):
            with self.subTest(issue=issue):
                f = Fixture()
                if issue == "incomplete":
                    f.observed_job_count += 1
                else:
                    f.jobs[0 if issue == "missing-coverage" else 1]["conclusion"] = "skipped"
                with self.assertRaisesRegex(RuntimeError, "checks|Incomplete"):
                    PUBLISH(f, ID)
                self.assertEqual(f.writes, [])

    def test_release_metadata_and_stable_order_cannot_be_bypassed(self):
        for field, value in (("version", "0.3.1-dev"), ("version", "02.0.0"),
                             ("version", "2.0.0"), ("readme", "no marker"), ("go_mod", "changed"),
                             ("go_mod", "module github.com/outside/sdk\n\ngo 1.23\n"),
                             ("previous_releases", [{"ref": "refs/tags/v0.4.0"}])):
            with self.subTest(field=field):
                f = Fixture()
                setattr(f, field, value)
                with self.assertRaisesRegex(RuntimeError, "SDK Version|README|Go |exceed"):
                    PUBLISH(f, ID)
                self.assertEqual(f.writes, [])


if __name__ == "__main__":
    unittest.main()
