package history

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
)

func newHistoryTestService(t *testing.T) *Service {
	t.Helper()
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatalf("create test store: %v", err)
	}
	return New(st)
}

func seedTrafficDays(t *testing.T, st *store.Store, count int) {
	t.Helper()
	start := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.Local)
	for i := 0; i < count; i++ {
		if err := st.AddTrafficBytes(int64(i+1), int64((i+1)*10), start.AddDate(0, 0, i)); err != nil {
			t.Fatalf("add traffic: %v", err)
		}
	}
}

func seedLatencySamples(t *testing.T, st *store.Store, count int) {
	t.Helper()
	samples := make([]model.LatencySample, 0, count)
	for i := 0; i < count; i++ {
		samples = append(samples, model.LatencySample{
			NodeID:    "node-" + string(rune('a'+i)),
			NodeName:  "Node",
			LatencyMS: int64(i + 1),
			CheckedAt: "2026-01-01T00:00:00Z",
		})
	}
	if err := st.AddLatencySamples(samples); err != nil {
		t.Fatalf("add latency samples: %v", err)
	}
}

func TestServiceName(t *testing.T) {
	if got := New(nil).ServiceName(); got != "HistoryService" {
		t.Fatalf("unexpected service name: %q", got)
	}
}

func TestTrafficClampsDays(t *testing.T) {
	service := newHistoryTestService(t)
	st := service.store
	seedTrafficDays(t, st, 5)

	cases := []struct {
		name string
		days int
		want int
	}{
		{"zero clamps to one day", 0, 1},
		{"negative clamps to one day", -7, 1},
		{"one day", 1, 1},
		{"three days", 3, 3},
		{"above retained count returns everything", 999, 5},
		{"hard upper bound", store.MaxTrafficHistoryDays + 10, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := service.Traffic(tc.days)
			if len(got) != tc.want {
				t.Fatalf("Traffic(%d) returned %d days, want %d", tc.days, len(got), tc.want)
			}
		})
	}

	// The clamped result is the newest window in ascending order.
	lastTwo := service.Traffic(2)
	if len(lastTwo) != 2 || lastTwo[0].Date != "2026-01-04" || lastTwo[1].Date != "2026-01-05" {
		t.Fatalf("unexpected last two days: %+v", lastTwo)
	}
	if one := service.Traffic(0); len(one) != 1 || one[0].Date != "2026-01-05" {
		t.Fatalf("Traffic(0) must clamp to the newest day: %+v", one)
	}
}

func TestLatenciesClampLimit(t *testing.T) {
	service := newHistoryTestService(t)
	st := service.store
	seedLatencySamples(t, st, 5)

	cases := []struct {
		name  string
		limit int
		want  int
	}{
		{"zero clamps to one sample", 0, 1},
		{"negative clamps to one sample", -3, 1},
		{"one sample", 1, 1},
		{"three samples", 3, 3},
		{"above retained count returns everything", 999, 5},
		{"hard upper bound", store.MaxLatencyHistorySamples + 10, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := service.Latencies(tc.limit)
			if len(got) != tc.want {
				t.Fatalf("Latencies(%d) returned %d samples, want %d", tc.limit, len(got), tc.want)
			}
		})
	}

	// The clamped result keeps chronological order with the newest sample last.
	newest := service.Latencies(0)
	if len(newest) != 1 || newest[0].LatencyMS != 5 {
		t.Fatalf("Latencies(0) must clamp to the newest sample: %+v", newest)
	}
	lastTwo := service.Latencies(2)
	if len(lastTwo) != 2 || lastTwo[0].LatencyMS != 4 || lastTwo[1].LatencyMS != 5 {
		t.Fatalf("unexpected last two samples: %+v", lastTwo)
	}
}

func TestClampHelpers(t *testing.T) {
	if got := clampTrafficDays(0); got != 1 {
		t.Fatalf("clampTrafficDays(0) = %d, want 1", got)
	}
	if got := clampTrafficDays(store.MaxTrafficHistoryDays + 1); got != store.MaxTrafficHistoryDays {
		t.Fatalf("clampTrafficDays(max+1) = %d, want %d", got, store.MaxTrafficHistoryDays)
	}
	if got := clampLatencyLimit(0); got != 1 {
		t.Fatalf("clampLatencyLimit(0) = %d, want 1", got)
	}
	if got := clampLatencyLimit(store.MaxLatencyHistorySamples + 1); got != store.MaxLatencyHistorySamples {
		t.Fatalf("clampLatencyLimit(max+1) = %d, want %d", got, store.MaxLatencyHistorySamples)
	}
}

func TestClearRemovesHistoryButKeepsConfiguration(t *testing.T) {
	service := newHistoryTestService(t)
	st := service.store
	seedTrafficDays(t, st, 2)
	seedLatencySamples(t, st, 2)

	settings := store.DefaultSettings()
	settings.Theme = "dark"
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	if err := service.Clear(); err != nil {
		t.Fatalf("clear history: %v", err)
	}
	if got := service.Traffic(30); len(got) != 0 {
		t.Fatalf("traffic history was not cleared: %+v", got)
	}
	if got := service.Latencies(30); len(got) != 0 {
		t.Fatalf("latency history was not cleared: %+v", got)
	}
	if st.Settings().Theme != "dark" {
		t.Fatal("clearing history must not change settings")
	}
}
