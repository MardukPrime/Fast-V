package ruleset

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/MardukPrime/Fast-V/internal/buildinfo"
	"github.com/MardukPrime/Fast-V/internal/model"
)

const (
	// fetchTimeout bounds one rule set download (connect + read).
	fetchTimeout = 20 * time.Second
	// maxResponseBytes is the largest rule set body accepted. Remote lists are
	// a few megabytes at most; anything bigger is rejected instead of parsed.
	maxResponseBytes = 4 << 20
)

// UserAgent returns the User-Agent header sent to rule set endpoints, for
// example "FastV/0.1.0".
func UserAgent() string {
	version := strings.TrimSpace(buildinfo.Version)
	if version == "" {
		version = "dev"
	}
	return buildinfo.ProductName + "/" + version
}

// FetchRules downloads a remote rule set and parses it into routing rules. The
// download uses a 20 second timeout, accepts at most 4 MB and identifies the
// client with the FastV version. HTTP status errors and oversize responses are
// reported in Chinese for the UI.
func FetchRules(ctx context.Context, rawURL string, kind string) ([]model.RoutingRule, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(rawURL), nil)
	if err != nil {
		return nil, fmt.Errorf("规则集地址无效: %w", err)
	}
	request.Header.Set("User-Agent", UserAgent())
	request.Header.Set("Accept", "*/*")

	client := &http.Client{Timeout: fetchTimeout}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("规则集下载失败: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("规则集下载失败: 服务器返回 HTTP %d", response.StatusCode)
	}

	// Read one byte past the limit so an oversize body is detectable.
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("规则集读取失败: %w", err)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("规则集文件超过 %d MB 限制", maxResponseBytes>>20)
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil, errors.New("规则集内容为空")
	}
	return Parse(string(body), kind)
}
