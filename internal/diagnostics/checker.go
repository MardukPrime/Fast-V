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
	defaultGeoIPURL = "https://ipwho.is/"
	defaultSpeedURL = "https://speed.cloudflare.com/__down?bytes=%d"
)

type Checker struct {
	traceURL          string
	traceFallbackURLs []string
	ipURL             string
	ipFallbackURLs    []string
	geoURL            string
	geoIPURL          string
	speedURL          string
	speedBytes        int64
	latencySamples    int
}

func NewChecker() *Checker {
	return &Checker{
		traceURL: defaultTraceURL,
		traceFallbackURLs: []string{
			"https://cloudflare.com/cdn-cgi/trace",
			"https://1.1.1.1/cdn-cgi/trace",
		},
		ipURL: defaultIPURL,
		ipFallbackURLs: []string{
			"https://checkip.amazonaws.com",
			"https://ifconfig.me/ip",
			"https://icanhazip.com",
		},
		geoURL:         defaultGeoURL,
		geoIPURL:       defaultGeoIPURL,
		speedURL:       defaultSpeedURL,
		speedBytes:     5_000_000,
		latencySamples: 3,
	}
}

func (c *Checker) Probe(ctx context.Context, mode string, proxyAddr string, includeSpeed bool) (model.NetworkProbe, error) {
	if mode == "proxy" && strings.TrimSpace(proxyAddr) == "" {
		return model.NetworkProbe{}, errors.New("local proxy is not running")
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		MaxIdleConns:          8,
	}
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

	trace, _, traceErr := c.fetchTrace(ctx, client)
	geoLoaded := false
	if traceErr != nil {
		if payload, geoErr := c.fetchGeoPayload(ctx, client, c.geoIPURL); geoErr == nil && payload.IP != "" {
			result.IP = payload.IP
			c.applyGeo(payload, &result)
			geoLoaded = true
		} else {
			ip, ipErr := c.fetchIP(ctx, client)
			if ipErr != nil {
				return result, errors.New("所有 IP 检测接口均不可达，请检查网络连接，或启动代理后检测代理出口")
			}
			result.IP = ip
		}
	} else {
		result.IP = trace["ip"]
		result.CountryCode = trace["loc"]
		result.Colo = trace["colo"]
		result.TLS = trace["tls"]
		result.WARP = trace["warp"]
	}
	result.IPVersion = ipVersion(result.IP)

	if result.IP != "" && !geoLoaded {
		if err := c.fillGeo(ctx, client, result.IP, &result); err != nil {
			result.GeoError = err.Error()
		}
	}

	result.LatencyMS, result.JitterMS = c.measureLatency(ctx, client)

	if includeSpeed {
		downloadBytes, downloadMbps, downloadErr := c.measureDownload(ctx, client)
		result.DownloadBytes = downloadBytes
		result.DownloadMbps = downloadMbps
		if downloadErr != nil {
			result.DownloadError = downloadErr.Error()
		}
	}

	return result, nil
}

func (c *Checker) fetchTrace(ctx context.Context, client *http.Client) (map[string]string, int64, error) {
	endpoints := append([]string{c.traceURL}, c.traceFallbackURLs...)
	var lastErr error
	for _, endpoint := range endpoints {
		trace, latency, err := c.fetchTraceEndpoint(ctx, client, endpoint)
		if err == nil {
			return trace, latency, nil
		}
		lastErr = err
	}
	return nil, 0, lastErr
}

func (c *Checker) fetchTraceEndpoint(ctx context.Context, client *http.Client, endpoint string) (map[string]string, int64, error) {
	start := time.Now()
	body, err := c.get(ctx, client, endpoint, 64<<10)
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
	endpoints := append([]string{c.ipURL}, c.ipFallbackURLs...)
	var lastErr error
	for _, endpoint := range endpoints {
		body, err := c.get(ctx, client, endpoint, 64<<10)
		if err != nil {
			lastErr = err
			continue
		}
		text := strings.TrimSpace(string(body))
		var payload struct {
			IP string `json:"ip"`
		}
		if json.Unmarshal(body, &payload) == nil && payload.IP != "" {
			return payload.IP, nil
		}
		if net.ParseIP(text) != nil {
			return text, nil
		}
		lastErr = errors.New("IP service returned an invalid response")
	}
	return "", lastErr
}

type geoPayload struct {
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

func (c *Checker) fillGeo(ctx context.Context, client *http.Client, ip string, result *model.NetworkProbe) error {
	payload, err := c.fetchGeoPayload(ctx, client, fmt.Sprintf(c.geoURL, url.PathEscape(ip)))
	if err != nil {
		return err
	}
	c.applyGeo(payload, result)
	return nil
}

func (c *Checker) fetchGeoPayload(ctx context.Context, client *http.Client, endpoint string) (geoPayload, error) {
	body, err := c.get(ctx, client, endpoint, 512<<10)
	if err != nil {
		return geoPayload{}, err
	}
	var payload geoPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return geoPayload{}, err
	}
	if !payload.Success {
		return geoPayload{}, errors.New("geo IP service returned an unsuccessful response")
	}
	if payload.IP == "" {
		return geoPayload{}, errors.New("geo IP service returned an empty IP")
	}
	return payload, nil
}

func (c *Checker) applyGeo(payload geoPayload, result *model.NetworkProbe) {
	if result.IP == "" {
		result.IP = payload.IP
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
}
func (c *Checker) measureLatency(ctx context.Context, client *http.Client) (int64, int64) {
	endpoints := append([]string{c.traceURL}, c.traceFallbackURLs...)
	endpoints = append(endpoints, c.geoIPURL)
	samples := make([]int64, 0, c.latencySamples)
	for i := 0; i < c.latencySamples; i++ {
		for _, endpoint := range endpoints {
			start := time.Now()
			resp, err := client.Get(endpoint)
			if err != nil {
				continue
			}
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			_ = resp.Body.Close()
			samples = append(samples, time.Since(start).Milliseconds())
			break
		}
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
		elapsed = 0.001
	}
	mbps := float64(written) * 8 / elapsed / 1_000_000
	return written, mbps, nil
}

func (c *Checker) get(ctx context.Context, client *http.Client, endpoint string, limit int64) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
		if err != nil {
			cancel()
			return nil, err
		}
		req.Header.Set("User-Agent", "FastV/0.1.0")
		req.Header.Set("Accept", "*/*")
		req.Header.Set("Cache-Control", "no-cache")
		req.Close = true
		resp, err := client.Do(req)
		if err != nil {
			cancel()
			lastErr = err
			if attempt == 0 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(250 * time.Millisecond):
				}
			}
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, limit))
		_ = resp.Body.Close()
		cancel()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("request returned HTTP %d", resp.StatusCode)
			continue
		}
		if readErr != nil {
			lastErr = readErr
			continue
		}
		return body, nil
	}
	return nil, lastErr
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
