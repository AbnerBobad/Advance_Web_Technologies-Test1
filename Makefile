# ImageLab Makefile. Run `make help` to see available targets.
# Secrets-free: .envrc is gitignored and only provides DSN values locally.

-include .envrc

# ---------------------------------------------------------------------------
# CONNECTION
# ---------------------------------------------------------------------------

# POOLING_DB_DSN comes from .profile/.envrc (defaults to the local dev DSN).
POOLING_DB_DSN ?= postgres://pooling:pa55word@localhost:5432/pooling?sslmode=disable

# Migrations are tracked inside the imagelab schema because the pooling role
# cannot write to the shared public schema (PostgreSQL 15+ default).
MIGRATE_DB_URL = $(POOLING_DB_DSN)&options=-csearch_path=imagelab,public&x-migrations-table=schema_migrations

# ==================================================================================== #
# HELPERS
# ==================================================================================== #

## help: print this help message
.PHONY: help
help:
	@echo 'Usage:'
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' | sed -e 's/^/ /'

.PHONY: confirm
confirm:
	@echo -n 'Are you sure? [y/N] ' && read ans && [ $${ans:-N} = y ]

# ==================================================================================== #
# DEVELOPMENT
# ==================================================================================== #

## run/api: run the ImageLab application
.PHONY: run/api
run/api:
	go run ./cmd/api -db-dsn=${POOLING_DB_DSN}

## db/psql: connect to the database using psql
.PHONY: db/psql
db/psql:
	psql ${POOLING_DB_DSN}

## db/migrations/new name=$1: create a new database migration pair
.PHONY: db/migrations/new
db/migrations/new:
	@echo 'Creating migration files for ${name}...'
	migrate create -seq -ext=.sql -dir=./migrations ${name}

## db/migrations/up: apply all up database migrations
.PHONY: db/migrations/up
db/migrations/up: confirm
	@echo 'Running up migrations...'
	migrate -path ./migrations -database ${MIGRATE_DB_URL} up

## db/migrations/down: roll back the last database migration
.PHONY: db/migrations/down
db/migrations/down: confirm
	@echo 'Rolling back last migration...'
	migrate -path ./migrations -database ${MIGRATE_DB_URL} down 1

# ==================================================================================== #
# QUALITY CONTROL
# ==================================================================================== #

## tidy: format all .go files and tidy module dependencies
.PHONY: tidy
tidy:
	@echo 'Tidying module dependencies...'
	go mod tidy
	@echo 'Formatting .go files...'
	go fmt ./...

## audit: run quality control checks
.PHONY: audit
audit:
	@echo 'Checking module dependencies...'
	go mod tidy -diff
	@echo 'Vetting code...'
	go vet ./...
	@echo 'Running tests...'
	go test ./...

## smoke: run the API acceptance smoke test (server must be running)
.PHONY: smoke
smoke:
	./scripts/smoke.sh

# ==================================================================================== #
# BUILD
# ==================================================================================== #

## build/api: build the cmd/api application
.PHONY: build/api
build/api:
	@echo 'Building cmd/api...'
	go build -ldflags="-s" -o=./bin/api ./cmd/api