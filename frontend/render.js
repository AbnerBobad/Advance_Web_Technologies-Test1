// render.js owns every DOM update. It keeps the file preview, messages, and
// job card in sync with the page state without embedding business logic.
export function formatBytes(bytes) {
  if (bytes < 1024) return bytes + " B";
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + " KB";
  return (bytes / (1024 * 1024)).toFixed(2) + " MB";
}

export function renderPreview(file, objectURL) {
  const previewArea = document.getElementById("previewArea");
  const previewImage = document.getElementById("previewImage");

  previewImage.src = objectURL;
  previewImage.alt = "Selected image preview";
  document.getElementById("fileName").textContent = file.name;
  document.getElementById("fileSize").textContent = formatBytes(file.size);
  document.getElementById("fileType").textContent = file.type || "unknown";
  previewArea.classList.remove("hidden");
}

export function clearPreview() {
  const previewArea = document.getElementById("previewArea");
  previewArea.classList.add("hidden");
  document.getElementById("previewImage").removeAttribute("src");
}

export function renderAcceptedJob(data) {
  const jobCardBody = document.getElementById("jobCardBody");
  jobCardBody.innerHTML = "";

  const meta = document.createElement("pre");
  meta.className = "job-meta";
  meta.textContent =
    "Status: 202 Accepted\n" +
    "Job ID:   " + (data.job_id ?? "-") + "\n" +
    "Image ID: " + (data.image_id ?? "-") + "\n" +
    "Status:   " + (data.status ?? "-") + "\n" +
    "Status URL: " + (data.status_url ?? "-");
  jobCardBody.appendChild(meta);

  const note = document.createElement("p");
  note.textContent = "Work accepted. Background processing will begin separately.";
  jobCardBody.appendChild(note);
}

export function showMessage(el, text, className) {
  el.textContent = text;
  el.className = "message " + className;
  el.classList.remove("hidden");
}

export function hideMessage(el) {
  el.classList.add("hidden");
  el.textContent = "";
}