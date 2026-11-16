package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
)

func newHistoryTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatalf("create test store: %v", err)
	}
	return st
}

func mustAddTrafficBytes(t *testing.T, st *Store, uplink, downlink int64, at time.Time) {
	t.Helper()
	if err := st.AddTrafficBytes(uplink, downlink, at); err != nil {
		t.Fatalf("add traffic bytes: %v", err)
	}
}

func mustAddLatencySamples(t *testing.T, st *Store, samples []model.LatencySample) {
	t.Helper()
	if err := st.AddLatencySamples(samples); err != nil {
		t.Fatalf("add latency samples: %v", err)
	}
}

func localDay(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 12, 0, 0, 0, time.Local)
}

func wantTraffic(t *testing.T, got, want []model.DailyTraffic) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("traffic history length: got %d, want %d (%+v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("traffic history at %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func wantLatency(t *testing.T, got, want []model.LatencySample) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("latency history length: got %d, want %d (%+v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("latency history at %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestAddTrafficBytesAggregatesByLocalDay(t *testing.T) {
	st := newHistoryTestStore(t)

	// Days are written out of order on purpose: the history must stay ascending.
	mustAddTrafficBytes(t, st, 1, 2, localDay(2026, time.January, 3))
	mustAddTrafficBytes(t, st, 100, 200, localDay(2026, time.January, 2))
	mustAddTrafficBytes(t, st, 5, 7, time.Date(2026, time.January, 2, 23, 59, 0, 0, time.Local))

	wantTraffic(t, st.TrafficHistory(0), []model.DailyTraffic{
		{Date: "2026-01-02", UplinkBytes: 105, DownlinkBytes: 207},
		{Date: "2026-01-03", UplinkBytes: 1, DownlinkBytes: 2},
	})
}

func TestAddTrafficBytesIgnoresNonPositiveDeltas(t *testing.T) {
	st := newHistoryTestStore(t)

	if err := st.AddTrafficBytes(0, 0, localDay(2026, time.March, 1)); err != nil {
		t.Fatalf("zero delta: %v", err)
	}
	if err := st.AddTrafficBytes(-10, -20, localDay(2026, time.March, 1)); err != nil {
		t.Fatalf("negative delta: %v", err)
	}
	if got := st.TrafficHistory(0); len(got) != 0 {
		t.Fatalf("non-positive deltas must not be recorded: %+v", got)
	}
}

func TestAddTrafficBytesKeepsNewestDays(t *testing.T) {
	st := newHistoryTestStore(t)

	const totalDays = MaxTrafficHistoryDays + 5
	start := localDay(2025, time.January, 1)
	for i := 0; i < totalDays; i++ {
		mustAddTrafficBytes(t, st, int64(i+1), int64(i+2), start.AddDate(0, 0, i))
	}

	entries := st.TrafficHistory(0)
	if len(entries) != MaxTrafficHistoryDays {
		t.Fatalf("expected %d retained days, got %d", MaxTrafficHistoryDays, len(entries))
	}
	wantFirst := start.AddDate(0, 0, totalDays-MaxTrafficHistoryDays).Format(localDateLayout)
	wantLast := start.AddDate(0, 0, totalDays-1).Format(localDateLayout)
	if entries[0].Date != wantFirst {
		t.Fatalf("oldest retained day: got %s, want %s", entries[0].Date, wantFirst)
	}
	if entries[len(entries)-1].Date != wantLast {
		t.Fatalf("newest retained day: got %s, want %s", entries[len(entries)-1].Date, wantLast)
	}
}

func TestTrafficHistoryDaysLimitAndCopy(t *testing.T) {
	st := newHistoryTestStore(t)
	for i := 1; i <= 5; i++ {
		mustAddTrafficBytes(t, st, int64(i), int64(i*10), localDay(2026, time.February, i))
	}

	lastTwo := st.TrafficHistory(2)
	if len(lastTwo) != 2 || lastTwo[0].Date != "2026-02-04" || lastTwo[1].Date != "2026-02-05" {
		t.Fatalf("unexpected last two days: %+v", lastTwo)
	}
	if all := st.TrafficHistory(0); len(all) != 5 {
		t.Fatalf("days<=0 must return all retained days, got %d", len(all))
	}
	if all := st.TrafficHistory(999); len(all) != 5 {
		t.Fatalf("days above the retained count must return everything, got %d", len(all))
	}

	// The returned slice is a copy: mutating it must not corrupt the store.
	lastTwo[0].UplinkBytes = 1 << 40
	if got := st.TrafficHistory(2); got[0].UplinkBytes != 4 {
		t.Fatalf("store history was mutated through the returned slice: %+v", got[0])
	}
}

func TestAddLatencySamplesKeepsNewestSamples(t *testing.T) {
	st := newHistoryTestStore(t)

	const total = MaxLatencyHistorySamples + 5
	samples := make([]model.LatencySample, 0, total)
	for i := 0; i < total; i++ {
		samples = append(samples, model.LatencySample{
			NodeID:    fmt.Sprintf("node-%03d", i),
			NodeName:  fmt.Sprintf("Node %d", i),
			LatencyMS: int64(i + 1),
			CheckedAt: time.Date(2026, time.April, 1, 0, 0, i, 0, time.Local).Format(time.RFC3339),
		})
	}
	mustAddLatencySamples(t, st, samples)

	entries := st.LatencyHistory(0)
	if len(entries) != MaxLatencyHistorySamples {
		t.Fatalf("expected %d retained samples, got %d", MaxLatencyHistorySamples, len(entries))
	}
	if entries[0].NodeID != "node-005" || entries[len(entries)-1].NodeID != "node-304" {
		t.Fatalf("newest samples were not retained: first=%+v last=%+v", entries[0], entries[len(entries)-1])
	}

	// A second batch also trims from the front and keeps chronological order.
	more := make([]model.LatencySample, 0, 10)
	for i := 0; i < 10; i++ {
		more = append(more, model.LatencySample{NodeID: fmt.Sprintf("extra-%d", i), LatencyMS: 1})
	}
	mustAddLatencySamples(t, st, more)
	entries = st.LatencyHistory(0)
	if len(entries) != MaxLatencyHistorySamples {
		t.Fatalf("expected %d retained samples after second batch, got %d", MaxLatencyHistorySamples, len(entries))
	}
	if entries[0].NodeID != "node-015" || entries[len(entries)-1].NodeID != "extra-9" {
		t.Fatalf("unexpected trim after second batch: first=%+v last=%+v", entries[0], entries[len(entries)-1])
	}
}

func TestLatencyHistoryLimitAndCopy(t *testing.T) {
	st := newHistoryTestStore(t)
	mustAddLatencySamples(t, st, []model.LatencySample{
		{NodeID: "a", NodeName: "A", LatencyMS: 10},
		{NodeID: "b", NodeName: "B", LatencyMS: 20},
		{NodeID: "c", NodeName: "C", LatencyMS: 30, Error: "timeout"},
	})

	lastTwo := st.LatencyHistory(2)
	wantLatency(t, lastTwo, []model.LatencySample{
		{NodeID: "b", NodeName: "B", LatencyMS: 20},
		{NodeID: "c", NodeName: "C", LatencyMS: 30, Error: "timeout"},
	})
	if all := st.LatencyHistory(0); len(all) != 3 {
		t.Fatalf("limit<=0 must return all retained samples, got %d", len(all))
	}
	if all := st.LatencyHistory(999); len(all) != 3 {
		t.Fatalf("limit above the retained count must return everything, got %d", len(all))
	}

	lastTwo[0].LatencyMS = 1 << 40
	if got := st.LatencyHistory(2); got[0].LatencyMS != 20 {
		t.Fatalf("store history was mutated through the returned slice: %+v", got[0])
	}
}

func TestAddLatencySamplesEmptyIsNoOp(t *testing.T) {
	st := newHistoryTestStore(t)

	if err := st.AddLatencySamples(nil); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
	if err := st.AddLatencySamples([]model.LatencySample{}); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
	if got := st.LatencyHistory(0); len(got) != 0 {
		t.Fatalf("empty batches must not record samples: %+v", got)
	}
}

func TestHistoryPersistsAcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	st, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	mustAddTrafficBytes(t, st, 123, 456, localDay(2026, time.June, 1))
	mustAddLatencySamples(t, st, []model.LatencySample{
		{NodeID: "n1", NodeName: "One", LatencyMS: 12, CheckedAt: "2026-06-01T10:00:00+09:00"},
	})

	reloaded, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	wantTraffic(t, reloaded.TrafficHistory(0), []model.DailyTraffic{
		{Date: "2026-06-01", UplinkBytes: 123, DownlinkBytes: 456},
	})
	wantLatency(t, reloaded.LatencyHistory(0), []model.LatencySample{
		{NodeID: "n1", NodeName: "One", LatencyMS: 12, CheckedAt: "2026-06-01T10:00:00+09:00"},
	})
}

func TestClearHistoryKeepsConfiguration(t *testing.T) {
	st := newHistoryTestStore(t)
	mustAddTrafficBytes(t, st, 10, 20, localDay(2026, time.July, 1))
	mustAddLatencySamples(t, st, []model.LatencySample{{NodeID: "n1", NodeName: "One", LatencyMS: 5}})

	settings := DefaultSettings()
	settings.Theme = "dark"
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	if err := st.AddManualNodes([]Node{{
		ID: "node-1", Name: "Manual", Protocol: "vless", Address: "example.com",
		Port: 443, Raw: "vless://node-1", Enabled: true,
	}}); err != nil {
		t.Fatalf("add manual node: %v", err)
	}

	if err := st.ClearHistory(); err != nil {
		t.Fatalf("clear history: %v", err)
	}
	if got := st.TrafficHistory(0); len(got) != 0 {
		t.Fatalf("traffic history was not cleared: %+v", got)
	}
	if got := st.LatencyHistory(0); len(got) != 0 {
		t.Fatalf("latency history was not cleared: %+v", got)
	}
	if st.Settings().Theme != "dark" {
		t.Fatal("clearing history must not change settings")
	}
	if nodes := st.ListNodes(); len(nodes) != 1 || nodes[0].ID != "node-1" {
		t.Fatalf("clearing history must not change nodes: %+v", nodes)
	}

	// The cleared state is persisted too.
	reloaded, err := NewStoreAt(filepath.Join(st.DataDir(), "data.json"))
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	if got := reloaded.TrafficHistory(0); len(got) != 0 {
		t.Fatalf("cleared traffic history reappeared after reload: %+v", got)
	}
	if got := reloaded.LatencyHistory(0); len(got) != 0 {
		t.Fatalf("cleared latency history reappeared after reload: %+v", got)
	}
}

func TestExportRestoreRoundTripWithHistory(t *testing.T) {
	source := newHistoryTestStore(t)
	mustAddTrafficBytes(t, source, 100, 200, localDay(2026, time.May, 1))
	mustAddTrafficBytes(t, source, 1, 2, localDay(2026, time.May, 2))
	mustAddLatencySamples(t, source, []model.LatencySample{
		{NodeID: "n1", NodeName: "One", LatencyMS: 42, CheckedAt: "2026-05-01T10:00:00+09:00"},
		{NodeID: "n2", NodeName: "Two", Error: "timeout", CheckedAt: "2026-05-01T10:00:05+09:00"},
	})

	before, err := source.Export()
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(before, &payload); err != nil {
		t.Fatalf("export is not valid JSON: %v", err)
	}
	for _, key := range []string{"trafficHistory", "latencyHistory"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("export is missing %q", key)
		}
	}

	target := newHistoryTestStore(t)
	if _, err := target.Restore(before); err != nil {
		t.Fatalf("restore: %v", err)
	}
	after, err := target.Export()
	if err != nil {
		t.Fatalf("export after restore: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("restored history differs from the export\nbefore:\n%s\nafter:\n%s", before, after)
	}

	wantTraffic(t, target.TrafficHistory(0), []model.DailyTraffic{
		{Date: "2026-05-01", UplinkBytes: 100, DownlinkBytes: 200},
		{Date: "2026-05-02", UplinkBytes: 1, DownlinkBytes: 2},
	})
	wantLatency(t, target.LatencyHistory(0), []model.LatencySample{
		{NodeID: "n1", NodeName: "One", LatencyMS: 42, CheckedAt: "2026-05-01T10:00:00+09:00"},
		{NodeID: "n2", NodeName: "Two", Error: "timeout", CheckedAt: "2026-05-01T10:00:05+09:00"},
	})
}

func TestRestoreToleratesMissingAndInvalidHistory(t *testing.T) {
	cases := []struct {
		name    string
		payload string
	}{
		{
			"missing history fields",
			fmt.Sprintf(`{"version":%d,"settings":{},"subscriptions":[],"nodes":[],"routingRules":[]}`, storeVersion),
		},
		{
			"null history fields",
			fmt.Sprintf(`{"version":%d,"settings":{},"subscriptions":[],"nodes":[],"routingRules":[],"trafficHistory":null,"latencyHistory":null}`, storeVersion),
		},
		{
			"history fields of the wrong JSON type",
			fmt.Sprintf(`{"version":%d,"settings":{},"subscriptions":[],"nodes":[],"routingRules":[],"trafficHistory":"oops","latencyHistory":42}`, storeVersion),
		},
		{
			"history entries with invalid field types",
			fmt.Sprintf(`{"version":%d,"settings":{},"subscriptions":[],"nodes":[],"routingRules":[],"trafficHistory":[{"date":123}],"latencyHistory":[{"latencyMs":"fast"}]}`, storeVersion),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := newHistoryTestStore(t)
			if _, err := st.Restore([]byte(tc.payload)); err != nil {
				t.Fatalf("restore must tolerate an unusable history section: %v", err)
			}
			if got := st.TrafficHistory(0); len(got) != 0 {
				t.Fatalf("unusable traffic history was kept: %+v", got)
			}
			if got := st.LatencyHistory(0); len(got) != 0 {
				t.Fatalf("unusable latency history was kept: %+v", got)
			}
			// The restored configuration remains usable.
			if err := st.SaveSettings(DefaultSettings()); err != nil {
				t.Fatalf("store unusable after tolerant restore: %v", err)
			}
		})
	}
}

