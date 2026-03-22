package model

import "time"

// RateSource indicates where a rate came from for observability.
type RateSource string

const (
	RateSourceCache    RateSource = "cache"
	RateSourceDB       RateSource = "db"
	RateSourceProvider RateSource = "provider"
)

// ExchangeRate stores a single pair value for a specific day.
type ExchangeRate struct {
	ID        string    `json:"id,omitempty"`
	Base      string    `json:"base"`
	Target    string    `json:"target"`
	Rate      float64   `json:"rate"`
	RateDate  time.Time `json:"rate_date"`
	Source    string    `json:"source"`
	FetchedAt time.Time `json:"fetched_at"`
	ExpiresAt time.Time `json:"-"`
}

// CurrencyInfo contains code and display name for a currency.
type CurrencyInfo struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// HistoricalPoint is a time-series point used by API responses.
type HistoricalPoint struct {
	Date string  `json:"date"`
	Rate float64 `json:"rate"`
}
