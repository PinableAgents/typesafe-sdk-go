"""Compare real Go/Python SDK encoders and decoders using fixed mock HTTP only.

Install the official Python SDK at docs/parity-lock.json's pinned revision in CI.
No TypeSafe API key or connection is used. A matching corpus is finite evidence,
not a proof that every runtime behavior in two languages is identical.
"""
import argparse
import json
from pathlib import Path
import httpx2
from typesafe_sdk import Choice, Noul, Score, TypeSafeClient, RetryPolicy


def compare(corpus_path: Path, go_path: Path, output: Path) -> None:
    corpus = json.loads(corpus_path.read_text(encoding="utf-8"))
    go_results = json.loads(go_path.read_text(encoding="utf-8"))
    python_results = {}
    for case in corpus:
        captured = {"request": None}

        def transport(request):
            assert request.headers["authorization"] == "Bearer fixture-key"
            if case.get("method") == "GET":
                assert request.method == "GET" and request.url.path == "/v1/models"
            else:
                assert request.method == "POST" and request.url.path == "/v1/systemone"
                captured["request"] = json.loads(request.content)
                assert captured["request"] == case["expected_request"], case["name"]
            return httpx2.Response(200, json=case["response"], headers={"x-typesafe-request-id": "fixture-request"})

        with TypeSafeClient(api_key="fixture-key", base_url="https://fixture.invalid", model="jev-latest",
                            retry=RetryPolicy(max_retries=0), transport=httpx2.MockTransport(transport)) as client:
            if case.get("method") == "GET":
                result = client.models.list()
            else:
                questions = case["request"]["questions"]
                if case.get("typed"):
                    types = {"choice": Choice, "score": Score, "noul": Noul}
                    questions = {name: types[q["type"]](**{k: v for k, v in q.items() if k != "type"})
                                 for name, q in questions.items()}
                result = client.system_one(case["request"]["state"], questions,
                                           model=case.get("model"), extra_body=case.get("extra_body"))
            captured["response"] = result.model_dump(mode="json")
            assert captured["response"] == case["expected_response"], case["name"]
        assert captured == go_results[case["name"]], f"Go/Python mismatch: {case['name']}"
        python_results[case["name"]] = captured
    assert set(python_results) == set(go_results), "corpus case sets differ"
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps({"status": "PASS", "cases": len(corpus), "live_api": False,
                                  "results": python_results}, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"PASS: {len(corpus)}/{len(corpus)} Go / pinned official Python SDK contract cases; MOCK HTTP, no live inference")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--corpus", type=Path, default=Path("testdata/docs-contract.json"))
    parser.add_argument("--go", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    compare(args.corpus, args.go, args.output)
