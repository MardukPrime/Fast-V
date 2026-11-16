package proxy

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatalf("create test store: %v", err)
	}
	return st
}

func runningTraffic(uplink, downlink int64) model.TrafficStats {
	return model.TrafficStats{Running: true, UplinkBytes: uplink, DownlinkBytes: downlink}
}

// newTestRecorder returns a recorder with a deterministic clock. The clock is
// moved by returning the current time value, so tests control the flush window.
func newTestRecorder(t *testing.T, st *store.Store, interval time.Duration) (*trafficRecorder, *time.Time) {
	t.Helper()
	recorder := newTrafficRecorder(st, interval)
	now := time.Date(2026, time.February, 3, 12, 0, 0, 0, time.Local)
	recorder.now = func() time.Time { return now }
	recorder.lastFlushAt = now
	return recorder, &now
}

func TestTrafficRecorderBatchesUntilFlushInterval(t *testing.T) {
	st := newTestStore(t)
	recorder, now := newTestRecorder(t, st, time.Minute)

	if err := recorder.observe(runningTraffic(100, 200)); err != nil {
		t.Fatalf("observe: %v", err)
	}
	if got := st.TrafficHistory(0); len(got) != 0 {
		t.Fatalf("traffic must not be flushed before the interval elapsed: %+v", got)
	}
	if up, down := recorder.pending(); up != 100 || down != 200 {
		t.Fatalf("pending totals: got %d/%d, want 100/200", up, down)
	}

	*now = now.Add(30 * time.Second)
	if err := recorder.observe(runningTraffic(50, 25)); err != nil {
		t.Fatalf("observe: %v", err)
	}
	if got := st.TrafficHistory(0); len(got) != 0 {
		t.Fatalf("traffic must still be buffered inside the interval: %+v", got)
	}

	// One second past the interval the buffered deltas are written in one batch.
	*now = now.Add(31 * time.Second)
	if err := recorder.observe(runningTraffic(7, 3)); err != nil {
		t.Fatalf("observe: %v", err)
	}
	got := st.TrafficHistory(0)
	want := model.DailyTraffic{Date: "2026-02-03", UplinkBytes: 157, DownlinkBytes: 228}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("unexpected flushed history: got %+v, want %+v", got, want)
	}
	if up, down := recorder.pending(); up != 0 || down != 0 {
		t.Fatalf("pending totals must be cleared by a flush: %d/%d", up, down)
	}

	// A flush opens a new window: the next sample stays in memory.
	if err := recorder.observe(runningTraffic(1, 1)); err != nil {
		t.Fatalf("observe: %v", err)
	}
	if got := st.TrafficHistory(0); len(got) != 1 {
		t.Fatalf("unexpected flush right after the previous one: %+v", got)
	}
	if up, down := recorder.pending(); up != 1 || down != 1 {
		t.Fatalf("new window pending totals: got %d/%d, want 1/1", up, down)
	}
}

func TestTrafficRecorderIgnoresUncountableSnapshots(t *testing.T) {
	st := newTestStore(t)
	recorder, _ := newTestRecorder(t, st, time.Minute)

	// Error snapshots repeat the last successful window's deltas and must not be
	// counted again; stopped snapshots carry no deltas at all.
	if err := recorder.observe(model.TrafficStats{Running: true, Error: "statsquery failed", UplinkBytes: 100, DownlinkBytes: 100}); err != nil {
		t.Fatalf("observe: %v", err)
	}
	if err := recorder.observe(model.TrafficStats{UplinkBytes: 100, DownlinkBytes: 100}); err != nil {
		t.Fatalf("observe: %v", err)
	}
	if err := recorder.observe(runningTraffic(-5, -6)); err != nil {
		t.Fatalf("observe: %v", err)
	}
	if up, down := recorder.pending(); up != 0 || down != 0 {
		t.Fatalf("uncountable snapshots must be ignored: %d/%d", up, down)
	}
	if err := recorder.flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if got := st.TrafficHistory(0); len(got) != 0 {
		t.Fatalf("uncountable snapshots were persisted: %+v", got)
	}
}

