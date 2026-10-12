"""Real pinned Python SDK vs Go: invalid input, explicit float compatibility,
HTTP errors/retries, connection failures, timeouts and cancellation. Mock only.
"""
import argparse
import asyncio
import json
from pathlib import Path
import httpx2
from typesafe_sdk import (Choice, Noul, Score, RetryPolicy, TypeSafeClient,
                         TypeSafeError, TypeSafeAPIError,
                         TypeSafeAPIConnectionError, TypeSafeAPITimeoutError)


def expand(value):
    if isinstance(value, dict):
        if "$nonfinite" in value:
            return float(value["$nonfinite"])
        return {k: expand(v) for k, v in value.items()}
    if isinstance(value, list):
        return [expand(v) for v in value]
    return value


def compare(corpus, go_results):
    results = {}
    for case in corpus:
        observed = {"calls": 0, "error": "", "request": None, "retry_counts": []}
        def transport(request):
            observed["calls"] += 1
            observed["retry_counts"].append(request.headers.get("x-typesafe-retry-count", ""))
            observed["request"] = json.loads(request.content)
            mode = case.get("transport_error")
            if mode == "connection":
                raise httpx2.ConnectError("synthetic connection failure")
            if mode == "timeout":
                raise httpx2.ReadTimeout("synthetic timeout")
            if mode == "canceled":
                raise asyncio.CancelledError()
            statuses = case.get("statuses", [200])
            status = statuses[min(observed["calls"] - 1, len(statuses) - 1)]
            body = {"model": "mock", "usage": {}, "answers": {"q": {"type": "noul", "noul": 0}}}
            if status != 200:
                body = {"message": "fixture error"}
            return httpx2.Response(status, json=body, headers={"retry-after-ms": "0"})
        try:
            retry = RetryPolicy(max_retries=case.get("retries", 0), backoff_initial=0, backoff_max=0)
            with TypeSafeClient(api_key="fixture-key", transport=httpx2.MockTransport(transport), retry=retry) as client:
                q = expand(case["question"])
                if case.get("typed"):
                    cls = {"choice": Choice, "score": Score, "noul": Noul}[q["type"]]
                    q = cls(**{k: v for k, v in q.items() if k != "type"})
                client.system_one(expand(case.get("state", "fixture state")), {"q": q},
                                  extra_body=expand(case.get("extra_body")))
        except asyncio.CancelledError:
            observed["error"] = "canceled"
        except TypeSafeAPITimeoutError:
            observed["error"] = "timeout"
        except TypeSafeAPIConnectionError:
            observed["error"] = "connection"
        except TypeSafeAPIError as exc:
            observed["error"] = f"http_{exc.status}"
        except TypeSafeError:
            observed["error"] = "invalid_request"
        assert observed["error"] == case["error"], (case["name"], observed)
        assert observed["calls"] == case["calls"], (case["name"], observed)
        assert observed == go_results[case["name"]], (case["name"], observed, go_results[case["name"]])
        results[case["name"]] = observed
    assert set(results) == set(go_results), "case sets differ"
    return {"status": "PASS", "cases": len(results), "live_api": False, "results": results}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--corpus", type=Path, default=Path("testdata/python074-behavior.json"))
    parser.add_argument("--go", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    result = compare(json.loads(args.corpus.read_text()), json.loads(args.go.read_text()))
    args.output.write_text(json.dumps(result, indent=2, ensure_ascii=False) + "\n")
    print(f"PASS: {result['cases']} Python v0.7.4 / Go behavior cases; no live inference")
