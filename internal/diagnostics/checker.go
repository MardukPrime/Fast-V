package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
)

const (
	defaultTraceURL = "https://www.cloudflare.com/cdn-cgi/trace"
	defaultIPURL    = "https://api64.ipify.org?format=json"
	defaultGeoURL   = "https://ipwho.is/%s"
	defaultSpeedURL = "https://speed.cloudflare.com/__down?bytes=%d"
)

type Checker struct {
	traceURL       string
	ipURL          string
	geoURL         string
	speedURL       string
	speedBytes     int64
	latencySamples int
}

func NewChecker() *Checker {
	return &Checker{
		traceURL:       defaultTraceURL,
		ipURL:          defaultIPURL,
		geoURL:         defaultGeoURL,
		speedURL:       defaultSpeedURL,
		speedBytes:     5_000_000,
		latencySamples: 3,
	}
}

func (c *Checker) Probe(ctx context.Context, mode string, proxyAddr string, includeSpeed bool) (model.NetworkProbe, error) {
	if mode == "proxy" && strings.TrimSpace(proxyAddr) == "" {
		return model.NetworkProbe{}, errors.New("local proxy is not running")
	}

	transport := &http.Transport{ForceAttemptHTTP2: true}
	if proxyAddr != "" {
		parsed, err := url.Parse("http://" + proxyAddr)
		if err != nil {
			return model.NetworkProbe{}, fmt.Errorf("invalid local proxy address: %w", err)
		}
		transport.Proxy = http.ProxyURL(parsed)
	}
	client := &http.Client{Transport: transport, Timeout: 60 * time.Second}

	result := model.NetworkProbe{
		Mode:      mode,
		ProxyAddr: proxyAddr,
		CheckedAt: model.NowRFC3339(),
	}

	trace, _, err := c.fetchTrace(ctx, client)
	if err != nil {
		ip, ipErr := c.fetchIP(ctx, client)
		if ipErr != nil {
			return result, fmt.Errorf("IP detection failed: %w", err)
		}
		result.IP = ip
	} else {
		result.IP = trace["ip"]
		result.CountryCode = trace["loc"]
		result.Colo = trace["colo"]
		result.TLS = trace["tls"]
		result.WARP = trace["warp"]
	}
	result.IPVersion = ipVersion(result.IP)

	if result.IP != "" {
		if err := c.fillGeo(ctx, client, result.IP, &result); err != nil {
			result.GeoError = err.Error()
		}
	}

	result.LatencyMS, result.JitterMS = c.measureLatency(ctx, client)

	if includeSpeed {
		result.DownloadBytes, result.DownloadMbps, err = c.measureDownload(ctx, client)
		if err != nil {
			result.DownloadError = err.Error()
		}
	}

	return result, nil
}

func (c *Checker) fetchTrace(ctx context.Context, client *http.Client) (map[string]string, int64, error) {
	start := time.Now()
	body, err := c.get(ctx, client, c.traceURL, 64<<10)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return nil, latency, err
	}
	trace := make(map[string]string)
	for _, line := range strings.Split(string(body), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(parts) == 2 {
			trace[parts[0]] = parts[1]
		}
	}
	if trace["ip"] == "" {
		return nil, latency, errors.New("Cloudflare trace did not include an IP")
	}
	return trace, latency, nil
}

func (c *Checker) fetchIP(ctx context.Context, client *http.Client) (string, error) {
	body, err := c.get(ctx, client, c.ipURL, 64<<10)
	if err != nil {
		return "", err
	}
	var payload struct {
		IP string `json:"ip"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	if payload.IP == "" {
		return "", errors.New("IP service returned an empty address")
	}
	return payload.IP, nil
}

func (c *Checker) fillGeo(ctx context.Context, client *http.Client, ip string, result *model.NetworkProbe) error {
	endpoint := fmt.Sprintf(c.geoURL, url.PathEscape(ip))
	body, err := c.get(ctx, client, endpoint, 512<<10)
	if err != nil {
		return err
	}
	var payload struct {
		Success     bool   `json:"success"`
		IP          string `json:"ip"`
		Type        string `json:"type"`
		Country     string `json:"country"`
		CountryCode string `json:"country_code"`
		Region      string `json:"region"`
		City        string `json:"city"`
		Connection  struct {
			ASN int    `json:"asn"`
			Org string `json:"org"`
			ISP string `json:"isp"`
		} `json:"connection"`
		Timezone struct {
			ID string `json:"id"`
		} `json:"timezone"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}
	if !payload.Success {
		return errors.New("geo IP service returned an unsuccessful response")
	}
	result.Country = payload.Country
	result.CountryCode = firstNonEmpty(payload.CountryCode, result.CountryCode)
	result.Region = payload.Region
	result.City = payload.City
	result.ASN = payload.Connection.ASN
	result.Organization = payload.Connection.Org
	result.ISP = payload.Connection.ISP
	result.Timezone = payload.Timezone.ID
	if result.IPVersion == "" {
		result.IPVersion = payload.Type
	}
	return nil
}

func (c *Checker) measureLatency(ctx context.Context, client *http.Client) (int64, int64) {
	samples := make([]int64, 0, c.latencySamples)
	for i := 0; i < c.latencySamples; i++ {
		start := time.Now()
		resp, err := client.Get(c.traceURL)
		if err != nil {
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		samples = append(samples, time.Since(start).Milliseconds())
	}
	if len(samples) == 0 {
		return 0, 0
	}
	var total int64
	for _, sample := range samples {
		total += sample
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	return total / int64(len(samples)), samples[len(samples)-1] - samples[0]
}

func (c *Checker) measureDownload(ctx context.Context, client *http.Client) (int64, float64, error) {
	endpoint := fmt.Sprintf(c.speedURL, c.speedBytes)
	start := time.Now()
	resp, err := client.Get(endpoint)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("speed test returned HTTP %d", resp.StatusCode)
	}
	written, err := io.Copy(io.Discard, resp.Body)
	elapsed := time.Since(start).Seconds()
	if err != nil {
		return written, 0, err
	}
	if elapsed <= 0 {
		return written, 0, errors.New("speed test completed too quickly")
	}
	mbps := float64(written) * 8 / elapsed / 1_000_000
	return written, mbps, nil
}

func (c *Checker) get(ctx context.Context, client *http.Client, endpoint string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "FastV/0.1.0")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request returned HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func ipVersion(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ""
	}
	if parsed.To4() != nil {
		return "IPv4"
	}
	return "IPv6"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
