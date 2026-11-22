package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const railKitAPIBaseURL = "https://api.railkit.in"

var (
	ErrRailKitNotConfigured = errors.New("railway status provider is not configured")
	ErrRailKitUnavailable   = errors.New("railway status provider unavailable")
	ErrRailKitTrainNotFound = errors.New("train information was not found")
)

// RailKitService retrieves PNR data from RailKit's REST API.
type RailKitService struct {
	apiKey string
	client *http.Client
}

// PNRStatus is the RailKit PNR response data exposed by the Toolbox API.
type PNRStatus struct {
	PNR        string         `json:"pnr"`
	Train      PNRTrain       `json:"train"`
	Journey    PNRJourney     `json:"journey"`
	Chart      *PNRChart      `json:"chart,omitempty"`
	Passengers []PNRPassenger `json:"passengers"`
}

type PNRTrain struct {
	Number string `json:"number"`
	Name   string `json:"name"`
}

type PNRStation struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type PNRJourney struct {
	DateOfJourney string      `json:"dateOfJourney"`
	Class         string      `json:"class,omitempty"`
	Quota         string      `json:"quota,omitempty"`
	Source        PNRStation  `json:"source"`
	Destination   PNRStation  `json:"destination"`
	BoardingPoint *PNRStation `json:"boardingPoint,omitempty"`
}

type PNRChart struct {
	Status string `json:"status"`
}

type PNRPassenger struct {
	SerialNumber string             `json:"serialNumber"`
	Booking      PNRPassengerStatus `json:"booking"`
	Current      PNRPassengerStatus `json:"current"`
}

type PNRPassengerStatus struct {
	Status    string          `json:"status"`
	Details   string          `json:"details,omitempty"`
	Coach     string          `json:"coach,omitempty"`
	BerthNo   json.RawMessage `json:"berthNo,omitempty"`
	BerthCode string          `json:"berthCode,omitempty"`
}

type railKitPNRResponse struct {
	Success bool      `json:"success"`
	Message string    `json:"message"`
	Error   string    `json:"error"`
	Data    PNRStatus `json:"data"`
}

// TrainInfo contains public train timetable and route data from RailKit.
type TrainInfo struct {
	TrainInfo TrainDetails `json:"trainInfo"`
	Route     []TrainStop  `json:"route"`
}

type TrainDetails struct {
	Number        string `json:"train_no"`
	Name          string `json:"train_name"`
	Origin        any    `json:"origin,omitempty"`
	Destination   any    `json:"destination,omitempty"`
	FromStation   any    `json:"from_station,omitempty"`
	ToStation     any    `json:"to_station,omitempty"`
	DepartureTime any    `json:"from_time,omitempty"`
	ArrivalTime   any    `json:"to_time,omitempty"`
	RunningDays   any    `json:"running_days,omitempty"`
	TrainType     any    `json:"train_type,omitempty"`
	Distance      any    `json:"distance,omitempty"`
	Classes       any    `json:"classes,omitempty"`
}

type TrainStop struct {
	StationCode string `json:"station_code"`
	StationName string `json:"station_name"`
	Arrival     any    `json:"arrival,omitempty"`
	Departure   any    `json:"departure,omitempty"`
	Halt        any    `json:"halt,omitempty"`
	Distance    any    `json:"distance,omitempty"`
	Day         any    `json:"day,omitempty"`
	Platform    any    `json:"platform,omitempty"`
}

type railKitTrainInfoResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error"`
	Data    struct {
		TrainInfo railKitTrainDetails `json:"trainInfo"`
		Route     []railKitTrainStop  `json:"route"`
	} `json:"data"`
}

