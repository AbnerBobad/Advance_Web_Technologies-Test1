package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"imagelab/internal/data"
	"imagelab/internal/validator"
)

const (
	maxImageBytes = int64(10 * 1024 * 1024) // 10 MB advertised limit
	// multipartOverhead covers the boundary line, part headers, and any
	// small form fields that ride along with the file part.
	multipartOverhead = int64(512 * 1024)
	bodyLimit         = maxImageBytes + multipartOverhead
)

// allowedMediaTypes maps a server-sniffed media type to the stored file
// extension. The client-reported type is never trusted.
var allowedMediaTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
}

// createImageHandler is the acceptance boundary of ImageLab. It validates the
// multipart upload, stores the original, durably records an image with a
// queued job, and acknowledges with 202 Accepted plus a status URL. It never
// performs image transformation; that work belongs to the background worker.
func (app *application) createImageHandler(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, bodyLimit)

	err := r.ParseMultipartForm(bodyLimit)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			app.errorResponse(w, r, http.StatusRequestEntityTooLarge, "upload exceeds the 10 MB limit")
			return
		}
		app.badRequestResponse(w, r, fmt.Errorf("expected a multipart form upload containing one image"))
		return
	}
	defer r.MultipartForm.RemoveAll()

	fh, fhHeader, err := r.FormFile("file")
	if err != nil {
		v := validator.New()
		v.Check(false, "file", "must provide an image in the 'file' field")
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	content, err := io.ReadAll(fh)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			app.errorResponse(w, r, http.StatusRequestEntityTooLarge, "upload exceeds the 10 MB limit")
			return
		}
		app.badRequestResponse(w, r, fmt.Errorf("could not read the uploaded file"))
		return
	}
	if len(content) == 0 {
		app.badRequestResponse(w, r, fmt.Errorf("the uploaded file is empty"))
		return
	}
	if int64(len(content)) > maxImageBytes {
		app.errorResponse(w, r, http.StatusRequestEntityTooLarge, "upload exceeds the 10 MB limit")
		return
	}

	// The server decides the media type by sniffing bytes, not by trusting
	// the client-supplied Content-Type or file extension.
	mediaType := strings.TrimSpace(strings.Split(http.DetectContentType(content), ";")[0])
	ext, ok := allowedMediaTypes[mediaType]
	if !ok {
		app.errorResponse(w, r, http.StatusUnsupportedMediaType, "only JPEG and PNG images are supported")
		return
	}

	// Level-2 validation: the bytes must decode as an actual image.
	if _, _, err := image.Decode(bytes.NewReader(content)); err != nil {
		app.badRequestResponse(w, r, fmt.Errorf("the file is not a decodable JPEG or PNG image"))
		return
	}

	// The stored name is generated on the server; the submitted name is kept
	// only as display metadata.
	originalName := filepath.Base(fhHeader.Filename)
	if originalName == "." || originalName == string(filepath.Separator) {
		originalName = "upload"
	}

	storedName, err := app.images.Save(content, ext)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	image := &data.Image{
		OriginalFilename: originalName,
		StoredFilename:   storedName,
		MediaType:        mediaType,
		SizeBytes:        int64(len(content)),
	}
	job := &data.Job{}

	if err := app.models.AcceptOriginal(image, job); err != nil {
		// Clean up the stored input where practical: the durable record is
		// what establishes acceptance, not the file on disk.
		_ = app.images.Remove(storedName)
		app.serverErrorResponse(w, r, err)
		return
	}

	app.logger.Info("job created", "job_id", job.PublicID, "image_id", image.ID, "status", job.Status)

	statusURL := fmt.Sprintf("/v1/jobs/%s", job.PublicID)
	headers := make(http.Header)
	headers.Set("Location", statusURL)

	err = app.writeJSON(w, http.StatusAccepted, envelope{
		"image_id":   image.ID,
		"job_id":     job.PublicID,
		"status":     job.Status,
		"status_url": statusURL,
	}, headers)
	if err != nil {
		app.serverErrorResponse(w, r, err)
	}
}
