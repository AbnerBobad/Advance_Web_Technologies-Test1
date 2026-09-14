package main

import (
	"context"
	"errors"
	"net/http"

	"imagelab/internal/data"
)

// getImageVariantHandler serves one known generated variant file. Only the
// fixed names are accepted (enforced by the variants_name_check constraint),
// so arbitrary paths are never exposed.
func (app *application) getImageVariantHandler(w http.ResponseWriter, r *http.Request) {
	variant, err := app.models.Variants.Get(context.Background(),
		r.PathValue("image_id"), r.PathValue("name"))
	if err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.notFoundResponse(w, r)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	content, err := app.images.Read(variant.StoredFilename)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	w.Header().Set("Content-Type", mediaTypeForFilename(variant.StoredFilename))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}
