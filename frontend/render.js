// render.js owns every DOM update. It keeps the file preview, messages, the
// processing-job card, and the results grid in sync with page state without
// embedding business logic (polling timing, retry decisions, etc. live in
// app.js). Every value that came from the server is written with
// textContent, never innerHTML, per the Section 13 safeguard.

export function formatBytes(bytes) {
  if (bytes < 1024) return bytes + " B";
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + " KB";
  return (bytes / (1024 * 1024)).toFixed(2) + " MB";
}

function formatTime(iso) {
  if (!iso) return null;
  const d = new Date(iso);
  if (isNaN(d.getTime())) return null;
  return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined && text !== null) node.textContent = text;
  return node;
}

const ICONS = { done: "\u2713", error: "!" };

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

// ---------- processing job card ----------

const STATUS_BADGE = {
  queued: { text: "Queued", className: "status-badge status-queued" },
  processing: { text: "Processing", className: "status-badge status-processing" },
  completed: { text: "Completed", className: "status-badge status-completed" },
  failed: { text: "Failed", className: "status-badge status-failed" },
};

// formatJobId keeps long UUID-style public IDs from wrapping into an
// unreadable block. The full value is always available via the title
// attribute for reference.
function formatJobId(id) {
  const value = String(id ?? "-");
  if (value.length <= 14) return value;
  return value.slice(0, 8) + "\u2026" + value.slice(-4);
}

function buildStep(stepState, title, time) {
  const step = el("li", "timeline-step timeline-step--" + stepState);
  const dot = el("span", "timeline-dot");
  if (stepState === "done") dot.textContent = ICONS.done;
  else if (stepState === "error") dot.textContent = ICONS.error;
  else if (stepState === "active") dot.appendChild(el("span", "timeline-spinner"));
  step.appendChild(dot);

  const text = el("div", "timeline-text");
  text.appendChild(el("p", "timeline-title", title));
  if (time) text.appendChild(el("p", "timeline-time", time));
  step.appendChild(text);
  return step;
}

// buildTimeline derives the four required steps (UI-11) purely from the
// job's status and timestamps, so it always matches the server's view.
function buildTimeline(job) {
  const list = el("ol", "timeline");

  const acceptedTime = formatTime(job.queued_at) || "Just now";
  list.appendChild(buildStep("done", "Upload accepted", acceptedTime));
  list.appendChild(buildStep("done", "Original stored", acceptedTime));

  if (job.status === "queued") {
    list.appendChild(buildStep("pending", "Generating variants", null));
  } else if (job.status === "processing") {
    list.appendChild(buildStep("active", "Generating variants", formatTime(job.started_at)));
  } else {
    // completed or failed: the worker did claim the job.
    list.appendChild(buildStep("done", "Generating variants", formatTime(job.started_at)));
  }

  if (job.status === "completed") {
    list.appendChild(buildStep("done", "Complete", formatTime(job.completed_at)));
  } else if (job.status === "failed") {
    list.appendChild(buildStep("error", "Failed", formatTime(job.failed_at)));
  } else {
    list.appendChild(buildStep("pending", "Complete", null));
  }

  return list;
}

// buildPollPanel renders exactly one of: the automatic-polling indicator
// (UI-12), a terminal summary, or the retrieval-error / Try again state
// (POLL-09). app.js decides which via the `mode` flag; this module only
// renders it.
function buildPollPanel(job, mode) {
  const panel = el("div", "poll-panel");

  if (mode === "retrieval-error") {
    panel.classList.add("poll-panel--error");
    const row = el("div", "poll-row");
    row.appendChild(el("span", "poll-icon poll-icon--warn", "!"));
    row.appendChild(el("span", "poll-title", "Unable to check status"));
    panel.appendChild(row);
    panel.appendChild(
      el("p", "poll-sub", "The job may still be running. Your last known status is shown.")
    );
    const button = el("button", "button button-outline poll-retry");
    button.type = "button";
    button.id = "tryAgainButton";
    button.textContent = "Try again";
    panel.appendChild(button);
    return panel;
  }

  if (job.status === "completed") {
    panel.classList.add("poll-panel--done");
    const row = el("div", "poll-row");
    row.appendChild(el("span", "poll-icon poll-icon--done", "\u2713"));
    row.appendChild(el("span", "poll-title", "Processing complete"));
    panel.appendChild(row);
    panel.appendChild(el("p", "poll-sub", "All variants are ready below."));
    return panel;
  }

  if (job.status === "failed") {
    panel.classList.add("poll-panel--error");
    const row = el("div", "poll-row");
    row.appendChild(el("span", "poll-icon poll-icon--warn", "!"));
    row.appendChild(el("span", "poll-title", "Processing failed"));
    panel.appendChild(row);
    panel.appendChild(el("p", "poll-sub", job.error || "The job could not be completed."));
    return panel;
  }

  // queued or processing: automatic short polling is active.
  const row = el("div", "poll-row");
  row.appendChild(el("span", "poll-spinner"));
  row.appendChild(el("span", "poll-title", "Checking status automatically"));
  panel.appendChild(row);
  panel.appendChild(el("p", "poll-sub", "Every 1 second"));
  if (job.__pollCount) {
    const label = job.__pollCount + " status check" + (job.__pollCount === 1 ? "" : "s") + " so far";
    panel.appendChild(el("p", "poll-count", label));
  }
  return panel;
}

