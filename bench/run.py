#!/usr/bin/env python3
"""Dependency-free latency and context-budget benchmark for Slice 0."""

from __future__ import annotations

import argparse
import json
import math
import os
from pathlib import Path
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[1]


def percentile(values: list[float], q: float) -> float:
    if not values:
        return 0.0
    ordered = sorted(values)
    return ordered[max(0, math.ceil(q * len(ordered)) - 1)]


def resolve_command(requested: str | None) -> tuple[list[str], bool]:
    candidate = requested or os.environ.get("COMMONS_CLI")
    if candidate:
        path = Path(candidate).expanduser().resolve()
        if not path.is_file():
            raise SystemExit(f"binary not found: {path}")
        return [str(path)], True
    built = ROOT / "bin" / "commons"
    if built.is_file():
        return [str(built)], True
    return ["go", "run", "./cmd/commons"], False


def invoke(command: list[str], args: list[str]) -> dict:
    started = time.perf_counter_ns()
    proc = subprocess.run(
        command + args,
        cwd=ROOT,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    elapsed_ms = (time.perf_counter_ns() - started) / 1_000_000
    stdout = proc.stdout
    return {
        "exit": proc.returncode,
        "stdout": stdout.decode("utf-8", errors="replace"),
        "stderr": proc.stderr.decode("utf-8", errors="replace"),
        "elapsed_ms": elapsed_ms,
        "bytes": len(stdout),
        "lines": len(stdout.splitlines()),
        "tokens_est": math.ceil(len(stdout) / 3),
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", help="compiled commons binary")
    parser.add_argument("--runs", type=int, default=100, help="warm runs per scenario")
    parser.add_argument("--json", action="store_true", help="print compact JSON")
    parser.add_argument("--output", help="also write JSON report")
    options = parser.parse_args()
    if options.runs < 1:
        parser.error("--runs must be positive")

    command, compiled = resolve_command(options.binary)
    scenarios = json.loads((ROOT / "bench" / "scenarios.json").read_text())["scenarios"]
    budgets = json.loads((ROOT / "bench" / "budgets.json").read_text())
    results = []
    all_cold: list[float] = []
    all_warm: list[float] = []

    for scenario in scenarios:
        cold = invoke(command, scenario["args"])
        warm = [invoke(command, scenario["args"]) for _ in range(options.runs)]
        warm_times = [sample["elapsed_ms"] for sample in warm]
        all_cold.append(cold["elapsed_ms"])
        all_warm.extend(warm_times)
        ceiling = budgets["surfaces"][scenario["surface"]]
        failures: list[str] = []

        if cold["exit"] != 0:
            failures.append(f"exit={cold['exit']} stderr={cold['stderr'].strip()!r}")
        if cold["stderr"]:
            failures.append(f"unexpected stderr={cold['stderr'].strip()!r}")
        for marker in scenario["expect"]:
            if marker not in cold["stdout"]:
                failures.append(f"missing marker {marker!r}")
        for metric in ("bytes", "lines", "tokens_est"):
            budget_key = "tokens" if metric == "tokens_est" else metric
            if cold[metric] > ceiling[budget_key]:
                failures.append(f"{metric}={cold[metric]} > {ceiling[budget_key]}")
        if any(sample["exit"] != 0 or sample["stdout"] != cold["stdout"] for sample in warm):
            failures.append("warm response changed or failed")

        result = {
            "name": scenario["name"],
            "surface": scenario["surface"],
            "pass": not failures,
            "failures": failures,
            "cold_ms": round(cold["elapsed_ms"], 3),
            "warm_p50_ms": round(percentile(warm_times, 0.50), 3),
            "warm_p95_ms": round(percentile(warm_times, 0.95), 3),
            "bytes": cold["bytes"],
            "lines": cold["lines"],
            "tokens_est": cold["tokens_est"],
        }
        results.append(result)

    cold_p95 = percentile(all_cold, 0.95)
    warm_p95 = percentile(all_warm, 0.95)
    latency_failures: list[str] = []
    if compiled and cold_p95 > budgets["latency_ms"]["cold_p95"]:
        latency_failures.append(f"cold_p95_ms={cold_p95:.3f}")
    if compiled and warm_p95 > budgets["latency_ms"]["warm_p95"]:
        latency_failures.append(f"warm_p95_ms={warm_p95:.3f}")

    report = {
        "ok": all(item["pass"] for item in results) and not latency_failures,
        "command": command,
        "compiled_latency_enforced": compiled,
        "runs_per_scenario": options.runs,
        "token_estimator": budgets["token_estimator"],
        "summary": {
            "scenarios": len(results),
            "passed": sum(item["pass"] for item in results),
            "total_visible_tokens_est": sum(item["tokens_est"] for item in results),
            "cold_p95_ms": round(cold_p95, 3),
            "warm_p95_ms": round(warm_p95, 3),
            "latency_failures": latency_failures,
        },
        "results": results,
    }

    rendered = json.dumps(report, indent=None if options.json else 2, sort_keys=True)
    print(rendered)
    if options.output:
        destination = Path(options.output)
        if not destination.is_absolute():
            destination = ROOT / destination
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
    return 0 if report["ok"] else 1


if __name__ == "__main__":
    sys.exit(main())
