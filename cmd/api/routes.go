package main

import "net/http"

// routes registers every HTTP endpoint on a ServeMux. Go 1.22+ method and
// path patterns are used; {id} captures the job's public ID path segment and
// {image_id}/{name} address one known generated variant. Any other path falls
// through to the static frontend.
func (app *application) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/healthcheck", app.healthcheckHandler)
	mux.HandleFunc("POST /v1/images", app.createImageHandler)
	mux.HandleFunc("GET /v1/jobs/{id}", app.getJobHandler)
	mux.HandleFunc("GET /v1/images/{image_id}/variants/{name}", app.getImageVariantHandler)
	mux.Handle("/", http.FileServer(http.Dir(app.config.frontendDir)))
	return mux
}
