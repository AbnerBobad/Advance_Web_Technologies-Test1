# Section 4 — Predictions (recorded before the measured runs)

These predictions were written before running `scripts/experiment.sh`. Each is
stated per condition and observation mode. The measured results are compared
against them in the Week 5 report.

The completion-detection delay is `client receipt of the terminal snapshot −
the server's post-commit wall-clock log time`, joined by job id. On a
same-machine loopback run the network floor is sub-millisecond, so any
additional delay is dominated by the observation cadence.

## A — Quiet job (30 s of processing, no intermediate updates)

- **Short polling:** one request per second, so roughly 30–32 requests over the
  quiet window. Completion is only discovered at the next 1 s tick, so the
  detection delay is up to ~1000 ms (mean ~500 ms). No version changes are
  observed between the claim and completion.
- **Long polling:** the client waits on `after/wait`, so it needs no periodic
  requests. Expect very few requests: the claim snapshot, possibly one 20 s
  holding-deadline `204`, then the completion snapshot (≈2–4 requests). The
  detection delay should be near the network floor (< 10 ms), because the
  completion broadcast wakes the waiter immediately.

## B — Real progress (actual variant stages, no simulation)

- Real transformation of the 1200×800 fixture completes in a few milliseconds,
  so the three variant commits and the completed status arrive in a burst.
- **Short polling:** likely observes the claim or an early stage, then a single
  later snapshot that is already terminal. Expect 2–3 requests, low version
  coverage, and a detection delay in the 0–1000 ms range depending on where the
  burst falls relative to the 1 s tick.
- **Long polling:** the initial prompt snapshot may already be terminal, or a
  single `after/wait` request returns the completion. Expect 1–2 requests and a
  near-floor detection delay (< 10 ms).

## C1 — Simulated 10 s phase, updates every 2 s

- Five simulated steps (10 s ÷ 2 s), plus the claim and three variant commits,
  so about ten committed versions after the initial one.
- **Short polling:** ~10–12 requests over ~10 s. With a 2 s update cadence and a
  1 s poll cadence most versions are observed; expect partial coverage
  (roughly 0.5–0.8) and a detection delay dominated by the 1 s tick
  (~500–1000 ms).
- **Long polling:** one request per committed change, so ~10–12 requests and
  full version coverage (1.0). Detection delay near the network floor
  (< 10 ms).

## C2 — Simulated 10 s phase, updates every 200 ms

- About fifty simulated steps plus the real milestones.
- **Short polling:** still ~10–12 requests, so it observes only a small fraction
  of versions; expect low coverage (roughly 0.2) and a ~500–1000 ms detection
  delay.
- **Long polling:** ~50+ requests, one per change, full coverage (1.0) and a
  near-floor detection delay. This is the case where the request *count* cost of
  long polling is highest.

## C3 — C2 plus 200 ms added round-trip latency

- Same server behaviour as C2; the client adds 200 ms of round-trip latency to
  every observation request (100 ms before sending, 100 ms after receiving).
- **Short polling:** detection delay increases by up to the added latency
  (≈500–1200 ms), coverage still low.
- **Long polling:** because each request now costs ~200 ms while updates arrive
  every 200 ms, the client may skip some versions, so coverage can drop below
  1.0 and the detection delay grows by roughly the added round-trip latency
  (~200 ms). Request count stays high (~50).

## Overall hypothesis

Long polling yields markedly lower completion-detection delay in every
condition, at the cost of far more requests per job in the fast-update
conditions (C2/C3). Short polling keeps the request count low and predictable
but pays up to one polling interval of detection delay and, in fast-update
conditions, observes only a fraction of the committed versions.
