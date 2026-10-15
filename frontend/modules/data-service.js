// Short polling uses a short per-request timeout because the server answers
// promptly. Long polling holds the request for up to 20 seconds, so its
// browser timeout must exceed the server's holding deadline (WAIT-03): a
// normal server 204 and an actual client timeout have different meanings.
const SHORT_TIMEOUT_MS = 5000;
const LONG_TIMEOUT_MS = 30000;
const LONG_WAIT_SECONDS = 20;

const JOB_STATES = ["queued", "processing", "completed", "failed"];

// validateSnapshot checks that a 200 body is a usable job representation and
// returns it, or throws a retrieval-error message.
function validateSnapshot(body) {
  if (!body || !JOB_STATES.includes(body.status)) {
    throw new Error("status response was not usable");
  }
  if (typeof body.version !== "number") {
    throw new Error("status response had no version");
  }
  if (body.status === "completed" && !Array.isArray(body.variants)) {
    throw new Error("completed status response had no variants");
  }
  return body;
}

export async function submitImage(file) {
  const formData = new FormData();
  formData.append("file", file);

  const response = await fetch("/v1/images", { method: "POST", body: formData });

  let body = null;
  try {
    body = await response.json();
  } catch (_) {
    body = null;
  }

  return { accepted: response.status === 202, status: response.status, body };
}

// fetchJobSnapshot performs one prompt GET (no query parameters) and returns
// the current representation immediately. The server never waits on this
// path (API-01), so a short timeout applies.
export async function fetchJobSnapshot(statusUrl, signal) {
  const response = await fetch(statusUrl, {
    signal: AbortSignal.any([signal, AbortSignal.timeout(SHORT_TIMEOUT_MS)]),
    headers: { Accept: "application/json" },
    cache: "no-store",
  });

  if (!response.ok) {
    throw new Error("status check returned HTTP " + response.status);
  }

  let body;
  try {
    body = await response.json();
  } catch (_) {
    throw new Error("status response could not be read");
  }

  return validateSnapshot(body);
}

// fetchJobStatus performs exactly one short-polling GET. The server returns
// the current representation promptly and never waits; the caller decides
// when to ask again.
export async function fetchJobStatus(statusUrl, signal) {
  const response = await fetch(statusUrl, {
    signal: AbortSignal.any([signal, AbortSignal.timeout(SHORT_TIMEOUT_MS)]),
    headers: { Accept: "application/json" },
    cache: "no-store",
  });

  if (!response.ok) {
    throw new Error("status check returned HTTP " + response.status);
  }

  let body;
  try {
    body = await response.json();
  } catch (_) {
    throw new Error("status response could not be read");
  }

  return validateSnapshot(body);
}

// observeJobAfter performs one long-polling GET for a snapshot newer than
// `version`. The server holds the request until a committed change, the
// holding deadline (answered as 204 with no body), or cancellation.
//
// It resolves to { status: 200, body } for a snapshot or { status: 204,
// body: null } for a normal holding deadline. It rejects only on a genuine
// retrieval error: network failure, an actual client timeout, a non-success
// response other than the normal 204, or an unparsable/invalid body. A 204
// is never an error (UI-03, UI-05).
export async function observeJobAfter(statusUrl, version, signal) {
  const url =
    statusUrl +
    "?after=" +
    encodeURIComponent(version) +
    "&wait=" +
    LONG_WAIT_SECONDS;

  const response = await fetch(url, {
    signal: AbortSignal.any([signal, AbortSignal.timeout(LONG_TIMEOUT_MS)]),
    headers: { Accept: "application/json" },
    cache: "no-store",
  });

  if (response.status === 204) {
    return { status: 204, body: null };
  }

  if (!response.ok) {
    let problem = null;
    try {
      problem = await response.json();
    } catch (_) {
      problem = null;
    }
    const message =
      problem && problem.error ? problem.error : "HTTP " + response.status;
    throw new Error("status check returned " + message);
  }

  let body;
  try {
    body = await response.json();
  } catch (_) {
    throw new Error("status response could not be read");
  }

  return { status: 200, body: validateSnapshot(body) };
}
