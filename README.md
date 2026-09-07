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

The connection string is read from `POOLING_DB_DSN`, which your shell profile
(and `.envrc`) exports:

```sh
export POOLING_DB_DSN='postgres://pooling:pa55word@localhost:5432/pooling?sslmode=disable'
```

Create the environment file (already present locally, gitignored):

```sh
cp .envrc.example .envrc
```

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

Open <http://localhost:8080>.

Flags (all optional, defaults shown):

| Flag                   | Default                        | Purpose                         |
| ---------------------- | ------------------------------ | ------------------------------- |
| `-port`                | `8080`                         | HTTP listen port                |
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
Location: /v1/jobs/42
Content-Type: application/json

{
  "image_id": 108,
  "job_id": 42,
  "status": "queued",
  "status_url": "/v1/jobs/42"
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

## Week 1 status

Implemented: initial / image-selected / uploading UI states, local preview
without upload, server-side validation (type, size, presence, decodability),
server-controlled stored filenames, durable image + queued job records, and
`202 Accepted` acknowledgement with duplicate-submission protection.

Not yet implemented: the background worker and variant generation (Week 2), the
`GET /v1/jobs/{id}` resource (Week 2), short polling and the full job/UI
lifecycle (Week 3), measurements (Week 4).