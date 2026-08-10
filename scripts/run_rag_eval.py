#!/usr/bin/env python3
"""Run GoAI RAG evaluation cases through the HTTP API."""

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any

DEFAULT_BASE_URL = "http://localhost:9090"
DEFAULT_GOAI_TOKEN = ""
DEFAULT_INPUT = "eval/rag_eval_cases.json"
DEFAULT_OUTPUT = "eval/rag_eval_result.json"
SUCCESS_STATUS_CODE = 1000
DEFAULT_BATCH_SIZE = 5
DEFAULT_TIMEOUT_SECONDS = 900


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Batch call /api/v2/AI/rag/evaluate and save the result."
    )
    parser.add_argument(
        "--base-url",
        default=os.getenv("GOAI_BASE_URL", DEFAULT_BASE_URL),
        help=f"GoAI server base URL. Defaults to GOAI_BASE_URL or {DEFAULT_BASE_URL}.",
    )
    parser.add_argument(
        "--token",
        default=os.getenv("GOAI_TOKEN", DEFAULT_GOAI_TOKEN),
        help=f"JWT token. Defaults to GOAI_TOKEN or {DEFAULT_GOAI_TOKEN}.",
    )
    parser.add_argument(
        "--batch-size",
        type=int,
        default=DEFAULT_BATCH_SIZE,
        help=f"Cases per API request. Defaults to {DEFAULT_BATCH_SIZE}.",
    )
    parser.add_argument(
        "--timeout",
        type=int,
        default=DEFAULT_TIMEOUT_SECONDS,
        help=f"HTTP timeout seconds per batch. Defaults to {DEFAULT_TIMEOUT_SECONDS}.",
    )
    parser.add_argument(
        "--input",
        default=DEFAULT_INPUT,
        help=f"Evaluation dataset JSON path. Defaults to {DEFAULT_INPUT}.",
    )
    parser.add_argument(
        "--output",
        default=DEFAULT_OUTPUT,
        help=f"Output JSON path. Defaults to {DEFAULT_OUTPUT}.",
    )
    parser.add_argument(
        "--limit",
        type=int,
        default=0,
        help="Only evaluate the first N cases from the input dataset. Defaults to 0, which evaluates all cases.",
    )
    parser.add_argument(
        "--fail-on-low-score",
        action="store_true",
        help="Exit non-zero if summary metrics are below thresholds.",
    )
    parser.add_argument(
        "--min-answer-pass-rate",
        type=float,
        default=0.8,
        help="Minimum answer_pass_rate when --fail-on-low-score is enabled.",
    )
    parser.add_argument(
        "--min-retrieval-recall",
        type=float,
        default=0.8,
        help="Minimum retrieval_recall_at_5 when --fail-on-low-score is enabled.",
    )
    parser.add_argument(
        "--no-judge",
        action="store_true",
        help="Disable LLM-as-a-Judge evaluation.",
    )
    parser.add_argument(
        "--min-judge-pass-rate",
        type=float,
        default=0.7,
        help="Minimum judge_pass_rate when Judge and --fail-on-low-score are enabled.",
    )
    return parser.parse_args()


def load_dataset(path: Path) -> dict[str, Any]:
    if not path.exists():
        raise ValueError(f"input file does not exist: {path}")
    try:
        with path.open("r", encoding="utf-8") as f:
            data = json.load(f)
    except json.JSONDecodeError as exc:
        raise ValueError(f"input file is not valid JSON: {exc}") from exc

    cases = data.get("cases")
    if not isinstance(cases, list) or not cases:
        raise ValueError("input JSON must contain a non-empty 'cases' array")
    data = dict(data)
    data["cases"] = [normalize_case(case, index) for index, case in enumerate(cases)]
    return data


def apply_case_limit(dataset: dict[str, Any], limit: int) -> dict[str, Any]:
    if limit < 0:
        raise ValueError("--limit must be greater than or equal to 0")
    if limit == 0:
        return dataset

    limited = dict(dataset)
    limited["cases"] = dataset["cases"][:limit]
    return limited


