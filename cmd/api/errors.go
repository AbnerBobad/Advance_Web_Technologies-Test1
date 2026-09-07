package main

import (
	"net/http"
)

// logError records an error with the HTTP method and URI that triggered it,
// giving context to server-side failures in the logs.
func (app *application) logError(r *http.Request, err error) {
	var (
		method = r.Method
		uri    = r.URL.RequestURI()
	)

	app.logger.Error(err.Error(), "method", method, "uri", uri)
}

// errorResponse wraps a message in the standard {"error": ...} envelope and
// writes it with the given status code.
func (app *application) errorResponse(w http.ResponseWriter, r *http.Request, status int, message any) {
	env := envelope{"error": message}

	err := app.writeJSON(w, status, env, nil)
	if err != nil {
		app.logError(r, err)
		w.WriteHeader(500)
	}
}

// serverErrorResponse logs the underlying error but only exposes a generic
// message to the client, avoiding leakage of stack traces or database errors.
func (app *application) serverErrorResponse(w http.ResponseWriter, r *http.Request, err error) {
	app.logError(r, err)

	message := "the server encountered a problem and could not process your request"
	app.errorResponse(w, r, http.StatusInternalServerError, message)
}

// badRequestResponse reports a 400 with the exact reason.
func (app *application) badRequestResponse(w http.ResponseWriter, r *http.Request, err error) {
	app.errorResponse(w, r, http.StatusBadRequest, err.Error())
}

// notFoundResponse reports a 404 for unknown resources.
func (app *application) notFoundResponse(w http.ResponseWriter, r *http.Request) {
	app.errorResponse(w, r, http.StatusNotFound, "the requested resource could not be found")
}

// failedValidationResponse reports the request's field errors as JSON.
func (app *application) failedValidationResponse(w http.ResponseWriter, r *http.Request, errors_ map[string]string) {
	app.errorResponse(w, r, http.StatusBadRequest, errors_)
}
