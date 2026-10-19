import { state, MAX_IMAGE_BYTES } from "./state.js";
import {
  submitImage,
  fetchJobSnapshot,
  fetchJobStatus,
  observeJobAfter,
} from "./modules/data-service.js";
import {
  renderPreview,
  clearPreview,
  renderJobCard,
  renderResultsEmpty,
  renderResultsProcessing,
  renderResults,
  renderResultsFailed,
  showMessage,
  hideMessage,
} from "./render.js";

const POLL_INTERVAL_MS = 1000;

const fileInput = document.getElementById("fileInput");
const chooseButton = document.getElementById("chooseButton");
const processButton = document.getElementById("processButton");
const replaceButton = document.getElementById("replaceButton"); //Replace Btn
const selectionMessage = document.getElementById("selectionMessage");
const statusMessage = document.getElementById("statusMessage");
const errorMessage = document.getElementById("errorMessage");
const dropzone = document.getElementById("dropzone");
const previewArea = document.getElementById("previewArea");
const jobCardBody = document.getElementById("jobCardBody");

chooseButton.addEventListener("click", () => fileInput.click());
replaceButton.addEventListener("click", () => fileInput.click()); //Replace Btn
fileInput.addEventListener("change", handleFileSelection);
processButton.addEventListener("click", submit);

// Event delegation: the Try again button is re-created every time the job
// card re-renders, so it's cheaper and more robust to listen on the
// (stable) container than to re-bind a listener after every render.
jobCardBody.addEventListener("click", (event) => {
  if (event.target.closest("#tryAgainButton")) {
    resumeObservation();
  }
});

// Changing the observation mode while a job is being observed restarts
// observation of the same job in the new mode. It never uploads the image
// again or creates a new job (UI-04).
for (const input of document.querySelectorAll('input[name="observationMode"]')) {
  input.addEventListener("change", handleModeChange);
}

renderResultsEmpty();

// handleFileSelection produces the image-selected state: a browser-owned
// preview only. Selecting a file never uploads it or starts a job.
function handleFileSelection() {
  const file = fileInput.files[0];
  if (!file) return;

  hideMessage(selectionMessage);
  hideMessage(errorMessage);
  dropzone.classList.remove("dropzone-error");

  const typeOk = file.type === "image/jpeg" || file.type === "image/png";
  const sizeOk = file.size > 0 && file.size <= MAX_IMAGE_BYTES;

  if (!typeOk || !sizeOk) {
    clearSelection();
    dropzone.classList.remove("hidden");
    dropzone.classList.add("dropzone-error");
    showMessage(
      selectionMessage,
      "Unsupported file. Choose a JPEG or PNG image no larger than 10 MB.",
      "message-error"
    );
    return;
  }

  if (state.previewObjectURL) URL.revokeObjectURL(state.previewObjectURL);
  state.previewObjectURL = URL.createObjectURL(file);

  renderPreview(file, state.previewObjectURL);
  dropzone.classList.add("hidden");
  previewArea.classList.remove("hidden");
  state.selectedFile = file;
  state.jobAccepted = false;
  processButton.disabled = false;
  replaceButton.disabled = false;
}

function clearSelection() {
  if (state.previewObjectURL) {
    URL.revokeObjectURL(state.previewObjectURL);
    state.previewObjectURL = null;
  }
  clearPreview();
  state.selectedFile = null;
  state.jobAccepted = false;
  processButton.disabled = true;
  processButton.textContent = "Process image";
  fileInput.value = "";
  previewArea.classList.add("hidden");
  dropzone.classList.remove("hidden", "dropzone-error");
}

