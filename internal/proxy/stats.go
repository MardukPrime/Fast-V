package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
)

const (
	// trafficSampleInterval is how often the Xray stats API is polled while the
	// proxy is running. Querying the running core spawns a short-lived CLI
	// process, so two seconds keeps the overhead low while still feeling live.
	trafficSampleInterval = 2 * time.Second

	// trafficFirstSampleDelay gives the freshly started core a moment to bring
	// its API inbound up before the first query.
	trafficFirstSampleDelay = 500 * time.Millisecond

	// trafficQueryTimeoutSeconds is forwarded to `xray api statsquery -t`, and
	// trafficExecDeadline is the local hard deadline for the child process so a
	// wedged API can never block the sampler goroutine.
	trafficQueryTimeoutSeconds = 2
	trafficExecDeadline        = 5 * time.Second

	// trafficQueryPattern asks for every outbound counter. The parser below
	// selects the counters that belong to user proxy traffic, so the pattern also
	// covers the per-node balancer outbounds of load balancing mode.
	trafficQueryPattern = "outbound>>>"
	// trafficOutboundPrefix and trafficTrafficMarker split a counter name of the
	// form outbound>>>TAG>>>traffic>>>(uplink|downlink).
	trafficOutboundPrefix = "outbound>>>"
	trafficTrafficMarker  = ">>>traffic>>>"
	// trafficUplinkSuffix / trafficDownlinkSuffix are the two counted directions.
	trafficUplinkSuffix   = "uplink"
	trafficDownlinkSuffix = "downlink"
)

// countedOutboundTag reports whether an Xray outbound tag carries user proxy
// traffic. Single-node mode uses the "proxy" tag while load balancing mode uses
// one "balancer-node-<index>" outbound per node; everything else (direct, block,
// api, the internal dns outbound, ...) must never be added to the dashboard
// traffic counters.
func countedOutboundTag(tag string) bool {
	tag = strings.TrimSpace(tag)
	return tag == proxyOutboundTag || strings.HasPrefix(tag, loadBalanceOutboundPrefix)
}

// parseStatsOutput converts the output of `xray api statsquery -pattern
// outbound>>>` into the cumulative uplink/downlink byte counters of the proxy
// traffic. In single-node mode that is the "proxy" outbound; in load balancing
// mode every "balancer-node-<index>" outbound counter is summed.
//
// Xray 26.x prints JSON:
//
//	{
//	    "stat": [
//	        {"name": "outbound>>>proxy>>>traffic>>>uplink", "value": 237},
//	        {"name": "outbound>>>proxy>>>traffic>>>downlink", "value": 420}
//	    ]
//	}
//
// Older cores print protobuf text, for example:
//
//	stat: <
//	  name: "outbound>>>proxy>>>traffic>>>uplink"
//	  value: 237
//	>
//
// Both shapes are accepted. Other counters in the response (inbound, api,
// direct, block, ...) are ignored. Counters whose value has not been recorded
// yet are omitted by the JSON encoder and are treated as zero.
func parseStatsOutput(output []byte) (uplink, downlink int64, err error) {
	trimmed := bytes.TrimSpace(output)
	if len(trimmed) == 0 {
		return 0, 0, errors.New("empty statsquery output")
	}
	if up, down, found, jsonErr := parseJSONStats(trimmed); jsonErr == nil {
		if !found {
			return 0, 0, errors.New("statsquery returned no outbound traffic counters")
		}
		return up, down, nil
	}
	if up, down, found := parseTextStats(trimmed); found {
		return up, down, nil
	}
	return 0, 0, fmt.Errorf("unrecognized statsquery output: %s", shortenOutput(trimmed))
}

type statsJSONEntry struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

// parseOutboundCounterName splits an Xray stats counter name into its outbound
// tag and direction. Non outbound counters are reported as not parsed.
func parseOutboundCounterName(name string) (tag string, direction string, ok bool) {
	if !strings.HasPrefix(name, trafficOutboundPrefix) {
		return "", "", false
	}
	rest := name[len(trafficOutboundPrefix):]
	marker := strings.Index(rest, trafficTrafficMarker)
	if marker < 0 {
		return "", "", false
	}
	tag = rest[:marker]
	switch tail := rest[marker+len(trafficTrafficMarker):]; tail {
	case trafficUplinkSuffix:
		return tag, trafficUplinkSuffix, true
	case trafficDownlinkSuffix:
		return tag, trafficDownlinkSuffix, true
	default:
		return "", "", false
	}
}

