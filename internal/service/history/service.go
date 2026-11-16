// Package history exposes the recorded traffic and latency history to the
// frontend. Recording itself happens in the proxy service (traffic deltas) and
// the node service (URL latency samples); this service only reads and clears the
// persisted records owned by the store.
package history

import (
	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
)

// Service is the Wails adapter for the persisted traffic and latency history.
type Service struct {
	store *store.Store
}

func New(st *store.Store) *Service {
	return &Service{store: st}
}

func (s *Service) ServiceName() string {
	return "HistoryService"
}

// Traffic returns up to days of the newest daily traffic aggregates in ascending
// date order. days is clamped to the supported 1..MaxTrafficHistoryDays range so
// a hand-edited or stale frontend value can never request an unbounded read.
func (s *Service) Traffic(days int) []model.DailyTraffic {
	return s.store.TrafficHistory(clampTrafficDays(days))
}

// Latencies returns up to limit of the newest URL latency samples in
// chronological order (newest last). limit is clamped to the supported
// 1..MaxLatencyHistorySamples range.
func (s *Service) Latencies(limit int) []model.LatencySample {
	return s.store.LatencyHistory(clampLatencyLimit(limit))
}

// Clear removes all recorded traffic and latency history. Configuration,
// subscriptions and nodes are not touched.
func (s *Service) Clear() error {
	return s.store.ClearHistory()
}

// clampTrafficDays keeps the requested traffic window inside 1..180 days.
func clampTrafficDays(days int) int {
	if days < 1 {
		return 1
	}
	if days > store.MaxTrafficHistoryDays {
		return store.MaxTrafficHistoryDays
	}
	return days
}

// clampLatencyLimit keeps the requested latency sample count inside 1..300.
func clampLatencyLimit(limit int) int {
	if limit < 1 {
		return 1
	}
	if limit > store.MaxLatencyHistorySamples {
		return store.MaxLatencyHistorySamples
	}
	return limit
}