def normalize_case(case: Any, index: int) -> dict[str, Any]:
    if not isinstance(case, dict):
        raise ValueError(f"cases[{index}] must be an object")

    normalized = dict(case)
    if "id" in normalized and normalized["id"] is not None:
        normalized["id"] = str(normalized["id"])

    question = normalized.get("question")
    if not isinstance(question, str) or not question.strip():
        raise ValueError(f"cases[{index}].question must be a non-empty string")

    for field in (
        "expected_keywords",
        "forbidden_keywords",
        "expected_sources",
    ):
        value = normalized.get(field)
        if value is not None and not is_string_list(value):
            raise ValueError(f"cases[{index}].{field} must be an array of strings")

    return normalized


def is_string_list(value: Any) -> bool:
    return isinstance(value, list) and all(isinstance(item, str) for item in value)


def call_eval_api(
    base_url: str,
    token: str,
    payload: dict[str, Any],
    timeout: int,
) -> dict[str, Any]:
    url = base_url.rstrip("/") + "/api/v2/AI/rag/evaluate"
    body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    request = urllib.request.Request(
        url,
        data=body,
        method="POST",
        headers={
            "Content-Type": "application/json",
            "Authorization": f"Bearer {token}",
        },
    )

    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            response_body = response.read().decode("utf-8")
    except urllib.error.HTTPError as exc:
        error_body = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"HTTP {exc.code} from {url}: {error_body}") from exc
    except urllib.error.URLError as exc:
        raise RuntimeError(f"failed to call {url}: {exc}") from exc

    try:
        return json.loads(response_body)
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"API response is not valid JSON: {response_body}") from exc


def split_batches(cases: list[dict[str, Any]], batch_size: int) -> list[list[dict[str, Any]]]:
    if batch_size <= 0:
        raise ValueError("--batch-size must be greater than 0")
    return [cases[i : i + batch_size] for i in range(0, len(cases), batch_size)]


def run_eval_batches(
    base_url: str,
    token: str,
    dataset: dict[str, Any],
    batch_size: int,
    timeout: int,
    judge_enabled: bool,
) -> dict[str, Any]:
    cases = dataset["cases"]
    batches = split_batches(cases, batch_size)
    all_results: list[dict[str, Any]] = []

    for index, batch in enumerate(batches, start=1):
        print(f"Running batch {index}/{len(batches)} ({len(batch)} cases)...", flush=True)
        result = call_eval_api(
            base_url,
            token,
            {"cases": batch, "judge_enabled": judge_enabled},
            timeout=timeout,
        )
        validate_api_success(result)
        all_results.extend(result.get("cases") or [])

    return {
        "status_code": SUCCESS_STATUS_CODE,
        "status_msg": "success",
        "summary": summarize_result_cases(all_results, cases),
        "cases": all_results,
    }


