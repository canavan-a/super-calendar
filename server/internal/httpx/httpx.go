// Package httpx holds the small JSON helpers the API handlers share.
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// maxBody caps request bodies; an .ics import is the largest thing sent.
const maxBody = 5 << 20

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, map[string]string{"error": message})
}

// Decode reads a JSON body into v, writing a 400 and returning false if it
// can't.
func Decode(w http.ResponseWriter, r *http.Request, v any) bool {
	err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(v)
	if err != nil && !errors.Is(err, io.EOF) {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}
