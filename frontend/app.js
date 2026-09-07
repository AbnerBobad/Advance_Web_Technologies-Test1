import { state, MAX_IMAGE_BYTES } from "./state.js";
import { submitImage } from "./modules/data-service.js";
import {
  renderPreview,
  clearPreview,
  renderAcceptedJob,
  showMessage,
  hideMessage,
} from "./render.js";

const fileInput = document.getElementById("fileInput");
const chooseButton = document.getElementById("chooseButton");
const processButton = document.getElementById("processButton");
const selectionMessage = document.getElementById("selectionMessage");
const statusMessage = document.getElementById("statusMessage");
const errorMessage = document.getElementById("errorMessage");

chooseButton.addEventListener("click", () => fileInput.click());
fileInput.addEventListener("change", handleFileSelection);
processButton.addEventListener("click", submit);

// handleFileSelection produces the image-selected state: a browser-owned
// preview only. Selecting a file never uploads it or starts a job.
function handleFileSelection() {
  const file = fileInput.files[0];
  if (!file) return;

  hideMessage(selectionMessage);
  hideMessage(errorMessage);

  const typeOk = file.type === "image/jpeg" || file.type === "image/png";
  const sizeOk = file.size > 0 && file.size <= MAX_IMAGE_BYTES;

  if (!typeOk || !sizeOk) {
    clearSelection();
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

  state.selectedFile = file;
  state.jobAccepted = false;
  processButton.disabled = false;
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
}

// submit sends exactly one POST. The isSubmitting guard and the disabled
// button prevent an overlapping second request from this page.
async function submit() {
  if (state.isSubmitting) return;
  if (!state.selectedFile || state.jobAccepted) return;

  state.isSubmitting = true;
  processButton.disabled = true;
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
    renderAcceptedJob(result.body);
    processButton.textContent = "Process image";
    processButton.disabled = true;
  } catch (error) {
    // A rejection or network failure must not leave the page unusable: the
    // user may choose a new file or attempt the submission again.
    const message = error && error.message ? error.message : "could not reach the server";
    showMessage(errorMessage, "Upload failed: " + message, "message-error");

    if (!state.jobAccepted) {
      processButton.disabled = !state.selectedFile;
    }
    processButton.textContent = "Process image";
  } finally {
    state.isSubmitting = false;
  }
}