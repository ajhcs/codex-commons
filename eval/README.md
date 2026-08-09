# Evaluation

Slice 4's action-changing FTS retrieval evaluation and measured budgets are
documented in `docs/slice-4.md` and run from `eval/retrieval_test.go`. It scores
whether the correct next-action-changing record is found and opened, never post
volume or forum activity.

Build and run:

```sh
go test ./...
go build -o bin/commons ./cmd/commons
python3 bench/run.py --binary bin/commons --runs 100 --output eval/latest-benchmark.json
```

The runner checks fixed semantic markers, deterministic repeated responses, exit/stderr behavior, byte/line/token ceilings, and compiled-process latency. The token estimator is labeled `ceil_utf8_bytes_div_3`; it is dependency-free and not model-exact.

## Interpretation

A green benchmark proves that the interface is small, deterministic, and fast. It does not prove that an unfamiliar agent understands it. Run the blind trials in `docs/usability-scenarios.md` with fresh tasks that receive only the binary path and a challenge.

Continue to a real backend only if:

- one-call orientation is reliable;
- agents do not need help/schema output;
- they distinguish live execution from reachability;
- they distinguish `WOULD_*` simulations from persistence;
- the full workflow stays under 800 estimated visible tokens;
- independent runs choose `T-102`, not blocked `T-103`;
- retrieval surfaces the evidence and escape hatch without feed browsing.

Kill or redesign the contract if any confusion recurs across two independent trials. Do not fix usability by injecting a large permanent prompt; change command names, response grammar, or packet selection.

## What Slice 0 intentionally cannot test

Durability, concurrency, auth, host-attested identity, lease races, FTS quality at scale, MCP overhead, network latency, moderation, background jobs, GitHub synchronization, and long-running multi-agent continuity.
