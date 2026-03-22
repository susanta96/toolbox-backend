package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/susanta96/toolbox-backend/internal/model"
	"github.com/susanta96/toolbox-backend/internal/repository"
)

var (
	// ErrInvalidCurrency indicates an invalid ISO currency code.
	ErrInvalidCurrency = errors.New("invalid currency code")
	// ErrRateUnavailable indicates no fresh or stale rate can be resolved.
	ErrRateUnavailable = errors.New("rate unavailable")
)

const usdCode = "USD"

type cacheEntry struct {
	rate      float64
	expiresAt time.Time
	fetchedAt time.Time
}

// ConvertResult is the resolved response for conversion requests.
type ConvertResult struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	Amount    float64   `json:"amount"`
	Rate      float64   `json:"rate"`
	Converted float64   `json:"converted"`
	Source    string    `json:"source"`
	Stale     bool      `json:"stale"`
	UpdatedAt time.Time `json:"updated_at"`
}

// HistoricalResult represents historical series data.
type HistoricalResult struct {
	From        string                  `json:"from"`
	To          string                  `json:"to"`
	Aggregation string                  `json:"aggregation"`
	Source      string                  `json:"source"`
	Stale       bool                    `json:"stale"`
	UpdatedAt   time.Time               `json:"updated_at"`
	Points      []model.HistoricalPoint `json:"points"`
}

// CurrencyPair is used by warmup strategy.
type CurrencyPair struct {
	From string
	To   string
}

// CurrencyService orchestrates cache, DB and provider lookups.
type CurrencyService struct {
	repo             *repository.ExchangeRateRepository
	provider         *FrankfurterProvider
	cacheTTL         time.Duration
	staleWindow      time.Duration
	historyRetention time.Duration

	mu           sync.RWMutex
	latestCache  map[string]cacheEntry
	requestCount map[string]int
}

// NewCurrencyService creates a new currency service.
func NewCurrencyService(
	repo *repository.ExchangeRateRepository,
	provider *FrankfurterProvider,
	cacheTTL time.Duration,
	staleWindow time.Duration,
	historyRetention time.Duration,
) *CurrencyService {
	return &CurrencyService{
		repo:             repo,
		provider:         provider,
		cacheTTL:         cacheTTL,
		staleWindow:      staleWindow,
		historyRetention: historyRetention,
		latestCache:      make(map[string]cacheEntry),
		requestCount:     make(map[string]int),
	}
}

// GetSupportedCurrencies returns top curated currencies with display names.
func (s *CurrencyService) GetSupportedCurrencies(ctx context.Context) ([]model.CurrencyInfo, error) {
	providerCurrencies, err := s.provider.GetSupportedCurrencies(ctx)
	if err != nil {
		return nil, err
	}

	selected := []string{
		"USD", "EUR", "GBP", "INR", "JPY", "AUD", "CAD", "CHF", "CNY", "HKD",
		"SGD", "SEK", "NOK", "DKK", "NZD", "ZAR", "BRL", "MXN", "AED", "SAR",
		"KWD", "QAR", "BHD", "OMR", "KRW", "THB", "MYR", "IDR", "PHP", "VND",
		"PLN", "TRY", "RUB", "ILS", "CZK",
	}

	currencies := make([]model.CurrencyInfo, 0, len(selected))
	for _, code := range selected {
		name := providerCurrencies[code]
		if name == "" {
			name = code
		}
		currencies = append(currencies, model.CurrencyInfo{Code: code, Name: name})
	}

	return currencies, nil
}