func TestRestoreRepairsPartiallyValidHistory(t *testing.T) {
	document := map[string]any{
		"version":       storeVersion,
		"settings":      map[string]any{},
		"subscriptions": []any{},
		"nodes":         []any{},
		"routingRules":  []any{},
		"trafficHistory": []any{
			map[string]any{"date": "2026-01-02", "uplinkBytes": 7, "downlinkBytes": 8},
			map[string]any{"date": "not-a-date", "uplinkBytes": 100},
			map[string]any{"date": "2026-01-01", "uplinkBytes": 1, "downlinkBytes": 2},
		},
		"latencyHistory": []any{
			map[string]any{"nodeId": "node-1", "nodeName": "One", "latencyMs": 10},
			map[string]any{},
			map[string]any{"nodeId": "node-2", "nodeName": "Two", "latencyMs": -5},
		},
	}
	payload, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal backup: %v", err)
	}

	st := newHistoryTestStore(t)
	if _, err := st.Restore(payload); err != nil {
		t.Fatalf("restore: %v", err)
	}
	wantTraffic(t, st.TrafficHistory(0), []model.DailyTraffic{
		{Date: "2026-01-01", UplinkBytes: 1, DownlinkBytes: 2},
		{Date: "2026-01-02", UplinkBytes: 7, DownlinkBytes: 8},
	})
	wantLatency(t, st.LatencyHistory(0), []model.LatencySample{
		{NodeID: "node-1", NodeName: "One", LatencyMS: 10},
		{NodeID: "node-2", NodeName: "Two", LatencyMS: 0},
	})
}

