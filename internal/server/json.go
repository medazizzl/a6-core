package server

import (
	"encoding/json"
	"log"
	"net/http"
)

// writeJSON encodes v as the response body with the given status
// and correct content type.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("server: failed to encode response: %v", err)
	}
}

// writeJSONError writes the standard error envelope from the frozen
// spec (§2-4): {"error": "...", "message": "..."}.
func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}