// Convert converts an amount from one currency to another.
func (s *CurrencyService) Convert(ctx context.Context, from, to string, amount float64) (*ConvertResult, error) {
	from = normalizeCurrency(from)
	to = normalizeCurrency(to)
	if !isValidCurrency(from) || !isValidCurrency(to) {
		return nil, ErrInvalidCurrency
	}
	if amount < 0 {
		return nil, fmt.Errorf("amount must be non-negative")
	}

	s.incrementPair(from, to)

	if from == to {
		return &ConvertResult{
			From:      from,
			To:        to,
			Amount:    amount,
			Rate:      1,
			Converted: amount,
			Source:    string(model.RateSourceCache),
			Stale:     false,
			UpdatedAt: time.Now().UTC(),
		}, nil
	}

	rate, source, stale, updatedAt, err := s.resolveLatestRate(ctx, from, to)
	if err != nil {
		return nil, err
	}

	return &ConvertResult{
		From:      from,
		To:        to,
		Amount:    amount,
		Rate:      roundRate(rate),
		Converted: roundAmount(amount * rate),
		Source:    source,
		Stale:     stale,
		UpdatedAt: updatedAt,
	}, nil
}

// Historical returns a historical time-series for a pair and aggregation type.
func (s *CurrencyService) Historical(ctx context.Context, from, to string, startDate, endDate time.Time, aggregation string) (*HistoricalResult, error) {
	from = normalizeCurrency(from)
	to = normalizeCurrency(to)
	if !isValidCurrency(from) || !isValidCurrency(to) {
		return nil, ErrInvalidCurrency
	}
	if endDate.Before(startDate) {
		return nil, fmt.Errorf("end date must be after start date")
	}

	s.incrementPair(from, to)

	if from == to {
		points := make([]model.HistoricalPoint, 0)
		for day := startDate; !day.After(endDate); day = day.Add(24 * time.Hour) {
			points = append(points, model.HistoricalPoint{Date: day.Format("2006-01-02"), Rate: 1})
		}
		aggPoints := aggregatePoints(points, aggregation)
		return &HistoricalResult{
			From:        from,
			To:          to,
			Aggregation: aggregation,
			Source:      string(model.RateSourceCache),
			Stale:       false,
			UpdatedAt:   time.Now().UTC(),
			Points:      aggPoints,
		}, nil
	}

	points, source, stale, updatedAt, err := s.resolveHistoricalSeries(ctx, from, to, startDate, endDate)
	if err != nil {
		return nil, err
	}

	return &HistoricalResult{
		From:        from,
		To:          to,
		Aggregation: aggregation,
		Source:      source,
		Stale:       stale,
		UpdatedAt:   updatedAt,
		Points:      aggregatePoints(points, aggregation),
	}, nil
}

// WarmMostRequested pre-warms top requested pairs into cache and DB.
func (s *CurrencyService) WarmMostRequested(ctx context.Context, limit int) error {
	pairs := s.topPairs(limit)
	for _, pair := range pairs {
		if _, _, _, _, err := s.resolveLatestRate(ctx, pair.From, pair.To); err != nil {
			continue
		}
	}

	return nil
}

// PruneHistoricalData enforces rolling retention for historical rates.
func (s *CurrencyService) PruneHistoricalData(ctx context.Context) (int64, error) {
	cutoff := time.Now().UTC().Add(-s.historyRetention)
	return s.repo.PruneOlderThan(ctx, cutoff)
}

func (s *CurrencyService) resolveLatestRate(ctx context.Context, from, to string) (float64, string, bool, time.Time, error) {
	if from != usdCode && to != usdCode {
		baseToUSD, srcA, staleA, timeA, err := s.getLatestRateSinglePair(ctx, from, usdCode)
		if err != nil {
			return 0, "", false, time.Time{}, err
		}
		usdToTarget, srcB, staleB, timeB, err := s.getLatestRateSinglePair(ctx, usdCode, to)
		if err != nil {
			return 0, "", false, time.Time{}, err
		}

		source := chooseSource(srcA, srcB)
		updatedAt := maxTime(timeA, timeB)
		return baseToUSD * usdToTarget, source, staleA || staleB, updatedAt, nil
	}

	return s.getLatestRateSinglePair(ctx, from, to)
}

