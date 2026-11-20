package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/MardukPrime/Fast-V/internal/model"
)

// singboxClashTrafficPath is the Clash API endpoint that streams per-second
// upload/download rates for every connection handled by the running core.
const singboxClashTrafficPath = "/traffic"

// singboxTrafficSampler keeps a WebSocket to the embedded/attached sing-box
// Clash API alive and turns its per-second rates into the same TrafficStats
// contract the Xray sampler produces. Clash reports rates, not cumulative
// counters, so the sampler accumulates them into session totals before feeding
// the shared trafficMeter.
type singboxTrafficSampler struct {
	onSample func(model.TrafficStats)

	lifecycleMu sync.Mutex
	started     bool
	stopped     bool
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	apiPort     int

	mu     sync.Mutex
	meter  trafficMeter
	latest model.TrafficStats
}

func newSingBoxTrafficSampler(onSample func(model.TrafficStats)) *singboxTrafficSampler {
	return &singboxTrafficSampler{onSample: onSample}
}

func (s *singboxTrafficSampler) start(ctx context.Context, apiPort int) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.started || s.stopped || apiPort <= 0 {
		return
	}
	s.started = true
	s.apiPort = apiPort
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.done = make(chan struct{})
	go s.run()
}

func (s *singboxTrafficSampler) stop() {
	s.lifecycleMu.Lock()
	done := s.done
	if s.stopped || !s.started {
		s.stopped = true
		s.lifecycleMu.Unlock()
		if done != nil {
			<-done
		}
		return
	}
	s.stopped = true
	cancel := s.cancel
	s.lifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	<-done
}

func (s *singboxTrafficSampler) stats() model.TrafficStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := s.latest
	snapshot.Running = true
	return snapshot
}

func (s *singboxTrafficSampler) run() {
	defer close(s.done)
	for {
		if s.ctx.Err() != nil {
			return
		}
		err := s.stream()
		if s.ctx.Err() != nil {
			return
		}
		if err != nil {
			s.publishError(err)
		}
		// Reconnect after a short delay; the core API may still be starting or
		// was restarted by the watchdog.
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (s *singboxTrafficSampler) stream() error {
	url := fmt.Sprintf("ws://127.0.0.1:%d%s", s.apiPort, singboxClashTrafficPath)
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, response, err := dialer.DialContext(s.ctx, url, nil)
	if err != nil {
		if response != nil {
			_ = response.Body.Close()
		}
		return fmt.Errorf("连接 sing-box 流量 API 失败: %w", err)
	}
	defer conn.Close()

	var totalUp, totalDown int64
	for {
		select {
		case <-s.ctx.Done():
			return nil
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		_, payload, err := conn.ReadMessage()
		if err != nil {
			if s.ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("读取 sing-box 流量数据失败: %w", err)
		}
		var sample struct {
			Up   int64 `json:"up"`
			Down int64 `json:"down"`
		}
		if err := json.Unmarshal(payload, &sample); err != nil {
			continue
		}
		if sample.Up < 0 || sample.Down < 0 {
			continue
		}
		totalUp += sample.Up
		totalDown += sample.Down
		s.publish(totalUp, totalDown)
	}
}

func (s *singboxTrafficSampler) publish(totalUp, totalDown int64) {
	at := time.Now()
	s.mu.Lock()
	sample := s.meter.advance(totalUp, totalDown, at)
	stats := model.TrafficStats{
		Running:       true,
		UplinkBytes:   sample.UplinkBytes,
		DownlinkBytes: sample.DownlinkBytes,
		UplinkRate:    sample.UplinkRate,
		DownlinkRate:  sample.DownlinkRate,
		TotalUplink:   sample.TotalUplink,
		TotalDownlink: sample.TotalDownlink,
		UpdatedAt:     model.NowRFC3339(),
	}
	s.latest = stats
	s.mu.Unlock()
	s.emit(stats)
}

func (s *singboxTrafficSampler) publishError(err error) {
	s.mu.Lock()
	s.latest.Running = true
	s.latest.Error = err.Error()
	snapshot := s.latest
	s.mu.Unlock()
	s.emit(snapshot)
}

func (s *singboxTrafficSampler) emit(stats model.TrafficStats) {
	if s.onSample != nil {
		s.onSample(stats)
	}
}
