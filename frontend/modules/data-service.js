// data-service.js is the only module that talks to the ImageLab API.
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
// status URL (POLL-02, POLL-03). It never retries and never waits for a
// state change server-side; the caller decides when to ask again.
//
// A rejected promise here always means a *retrieval* error (network
// failure, timeout, abort, a non-2xx response, or an unparsable body) -
// never that the job itself failed. The caller must keep that distinction
// (see the Retrieval-error policy in the spec).
export async function fetchJobStatus(statusUrl, signal) {
  const response = await fetch(statusUrl, { signal, headers: { Accept: "application/json" } });

  if (!response.ok) {
    throw new Error("status check returned HTTP " + response.status);
  }

  let body;
  try {
    body = await response.json();
  } catch (_) {
    throw new Error("status response could not be read");
  }

  return body;
}