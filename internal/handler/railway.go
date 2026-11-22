package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/susanta96/toolbox-backend/internal/service"
	"github.com/susanta96/toolbox-backend/pkg/response"
)

var validTrainNumberPattern = regexp.MustCompile(`^[0-9]{1,5}$`)

// RailwayHandler handles railway information requests.
type RailwayHandler struct {
	service *service.RailKitService
}

// NewRailwayHandler creates a railway handler.
func NewRailwayHandler(railKitService *service.RailKitService) *RailwayHandler {
	return &RailwayHandler{service: railKitService}
}

// LookupPNR handles POST /api/v1/railway/pnr.
func (h *RailwayHandler) LookupPNR(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	var request struct {
		PNR string `json:"pnr"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		response.Error(w, http.StatusBadRequest, "request body must contain a 10-digit pnr")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		response.Error(w, http.StatusBadRequest, "request body must contain a single JSON object")
		return
	}

	pnr := strings.TrimSpace(request.PNR)
	if !validPNR(pnr) {
		response.Error(w, http.StatusBadRequest, "pnr must be exactly 10 digits")
		return
	}

	status, err := h.service.LookupPNR(r.Context(), pnr)
	if err != nil {
		slog.Warn("RailKit PNR lookup failed", "error", err)
		if errors.Is(err, service.ErrRailKitNotConfigured) {
			response.Error(w, http.StatusServiceUnavailable, "PNR lookup is not configured")
			return
		}
		response.Error(w, http.StatusBadGateway, "railway status is temporarily unavailable")
		return
	}

	response.Success(w, http.StatusOK, "PNR status fetched", status)
}

// LookupTrainInfo handles GET /api/v1/railway/train/{trainNumber}.
func (h *RailwayHandler) LookupTrainInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	trainNumber := strings.TrimSpace(chi.URLParam(r, "trainNumber"))
	if !validTrainNumberPattern.MatchString(trainNumber) {
		response.Error(w, http.StatusBadRequest, "train number must contain 1 to 5 digits")
		return
	}

	info, err := h.service.LookupTrainInfo(r.Context(), trainNumber)
	if err != nil {
		slog.Warn("RailKit train info lookup failed", "error", err)
		if errors.Is(err, service.ErrRailKitNotConfigured) {
			response.Error(w, http.StatusServiceUnavailable, "train information lookup is not configured")
			return
		}
		if errors.Is(err, service.ErrRailKitTrainNotFound) {
			response.Error(w, http.StatusNotFound, "train information was not found")
			return
		}
		response.Error(w, http.StatusBadGateway, "train information is temporarily unavailable")
		return
	}

	response.Success(w, http.StatusOK, "train information fetched", info)
}

func validPNR(pnr string) bool {
	if len(pnr) != 10 {
		return false
	}
	for _, digit := range pnr {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
