package main

import (
	"errors"
	"net/http"

	"imagelab/internal/data"
)

// variantJSON is the public shape of one generated variant on a completed
// job, as required by the API contract: name, actual dimensions, and a URL
// the client can fetch.
type variantJSON struct {
	Name   string `json:"name"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	URL    string `json:"url"`
}

// getJobHandler serves GET /v1/jobs/{id}. Without query parameters it returns
// a prompt snapshot and never waits (API-01). With after (and optionally
// wait) it delegates to the long-poll branch, which holds the request until a
// committed change, the holding deadline, or cancellation (API-02).
func (app *application) getJobHandler(w http.ResponseWriter, r *http.Request) {
	req, err := parseObservationRequest(r)
	if err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	if req.waitMode {
		app.observeJobHandler(w, r, req)
		return
	}

	job, err := app.models.Jobs.GetByPublicID(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.notFoundResponse(w, r)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	if err := app.writeJobSnapshot(w, r, job); err != nil {
		app.serverErrorResponse(w, r, err)
	}
}

// writeJobSnapshot writes the shared job representation used by both
// observation modes: the existing fields plus version, and the variant
// metadata once the job is completed. Observation responses are marked
// Cache-Control: no-store so no cache serves a stale snapshot (API-03).
func (app *application) writeJobSnapshot(w http.ResponseWriter, r *http.Request, job *data.Job) error {
	response := envelope{
		"id":           job.PublicID,
		"image_id":     job.ImageID,
		"status":       job.Status,
		"version":      job.Version,
		"queued_at":    job.QueuedAt,
		"started_at":   job.StartedAt,
		"completed_at": job.CompletedAt,
	}
	if job.SafeError != nil {
		response["error"] = *job.SafeError
	}
	if job.FailedAt != nil {
		response["failed_at"] = job.FailedAt
	}

	// Variants exist only at completion; the worker records them before the
	// job is marked completed, so a completed job always has all three.
	if job.Status == "completed" {
		variants, err := app.models.Variants.GetByImageID(r.Context(), job.ImageID)
		if err != nil {
			return err
		}
		response["variants"] = jobVariants(variants)
	}

	headers := make(http.Header)
	headers.Set("Cache-Control", "no-store")

	return app.writeJSON(w, http.StatusOK, response, headers)
}

// noContentResponse answers a holding deadline that expired with no newer
// version: 204 with no body and no caching (API-02, API-03).
func (app *application) noContentResponse(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// jobVariants converts the stored variant records into the public response
// shape, building each variant's fetchable URL.
func jobVariants(variants []data.Variant) []variantJSON {
	out := make([]variantJSON, 0, len(variants))
	for _, v := range variants {
		out = append(out, variantJSON{
			Name:   v.Name,
			Width:  v.Width,
			Height: v.Height,
			URL:    "/v1/images/" + v.ImageID + "/variants/" + v.Name,
		})
	}
	return out
}
