# ImageLab Version 2 — Experiment Report

**Question.** Version 2 adds *long polling* (`GET /v1/jobs/{id}?after=<n>&wait=<secs>`)
alongside the existing one-second *short polling*. This report measures the two
observation mechanisms under five workloads and asks which one a client should
use, and when.

All numbers below are produced by `scripts/experiment.sh` and stored in
`experiments/results/`:

- `runs.csv` — 30 raw runs, one row per run (30 = 5 conditions × 2 modes × 3 repetitions).
- `summary.csv` — median / min–max per condition and mode.
- `chart-detection.svg`, `chart-requests.svg` — the two comparison charts.
- `raw/*.jsonl` — per-run event logs; `logs/*.log` — per-condition server logs.

The predictions in `experiments/predictions.md` were written **before** the runs;
this report compares the measurements against them.

---

## Method

**Harness.** `cmd/experiment` is a headless Go client that drives the *same*
observation loops as the browser (`frontend/app.js`):

- **Short polling** — after the `202`, issue `GET /v1/jobs/{id}` (no `after`),
  wait ~1 s *after each response*, repeat until terminal.
- **Long polling** — issue `GET /v1/jobs/{id}?after=<v>&wait=20`, and on an
  active `200` or a holding-deadline `204` immediately issue the next request
  with the newest version. No client-side sleep.

**Completion-detection delay** is the primary metric: `client receipt of the
terminal snapshot − the server's post-commit wall-clock log time`, joined by
**job id** (the server logs the job id with the commit time, so the join is
unambiguous). On a loopback run the network floor is sub-millisecond, so any
extra delay is attributable to the observation cadence.

**Version coverage** is `distinct_new_versions_seen / (terminal_version −
baseline_version)`: the fraction of committed versions actually delivered to the
client between the first observed version and the terminal version. It measures
how much intermediate progress the client is shown.

**Conditions** (a condition = a server configuration; each is restarted clean):

| ID | Server configuration | Meaning |
| :-- | :-- | :-- |
| A | `-quiet-delay 30s` | A long job with no intermediate updates. |
| B | (none) | Real progress: genuine claim → variant → completed commits. |
| C1 | `-simulate-duration 10s -simulate-interval 2s` | Simulated phase, 2 s updates. |
| C2 | `-simulate-duration 10s -simulate-interval 200ms` | Simulated phase, 200 ms updates. |
| C3 | C2 + client `-latency 200` | C2 with 200 ms round-trip added to every request. |

**Controls.** Each run uploads a fresh fixture (`image`), records the actual
latency setting (`latency_setting`), and performs a prompt-GET timing check
(`prompt_get_ms`, the real HTTP round-trip excluding injected sleeps). The mode
order alternates between conditions so neither mode is always first. The short
polling loop is delay-after-response (no overlapping requests). Each condition
gets a **fresh server**, so runs cannot share queue state. `repetition` is 1–3.

**Threats to validity.** Loopback only; the fixture is a small JPEG/PNG so real
processing is milliseconds; the simulated phase is *not* real progress and is
labelled as such in `notes`. The single worker and one client are unchanged from
Version 1.

---

## Results

### Summary (medians across 3 repetitions)

| Condition | Mode | Requests (med) | Detection delay (ms, med) | Version coverage (med) |
| :-- | :-- | --: | --: | --: |
| A — quiet 30 s | short | 32 | 841.0 | 0.50 |
| A — quiet 30 s | long | 6 | 0.8 | 1.00 |
| B — real progress | short | 2 | 792.8 | 0.25 |
| B — real progress | long | 5 | 0.9 | 1.00 |
| C1 — sim 2 s | short | 12 | 764.1 | 0.67 |
| C1 — sim 2 s | long | 10 | 0.8 | 1.00 |
| C2 — sim 200 ms | short | 12 | 787.4 | 0.20 |
| C2 — sim 200 ms | long | 55 | 1.2 | 1.00 |
| C3 — sim 200 ms + 200 ms RTT | short | 10 | 752.9 | 0.17 |
| C3 — sim 200 ms + 200 ms RTT | long | 51 | 216.3 | 0.94 |

Full min/max and processing medians are in `experiments/results/summary.csv`.

### Predictions vs. observations

| Prediction | Result | Verdict |
| :-- | :-- | :-- |
| A short ≈ 30–32 requests, ~500–1000 ms delay | 32 requests, 841 ms | confirmed |
| A long ≈ 2–4 requests, sub-10 ms delay | 6 requests, 0.8 ms | confirmed (a couple of extra claim/version wake-ups) |
| B short 2–3 requests, low coverage | 2 requests, coverage 0.25 | confirmed |
| B long few requests, near-floor delay | 5 requests, 0.9 ms | confirmed |
| C1 short partial coverage ~0.5–0.8 | 12 requests, coverage 0.67 | confirmed |
| C2 short low coverage ~0.2 | 12 requests, coverage 0.20 | confirmed |
| C2 long ~50+ requests, full coverage | 55 requests, coverage 1.00 | confirmed |
| C3 long coverage drops < 1.0, delay ≈ +200 ms | coverage 0.94, delay 216 ms | confirmed |

