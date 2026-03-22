package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/susanta96/toolbox-backend/internal/service"
	"github.com/susanta96/toolbox-backend/pkg/response"
)

// CurrencyHandler handles currency conversion APIs.
type CurrencyHandler struct {
	currencyService *service.CurrencyService
}

// NewCurrencyHandler creates a new currency handler.
func NewCurrencyHandler(currencyService *service.CurrencyService) *CurrencyHandler {
	return &CurrencyHandler{currencyService: currencyService}
}

// Supported handles GET /api/v1/currency/supported.
func (h *CurrencyHandler) Supported(w http.ResponseWriter, r *http.Request) {
	currencies, err := h.currencyService.GetSupportedCurrencies(r.Context())
	if err != nil {
		response.Error(w, http.StatusServiceUnavailable, "failed to fetch supported currencies")
		return
	}

	response.Success(w, http.StatusOK, "Supported currencies fetched", map[string]any{
		"currencies": currencies,
	})
}

// Convert handles GET /api/v1/currency/convert?from=USD&to=INR&amount=100.
func (h *CurrencyHandler) Convert(w http.ResponseWriter, r *http.Request) {
	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	amountRaw := strings.TrimSpace(r.URL.Query().Get("amount"))
	if from == "" || to == "" || amountRaw == "" {
		response.Error(w, http.StatusBadRequest, "from, to and amount are required")
		return
	}

	amount, err := strconv.ParseFloat(amountRaw, 64)
	if err != nil || amount < 0 {
		response.Error(w, http.StatusBadRequest, "amount must be a valid non-negative number")
		return
	}

	result, err := h.currencyService.Convert(r.Context(), from, to, amount)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "Conversion successful", result)
}

// Historical handles GET /api/v1/currency/historical?from=USD&to=INR&range=30d&aggregation=daily.
func (h *CurrencyHandler) Historical(w http.ResponseWriter, r *http.Request) {
	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	rangeRaw := strings.TrimSpace(r.URL.Query().Get("range"))
	aggregation := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("aggregation")))

	if from == "" || to == "" {
		response.Error(w, http.StatusBadRequest, "from and to are required")
		return
	}
	if rangeRaw == "" {
		rangeRaw = "30d"
	}
	if aggregation == "" {
		aggregation = "daily"
	}
	if aggregation != "daily" && aggregation != "weekly" && aggregation != "monthly" {
		response.Error(w, http.StatusBadRequest, "aggregation must be one of daily, weekly, monthly")
		return
	}

	days, err := parseRangeDays(rangeRaw)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid range value, expected format like 7d, 30d, 90d, 365d")
		return
	}

	endDate := time.Now().UTC()
	startDate := endDate.AddDate(0, 0, -days)

	result, err := h.currencyService.Historical(r.Context(), from, to, startDate, endDate, aggregation)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "Historical rates fetched", result)
}

func (h *CurrencyHandler) handleServiceError(w http.ResponseWriter, err error) {
	if err == service.ErrInvalidCurrency {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err == service.ErrRateUnavailable {
		response.Error(w, http.StatusServiceUnavailable, "rate currently unavailable")
		return
	}
	response.Error(w, http.StatusInternalServerError, err.Error())
}

func parseRangeDays(value string) (int, error) {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if !strings.HasSuffix(trimmed, "d") {
		return 0, strconv.ErrSyntax
	}
	n := strings.TrimSuffix(trimmed, "d")
	days, err := strconv.Atoi(n)
	if err != nil {
		return 0, err
	}
	if days < 1 || days > 365 {
		return 0, strconv.ErrRange
	}
	return days, nil
}