func TestTrafficRecorderFlushIsImmediateAndIdempotent(t *testing.T) {
	st := newTestStore(t)
	recorder, _ := newTestRecorder(t, st, time.Hour)

	if err := recorder.observe(runningTraffic(11, 22)); err != nil {
		t.Fatalf("observe: %v", err)
	}
	if got := st.TrafficHistory(0); len(got) != 0 {
		t.Fatalf("traffic must be buffered before an explicit flush: %+v", got)
	}
	if err := recorder.flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	got := st.TrafficHistory(0)
	if len(got) != 1 || got[0].UplinkBytes != 11 || got[0].DownlinkBytes != 22 {
		t.Fatalf("unexpected flushed history: %+v", got)
	}
	// Flushing again without new samples must not duplicate traffic.
	if err := recorder.flush(); err != nil {
		t.Fatalf("second flush: %v", err)
	}
	if got := st.TrafficHistory(0); len(got) != 1 || got[0].UplinkBytes != 11 || got[0].DownlinkBytes != 22 {
		t.Fatalf("second flush duplicated traffic: %+v", got)
	}
}

func TestProxyServiceStopFlushesPendingTraffic(t *testing.T) {
	st := newTestStore(t)
	service := New(st, nil)
	service.traffic.pendingUplink = 123
	service.traffic.pendingDown = 456

	if _, err := service.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	got := st.TrafficHistory(0)
	wantDate := time.Now().In(time.Local).Format("2006-01-02")
	if len(got) != 1 || got[0].UplinkBytes != 123 || got[0].DownlinkBytes != 456 || got[0].Date != wantDate {
		t.Fatalf("stop did not persist pending traffic: got %+v", got)
	}
	if up, down := service.traffic.pending(); up != 0 || down != 0 {
		t.Fatalf("pending totals were not cleared by stop: %d/%d", up, down)
	}
}

func TestProxyServiceShutdownFlushesPendingTraffic(t *testing.T) {
	st := newTestStore(t)
	service := New(st, nil)
	service.traffic.pendingUplink = 7
	service.traffic.pendingDown = 9

	if err := service.ServiceShutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	got := st.TrafficHistory(0)
	if len(got) != 1 || got[0].UplinkBytes != 7 || got[0].DownlinkBytes != 9 {
		t.Fatalf("shutdown did not persist pending traffic: %+v", got)
	}
}

func TestProxyServiceTerminalStateFlushesPendingTraffic(t *testing.T) {
	for _, status := range []string{"stopped", "error"} {
		t.Run(status, func(t *testing.T) {
			st := newTestStore(t)
			service := New(st, nil)
			service.traffic.pendingUplink = 5
			service.traffic.pendingDown = 6

			service.emit(model.ProxyState{Status: status})

			got := st.TrafficHistory(0)
			if len(got) != 1 || got[0].UplinkBytes != 5 || got[0].DownlinkBytes != 6 {
				t.Fatalf("terminal state %q did not persist pending traffic: %+v", status, got)
			}
		})
	}

	// Non-terminal states keep accumulating instead of writing on every event.
	st := newTestStore(t)
	service := New(st, nil)
	service.traffic.pendingUplink = 5
	service.traffic.pendingDown = 6
	service.emit(model.ProxyState{Status: "running"})
	if got := st.TrafficHistory(0); len(got) != 0 {
		t.Fatalf("running state must not flush the buffer: %+v", got)
	}
}

func TestProxyServiceEmitTrafficAccumulatesDeltas(t *testing.T) {
	st := newTestStore(t)
	service := New(st, nil)

	service.emitTraffic(runningTraffic(30, 40))
	if up, down := service.traffic.pending(); up != 30 || down != 40 {
		t.Fatalf("live sample was not buffered: %d/%d", up, down)
	}
	// Error snapshots repeat deltas and must be ignored.
	service.emitTraffic(model.TrafficStats{Running: true, Error: "boom", UplinkBytes: 99, DownlinkBytes: 99})
	if up, down := service.traffic.pending(); up != 30 || down != 40 {
		t.Fatalf("error snapshot changed the buffer: %d/%d", up, down)
	}
	if got := st.TrafficHistory(0); len(got) != 0 {
		t.Fatalf("traffic must stay buffered within the flush interval: %+v", got)
	}
}