func (s *CurrencyService) getLatestRateSinglePair(ctx context.Context, from, to string) (float64, string, bool, time.Time, error) {
	cacheKey := pairKey(from, to)
	now := time.Now().UTC()

	s.mu.RLock()
	cached, ok := s.latestCache[cacheKey]
	s.mu.RUnlock()
	if ok && now.Before(cached.expiresAt) {
		return cached.rate, string(model.RateSourceCache), false, cached.fetchedAt, nil
	}

	rec, err := s.repo.GetLatest(ctx, from, to)
	if err == nil {
		if now.Before(rec.ExpiresAt) {
			s.cacheSet(cacheKey, rec.Rate, rec.FetchedAt)
			return rec.Rate, string(model.RateSourceDB), false, rec.FetchedAt, nil
		}
		if now.Sub(rec.FetchedAt) <= s.staleWindow {
			s.cacheSet(cacheKey, rec.Rate, now.Add(5*time.Minute))
			return rec.Rate, string(model.RateSourceDB), true, rec.FetchedAt, nil
		}
	}

	rates, fetchedAt, apiErr := s.provider.GetLatest(ctx, from, []string{to})
	if apiErr == nil {
		rate, found := rates[to]
		if !found {
			return 0, "", false, time.Time{}, ErrRateUnavailable
		}
		rateDate := time.Date(fetchedAt.Year(), fetchedAt.Month(), fetchedAt.Day(), 0, 0, 0, 0, time.UTC)
		rec := &model.ExchangeRate{
			Base:      from,
			Target:    to,
			Rate:      rate,
			RateDate:  rateDate,
			Source:    string(model.RateSourceProvider),
			FetchedAt: fetchedAt,
			ExpiresAt: now.Add(s.cacheTTL),
		}
		if err := s.repo.Upsert(ctx, rec); err != nil {
			return 0, "", false, time.Time{}, err
		}
		s.cacheSet(cacheKey, rate, fetchedAt)
		return rate, string(model.RateSourceProvider), false, fetchedAt, nil
	}

	if rec != nil && now.Sub(rec.FetchedAt) <= s.staleWindow {
		s.cacheSet(cacheKey, rec.Rate, now.Add(5*time.Minute))
		return rec.Rate, string(model.RateSourceDB), true, rec.FetchedAt, nil
	}

	return 0, "", false, time.Time{}, fmt.Errorf("%w: %v", ErrRateUnavailable, apiErr)
}

func (s *CurrencyService) resolveHistoricalSeries(ctx context.Context, from, to string, startDate, endDate time.Time) ([]model.HistoricalPoint, string, bool, time.Time, error) {
	if from != usdCode && to != usdCode {
		left, srcA, staleA, timeA, err := s.resolveHistoricalSeries(ctx, from, usdCode, startDate, endDate)
		if err != nil {
			return nil, "", false, time.Time{}, err
		}
		right, srcB, staleB, timeB, err := s.resolveHistoricalSeries(ctx, usdCode, to, startDate, endDate)
		if err != nil {
			return nil, "", false, time.Time{}, err
		}
		combined := combineByDate(left, right)
		return combined, chooseSource(srcA, srcB), staleA || staleB, maxTime(timeA, timeB), nil
	}

	dbRows, err := s.repo.GetHistorical(ctx, from, to, startDate, endDate)
	if err == nil && len(dbRows) > 0 {
		last := dbRows[len(dbRows)-1]
		if !endDate.After(last.RateDate.Add(24 * time.Hour)) {
			points := make([]model.HistoricalPoint, 0, len(dbRows))
			for _, row := range dbRows {
				points = append(points, model.HistoricalPoint{Date: row.RateDate.Format("2006-01-02"), Rate: roundRate(row.Rate)})
			}
			return points, string(model.RateSourceDB), false, last.FetchedAt, nil
		}
	}

	series, fetchedAt, providerErr := s.provider.GetHistorical(ctx, from, to, startDate, endDate)
	if providerErr == nil {
		toPersist := make([]*model.ExchangeRate, 0, len(series))
		for date, rate := range series {
			toPersist = append(toPersist, &model.ExchangeRate{
				Base:      from,
				Target:    to,
				Rate:      rate,
				RateDate:  date,
				Source:    string(model.RateSourceProvider),
				FetchedAt: fetchedAt,
				ExpiresAt: fetchedAt.Add(s.cacheTTL),
			})
		}
		if err := s.repo.UpsertMany(ctx, toPersist); err != nil {
			return nil, "", false, time.Time{}, err
		}

		points := make([]model.HistoricalPoint, 0, len(series))
		orderedDates := make([]time.Time, 0, len(series))
		for date := range series {
			orderedDates = append(orderedDates, date)
		}
		sort.Slice(orderedDates, func(i, j int) bool { return orderedDates[i].Before(orderedDates[j]) })
		for _, date := range orderedDates {
			points = append(points, model.HistoricalPoint{Date: date.Format("2006-01-02"), Rate: roundRate(series[date])})
		}
		return points, string(model.RateSourceProvider), false, fetchedAt, nil
	}

	if len(dbRows) > 0 {
		last := dbRows[len(dbRows)-1]
		if time.Since(last.FetchedAt) <= s.staleWindow {
			points := make([]model.HistoricalPoint, 0, len(dbRows))
			for _, row := range dbRows {
				points = append(points, model.HistoricalPoint{Date: row.RateDate.Format("2006-01-02"), Rate: roundRate(row.Rate)})
			}
			return points, string(model.RateSourceDB), true, last.FetchedAt, nil
		}
	}

	return nil, "", false, time.Time{}, fmt.Errorf("%w: %v", ErrRateUnavailable, providerErr)
}