// renderJobCard is the single entry point used for every job-card update:
// right after 202 Accepted, after every polling response, and after a
// retrieval error. options.mode is "polling" (default) or "retrieval-error".
export function renderJobCard(job, options) {
  const mode = (options && options.mode) || "polling";
  const jobCardBody = document.getElementById("jobCardBody");
  jobCardBody.innerHTML = "";
  jobCardBody.classList.add("job-body--active");
  jobCardBody.dataset.status = job.status || "";

  const grid = el("div", "job-card-grid");

  const main = el("div", "job-main");
  const idRow = el("div", "job-id-row");
  idRow.appendChild(el("span", "job-id-label", "Job ID"));
  const idChip = el("span", "job-id-value", formatJobId(job.id));
  idChip.title = String(job.id ?? "-");
  idRow.appendChild(idChip);
  main.appendChild(idRow);
  main.appendChild(buildTimeline(job));
  grid.appendChild(main);

  const side = el("div", "job-side");
  const badge = STATUS_BADGE[job.status] || { text: job.status || "Unknown", className: "status-badge" };
  side.appendChild(el("span", badge.className, badge.text));
  side.appendChild(buildPollPanel(job, mode));
  grid.appendChild(side);

  jobCardBody.appendChild(grid);
}

// ---------- results panel ----------

export function renderResultsEmpty() {
  const panel = document.getElementById("resultsBody");
  panel.innerHTML = "";
  const empty = el("div", "empty-state empty-state-wide");
  empty.appendChild(el("p", "empty-title", "No images generated yet"));
  empty.appendChild(el("p", "empty-sub", "Processed image variants will appear here"));
  panel.appendChild(empty);
}

export function renderResultsProcessing() {
  const panel = document.getElementById("resultsBody");
  panel.innerHTML = "";
  const empty = el("div", "empty-state empty-state-wide");
  empty.appendChild(el("span", "poll-spinner poll-spinner--lg"));
  empty.appendChild(el("p", "empty-title", "Images are being generated"));
  empty.appendChild(el("p", "empty-sub", "Results will appear when processing completes"));
  panel.appendChild(empty);
}

export function renderResultsFailed(safeError) {
  const panel = document.getElementById("resultsBody");
  panel.innerHTML = "";
  const empty = el("div", "empty-state empty-state-wide");
  empty.appendChild(el("p", "empty-title", "Processing failed"));
  empty.appendChild(el("p", "empty-sub", safeError || "No images were generated for this job."));
  panel.appendChild(empty);
}

const VARIANT_LABELS = { thumbnail: "Thumbnail", preview: "Preview", display: "Display" };

function buildResultCard(variant) {
  const card = el("article", "result-card");

  const thumb = el("div", "result-thumb");
  const img = document.createElement("img");
  img.src = variant.url;
  img.alt = (VARIANT_LABELS[variant.name] || variant.name) + " variant";
  thumb.appendChild(img);
  thumb.appendChild(el("span", "result-ready-badge", "Ready"));
  card.appendChild(thumb);

  const info = el("div", "result-info");
  info.appendChild(el("p", "result-name", VARIANT_LABELS[variant.name] || variant.name));
  info.appendChild(el("p", "result-dims", variant.width + " x " + variant.height));

  const link = document.createElement("a");
  link.className = "result-download";
  link.href = variant.url;
  link.setAttribute("download", "");
  link.setAttribute("aria-label", "Download " + (VARIANT_LABELS[variant.name] || variant.name));
  link.textContent = "Download";
  info.appendChild(link);

  card.appendChild(info);
  return card;
}

export function renderResults(variants) {
  const panel = document.getElementById("resultsBody");
  panel.innerHTML = "";

  if (!variants || variants.length === 0) {
    renderResultsEmpty();
    return;
  }

  const order = ["thumbnail", "preview", "display"];
  const sorted = variants.slice().sort((a, b) => order.indexOf(a.name) - order.indexOf(b.name));

  const grid = el("div", "results-grid");
  for (const variant of sorted) {
    grid.appendChild(buildResultCard(variant));
  }
  panel.appendChild(grid);
}

// ---------- messages ----------

export function showMessage(el, text, className) {
  el.textContent = text;
  el.className = "message " + className;
  el.classList.remove("hidden");
}

export function hideMessage(el) {
  el.classList.add("hidden");
  el.textContent = "";
}