def summarize_result_cases(
    results: list[dict[str, Any]],
    source_cases: list[dict[str, Any]],
) -> dict[str, Any]:
    total = len(results)
    if total == 0:
        return {
            "total": 0,
            "retrieval_evaluated": 0,
            "retrieval_recall_at_5": 0,
            "mrr": 0,
            "answer_pass_rate": 0,
            "error_rate": 0,
            "avg_retrieved_docs": 0,
            "avg_retrieval_latency_ms": 0,
            "avg_generation_latency_ms": 0,
            "judge_evaluated": 0,
            "judge_pass_rate": 0,
            "avg_judge_score": 0,
            "avg_judge_faithfulness_score": 0,
            "avg_judge_answer_relevance_score": 0,
            "avg_judge_context_precision_score": 0,
            "avg_judge_latency_ms": 0,
            "avg_latency_ms": 0,
        }

    retrieval_evaluated = 0
    retrieval_hits = 0
    mrr_sum = 0.0
    answer_passes = 0
    errors = 0
    retrieved_docs = 0
    retrieval_latency = 0
    generation_latency = 0
    total_latency = 0
    judge_evaluated = 0
    judge_passes = 0
    judge_score = 0
    judge_faithfulness_score = 0
    judge_answer_relevance_score = 0
    judge_context_precision_score = 0
    judge_latency = 0

    for i, result in enumerate(results):
        if i < len(source_cases) and source_cases[i].get("expected_sources"):
            retrieval_evaluated += 1
            if result.get("retrieval_hit"):
                retrieval_hits += 1
            mrr_sum += float(result.get("retrieval_score") or 0)
        if result.get("answer_pass"):
            answer_passes += 1
        if result.get("error"):
            errors += 1
        retrieved_docs += int(result.get("retrieved_doc_count") or 0)
        retrieval_latency += int(result.get("retrieval_latency_ms") or 0)
        generation_latency += int(result.get("generation_latency_ms") or 0)
        total_latency += int(result.get("latency_ms") or 0)
        if result.get("judge_evaluated"):
            judge_evaluated += 1
            if result.get("judge_pass"):
                judge_passes += 1
            judge_score += int(result.get("judge_score") or 0)
            judge_faithfulness_score += int(result.get("judge_faithfulness_score") or 0)
            judge_answer_relevance_score += int(result.get("judge_answer_relevance_score") or 0)
            judge_context_precision_score += int(result.get("judge_context_precision_score") or 0)
            judge_latency += int(result.get("judge_latency_ms") or 0)

    return {
        "total": total,
        "retrieval_evaluated": retrieval_evaluated,
        "retrieval_recall_at_5": (
            retrieval_hits / retrieval_evaluated if retrieval_evaluated else 0
        ),
        "mrr": mrr_sum / retrieval_evaluated if retrieval_evaluated else 0,
        "answer_pass_rate": answer_passes / total,
        "error_rate": errors / total,
        "avg_retrieved_docs": retrieved_docs / total,
        "avg_retrieval_latency_ms": retrieval_latency // total,
        "avg_generation_latency_ms": generation_latency // total,
        "judge_evaluated": judge_evaluated,
        "judge_pass_rate": judge_passes / judge_evaluated if judge_evaluated else 0,
        "avg_judge_score": judge_score / judge_evaluated if judge_evaluated else 0,
        "avg_judge_faithfulness_score": judge_faithfulness_score / judge_evaluated if judge_evaluated else 0,
        "avg_judge_answer_relevance_score": judge_answer_relevance_score / judge_evaluated if judge_evaluated else 0,
        "avg_judge_context_precision_score": judge_context_precision_score / judge_evaluated if judge_evaluated else 0,
        "avg_judge_latency_ms": judge_latency // judge_evaluated if judge_evaluated else 0,
        "avg_latency_ms": total_latency // total,
    }


