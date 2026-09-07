package main

import "net/http"

// routes registers every HTTP endpoint on a ServeMux. Go 1.22+ method and
// path patterns are used. API routes are explicit; any other path falls
// through to the static frontend.
func (app *application) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/healthcheck", app.healthcheckHandler)
	mux.HandleFunc("POST /v1/images", app.createImageHandler)
	mux.Handle("/", http.FileServer(http.Dir(app.config.frontendDir)))
	return mux
}