func (s *CurrencyService) cacheSet(key string, rate float64, fetchedAt time.Time) {
	s.mu.Lock()
	s.latestCache[key] = cacheEntry{rate: rate, fetchedAt: fetchedAt, expiresAt: time.Now().UTC().Add(s.cacheTTL)}
	s.mu.Unlock()
}

func (s *CurrencyService) incrementPair(from, to string) {
	s.mu.Lock()
	s.requestCount[pairKey(from, to)]++
	s.mu.Unlock()
}

func (s *CurrencyService) topPairs(limit int) []CurrencyPair {
	type pairCount struct {
		pair  string
		count int
	}

	s.mu.RLock()
	counts := make([]pairCount, 0, len(s.requestCount))
	for pair, count := range s.requestCount {
		counts = append(counts, pairCount{pair: pair, count: count})
	}
	s.mu.RUnlock()

	sort.Slice(counts, func(i, j int) bool { return counts[i].count > counts[j].count })
	if limit > 0 && len(counts) > limit {
		counts = counts[:limit]
	}

	pairs := make([]CurrencyPair, 0, len(counts))
	for _, item := range counts {
		parts := strings.Split(item.pair, ":")
		if len(parts) != 2 {
			continue
		}
		pairs = append(pairs, CurrencyPair{From: parts[0], To: parts[1]})
	}

	return pairs
}

func pairKey(from, to string) string {
	return from + ":" + to
}

