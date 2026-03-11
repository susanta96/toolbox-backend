package handler

import (
	"net/http"
	"runtime"

	"github.com/susanta96/toolbox-backend/pkg/response"
)

// Hello is a health/demo endpoint.
func Hello(w http.ResponseWriter, r *http.Request) {
	response.Success(w, http.StatusOK, "Welcome to Toolbox Backend API! 🧰", map[string]string{
		"version":    "1.0.0",
		"go_version": runtime.Version(),
		"status":     "healthy",
	})
}