// RailKit's train-info response uses station fields that differ from our
// public API names. Keep its wire shape separate so we can normalize it.
type railKitTrainDetails struct {
	Number          string `json:"train_no"`
	Name            string `json:"train_name"`
	FromStationName string `json:"from_stn_name"`
	FromStationCode string `json:"from_stn_code"`
	ToStationName   string `json:"to_stn_name"`
	ToStationCode   string `json:"to_stn_code"`
	DepartureTime   any    `json:"from_time"`
	ArrivalTime     any    `json:"to_time"`
	RunningDays     any    `json:"daysOfRunFromSource"`
	TrainType       any    `json:"type"`
	Distance        any    `json:"total_distance"`
	Classes         any    `json:"classes"`
}

type railKitTrainStop struct {
	StationCode string `json:"stnCode"`
	StationName string `json:"stnName"`
	Arrival     any    `json:"arrival"`
	Departure   any    `json:"departure"`
	Halt        any    `json:"halt"`
	Distance    any    `json:"distance"`
	Day         any    `json:"day"`
	Platform    any    `json:"platform"`
}

// NewRailKitService creates a bounded-timeout client for RailKit REST calls.
func NewRailKitService(apiKey string, timeout time.Duration) *RailKitService {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &RailKitService{
		apiKey: strings.TrimSpace(apiKey),
		client: &http.Client{Timeout: timeout},
	}
}

// LookupPNR fetches the current status for one 10-digit PNR.
func (s *RailKitService) LookupPNR(ctx context.Context, pnr string) (*PNRStatus, error) {
	if s.apiKey == "" {
		return nil, ErrRailKitNotConfigured
	}

	endpoint := railKitAPIBaseURL + "/api/v1/pnr/" + url.PathEscape(pnr)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create RailKit request: %w", err)
	}
	req.Header.Set("x-api-key", s.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("RailKit request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read RailKit response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("RailKit returned HTTP %d", resp.StatusCode)
	}

	var result railKitPNRResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse RailKit response: %w", err)
	}
	if !result.Success || result.Data.PNR == "" {
		return nil, ErrRailKitUnavailable
	}

	return &result.Data, nil
}

// LookupTrainInfo fetches a train's summary and ordered station route.
func (s *RailKitService) LookupTrainInfo(ctx context.Context, trainNumber string) (*TrainInfo, error) {
	if s.apiKey == "" {
		return nil, ErrRailKitNotConfigured
	}

	endpoint := railKitAPIBaseURL + "/api/v1/trains/" + url.PathEscape(trainNumber) + "/info"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create RailKit request: %w", err)
	}
	req.Header.Set("x-api-key", s.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("RailKit request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read RailKit response: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrRailKitTrainNotFound
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("RailKit returned HTTP %d", resp.StatusCode)
	}

	var result railKitTrainInfoResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse RailKit response: %w", err)
	}
	if !result.Success || result.Data.TrainInfo.Number == "" {
		return nil, ErrRailKitUnavailable
	}

	providerTrain := result.Data.TrainInfo
	train := TrainDetails{
		Number:        providerTrain.Number,
		Name:          providerTrain.Name,
		Origin:        providerTrain.FromStationName,
		Destination:   providerTrain.ToStationName,
		FromStation:   providerTrain.FromStationCode,
		ToStation:     providerTrain.ToStationCode,
		DepartureTime: providerTrain.DepartureTime,
		ArrivalTime:   providerTrain.ArrivalTime,
		RunningDays:   providerTrain.RunningDays,
		TrainType:     providerTrain.TrainType,
		Distance:      providerTrain.Distance,
		Classes:       providerTrain.Classes,
	}
	stops := make([]TrainStop, 0, len(result.Data.Route))
	for _, providerStop := range result.Data.Route {
		stops = append(stops, TrainStop{
			StationCode: providerStop.StationCode,
			StationName: providerStop.StationName,
			Arrival:     providerStop.Arrival,
			Departure:   providerStop.Departure,
			Halt:        providerStop.Halt,
			Distance:    providerStop.Distance,
			Day:         providerStop.Day,
			Platform:    providerStop.Platform,
		})
	}

	return &TrainInfo{TrainInfo: train, Route: stops}, nil
}
