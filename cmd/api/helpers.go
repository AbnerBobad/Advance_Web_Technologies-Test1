package main

import (
	"encoding/json"
	"net/http"
)

// envelope is the standard JSON wrapper used for every API response,
// e.g. {"image_id": 108, "job_id": 42, ...} or {"error": ...}.
type envelope map[string]any

// writeJSON marshals data as indented JSON, applies any extra headers, sets
// the Content-Type, and writes the response with the given status code.
func (app *application) writeJSON(w http.ResponseWriter, status int, data envelope, headers http.Header) error {
	js, err := json.MarshalIndent(data, "", "\t")
	if err != nil {
		return err
	}

	js = append(js, '\n')

	for key, values := range headers {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(js)

	return nil
}