// submit sends exactly one POST. The isSubmitting guard and the disabled
// button prevent an overlapping second request from this page (SUB-01/02).
async function submit() {
  if (state.isSubmitting) return;
  if (!state.selectedFile || state.jobAccepted) return;

  // Note: observation of any previous job is NOT cancelled here. It is
  // cancelled in beginObservingJob(), once the server has actually accepted
  // the new job. A rejected upload must not kill observation of a job that
  // is still running.
  state.isSubmitting = true;
  processButton.disabled = true;
  replaceButton.disabled = true;
  processButton.textContent = "Uploading...";
  hideMessage(errorMessage);
  hideMessage(statusMessage);
  showMessage(statusMessage, "Uploading...", "message-info");
  state.ackLatencyMs = null;
  state.detectionDelayMs = null;

  try {
    const requestStart = performance.now();
    const result = await submitImage(state.selectedFile);
    state.ackLatencyMs = performance.now() - requestStart;

    if (!result.accepted) {
      const reason = result.body && result.body.error
        ? result.body.error
        : "The server rejected the upload.";
      throw new Error(reason);
    }

    state.jobAccepted = true;
    hideMessage(statusMessage);
    beginObservingJob(result.body);
    processButton.textContent = "Process image";
    processButton.disabled = true;
    // The user may pick another file at any time (UI-07). Process image stays
    // disabled until they do, because this file already has a job.
    replaceButton.disabled = false;
  } catch (error) {
    // A rejection or network failure must not leave the page unusable: the
    // user may choose a new file or attempt the submission again (SUB-03).
    const message = error && error.message ? error.message : "could not reach the server";
    showMessage(errorMessage, "Upload failed: " + message, "message-error");

    if (!state.jobAccepted) {
      processButton.disabled = !state.selectedFile;
      replaceButton.disabled = false;
    }
    processButton.textContent = "Process image";
  } finally {
    state.isSubmitting = false;
  }
}

// ---------- job observation (short and long polling) ----------

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

function isTerminal(status) {
  return status === "completed" || status === "failed";
}

// selectedMode reads the observation control (UI-01): "short" or "long".
function selectedMode() {
  const checked = document.querySelector('input[name="observationMode"]:checked');
  return checked ? checked.value : "short";
}

// beginObservingJob starts observation right after 202 Accepted. It seeds
// state.job from the acceptance response and renders the initial card before
// the first status GET is sent.
function beginObservingJob(accepted) {
  // A new observed job cancels observation of the previous one (UI-04).
  stopObservation();

  state.job = {
    id: accepted.job_id,
    image_id: accepted.image_id,
    status: accepted.status || "queued",
    version: accepted.version ?? null,
    status_url: accepted.status_url,
    queued_at: null,
    started_at: null,
    completed_at: null,
    failed_at: null,
    error: null,
    variants: null,
    // Progress (PROG-01..03): stage and stored-variant count, plus the
    // separate simulated-step counter (EXP-02).
    stage: null,
    variants_completed: 0,
    simulation_step: null,
  };
  state.retrievalError = false;
  state.pollCount = 0;

  renderResultsProcessing();
  startObservation();
}

// startObservation begins (or restarts) observation of state.job in the
// selected mode. It cancels any previous loop so at most one request is ever
// outstanding, stamps a fresh generation token so late responses from the old
// loop can be ignored, fetches one initial prompt snapshot to establish the
// baseline version (UI-02), and then enters the mode-specific loop.
async function startObservation() {
  stopObservation();
  const token = ++state.generation;
  const mode = selectedMode();
  const statusUrl = state.job.status_url;
  const controller = new AbortController();
  state.mode = mode;
  state.observing = true;
  state.observationAbortController = controller;
  state.job.mode = mode;
  renderJobCard(state.job);

  // Initial prompt snapshot: the server never waits on this request (API-01).
  let job;
  try {
    job = await fetchJobSnapshot(statusUrl, controller.signal);
  } catch (error) {
    if (token !== state.generation) return; // superseded
    if (controller.signal.aborted) return; // cancelled deliberately
    handleRetrievalError();
    return;
  }
  if (token !== state.generation) return;

  state.pollCount += 1;
  applySnapshot(job);

  if (job.status === "completed") {
    finishCompleted(job);
    return;
  }
  if (job.status === "failed") {
    finishFailed(job);
    return;
  }

  if (mode === "long") {
    longPollLoop(token, statusUrl);
  } else {
    shortPollLoop(token, statusUrl);
  }
}

