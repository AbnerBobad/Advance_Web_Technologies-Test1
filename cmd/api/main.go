// Command api is the ImageLab HTTP server. It implements an asynchronous
// "202 Accepted + job + worker" pattern: an upload is durably accepted over
// HTTP and the image variants are produced in the background by a worker.
package main

import (
	"context"
	"database/sql"
	"flag"
	"log/slog"
	"os"
	"time"

	"imagelab/internal/data"
	"imagelab/internal/files"

	_ "github.com/lib/pq"
)

const (
	defaultDSN      = "postgres://pooling:pa55word@localhost:5432/pooling?sslmode=disable"
	defaultUpload   = "data/uploads"
	defaultFrontend = "frontend"
)

// config bundles every setting for the server, taken from command-line flags.
type config struct {
	port int
	env  string
	db   struct {
		dsn          string
		maxOpenConns int
		maxIdleConns int
		maxIdleTime  time.Duration
	}
	uploadDir   string
	frontendDir string
}

// application holds the long-lived dependencies shared by every handler:
// configuration, a logger, the database models, and the file store.
type application struct {
	config config
	logger *slog.Logger
	models data.Models
	images *files.Store
}

func main() {
	// -----------------------------------------------------------------
	// Configuration
	// -----------------------------------------------------------------
	var cfg config

	flag.IntVar(&cfg.port, "port", 8080, "API server port")
	flag.StringVar(&cfg.env, "env", "development", "Environment (development|staging|production)")
	flag.StringVar(&cfg.db.dsn, "db-dsn", os.Getenv("POOLING_DB_DSN"), "PostgreSQL DSN")
	flag.IntVar(&cfg.db.maxOpenConns, "db-max-open-conns", 25, "PostgreSQL max open connections")
	flag.IntVar(&cfg.db.maxIdleConns, "db-max-idle-conns", 25, "PostgreSQL max idle connections")
	flag.DurationVar(&cfg.db.maxIdleTime, "db-max-idle-time", 15*time.Minute, "PostgreSQL max connection idle time")
	flag.StringVar(&cfg.uploadDir, "upload-dir", defaultUpload, "Directory for original and generated image files")
	flag.StringVar(&cfg.frontendDir, "frontend-dir", defaultFrontend, "Directory for the served static frontend")

	flag.Parse()

	if cfg.db.dsn == "" {
		cfg.db.dsn = defaultDSN
	}

	// -----------------------------------------------------------------
	// Dependencies
	// -----------------------------------------------------------------
	// Structured logger writing to stdout.
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Open the PostgreSQL connection pool.
	db, err := openDB(cfg)
	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
	defer db.Close()

	logger.Info("database connection pool established")

	// Local filesystem store for uploaded originals (and later variants).
	fileStore, err := files.NewStore(cfg.uploadDir)
	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}

	// Build the application with the data models wired to the pool.
	app := &application{
		config: cfg,
		logger: logger,
		models: data.NewModels(db),
		images: fileStore,
	}

	// Block until the HTTP server shuts down gracefully, then exit.
	err = app.serve()
	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}

// openDB opens a PostgreSQL connection pool, tunes its limits, and verifies
// the database is reachable with a short ping before returning.
func openDB(cfg config) (*sql.DB, error) {
	db, err := sql.Open("postgres", cfg.db.dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(cfg.db.maxOpenConns)
	db.SetMaxIdleConns(cfg.db.maxIdleConns)
	db.SetConnMaxIdleTime(cfg.db.maxIdleTime)

	// Ping with a timeout so startup fails fast when the DB is unavailable
	// instead of hanging on the default connection timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = db.PingContext(ctx)
	if err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}
