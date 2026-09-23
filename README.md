# ImageLab
![ImageLab UI](https://github.com/user-attachments/assets/0e648f1a-700e-4da6-a913-41663e150807)

Asynchronous image processing with **202 Accepted** and **one-second short polling**.

The upload request accepts and preserves the work. A background worker performs
the transformation. PostgreSQL owns the authoritative job state, and the
browser observes that state through short polling (Weeks 2-3).

## Architecture

```
Select image → POST /v1/images → 202 Accepted + status URL
                                        ↓
                              one background worker (Week 2)
                                        ↓
        browser polls GET /v1/jobs/{id} every 1 second (Week 3)
                                        ↓
                   completed → display all variants
```

- **Browser** — selects and previews an image, submits it once, renders job state.
- **Go API** (`cmd/api`) — validates and stores the original, durably records an
  image + queued job, returns `202 Accepted`. It never generates variants.
- **PostgreSQL** — owns the authoritative `images`, `jobs`, and `variants` records.
- **Filesystem** — stores originals under `data/uploads` with server-controlled names.
- **Worker** — one in-process goroutine performs the transformations (Week 2).

## Prerequisites

- Go 1.25+
- PostgreSQL 14+ running locally
- [golang-migrate](https://github.com/golang-migrate/migrate) CLI (`migrate`)
- A database named `pooling` with a role `pooling` (password `pa55word`)

The connection string is provided by the project's `.envrc` file via
[direnv](https://direnv.net). It is not exported from `~/.profile`; direnv
loads it automatically when you enter the project directory:

```sh
# .envrc  (already present, gitignored)
export POOLING_DB_DSN='postgres://pooling:pa55word@localhost/pooling?sslmode=disable'
```

To enable direnv on a new machine: install direnv, add the hook to your shell
(`eval "$(direnv hook bash)"` in `~/.bashrc`), and allow the project's envrc
(`direnv allow` inside the project). On a fresh clone, create `.envrc` from the
tracked example (`cp .envrc.example .envrc`). The Makefile reads
`POOLING_DB_DSN` from the environment and never hardcodes it.

## Setup

```sh
# run this once so the object schema exists (migrations target the imagelab
# schema; it must exist before the migrate version table is created)
psql "$POOLING_DB_DSN" -c 'CREATE SCHEMA IF NOT EXISTS imagelab;'

# apply migrations
make db/migrations/up
# or: migrate -path ./migrations -database "${POOLING_DB_DSN}&options=-csearch_path=imagelab,public&x-migrations-table=schema_migrations" up
```

## Run

```sh
make run/api
# or directly:
go run ./cmd/api

# with a custom port:
go run ./cmd/api -port=4000 -db-dsn="${POOLING_DB_DSN}"
```

Open <http://localhost:4000>.

Flags (all optional, defaults shown):

| Flag                   | Default                        | Purpose                         |
| ---------------------- | ------------------------------ | ------------------------------- |
| `-port`                | `4000`                         | HTTP listen port                |
| `-env`                 | `development`                  | Reported by `/v1/healthcheck`   |
| `-db-dsn`              | `$POOLING_DB_DSN` or local DSN | PostgreSQL connection string    |
| `-db-max-open-conns`   | `25`                           | Pool: max open connections      |
| `-db-max-idle-conns`   | `25`                           | Pool: max idle connections      |
| `-db-max-idle-time`    | `15m`                          | Pool: max connection idle time  |
| `-upload-dir`          | `data/uploads`                 | Image file directory            |
| `-frontend-dir`        | `frontend`                     | Served static frontend dir      |

## API

### `GET /v1/healthcheck`

Reports availability and the configured environment.

### `POST /v1/images`

Multipart form upload with a single `file` field (JPEG or PNG, max 10 MB).

Acceptance boundary: validate → store the original → create the image record →
create the `queued` job → then return `202 Accepted`. The handler does not
process the image.

```http
HTTP/1.1 202 Accepted
Location: /v1/jobs/6bd9a1ba-253a-4bcd-beed-750796768611
Content-Type: application/json

{
  "image_id": "01a0892f-e8ff-7ebd-b999-7fd7ffc82042",
  "job_id": "6bd9a1ba-253a-4bcd-beed-750796768611",
  "status": "queued",
  "status_url": "/v1/jobs/6bd9a1ba-253a-4bcd-beed-750796768611"
}
```

Rejections use client-safe status codes (400/413/415) and never create a job.

## Project layout

```
cmd/api/            HTTP server: main, routes, server, helpers, errors,
                    healthcheck, images (upload handler)
frontend/           Vanilla JS client: app.js, state.js, render.js,
                    modules/data-service.js, style.css
internal/data/      Database models: images, jobs, variants, Models aggregate
internal/files/     Server-controlled filesystem storage
internal/validator/ Field-validation helper
migrations/         golang-migrate up/down pairs (imagelab schema)
scripts/smoke.sh    Repeatable API acceptance checks
Makefile            run, migrate, tidy, audit, build, smoke targets
```

## Smoke tests

```sh
go run ./cmd/api &
make smoke
```

Or run the checks individually against a live server with `BASE` set.

# Week 1 Report

## Deliverables Status

| Deliverable | Description | Link |
| :--- | :--- | :--- |
| **Week1-Progress-Report** | Google Docs | [View Report](https://docs.google.com/document/d/1jtus5H4Qxr6YPU9RgY7I9NvQBndn3Fz8Dca9ynAyc10/edit?usp=sharing) |

**Implemented:** Initial / image-selected / uploading UI states, local preview without upload, server-side validation (type, size, presence, decodability), server-controlled stored filenames, durable image + queued job records, and `202 Accepted` acknowledgement with duplicate-submission protection.

**Not Yet Implemented:** Background worker and variant generation (Week 2), `GET /v1/jobs/{id}` resource (Week 2), short polling and full job/UI lifecycle (Week 3), measurements (Week 4).

---

## Progress Summary

The ImageLab successfully covered the Week 1 scope by creating the asynchronous ingest boundary for the `POST /v1/images` API endpoint. The server verifies file uploads via byte-sniffing with the Go standard library, stores uploaded files in `data/uploads/`, performs atomic inserts of image and job data into the database in a single transaction, and then sends a `202 Accepted` response containing the job data and a `Location` header.

The framework is implemented on top of a multi-layer architecture consisting of `cmd/api/`, `internal/data`, `internal/validator`, and `internal/files`. The project skeleton also consists of a health check, a frontend static application with client-side pre-checks and error handling, migrations using the `golang-migrate` pair, and a Makefile-driven workflow.

End-to-end validation was performed using four migration pairs against a specific ImageLab database schema. A total of 11 tests were executed to verify the correct responses and ensure that the expected `400 Bad Request`, `413 Payload Too Large`, and `415 Unsupported Media Type` error responses were received. These tests also verified that database records and uploaded files were properly cleaned up after each test.

**Validation Pipeline Challenges**
Building an effective server-side validation pipeline for images proved more complex than initially expected because client-provided metadata was easy to spoof, rendering simple header-based checks inadequate. Using only Go's `http.DetectContentType` functionality introduced edge cases (such as handling the `charset=` parameter appended to the MIME type) and did not protect against simple file-renaming attacks. In addition, unchecked uploads could unnecessarily consume server resources.

To address these issues, the validation pipeline was implemented in strict sequence:
1. `http.MaxBytesReader` handles files that exceed the allowed size and returns a `413 Payload Too Large` response.
2. Sanitized header sniffing rejects files with a MIME type other than JPEG or PNG and returns a `415 Unsupported Media Type` response.
3. `image.Decode` verifies the actual image data and handles broken, truncated, or incorrectly formatted payloads, returning a `400 Bad Request` response when necessary.

---

## Week 1 Checklist

- [x] Run the starter project and document the setup procedure.
- [x] Configure PostgreSQL and apply the supplied or completed migrations.
- [x] Identify the responsibilities of the browser, API, database, worker, and filesystem.
- [x] Implement the initial, image-selected, and uploading UI states.
- [x] Show a local browser preview without uploading or creating a job.
- [x] Accept JPEG and PNG files up to 10 MB and reject unsupported input on the server.
- [x] Generate a server-controlled filename and store the original image.
- [x] Create or prepare the image, job, and variant data model.
- [x] Prevent a second overlapping POST from the same page by using a disabled button and an `isSubmitting` guard.

# Week 2 Report
 
## Deliverables Status
 
| Deliverable | Description | Link |
| :--- | :--- | :--- |
| **Week2-Progress-Report** | Google Docs | [View Report](https://docs.google.com/document/d/1FGbYIN3TEjEOyyEQi6hn9vln2Hi2iWua2oDCASdfX1c/edit?usp=sharing) |
 
**Implemented:** Background worker (`cmd/api/worker.go`), thumbnail/preview/display variant generation (`internal/imageproc`, standard library only), `GET /v1/jobs/{job_id}` resource with variant metadata, `GET /v1/images/{image_id}/variants/{name}` file serving, atomic job claiming (`SELECT ... FOR UPDATE SKIP LOCKED`), full `queued` → `processing` → `completed`/`failed` lifecycle with timestamps, and graceful worker shutdown.
 
**Not Yet Implemented:** Short polling and full job/UI lifecycle rendering (Week 3), retrieval-error handling and `Try again` (Week 3), measurements and five-image burst testing (Week 4).
 
---
 
## Progress Summary
 
Week 2 completed the server-side asynchronous path: the upload request accepts and responds with `202 Accepted` before any image manipulation occurs, with all transformation work happening in the background. Processing logic is implemented at two levels. The `internal/imageproc` package generates the three required variants using only the Go standard library: a 150×150 square thumbnail (center cropped from the original), a preview that fits within 800×600 pixels, and a display that fits within 1200×900 pixels. The two fitted variants preserve the original aspect ratio and are never upscaled; all outputs are encoded in the same format as the source.
 
The worker (`cmd/api/worker.go`) runs exactly one background worker in the application process. On each tick it claims one queued job at a time using `SELECT ... FOR UPDATE SKIP LOCKED`, sets `started_at`, reads the original image from the filesystem via the stored filename, generates all three variants, saves each variant file and its metadata row, and only then marks the job `completed`. If decoding, reading, generating, or saving fails at any point, every file created so far is removed and the job is marked `failed` with a client-safe error message.
 
The visible component was also added: `GET /v1/jobs/{id}` reports the job's current state and timestamps, and once completed, each variant's metadata and a URL to fetch it; a separate `GET /v1/images/{image_id}/variants/{name}` endpoint serves the actual files. `internal/data` gained the worker and status-facing methods (`GetByPublicID`, `ClaimNext`, `MarkCompleted`, `MarkFailed`), and `internal/files` gained a `Read` method. The application starts the worker on launch using a cancellable context and, on shutdown, cancels the worker and waits for it to finish before exiting.
 
**Verification**
The `POST /v1/images` response consistently returned in well under a second, with the worker's (optional, configurable) processing delay applied only inside the background loop, confirming the response genuinely precedes the work rather than merely appearing to. Querying the status endpoint during a live run showed the database cycling through `queued` → `processing` → `completed`, with `queued_at`, `started_at`, and `completed_at` set at the correct transition points. The upload handler itself created no variants: the `variants` table was empty at the moment `202` was returned. The filesystem held the original file plus all three generated outputs. A deliberately wide 2000×500 source image produced 150×150, 800×200, and 1200×300 outputs, confirming the aspect ratio and no distortion contracts. Deleting the stored original before the worker read it correctly produced a `failed` job with the safe message `could not read the original image`. The database's status CHECK constraint was also confirmed to reject an illegal status value directly. `go build`, `go vet`, and `go test` all pass, and the smoke suite passes all 21 checks. The Week 2 gate holds: transformations never run inside the request handler.
 
---
 
## Week 2 Checklist
 
- [x] Create the image and queued-job records as part of successful acceptance.
- [x] Return HTTP 202 Accepted with Location, image_id, job_id, status, and status_url.
- [x] Expose GET /v1/jobs/{job_id}.
- [x] Run exactly one background worker inside the Go application.
- [x] Find and claim queued work, then record started_at.
- [x] Generate thumbnail, preview, and display variants using the required contracts.
- [x] Store variant files and metadata.
- [x] Mark the job completed only after every variant is available.

# Week 3 Report
 
## Deliverables Status
 
| Deliverable | Description | Link |
| :--- | :--- | :--- |
| **Week3-Progress-Report** | Google Docs | [View Report](https://docs.google.com/document/d/1AQbBZnoQDHpdpN8CCbsh9oddJ1U5fjJtMYfrm7Qs6UI/edit?usp=sharing) |
 
**Implemented:** One-second short polling that starts only after `202 Accepted` (`frontend/app.js`), a `frontend/modules/data-service.js` status fetch with a 5 s per-request timeout, full job/UI lifecycle rendering (`frontend/render.js`), `AbortController` cancellation, retrieval-error handling with a `Try again` action that resumes the same job, and a replace-image flow.
 
**Not Yet Implemented:** Measurements (acknowledgement, queue wait, processing, job duration, polling count, detection delay), the five-image burst experiment, and the final Week 4 integration checklist.
 
---
 
## Progress Summary
 
Week 3 connected the browser to the durable job resource. Polling begins only after the POST returns `202 Accepted` with a `status_url` (`beginObservingJob` in `frontend/app.js`); selecting a file never starts a poll. `app.js` then runs a loop that sends one `GET /v1/jobs/{id}` roughly every second while the last known state is `queued` or `processing`, and stops immediately on `completed` or `failed`. Each response is fetched by `fetchJobStatus` in `frontend/modules/data-service.js`, which combines the canceled caller signal with a 5-second per-request timeout via `AbortSignal.any`, so a slow or failed response is treated as a retrieval error rather than processing failure.
 
The UI renders the authoritative four-step timeline (upload accepted, original stored, generating variants, complete) from the job's status and timestamps (`buildTimeline` in `frontend/render.js`), updates the status badge and timestamps after every response, and keeps the results panel in an "Images are being generated" state until `completed`. Results cards with names, actual dimensions, and view/download controls appear only at completion; a `failed` job shows the safe error and never marks `Complete`.
 
Retrieval errors follow the required decision rule: the job and its last known state are preserved, automatic polling stops, and the card offers `Try again` (`handleRetrievalError`/`resumePolling`). `Try again` re-requests the existing `status_url` without resubmitting the image, so it can discover that a job completed while observation was interrupted. A new accepted job cancels observation of any previous job, and a `beforeunload` handler cancels in-flight polling on page shutdown (`stopPolling`, driven by `AbortController`). The smoke suite's polling loop already observes the `queued` → `processing` → `completed` transition, `go build`, `go vet`, and `go test` all pass, and the smoke checks continue to pass.
 
---
 
## Week 3 Checklist
 
- [x] Begin polling only after receiving 202 and a status_url.
- [x] Send one GET status request approximately every second while queued or processing.
- [x] Ensure each server response returns promptly with the current state.
- [x] Update the status badge, timeline, timestamps, and polling indicator after every response.
- [x] Keep results hidden while queued or processing.
- [x] Stop polling on completed and display all three variants.
- [x] Stop polling on failed and display a safe processing error.
- [x] Use AbortController to cancel observation when a different job is started or the page unloads.
- [x] On a retrieval error, preserve the job and last known state, stop polling, and offer Try again.
- [x] Make Try again resume observation of the same status_url without resubmitting the image.