// shortPollLoop asks for a prompt snapshot approximately every second until a
// terminal state, a retrieval error, or cancellation (UI-02).
async function shortPollLoop(token, statusUrl) {
  while (token === state.generation) {
    await sleep(POLL_INTERVAL_MS);
    if (token !== state.generation) return;

    let job;
    try {
      job = await fetchJobStatus(statusUrl, state.observationAbortController.signal);
    } catch (error) {
      if (token !== state.generation) return;
      if (state.observationAbortController.signal.aborted) return;
      handleRetrievalError();
      return;
    }
    if (token !== state.generation) return;

    state.pollCount += 1;
    applySnapshot(job);

    if (job.status === "completed") return finishCompleted(job);
    if (job.status === "failed") return finishFailed(job);
    renderResultsProcessing();
  }
}

// longPollLoop waits for a version newer than the last one it received. A 200
// is rendered and the next long poll is sent immediately; a 204 keeps the
// last version and immediately sends another request without parsing JSON;
// a terminal state stops (UI-03).
async function longPollLoop(token, statusUrl) {
  let version = state.job.version;

  while (token === state.generation) {
    let result;
    try {
      result = await observeJobAfter(
        statusUrl,
        version,
        state.observationAbortController.signal
      );
    } catch (error) {
      if (token !== state.generation) return;
      if (state.observationAbortController.signal.aborted) return;
      handleRetrievalError();
      return;
    }
    if (token !== state.generation) return;

    state.pollCount += 1;

    // Normal holding-deadline expiry: no newer version, no body. Keep the
    // last version and continue immediately (UI-03).
    if (result.status === 204) {
      state.job.__pollCount = state.pollCount;
      renderJobCard(state.job);
      renderResultsProcessing();
      continue;
    }

    const job = result.body;
    version = job.version;
    applySnapshot(job);

    if (job.status === "completed") return finishCompleted(job);
    if (job.status === "failed") return finishFailed(job);
    renderResultsProcessing();
  }
}

// applySnapshot merges a server representation into state.job and renders it.
// It computes the detection delay the moment a completion is observed.
function applySnapshot(job) {
  state.job = {
    ...state.job,
    status: job.status,
    version: job.version,
    mode: state.mode,
    queued_at: job.queued_at,
    started_at: job.started_at,
    completed_at: job.completed_at,
    failed_at: job.failed_at ?? state.job.failed_at,
    error: job.error ?? state.job.error,
    variants: job.variants ?? state.job.variants,
    // Progress is monotonic while processing; a failure keeps the last known
    // progress (PROG-03), which the ?? fallbacks preserve.
    stage: job.stage ?? state.job.stage,
    variants_completed: job.variants_completed ?? state.job.variants_completed,
    simulation_step: job.simulation_step ?? state.job.simulation_step,
    __pollCount: state.pollCount,
    __ackLatencyMs: state.ackLatencyMs,
  };

  if (job.status === "completed" && job.completed_at) {
    state.detectionDelayMs = Date.now() - Date.parse(job.completed_at);
    state.job.__detectionDelayMs = state.detectionDelayMs;
  }

  renderJobCard(state.job);
}

function finishCompleted(job) {
  stopObservation();
  renderJobCard(state.job);
  renderResults(job.variants);
}

function finishFailed(job) {
  stopObservation();
  renderJobCard(state.job);
  renderResultsFailed(job.error);
}

// handleRetrievalError stops observation but preserves the last known state,
// distinguishing "could not reach the server" from a server-reported failure
// (UI-05).
function handleRetrievalError() {
  stopObservation();
  state.retrievalError = true;
  renderJobCard(state.job, { mode: "retrieval-error" });
}

// resumeObservation backs Try again (UI-05). It observes the same job using
// the selected mode and never resubmits the image.
function resumeObservation() {
  if (!state.job || !state.job.status_url || state.observing) return;
  state.retrievalError = false;
  startObservation();
}

// handleModeChange restarts observation of the current job in the newly
// selected mode without uploading again (UI-04).
function handleModeChange() {
  state.mode = selectedMode();
  if (!state.job || !state.jobAccepted || isTerminal(state.job.status)) return;
  startObservation();
}

// stopObservation cancels the loop and any in-flight request. Incrementing
// the generation invalidates late responses from the cancelled loop
// (UI-04).
function stopObservation() {
  state.generation += 1;
  state.observing = false;
  if (state.observationAbortController) {
    state.observationAbortController.abort();
    state.observationAbortController = null;
  }
}

// A page unload should cancel any in-flight observation request rather than
// leaving it dangling (UI-04).
window.addEventListener("beforeunload", stopObservation);