func TestRestoreTruncatesOversizedHistory(t *testing.T) {
	const extraDays = 20
	const extraSamples = 100
	start := localDay(2025, time.January, 1)

	traffic := make([]any, 0, MaxTrafficHistoryDays+extraDays)
	for i := 0; i < MaxTrafficHistoryDays+extraDays; i++ {
		traffic = append(traffic, map[string]any{
			"date":          start.AddDate(0, 0, i).Format(localDateLayout),
			"uplinkBytes":   i + 1,
			"downlinkBytes": i + 2,
		})
	}
	samples := make([]any, 0, MaxLatencyHistorySamples+extraSamples)
	for i := 0; i < MaxLatencyHistorySamples+extraSamples; i++ {
		samples = append(samples, map[string]any{
			"nodeId":    fmt.Sprintf("node-%03d", i),
			"nodeName":  fmt.Sprintf("Node %d", i),
			"latencyMs": i,
			"checkedAt": "2026-01-01T00:00:00Z",
		})
	}
	document := map[string]any{
		"version":        storeVersion,
		"settings":       map[string]any{},
		"subscriptions":  []any{},
		"nodes":          []any{},
		"routingRules":   []any{},
		"trafficHistory": traffic,
		"latencyHistory": samples,
	}
	payload, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal backup: %v", err)
	}

	st := newHistoryTestStore(t)
	if _, err := st.Restore(payload); err != nil {
		t.Fatalf("restore: %v", err)
	}

	days := st.TrafficHistory(0)
	if len(days) != MaxTrafficHistoryDays {
		t.Fatalf("expected %d daily entries after restore, got %d", MaxTrafficHistoryDays, len(days))
	}
	if want := start.AddDate(0, 0, extraDays).Format(localDateLayout); days[0].Date != want {
		t.Fatalf("oldest restored day: got %s, want %s", days[0].Date, want)
	}
	if want := start.AddDate(0, 0, MaxTrafficHistoryDays+extraDays-1).Format(localDateLayout); days[len(days)-1].Date != want {
		t.Fatalf("newest restored day: got %s, want %s", days[len(days)-1].Date, want)
	}

	latencies := st.LatencyHistory(0)
	if len(latencies) != MaxLatencyHistorySamples {
		t.Fatalf("expected %d latency samples after restore, got %d", MaxLatencyHistorySamples, len(latencies))
	}
	if latencies[0].NodeID != fmt.Sprintf("node-%03d", extraSamples) {
		t.Fatalf("oldest restored sample: got %s, want node-%03d", latencies[0].NodeID, extraSamples)
	}
	if want := fmt.Sprintf("node-%03d", MaxLatencyHistorySamples+extraSamples-1); latencies[len(latencies)-1].NodeID != want {
		t.Fatalf("newest restored sample: got %s, want %s", latencies[len(latencies)-1].NodeID, want)
	}
}

func TestHistoryAccessIsConcurrencySafe(t *testing.T) {
	st := newHistoryTestStore(t)
	at := localDay(2026, time.August, 1)

	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				_ = st.AddTrafficBytes(1, 2, at)
				_ = st.AddLatencySamples([]model.LatencySample{{
					NodeID: fmt.Sprintf("node-%d", worker), NodeName: "Worker", LatencyMS: int64(i),
				}})
				_ = st.TrafficHistory(30)
				_ = st.LatencyHistory(30)
			}
		}(worker)
	}
	wg.Wait()

	if got := st.TrafficHistory(0); len(got) != 1 {
		t.Fatalf("expected one aggregated day, got %+v", got)
	} else if got[0].UplinkBytes != 40 || got[0].DownlinkBytes != 80 {
		t.Fatalf("concurrent deltas were not aggregated correctly: %+v", got[0])
	}
	if got := st.LatencyHistory(0); len(got) != 40 {
		t.Fatalf("expected 40 recorded samples, got %d", len(got))
	}
}