def save_result(path: Path, result: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        json.dump(result, f, ensure_ascii=False, indent=2)
        f.write("\n")


def print_summary(result: dict[str, Any]) -> None:
    summary = result.get("summary") or {}
    cases = result.get("cases") or []

    print("RAG evaluation summary")
    print(f"  total: {summary.get('total', 0)}")
    print(f"  retrieval_evaluated: {summary.get('retrieval_evaluated', 0)}")
    print(f"  retrieval_recall_at_5: {summary.get('retrieval_recall_at_5', 0):.4f}")
    print(f"  mrr: {summary.get('mrr', 0):.4f}")
    print(f"  answer_pass_rate: {summary.get('answer_pass_rate', 0):.4f}")
    print(f"  error_rate: {summary.get('error_rate', 0):.4f}")
    print(f"  avg_retrieval_latency_ms: {summary.get('avg_retrieval_latency_ms', 0)}")
    print(f"  avg_generation_latency_ms: {summary.get('avg_generation_latency_ms', 0)}")
    print(f"  judge_evaluated: {summary.get('judge_evaluated', 0)}")
    if summary.get("judge_evaluated", 0):
        print(f"  judge_pass_rate: {summary.get('judge_pass_rate', 0):.4f}")
        print(f"  avg_judge_score: {summary.get('avg_judge_score', 0):.4f}")
        print(f"  avg_judge_faithfulness_score: {summary.get('avg_judge_faithfulness_score', 0):.4f}")
        print(f"  avg_judge_answer_relevance_score: {summary.get('avg_judge_answer_relevance_score', 0):.4f}")
        print(f"  avg_judge_context_precision_score: {summary.get('avg_judge_context_precision_score', 0):.4f}")
        print(f"  avg_judge_latency_ms: {summary.get('avg_judge_latency_ms', 0)}")
    else:
        print("  judge: not evaluated")
    print(f"  avg_latency_ms: {summary.get('avg_latency_ms', 0)}")

    failed = [
        case
        for case in cases
        if case.get("error") or not case.get("answer_pass") or case.get("retrieval_rank") == 0
        or case.get("judge_error") or case.get("judge_warning")
        or (case.get("judge_evaluated") and not case.get("judge_pass"))
    ]
    if failed:
        print("\nCases needing review:")
        for case in failed:
            case_id = case.get("id") or "<empty-id>"
            reasons = []
            if case.get("error"):
                reasons.append(f"error={case.get('error')}")
            if not case.get("answer_pass"):
                reasons.append("answer_pass=false")
            if case.get("retrieval_rank") == 0 and case.get("retrieved_sources"):
                reasons.append("retrieval_miss")
            if case.get("judge_error"):
                reasons.append(f"judge_error={case.get('judge_error')}")
            elif case.get("judge_evaluated") and not case.get("judge_pass"):
                reasons.append("judge_pass=false")
            if case.get("judge_warning"):
                reasons.append(f"judge_warning={case.get('judge_warning')}")
            print(f"  - {case_id}: {', '.join(reasons)}")


def validate_api_success(result: dict[str, Any]) -> None:
    status_code = result.get("status_code")
    if status_code != SUCCESS_STATUS_CODE:
        status_msg = result.get("status_msg", "")
        raise RuntimeError(f"API returned status_code={status_code}, status_msg={status_msg}")


def validate_thresholds(
    result: dict[str, Any],
    min_answer_pass_rate: float,
    min_retrieval_recall: float,
    min_judge_pass_rate: float,
    judge_enabled: bool,
) -> None:
    summary = result.get("summary") or {}
    answer_pass_rate = float(summary.get("answer_pass_rate") or 0)
    retrieval_recall = float(summary.get("retrieval_recall_at_5") or 0)
    failures = []

    if answer_pass_rate < min_answer_pass_rate:
        failures.append(
            f"answer_pass_rate {answer_pass_rate:.4f} < {min_answer_pass_rate:.4f}"
        )
    if retrieval_recall < min_retrieval_recall:
        failures.append(
            f"retrieval_recall_at_5 {retrieval_recall:.4f} < {min_retrieval_recall:.4f}"
        )
    if judge_enabled and int(summary.get("judge_evaluated") or 0) > 0:
        judge_pass_rate = float(summary.get("judge_pass_rate") or 0)
        if judge_pass_rate < min_judge_pass_rate:
            failures.append(
                f"judge_pass_rate {judge_pass_rate:.4f} < {min_judge_pass_rate:.4f}"
            )
    if failures:
        raise RuntimeError("; ".join(failures))


def main() -> int:
    args = parse_args()
    if not args.token:
        print("error: GOAI_TOKEN or --token is required", file=sys.stderr)
        return 2

    try:
        dataset = load_dataset(Path(args.input))
        dataset = apply_case_limit(dataset, args.limit)
        result = run_eval_batches(
            args.base_url,
            args.token,
            dataset,
            batch_size=args.batch_size,
            timeout=args.timeout,
            judge_enabled=not args.no_judge,
        )
        save_result(Path(args.output), result)
        print_summary(result)
        print(f"\nSaved full result to {args.output}")

        if args.fail_on_low_score:
            validate_thresholds(
                result,
                args.min_answer_pass_rate,
                args.min_retrieval_recall,
                args.min_judge_pass_rate,
                not args.no_judge,
            )
    except Exception as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
