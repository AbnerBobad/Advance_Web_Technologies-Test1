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
 
  // Polling bookkeeping. polling is true only while an automatic 1-second
  // loop is active. pollAbortController lets a new job or a page/tab change
  // cancel an in-flight GET. retrievalError is set only when the
  // browser could not obtain a status response; it must never be confused
  // with the server reporting status "failed" (see decision rule in spec).
  polling: false,
  pollAbortController: null,
  pollTimeoutId: null,
  retrievalError: false,
  pollCount: 0,


};

export { MAX_IMAGE_BYTES };