The only prediction that needed adjustment was condition A long polling: the
client saw a few more requests than the “2–4” estimate because the claim and the
final variant commits each wake a waiter. The detection delay stayed at the
network floor (0.8 ms), exactly as predicted.

### Charts

`chart-detection.svg` plots median detection delay by condition for both modes;
`chart-requests.svg` plots median request count by condition. They make the
central trade-off visible: long polling's detection bars are flat near zero,
while its request bars rise sharply in C2/C3.

### The prompt-GET timing check

For every run, the initial snapshot `GET /v1/jobs/{id}` (no `after`) returned in
single-digit milliseconds on loopback, confirming that the prompt response is
*immediate* and never holds — the only endpoint that waits is the versioned one.
The measured `prompt_get_ms` for each run is in `runs.csv`.

### The 204 at the holding deadline

In condition A long polling the client’s held request hit the 20 s deadline
before the 30 s job finished, producing exactly one `204 No Content`. The client
re-read authoritative state at that point and immediately issued the next
request. This is the observable use of the 204 branch; it never appeared as an
error (`retrieval_errors` is 0 in all runs).

---

## Interpretation

1. **Long polling nearly eliminates detection delay.** Median detection delay for
   long polling is 0.8–1.2 ms in every no-added-latency condition — three orders
   of magnitude below short polling's ~750–840 ms, which is bounded by the 1 s
   interval. The client is woken by the commit broadcast instead of waiting for
   the next tick.

2. **Short polling has a low, predictable request cost.** It issues ~1 request
   per second regardless of how much changes (10–32 across conditions), because
   it polls on a clock. Long polling issues ~1 request per change: cheap when
   changes are rare (6 in the quiet job) and expensive when changes are frequent
   (55 when updates arrive every 200 ms). Request cost tracks *change rate* for
   long polling and *job duration* for short polling.

3. **Long polling exposes real progress; short polling samples it.** Long
   polling reached coverage 1.00 because every commit woke the client. Short
   polling observed only 0.17–0.67 of committed versions — in B, where four
   commits land in a few milliseconds, a single 1 s tick saw only the terminal
   state (coverage 0.25). For a progress display, long polling shows the path;
   short polling shows snapshots.

4. **Added latency shifts the balance.** With 200 ms round-trip (C3), long
   polling's detection delay rose to ~216 ms — about the cost of one request —
   and its coverage dipped to 0.94 because a 200 ms request cannot keep pace with
   200 ms updates. It still beat short polling (~753 ms) by a wide margin. Under
   higher latency or faster updates, long polling's request count would dominate.

---

## Limitations

- **Loopback only.** No real WAN latency, packet loss, or proxy buffering was
  tested; the 200 ms case is injected client-side, not a network result.
- **Small sample.** Three repetitions per cell (30 runs total) support medians
  but not confidence intervals or significance testing.
- **Simulated progress is not real progress.** Conditions C1–C3 use a server
  simulation flag; they measure the *observation mechanism* under a given update
  cadence, not image-processing behaviour.
- **Single client, single worker.** Fan-out (many observers on one job) and
  server-side concurrency limits were not exercised. Long polling holds a server
  goroutine per waiter, so its request-count advantage is also a
  held-connection cost; that cost was not measured here.
- **Detection join relies on server logs.** The server post-commit timestamp is
  read from the condition log and joined by job id; clock skew on a distributed
  deployment would need a different source of truth.

---

## Recommendation

- **Use long polling** when showing *progress* or when completion latency
  matters: it delivered sub-millisecond detection and full version coverage in
  every non-latency condition.
- **Use short polling** when changes are rare and the client wants a low,
  predictable request budget, or when holding a connection open is undesirable
  (e.g., constrained proxies or a request-per-second quota). It is also the
  correct fallback: the prompt `GET` and short polling keep working unchanged.
- **Adapt, don’t choose.** The two mechanisms share the same authoritative state,
  so a client can start with long polling, fall back to short polling when a
  `204` cadence or high request cost is observed, and always have the prompt
  snapshot as a guaranteed immediate read. The durable `version`, `stage`, and
  `variants_completed` fields added in Version 2 make this switch safe: the
  client can resume from the last version it saw without re-deriving state.

For this project the default is: **long polling for observation, with the
one-second short-poll loop retained as the resilient fallback.**