// parseJSONStats parses the JSON form and sums every counted outbound counter. A
// nil error means the payload was valid JSON; found reports whether a counted
// proxy traffic counter was present at all.
func parseJSONStats(raw []byte) (uplink, downlink int64, found bool, err error) {
	var response struct {
		Stat []statsJSONEntry `json:"stat"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return 0, 0, false, err
	}
	for _, entry := range response.Stat {
		tag, direction, ok := parseOutboundCounterName(entry.Name)
		if !ok || !countedOutboundTag(tag) {
			continue
		}
		value := parseJSONCounter(entry.Value)
		if direction == trafficUplinkSuffix {
			uplink += value
		} else {
			downlink += value
		}
		found = true
	}
	return uplink, downlink, found, nil
}

// parseJSONCounter accepts a JSON number, a numeric string, or a missing value
// (which Xray omits for zero counters).
func parseJSONCounter(raw json.RawMessage) int64 {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return 0
	}
	var number json.Number
	if err := json.Unmarshal(trimmed, &number); err == nil {
		if value, err := number.Int64(); err == nil {
			return value
		}
		if value, err := number.Float64(); err == nil {
			return int64(value)
		}
	}
	var text string
	if err := json.Unmarshal(trimmed, &text); err == nil {
		if value, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64); err == nil {
			return value
		}
	}
	return 0
}

// parseTextStats parses protobuf text output (and tolerates plain
// "counter<separator>value" lines) and sums every counted outbound counter. The
// name and its value may sit on the same line or on consecutive lines.
func parseTextStats(raw []byte) (uplink, downlink int64, found bool) {
	text := string(raw)
	for searchFrom := 0; ; {
		index := strings.Index(text[searchFrom:], trafficOutboundPrefix)
		if index < 0 {
			break
		}
		nameStart := searchFrom + index + len(trafficOutboundPrefix)
		markerOffset := strings.Index(text[nameStart:], trafficTrafficMarker)
		if markerOffset < 0 {
			break
		}
		markerIndex := nameStart + markerOffset
		tag := strings.TrimSpace(text[nameStart:markerIndex])
		kindStart := markerIndex + len(trafficTrafficMarker)

		direction := ""
		directionEnd := kindStart
		switch {
		case strings.HasPrefix(text[kindStart:], trafficUplinkSuffix):
			direction = trafficUplinkSuffix
			directionEnd = kindStart + len(trafficUplinkSuffix)
		case strings.HasPrefix(text[kindStart:], trafficDownlinkSuffix):
			direction = trafficDownlinkSuffix
			directionEnd = kindStart + len(trafficDownlinkSuffix)
		default:
			// Not a traffic counter that ends with a known direction; keep
			// scanning after this marker.
			searchFrom = kindStart
			continue
		}
		searchFrom = directionEnd
		if !countedOutboundTag(tag) {
			continue
		}
		// The value belongs to this counter until the next counter name starts.
		windowEnd := len(text)
		if next := strings.Index(text[directionEnd:], "name:"); next >= 0 {
			windowEnd = directionEnd + next
		}
		value, ok := extractTextCounterValue(text[directionEnd:windowEnd])
		if !ok {
			continue
		}
		if direction == trafficUplinkSuffix {
			uplink += value
		} else {
			downlink += value
		}
		found = true
	}
	return uplink, downlink, found
}

// extractTextCounterValue looks for the value that belongs to a counter name.
// It understands `value: 123` / `value: <123>` / nested `value: < value: 123 >`
// and plain table style `name    123` layouts.
func extractTextCounterValue(window string) (int64, bool) {
	for from := 0; ; {
		index := strings.Index(window[from:], "value")
		if index < 0 {
			break
		}
		index += from
		if value, ok := parseNumberToken(window[index+len("value"):]); ok {
			return value, true
		}
		from = index + len("value")
	}
	// Fallback for layouts such as "counter>>>uplink    123" where the number is
	// not separated by a colon or a "value" keyword.
	if value, ok := parseNumberToken(window); ok {
		return value, true
	}
	return lastNumberToken(window)
}

// lastNumberToken returns the right-most integer in text. It is the fallback for
// layouts such as "counter>>>uplink    123" where the number is not separated by
// a colon or a "value" keyword.
func lastNumberToken(text string) (int64, bool) {
	end := len(text)
	for end > 0 && !isASCIIDigit(text[end-1]) {
		end--
	}
	if end == 0 {
		return 0, false
	}
	start := end
	for start > 0 && isASCIIDigit(text[start-1]) {
		start--
	}
	if start > 0 && text[start-1] == '-' {
		start--
	}
	value, err := strconv.ParseInt(text[start:end], 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func isASCIIDigit(char byte) bool {
	return char >= '0' && char <= '9'
}

// parseNumberToken parses an optional protobuf-text wrapper, quotes or
// separators followed by an integer.
func parseNumberToken(text string) (int64, bool) {
	text = strings.TrimLeft(strings.TrimSpace(text), ":=\"' \t")
	if strings.HasPrefix(text, "<") {
		text = strings.TrimLeft(strings.TrimSpace(text[1:]), ":=\"' \t")
	}
	end := 0
	for end < len(text) {
		char := text[end]
		if (char >= '0' && char <= '9') || (char == '-' && end == 0) {
			end++
			continue
		}
		break
	}
	if end == 0 || (end == 1 && text[0] == '-') {
		return 0, false
	}
	value, err := strconv.ParseInt(text[:end], 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func shortenOutput(raw []byte) string {
	const limit = 160
	text := strings.Join(strings.Fields(string(raw)), " ")
	if len(text) > limit {
		text = text[:limit] + "..."
	}
	return text
}

// trafficSample is one computed sampling window.
type trafficSample struct {
	UplinkBytes   int64
	DownlinkBytes int64
	UplinkRate    int64
	DownlinkRate  int64
	TotalUplink   int64
	TotalDownlink int64
}

// trafficMeter turns monotonic cumulative counters into per-window deltas,
// bytes-per-second rates and an accumulated session total.
type trafficMeter struct {
	lastUplink    int64
	lastDownlink  int64
	totalUplink   int64
	totalDownlink int64
	lastAt        time.Time
	started       bool
}

func (m *trafficMeter) advance(uplink, downlink int64, at time.Time) trafficSample {
	if !m.started {
		m.started = true
		m.lastUplink = uplink
		m.lastDownlink = downlink
		m.totalUplink = uplink
		m.totalDownlink = downlink
		m.lastAt = at
		return trafficSample{TotalUplink: uplink, TotalDownlink: downlink}
	}

	deltaUplink := uplink - m.lastUplink
	if deltaUplink < 0 {
		// Counter reset (for example the core was restarted); never make the
		// reported traffic negative.
		deltaUplink = 0
	}
	deltaDownlink := downlink - m.lastDownlink
	if deltaDownlink < 0 {
		deltaDownlink = 0
	}
	m.totalUplink += deltaUplink
	m.totalDownlink += deltaDownlink

	elapsed := at.Sub(m.lastAt)
	var uplinkRate, downlinkRate int64
	if elapsed > 0 {
		seconds := elapsed.Seconds()
		uplinkRate = int64(float64(deltaUplink) / seconds)
		downlinkRate = int64(float64(deltaDownlink) / seconds)
	}

	m.lastUplink = uplink
	m.lastDownlink = downlink
	m.lastAt = at
	return trafficSample{
		UplinkBytes:   deltaUplink,
		DownlinkBytes: deltaDownlink,
		UplinkRate:    uplinkRate,
		DownlinkRate:  downlinkRate,
		TotalUplink:   m.totalUplink,
		TotalDownlink: m.totalDownlink,
	}
}

// trafficSampler polls the Xray stats API for the running core and publishes
// live traffic snapshots. Query failures are reported through the Error field
// of the snapshot and never stop or fail the proxy itself.
type trafficSampler struct {
	onSample func(model.TrafficStats)
	interval time.Duration
	// firstDelay is the wait before the first query; overridable in tests.
	firstDelay time.Duration
	// query executes one statsquery call; nil means execQuery. Overridable in
	// tests so the sampling loop can be exercised without a real core.
	query func(ctx context.Context) ([]byte, error)

	lifecycleMu sync.Mutex
	started     bool
	stopped     bool
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	corePath    string
	apiPort     int

	mu     sync.Mutex
	meter  trafficMeter
	latest model.TrafficStats
}

func newTrafficSampler(onSample func(model.TrafficStats)) *trafficSampler {
	return &trafficSampler{
		onSample:   onSample,
		interval:   trafficSampleInterval,
		firstDelay: trafficFirstSampleDelay,
	}
}

// start launches the sampling goroutine. It is a no-op when the sampler was
// already started or stopped, which makes the manager's start/stop ordering
// racy-safe.
func (s *trafficSampler) start(ctx context.Context, corePath string, apiPort int) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.started || s.stopped || corePath == "" || apiPort <= 0 {
		return
	}
	s.started = true
	s.corePath = corePath
	s.apiPort = apiPort
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.done = make(chan struct{})
	go s.run()
}

// stop cancels any in-flight statsquery and waits for the sampling goroutine to
// exit. It is safe to call multiple times and before start.
func (s *trafficSampler) stop() {
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

// stats returns the latest snapshot. While the sampler is alive the proxy is
// running, so Running is always true; the manager turns this into a zero value
// once the sampler has been stopped.
func (s *trafficSampler) stats() model.TrafficStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := s.latest
	snapshot.Running = true
	return snapshot
}

func (s *trafficSampler) run() {
	defer close(s.done)
	interval := s.interval
	if interval <= 0 {
		interval = trafficSampleInterval
	}
	firstDelay := s.firstDelay
	if firstDelay <= 0 {
		firstDelay = trafficFirstSampleDelay
	}
	timer := time.NewTimer(firstDelay)
	defer timer.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-timer.C:
			s.sample()
			timer.Reset(interval)
		}
	}
}

func (s *trafficSampler) sample() {
	queryCtx, cancel := context.WithTimeout(s.ctx, trafficExecDeadline)
	output, err := s.runQuery(queryCtx)
	cancel()
	at := time.Now()
	if err != nil {
		if s.ctx.Err() != nil {
			// The sampler is shutting down; do not surface a cancellation as a
			// traffic error.
			return
		}
		s.publishError(err)
		return
	}
	uplink, downlink, err := parseStatsOutput(output)
	if err != nil {
		s.publishError(err)
		return
	}
	s.mu.Lock()
	sample := s.meter.advance(uplink, downlink, at)
	s.latest = model.TrafficStats{
		Running:       true,
		UplinkBytes:   sample.UplinkBytes,
		DownlinkBytes: sample.DownlinkBytes,
		UplinkRate:    sample.UplinkRate,
		DownlinkRate:  sample.DownlinkRate,
		TotalUplink:   sample.TotalUplink,
		TotalDownlink: sample.TotalDownlink,
		UpdatedAt:     model.NowRFC3339(),
	}
	snapshot := s.latest
	s.mu.Unlock()
	s.emit(snapshot)
}

// publishError keeps the last successful counters and only updates the Error
// field (and the running flag), as requested by the traffic contract.
func (s *trafficSampler) publishError(err error) {
	s.mu.Lock()
	s.latest.Running = true
	s.latest.Error = statsQueryErrorMessage(err)
	snapshot := s.latest
	s.mu.Unlock()
	s.emit(snapshot)
}

func (s *trafficSampler) emit(stats model.TrafficStats) {
	if s.onSample != nil {
		s.onSample(stats)
	}
}

func (s *trafficSampler) runQuery(ctx context.Context) ([]byte, error) {
	if s.query != nil {
		return s.query(ctx)
	}
	return s.execQuery(ctx)
}

// execQuery runs `xray api statsquery` against the core API. The CLI process is
// hidden on Windows and bounded by the timeout wrapped around ctx by sample().
func (s *trafficSampler) execQuery(ctx context.Context) ([]byte, error) {
	args := []string{
		"api", "statsquery",
		"--server=127.0.0.1:" + strconv.Itoa(s.apiPort),
		"-pattern", trafficQueryPattern,
		"-t", strconv.Itoa(trafficQueryTimeoutSeconds),
	}
	cmd := exec.CommandContext(ctx, s.corePath, args...)
	hideWindow(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, errors.New("statsquery request timed out")
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("statsquery failed: %s", message)
	}
	return output, nil
}

func statsQueryErrorMessage(err error) string {
	message := strings.TrimSpace(err.Error())
	if index := strings.IndexAny(message, "\r\n"); index >= 0 {
		message = strings.TrimSpace(message[:index])
	}
	if message == "" {
		return "traffic stats unavailable"
	}
	return message
}
