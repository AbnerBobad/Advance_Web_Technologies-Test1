package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// serve runs the HTTP server and blocks until it receives a shutdown signal
// (SIGINT/SIGTERM). On shutdown it gracefully drains in-flight requests. In
// Week 2 the background worker is started here and cancelled during shutdown
// so no new jobs are claimed while the server winds down.
func (app *application) serve() error {
	// The HTTP server with sane timeouts. Long-running processing belongs to
	// the background worker, never inside a request, so requests stay short.
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", app.config.port),
		Handler:      app.routes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// shutdownError carries the outcome of graceful shutdown back to serve().
	shutdownError := make(chan error)

	// Signal-listener goroutine: wait for SIGINT/SIGTERM, then shut down.
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		s := <-quit

		app.logger.Info("caught signal", "signal", s.String())

		// Give in-flight requests up to 30s to finish.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := srv.Shutdown(ctx)
		if err != nil {
			shutdownError <- err
			return
		}

		shutdownError <- nil
	}()

	app.logger.Info("starting server", "addr", srv.Addr, "env", app.config.env)

	// ListenAndServe blocks; it only returns ErrServerClosed after Shutdown.
	err := srv.ListenAndServe()
	if !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	// Wait for the shutdown goroutine to report success or failure.
	err = <-shutdownError
	if err != nil {
		return err
	}

	app.logger.Info("stopped server", "addr", srv.Addr)

	return nil
}
