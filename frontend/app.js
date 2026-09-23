import { state, MAX_IMAGE_BYTES } from "./state.js";
import { submitImage, fetchJobStatus } from "./modules/data-service.js";
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
    resumePolling();
  }
});

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

  // A new submission observes a new job, so any previous job's polling
  // loop must be cancelled first (POLL-06).
  stopPolling();

  state.isSubmitting = true;
  processButton.disabled = true;
  replaceButton.disabled = true;
  processButton.textContent = "Uploading...";
  hideMessage(errorMessage);
  hideMessage(statusMessage);
  showMessage(statusMessage, "Uploading...", "message-info");

  try {
    const result = await submitImage(state.selectedFile);

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

// ---------- job observation: short polling ----------

// beginObservingJob starts observation right after 202 Accepted. It seeds
// state.job from the acceptance response and renders the initial
// card before the first status GET has even been sent.
function beginObservingJob(accepted) {
  state.job = {
    id: accepted.job_id,
    image_id: accepted.image_id,
    status: accepted.status || "queued",
    status_url: accepted.status_url,
    queued_at: null,
    started_at: null,
    completed_at: null,
    failed_at: null,
    error: null,
    variants: null,
  };
  state.retrievalError = false;
  state.pollCount = 0;

  renderJobCard(state.job);
  renderResultsProcessing();

  startPolling(accepted.status_url);
}

function startPolling(statusUrl) {
  state.polling = true;
  state.pollAbortController = new AbortController();
  scheduleNextPoll(statusUrl, 0);
}

function scheduleNextPoll(statusUrl, delay) {
  state.pollTimeoutId = setTimeout(() => pollOnce(statusUrl), delay);
}

// pollOnce performs one status GET and decides what happens next: schedule
// another poll (still queued/processing), stop at a terminal state
// (completed/failed - POLL-05), or treat the failure as a retrieval error
// that stops the loop and offers Try again. It never
// treats "could not reach the server" as "the job failed".
async function pollOnce(statusUrl) {
  if (!state.polling || !state.pollAbortController) return;
  const controller = state.pollAbortController;

  let job;
  try {
    job = await fetchJobStatus(statusUrl, controller.signal);
  } catch (error) {
    if (controller.signal.aborted) return; // cancelled deliberately - not an error
    handleRetrievalError();
    return;
  }

  state.pollCount += 1;
  mergeJobResponse(job);
  state.job.__pollCount = state.pollCount;

  renderJobCard(state.job);

  if (job.status === "completed") {
    stopPolling();
    renderResults(job.variants);
    replaceButton.disabled = true;
    return;
  }

  if (job.status === "failed") {
    stopPolling();
    renderResultsFailed(job.error);
    replaceButton.disabled = true;
    return;
  }

  renderResultsProcessing();
  scheduleNextPoll(statusUrl, POLL_INTERVAL_MS);
}

function mergeJobResponse(job) {
  state.job = {
    ...state.job,
    status: job.status,
    queued_at: job.queued_at,
    started_at: job.started_at,
    completed_at: job.completed_at,
    failed_at: job.failed_at ?? state.job.failed_at,
    error: job.error ?? state.job.error,
    variants: job.variants ?? state.job.variants,
  };
}

function handleRetrievalError() {
  state.polling = false;
  state.retrievalError = true;
  if (state.pollTimeoutId) {
    clearTimeout(state.pollTimeoutId);
    state.pollTimeoutId = null;
  }
  renderJobCard(state.job, { mode: "retrieval-error" });
}

// resumePolling backs the Try again button (POLL-09/10). It re-requests the
// same status_url without resubmitting the image or creating a new job.
function resumePolling() {
  if (!state.job || !state.job.status_url) return;
  state.retrievalError = false;
  state.polling = true;
  state.pollAbortController = new AbortController();
  pollOnce(state.job.status_url);
}

// stopPolling cancels the loop and any in-flight request (POLL-05/POLL-06).
function stopPolling() {
  state.polling = false;
  if (state.pollTimeoutId) {
    clearTimeout(state.pollTimeoutId);
    state.pollTimeoutId = null;
  }
  if (state.pollAbortController) {
    state.pollAbortController.abort();
    state.pollAbortController = null;
  }
}

// A page unload should cancel any in-flight polling request rather than
// leaving it dangling (POLL-06).
window.addEventListener("beforeunload", stopPolling);