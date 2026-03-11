package response

import (
	"encoding/json"
	"net/http"
)

// JSON writes a JSON response with the given status code.
func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		json.NewEncoder(w).Encode(data) //nolint:errcheck
	}
}

// Error writes a JSON error response.
func Error(w http.ResponseWriter, status int, message string) {
	JSON(w, status, map[string]string{"error": message})
}

// Success writes a JSON success response with a message and optional data.
func Success(w http.ResponseWriter, status int, message string, data any) {
	resp := map[string]any{"message": message}
	if data != nil {
		resp["data"] = data
	}
	JSON(w, status, resp)
}
