# ImageLab

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