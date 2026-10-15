const MAX_IMAGE_BYTES = 10 * 1024 * 1024;

// state holds the observable UI state of the page: which file is selected,
// whether a submission is in flight, and whether a job has been accepted.
export const state = {
  selectedFile: null,
  previewObjectURL: null,
  jobAccepted: false,
  isSubmitting: false,

  // job is the last known server-reported snapshot for the job being
  // observed, merged with the fields we already knew from the 202 response.
  // It is never cleared on a retrieval error.
  job: null,

  // mode is the selected observation strategy: "short" or "long". It is read
  // from the selector each time an observation loop starts.
  mode: "short",

  // generation is the observation token. Every observation loop captures the
  // value it started with; starting or cancelling an observation increments
  // it, so a stale loop can detect that its answer must be ignored (UI-04).
  generation: 0,

  // Observation bookkeeping. observing is true only while an automatic loop
  // is active. observationAbortController lets a new job, a mode switch, or a
  // page unload cancel an in-flight request. retrievalError is set only when
  // the browser could not obtain a status response; it must never be confused
  // with the server reporting status "failed".
  observing: false,
  observationAbortController: null,
  retrievalError: false,
  pollCount: 0,

  // Week 4 measurements (Section 15). ackLatencyMs is the time between the
  // browser starting the upload POST and receiving the 202 response;
  // detectionDelayMs is the time between the server stamping completed_at
  // and this page observing it. Both are filled in by app.js.
  ackLatencyMs: null,
  detectionDelayMs: null,
};

export { MAX_IMAGE_BYTES };
