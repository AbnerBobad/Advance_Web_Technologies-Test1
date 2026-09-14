package main

import (
	"context"
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

// getJobHandler returns the current state of a job by its public ID. The
// client polls this endpoint until the job reaches completed or failed.
// Completed responses additionally carry the stored variant metadata.
func (app *application) getJobHandler(w http.ResponseWriter, r *http.Request) {
	job, err := app.models.Jobs.GetByPublicID(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.notFoundResponse(w, r)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	response := envelope{
		"id":           job.PublicID,
		"image_id":     job.ImageID,
		"status":       job.Status,
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
		variants, err := app.models.Variants.GetByImageID(context.Background(), job.ImageID)
		if err != nil {
			app.serverErrorResponse(w, r, err)
			return
		}
		response["variants"] = jobVariants(variants)
	}

	if err := app.writeJSON(w, http.StatusOK, response, nil); err != nil {
		app.serverErrorResponse(w, r, err)
	}
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
