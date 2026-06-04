package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/susanta96/toolbox-backend/internal/service"
	"github.com/susanta96/toolbox-backend/pkg/response"
)

type PasteHandler struct {
	svc *service.PasteService
}

func NewPasteHandler(svc *service.PasteService) *PasteHandler {
	return &PasteHandler{svc: svc}
}

func (h *PasteHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req service.CreatePasteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := h.svc.CreatePaste(r.Context(), req)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(w, http.StatusCreated, "Paste created successfully", result)
}

func (h *PasteHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		response.Error(w, http.StatusBadRequest, "paste id is required")
		return
	}

	paste, err := h.svc.GetPaste(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "paste not found")
			return
		}
		if err.Error() == "paste has expired" {
			response.Error(w, http.StatusGone, "paste has expired")
			return
		}
		response.Error(w, http.StatusNotFound, "paste not found")
		return
	}

	response.Success(w, http.StatusOK, "OK", paste)
}
