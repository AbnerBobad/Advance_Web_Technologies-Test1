const STATUS_TIMEOUT_MS = 5000;
const JOB_STATES = ["queued", "processing", "completed", "failed"];

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

// fetchJobStatus performs exactly one short-polling GET against the job's
// status URL. It never retries and never waits for a
// state change server-side; the caller decides when to ask again.
//
// A rejected promise here always means a *retrieval* error (network
// failure, timeout, abort, a non-2xx response, or an unparsable body) -
// never that the job itself failed. The caller must keep that distinction
// (see the Retrieval-error policy in the spec).
export async function fetchJobStatus(statusUrl, signal) {
  // Combine the caller's cancellation signal with a per-request timeout. A
  // timeout aborts the fetch but leaves the caller's own signal un-aborted,
  // so app.js correctly treats it as a retrieval error (POLL-07), while a
  // deliberate cancellation is still ignored.
  const response = await fetch(statusUrl, {
    signal: AbortSignal.any([signal, AbortSignal.timeout(STATUS_TIMEOUT_MS)]),
    headers: { Accept: "application/json" },
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

  // The server is expected to return a JSON object with a "status" field
  // that is one of the known job states. If it does not, treat it as a
  // retrieval error.
  if (!body || !JOB_STATES.includes(body.status)) {
    throw new Error("status response was not usable");
  }
  if (body.status === "completed" && !Array.isArray(body.variants)) {
    throw new Error("completed status response had no variants");
  }

  return body;
}