func normalizeCurrency(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func isValidCurrency(code string) bool {
	if len(code) != 3 {
		return false
	}
	for _, r := range code {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

func chooseSource(a, b string) string {
	if a == string(model.RateSourceProvider) || b == string(model.RateSourceProvider) {
		return string(model.RateSourceProvider)
	}
	if a == string(model.RateSourceDB) || b == string(model.RateSourceDB) {
		return string(model.RateSourceDB)
	}
	return string(model.RateSourceCache)
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func roundRate(rate float64) float64 {
	return float64(int64(rate*10000+0.5)) / 10000
}

func roundAmount(amount float64) float64 {
	return float64(int64(amount*100+0.5)) / 100
}

func aggregatePoints(points []model.HistoricalPoint, aggregation string) []model.HistoricalPoint {
	if aggregation == "daily" || len(points) == 0 {
		return points
	}

	lastByBucket := make(map[string]model.HistoricalPoint)
	orderedBuckets := make([]string, 0)
	seen := make(map[string]bool)

	for _, point := range points {
		date, err := time.Parse("2006-01-02", point.Date)
		if err != nil {
			continue
		}

		bucket := point.Date
		switch aggregation {
		case "weekly":
			year, week := date.ISOWeek()
			bucket = fmt.Sprintf("%04d-W%02d", year, week)
		case "monthly":
			bucket = date.Format("2006-01")
		default:
			bucket = point.Date
		}

		if !seen[bucket] {
			orderedBuckets = append(orderedBuckets, bucket)
			seen[bucket] = true
		}
		lastByBucket[bucket] = point
	}

	result := make([]model.HistoricalPoint, 0, len(orderedBuckets))
	for _, bucket := range orderedBuckets {
		result = append(result, lastByBucket[bucket])
	}

	return result
}

func combineByDate(left, right []model.HistoricalPoint) []model.HistoricalPoint {
	rightMap := make(map[string]float64, len(right))
	for _, point := range right {
		rightMap[point.Date] = point.Rate
	}

	combined := make([]model.HistoricalPoint, 0)
	for _, point := range left {
		other, ok := rightMap[point.Date]
		if !ok {
			continue
		}
		combined = append(combined, model.HistoricalPoint{
			Date: point.Date,
			Rate: roundRate(point.Rate * other),
		})
	}
	return combined
}

// FrankfurterProvider wraps Frankfurter APIs.
type FrankfurterProvider struct {
	baseURL string
	client  *http.Client
}

// NewFrankfurterProvider creates a Frankfurter API client.
func NewFrankfurterProvider(baseURL string, timeout time.Duration) *FrankfurterProvider {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.frankfurter.dev"
	}
	return &FrankfurterProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: timeout},
	}
}

// GetSupportedCurrencies fetches all currencies from provider.
func (p *FrankfurterProvider) GetSupportedCurrencies(ctx context.Context) (map[string]string, error) {
	endpoint := p.baseURL + "/v1/currencies"
	body, err := p.get(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	out := make(map[string]string)
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("parse provider currencies: %w", err)
	}
	return out, nil
}

// GetLatest gets latest rates for base against requested targets.
func (p *FrankfurterProvider) GetLatest(ctx context.Context, base string, targets []string) (map[string]float64, time.Time, error) {
	q := url.Values{}
	q.Set("from", base)
	q.Set("to", strings.Join(targets, ","))
	endpoint := p.baseURL + "/v1/latest?" + q.Encode()

	body, err := p.get(ctx, endpoint)
	if err != nil {
		return nil, time.Time{}, err
	}

	var payload struct {
		Date  string             `json:"date"`
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, time.Time{}, fmt.Errorf("parse provider latest rates: %w", err)
	}

	fetchedAt := time.Now().UTC()
	if payload.Date != "" {
		if parsed, err := time.Parse("2006-01-02", payload.Date); err == nil {
			fetchedAt = parsed.UTC()
		}
	}

	return payload.Rates, fetchedAt, nil
}

// GetHistorical gets historical rates for a base-target pair.
func (p *FrankfurterProvider) GetHistorical(ctx context.Context, base, target string, startDate, endDate time.Time) (map[time.Time]float64, time.Time, error) {
	rangePath := fmt.Sprintf("/v1/%s..%s", startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	q := url.Values{}
	q.Set("from", base)
	q.Set("to", target)
	endpoint := p.baseURL + rangePath + "?" + q.Encode()

	body, err := p.get(ctx, endpoint)
	if err != nil {
		return nil, time.Time{}, err
	}

	var payload struct {
		Rates map[string]map[string]float64 `json:"rates"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, time.Time{}, fmt.Errorf("parse provider historical rates: %w", err)
	}

	out := make(map[time.Time]float64, len(payload.Rates))
	for rawDate, byCurrency := range payload.Rates {
		date, err := time.Parse("2006-01-02", rawDate)
		if err != nil {
			continue
		}
		rate, ok := byCurrency[target]
		if !ok {
			continue
		}
		out[date.UTC()] = rate
	}

	if len(out) == 0 {
		return nil, time.Time{}, ErrRateUnavailable
	}

	return out, time.Now().UTC(), nil
}

func (p *FrankfurterProvider) get(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create provider request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("provider request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("provider error: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read provider response: %w", err)
	}

	return body, nil
}
