package subscription

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// UserInfo is the traffic and expiry metadata supplied by a subscription endpoint.
type UserInfo struct {
	UploadBytes   int64
	DownloadBytes int64
	TotalBytes    int64
	ExpireAt      int64
	Present       bool
}

// FetchResult contains both the subscription body and response metadata.
type FetchResult struct {
	Content  string
	UserInfo UserInfo
}

// Fetch downloads a subscription body with a bounded size and deterministic timeout.
func Fetch(ctx context.Context, rawURL string) (string, error) {
	result, err := FetchWithMeta(ctx, rawURL)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

// FetchWithMeta downloads a subscription and captures Subscription-Userinfo headers.
func FetchWithMeta(ctx context.Context, rawURL string) (FetchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return FetchResult{}, err
	}
	req.Header.Set("User-Agent", "FastV/0.1.0")
	req.Header.Set("Accept", "*/*")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return FetchResult{}, fmt.Errorf("failed to fetch subscription: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return FetchResult{}, fmt.Errorf("subscription returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return FetchResult{}, fmt.Errorf("failed to read subscription: %w", err)
	}
	if len(body) == 0 {
		return FetchResult{}, errors.New("subscription response is empty")
	}
	return FetchResult{
		Content:  string(body),
		UserInfo: ParseUserInfo(resp.Header.Get("Subscription-Userinfo")),
	}, nil
}

// ParseUserInfo parses the common Subscription-Userinfo header format.
func ParseUserInfo(value string) UserInfo {
	info := UserInfo{}
	for _, field := range strings.Split(value, ";") {
		parts := strings.SplitN(strings.TrimSpace(field), "=", 2)
		if len(parts) != 2 {
			continue
		}
		number, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil {
			continue
		}
		info.Present = true
		switch strings.ToLower(strings.TrimSpace(parts[0])) {
		case "upload":
			info.UploadBytes = number
		case "download":
			info.DownloadBytes = number
		case "total":
			info.TotalBytes = number
		case "expire":
			info.ExpireAt = number
		}
	}
	if info.ExpireAt > 1_000_000_000_000 {
		info.ExpireAt /= 1000
	}
	return info
}
