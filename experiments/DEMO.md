# Version 2 Demo — Script and Recording

A 20–30 minute narrated walkthrough of ImageLab Version 2. Record the screen
(terminal + browser), narrate each section, and upload as an **unlisted**
YouTube video (or provide a shareable link).

> **Video link:** _TODO — paste the unlisted video URL here before submitting._

## Before recording

1. Terminal A: start PostgreSQL (already running) and the API with a delay so the
   lifecycle is visible:
   ```sh
   make db/migrations/up          # schema version should reach 5
   go run ./cmd/api -processing-delay=2s -simulate-duration=0
   ```
2. Terminal B (ready to paste commands):
   ```sh
   BASE=http://localhost:4000
   ```
3. Browser: open <http://localhost:4000>, zoom to ~125%.
4. Optional: have `experiments/results/summary.csv` open in an editor for the
   results section.

## Timeline

### 0:00–2:00 — Intro and scope
- State the Version 1 → Version 2 shift: add **long polling** beside one-second
  **short polling**, without removing anything.
- Name the two new endpoints and distinguish them:
  - `GET /v1/jobs/{id}` — prompt, never waits (API-01).
  - `GET /v1/jobs/{id}?after=<n>&wait=<secs>` — holds for a newer version
    (API-02), with `204` at the holding deadline.

### 2:00–5:00 — Setup and baseline
- Show `git log --oneline` and point out the Assessment 1 deliverable:
  ```sh
  git tag --list                 # assessment1-baseline
  git show assessment1-baseline --stat | head
  ```
- Explain the baseline tag marks the last Version 1 commit; everything after it
  is Version 2.
- Run `make db/migrations/up` and show schema version 5 (`-progress` columns).

### 5:00–9:00 — Short polling (unchanged, baseline behaviour)
- In the browser, select the fixture and submit.
- Open DevTools → Network, filter `jobs`. Point out:
  - the `202` plus `status_url`;
  - one `GET` per second while queued/processing;
  - the terminal `200` that flips the UI to results.
- Emphasise: requests stop on `completed`; a new upload cancels the old job.

### 9:00–14:00 — Long polling (new)
- Reload with a query flag or use the mode selector to switch to **long polling**.
- Submit and show in DevTools:
  - the initial prompt `GET /v1/jobs/{id}` returns **immediately**;
  - a `GET ...?after=1&wait=20` that *stays pending* until the state changes;
  - the sequence of held requests, each returning the next version;
  - the terminal response that ends observation.
- Explain the `204`: with a long quiet job it appears at the 20 s deadline; the
  client re-reads state and re-issues.

### 14:00–18:00 — Progress and experimental controls
- Show the job card reporting **Stage**, **Variants: X of 3**, and (when a
  simulated phase runs) the italic **Simulated progress: step N** line.
- Restart the server with simulation to slow the visible transition:
  ```sh
  # Ctrl-C Terminal A, then:
  go run ./cmd/api -processing-delay=0 -simulate-duration=10s -simulate-interval=200ms
  ```
- Submit again and narrate the moving progress under long polling; switch to
  short polling and show that it *samples* the same progress (fewer updates).
- Point out the server startup log line naming the experimental controls.

### 18:00–24:00 — The experiment
- Explain the harness honestly: a headless Go client (`cmd/experiment`) drives the
  same loops as the browser because no browser automation is available.
- Show the conditions and run the orchestration (abbreviate if short on time):
  ```sh
  bash scripts/experiment.sh
  ```
- Open `experiments/results/summary.csv` and walk the two headline tables:
  - detection delay: long ≈ 1 ms vs short ≈ 750–840 ms;
  - request count: long 6→55 as updates speed up; short flat ~10–32;
  - coverage: long 1.00 vs short 0.17–0.67.
- Show `chart-detection.svg` and `chart-requests.svg`.

### 24:00–28:00 — Interpretation, limits, recommendation
- State the trade-off: long polling trades request volume for latency and
  completeness; short polling trades latency for a predictable, low budget.
- Name the limitations: loopback only, 3 repetitions, simulated ≠ real progress,
  single client/worker, held-connection cost unmeasured.
- Give the recommendation: long polling by default, short polling as the
  resilient fallback, prompt `GET` always safe.

### 28:00–30:00 — Wrap-up and evidence map
- List where the evidence lives: `runs.csv`, `summary.csv`, charts, raw JSONL,
  logs, `experiments/predictions.md`, `REPORT.md`, this script.
- Close with the git state: Version 2 commits and the repository URL.

## Evidence map

| Deliverable | Location |
| :-- | :-- |
| Raw run data | `experiments/results/runs.csv` |
| Summary table | `experiments/results/summary.csv` |
| Comparison charts | `experiments/results/chart-detection.svg`, `chart-requests.svg` |
| Per-run event logs | `experiments/results/raw/*.jsonl` |
| Per-condition server logs | `experiments/results/logs/*.log` |
| Pre-run predictions | `experiments/predictions.md` |
| Written report | `REPORT.md` |
