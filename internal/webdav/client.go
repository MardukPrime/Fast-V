// Package webdav implements the small WebDAV client FastV needs for cloud
// backups.
//
// The implementation uses only the standard library and supports exactly the
// protocol subset a backup workflow needs:
//
//   - MKCOL to create the parent collections of the backup file, level by level;
//   - PUT to upload the versioned JSON backup;
//   - GET to download it again;
//   - HEAD (with a PROPFIND fallback) to check whether a remote backup exists;
//   - DELETE to clean up the temporary connectivity probe.
//
// On top of the protocol, the client provides HTTP Basic authentication, path
// traversal protection, upload/download size caps and Chinese error messages
// that never echo the configured password. HTTP status codes are always
// included in the error text so the frontend can show an actionable hint.
package webdav

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

const (
	// DefaultTimeout bounds a single HTTP round trip when HTTPClient is nil.
	DefaultTimeout = 30 * time.Second
	// MaxDownloadSize limits how much of a remote backup is read into memory.
	MaxDownloadSize int64 = 16 << 20
	// MaxUploadSize limits the size of an uploaded backup.
	MaxUploadSize int64 = 16 << 20
	// ProbeFileName is the temporary file used by Test to verify write access.
	ProbeFileName = ".fastv-probe"

	backupContentType = "application/json"
	probeContentType  = "text/plain; charset=utf-8"
	userAgent         = "FastV-WebDAV/1.0"
)

// driveLetterPattern matches Windows style absolute paths such as "C:\FastV"
// (the backslashes are normalized to slashes before this check).
var driveLetterPattern = regexp.MustCompile(`^[A-Za-z]:`)

// Client is a minimal WebDAV client bound to one remote base URL.
type Client struct {
	// BaseURL is the WebDAV collection the backup paths are relative to, for
	// example "https://dav.example.com/remote.php/dav/files/alice".
	BaseURL string
	// Username and Password are sent with HTTP Basic authentication.
	Username string
	Password string
	// HTTPClient optionally overrides the HTTP client, for example to trust a
	// self-signed certificate in tests. When nil, a client using Timeout is
	// created for every request.
	HTTPClient *http.Client
	// Timeout applies when HTTPClient is nil. It defaults to DefaultTimeout.
	Timeout time.Duration
}

// New builds a client for a WebDAV server. The base URL is trimmed of
// surrounding spaces and trailing slashes; the http/https validation itself is
// deferred until the first request so callers can build a client from settings
// that are not complete yet.
func New(baseURL, username, password string) *Client {
	return &Client{
		BaseURL:  strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Username: username,
		Password: password,
	}
}

// Test verifies that the configured WebDAV account can write in the base
// directory. It uploads a temporary .fastv-probe file and deletes it again, so
// a successful test guarantees that a later backup upload will not be rejected
// because of authentication or permissions.
func (c *Client) Test(ctx context.Context) error {
	return c.TestPath(ctx, "")
}

// TestPath verifies that the account can write next to targetPath. The parent
// collection of targetPath is created if needed, then a temporary
// .fastv-probe file is uploaded and deleted.
func (c *Client) TestPath(ctx context.Context, targetPath string) error {
	ctx = nonNilContext(ctx)

	relative, err := normalizeRemotePath(targetPath)
	if err != nil {
		return err
	}

	dir := ""
	probePath := ProbeFileName
	if relative != "" {
		if dir = parentCollection(relative); dir != "" {
			probePath = dir + "/" + ProbeFileName
		}
	}
	if dir != "" {
		if err := c.ensureCollection(ctx, dir); err != nil {
			return err
		}
	}

	probeURL, _, err := c.endpoint(probePath, false)
	if err != nil {
		return err
	}

	putRequest, err := http.NewRequestWithContext(ctx, http.MethodPut, probeURL, bytes.NewReader([]byte("FastV WebDAV probe")))
	if err != nil {
		return fmt.Errorf("构造 WebDAV 测试请求失败: %v", err)
	}
	putRequest.Header.Set("Content-Type", probeContentType)
	putRequest.Header.Set("User-Agent", userAgent)
	putResponse, err := c.do(putRequest)
	if err != nil {
		return err
	}
	drainAndClose(putResponse.Body)
	if !isSuccessStatus(putResponse.StatusCode) {
		return statusError("WebDAV 连接测试", putResponse.StatusCode)
	}

	deleteRequest, err := http.NewRequestWithContext(ctx, http.MethodDelete, probeURL, nil)
	if err != nil {
		return fmt.Errorf("构造 WebDAV 测试清理请求失败: %v", err)
	}
	deleteRequest.Header.Set("User-Agent", userAgent)
	deleteResponse, err := c.do(deleteRequest)
	if err != nil {
		return err
	}
	drainAndClose(deleteResponse.Body)
	switch {
	case isSuccessStatus(deleteResponse.StatusCode):
		return nil
	case deleteResponse.StatusCode == http.StatusNotFound:
		// Nothing left to clean up; the probe is gone either way.
		return nil
	default:
		return statusError("清理 WebDAV 测试文件", deleteResponse.StatusCode)
	}
}

// Upload writes data to remotePath, creating every missing parent collection
// first. The payload is sent as application/json; 2xx statuses are considered
// successful (WebDAV servers commonly answer 200, 201 or 204).
func (c *Client) Upload(ctx context.Context, remotePath string, data []byte) error {
	ctx = nonNilContext(ctx)

	relative, err := validateFileRemotePath(remotePath)
	if err != nil {
		return err
	}
	if int64(len(data)) > MaxUploadSize {
		return fmt.Errorf("备份文件过大（%d 字节），最大支持 %d 字节", len(data), MaxUploadSize)
	}
	if dir := parentCollection(relative); dir != "" {
		if err := c.ensureCollection(ctx, dir); err != nil {
			return err
		}
	}

	target, _, err := c.endpoint(relative, false)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, target, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("构造 WebDAV 上传请求失败: %v", err)
	}
	request.Header.Set("Content-Type", backupContentType)
	request.Header.Set("User-Agent", userAgent)

	response, err := c.do(request)
	if err != nil {
		return err
	}
	drainAndClose(response.Body)
	if !isSuccessStatus(response.StatusCode) {
		return statusError("上传 WebDAV 备份", response.StatusCode)
	}
	return nil
}

// Download fetches remotePath. A missing file is reported with the descriptive
// message 远端没有备份文件; the response body is capped at MaxDownloadSize so a
// misconfigured or malicious server cannot exhaust memory.
func (c *Client) Download(ctx context.Context, remotePath string) ([]byte, error) {
	ctx = nonNilContext(ctx)

	relative, err := validateFileRemotePath(remotePath)
	if err != nil {
		return nil, err
	}

	target, _, err := c.endpoint(relative, false)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("构造 WebDAV 下载请求失败: %v", err)
	}
	request.Header.Set("Accept", backupContentType+", */*")
	request.Header.Set("User-Agent", userAgent)

	response, err := c.do(request)
	if err != nil {
		return nil, err
	}
	defer drainAndClose(response.Body)

	if !isSuccessStatus(response.StatusCode) {
		if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
			return nil, fmt.Errorf("远端没有备份文件（HTTP %d）", response.StatusCode)
		}
		return nil, statusError("下载 WebDAV 备份", response.StatusCode)
	}
	if response.ContentLength > MaxDownloadSize {
		return nil, fmt.Errorf("远端备份文件过大（%d 字节），最大支持 %d 字节", response.ContentLength, MaxDownloadSize)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, MaxDownloadSize+1))
	if err != nil {
		return nil, fmt.Errorf("读取远端备份内容失败: %v", err)
	}
	if int64(len(data)) > MaxDownloadSize {
		return nil, fmt.Errorf("远端备份文件超过大小限制，最大支持 %d 字节", MaxDownloadSize)
	}
	return data, nil
}

// Exists reports whether remotePath exists on the server. HEAD is used first;
// servers that do not implement it fall back to a depth-zero PROPFIND.
func (c *Client) Exists(ctx context.Context, remotePath string) (bool, error) {
	ctx = nonNilContext(ctx)

	relative, err := validateFileRemotePath(remotePath)
	if err != nil {
		return false, err
	}
	target, _, err := c.endpoint(relative, false)
	if err != nil {
		return false, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
	if err != nil {
		return false, fmt.Errorf("构造 WebDAV 请求失败: %v", err)
	}
	request.Header.Set("User-Agent", userAgent)
	response, err := c.do(request)
	if err != nil {
		return false, err
	}
	drainAndClose(response.Body)

	switch {
	case response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone:
		return false, nil
	case isSuccessStatus(response.StatusCode):
		return true, nil
	case response.StatusCode == http.StatusMethodNotAllowed || response.StatusCode == http.StatusNotImplemented:
		// Fall through to PROPFIND below.
	default:
		return false, statusError("检查远端备份", response.StatusCode)
	}

	propfindRequest, err := http.NewRequestWithContext(ctx, "PROPFIND", target, nil)
	if err != nil {
		return false, fmt.Errorf("构造 WebDAV 请求失败: %v", err)
	}
	propfindRequest.Header.Set("Depth", "0")
	propfindRequest.Header.Set("User-Agent", userAgent)
	propfindResponse, err := c.do(propfindRequest)
	if err != nil {
		return false, err
	}
	drainAndClose(propfindResponse.Body)

	switch {
	case propfindResponse.StatusCode == http.StatusNotFound || propfindResponse.StatusCode == http.StatusGone:
		return false, nil
	case isSuccessStatus(propfindResponse.StatusCode):
		return true, nil
	default:
		return false, statusError("检查远端备份", propfindResponse.StatusCode)
	}
}

// RemoteURL returns the absolute URL a relative backup path resolves to. The
// service layer uses it to show the user where the backup was written.
func (c *Client) RemoteURL(remotePath string) (string, error) {
	target, _, err := c.endpoint(remotePath, false)
	if err != nil {
		return "", err
	}
	return target, nil
}

// ensureCollection creates relCollection and every missing ancestor with MKCOL.
// Servers that answer 405 (already exists / method unsupported) or 409 are
// tolerated because the following PUT reports a real problem with a better
// message anyway.
func (c *Client) ensureCollection(ctx context.Context, relCollection string) error {
	current := ""
	for _, segment := range strings.Split(relCollection, "/") {
		if segment == "" {
			continue
		}
		if current == "" {
			current = segment
		} else {
			current += "/" + segment
		}
		if err := c.mkcol(ctx, current); err != nil {
			return err
		}
	}
	return nil
}

// mkcol sends one MKCOL request for the collection relPath.
func (c *Client) mkcol(ctx context.Context, relPath string) error {
	target, _, err := c.endpoint(relPath, true)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, "MKCOL", target, nil)
	if err != nil {
		return fmt.Errorf("构造 WebDAV 建目录请求失败: %v", err)
	}
	request.Header.Set("User-Agent", userAgent)

	response, err := c.do(request)
	if err != nil {
		return err
	}
	drainAndClose(response.Body)

	switch {
	case isSuccessStatus(response.StatusCode):
		return nil
	case response.StatusCode == http.StatusMethodNotAllowed || response.StatusCode == http.StatusConflict:
		// The directory already exists, which is exactly what we want.
		return nil
	default:
		return statusError("创建远端目录", response.StatusCode)
	}
}

// do sends a request with Basic authentication and wraps transport failures in
// a Chinese message that cannot leak the configured password.
func (c *Client) do(request *http.Request) (*http.Response, error) {
	if c.Username != "" || c.Password != "" {
		request.SetBasicAuth(c.Username, c.Password)
	}
	response, err := c.httpClient().Do(request)
	if err != nil {
		return nil, requestError(err)
	}
	return response, nil
}

// httpClient returns the configured client or a short-lived one using Timeout.
// The default transport is shared, so connection pooling is not lost.
func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &http.Client{Timeout: timeout}
}

// endpoint resolves remotePath against BaseURL. The returned relative path is
// the cleaned, slash-free form used to derive parent collections, and
// trailingSlash forces the conventional collection URL used by MKCOL.
func (c *Client) endpoint(remotePath string, trailingSlash bool) (string, string, error) {
	base, err := c.parsedBaseURL()
	if err != nil {
		return "", "", err
	}
	relative, err := normalizeRemotePath(remotePath)
	if err != nil {
		return "", "", err
	}

	joined := path.Join(base.Path, relative)
	if joined == "" {
		joined = "/"
	}
	if !strings.HasPrefix(joined, "/") {
		joined = "/" + joined
	}
	if trailingSlash && !strings.HasSuffix(joined, "/") {
		joined += "/"
	}

	resolved := *base
	resolved.Path = joined
	resolved.RawPath = ""
	return resolved.String(), relative, nil
}

// parsedBaseURL validates the configured base URL. Only plain http and https
// WebDAV endpoints are accepted.
func (c *Client) parsedBaseURL() (*url.URL, error) {
	raw := strings.TrimSpace(c.BaseURL)
	if raw == "" {
		return nil, errors.New("WebDAV 地址为空，请先配置 WebDAV 地址")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("WebDAV 地址格式无效，请输入完整的 http:// 或 https:// 地址")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("WebDAV 地址必须以 http:// 或 https:// 开头")
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return nil, errors.New("WebDAV 地址缺少主机名")
	}
	parsed.Fragment = ""
	return parsed, nil
}

// validateFileRemotePath normalizes a path that must point at a backup file.
func validateFileRemotePath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if strings.HasSuffix(trimmed, "/") || strings.HasSuffix(trimmed, `\`) {
		return "", errors.New("远端备份路径必须指向文件，不能以斜杠结尾")
	}
	relative, err := normalizeRemotePath(trimmed)
	if err != nil {
		return "", err
	}
	if relative == "" {
		return "", errors.New("远端备份路径不能为空")
	}
	return relative, nil
}

// normalizeRemotePath converts a user supplied WebDAV path into a safe,
// slash-free relative path. Absolute URLs, Windows drive letters and any ".."
// segment are rejected so a backup can never escape the configured collection.
func normalizeRemotePath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if strings.Contains(trimmed, "://") {
		return "", errors.New("远端路径必须是相对路径，不能包含协议前缀")
	}

	// Treat Windows separators like WebDAV separators before validating.
	normalized := strings.ReplaceAll(trimmed, `\`, "/")
	if strings.HasPrefix(normalized, "//") {
		return "", errors.New("远端路径必须是相对路径，不能以 // 开头")
	}
	if strings.ContainsAny(normalized, "\x00\r\n") {
		return "", errors.New("远端路径包含非法字符")
	}
	normalized = strings.Trim(normalized, "/")
	if normalized == "" {
		return "", errors.New("远端路径不能为空或根目录")
	}
	if driveLetterPattern.MatchString(normalized) {
		return "", errors.New("远端路径不能包含盘符，请使用 WebDAV 相对路径")
	}

	segments := strings.Split(normalized, "/")
	cleaned := make([]string, 0, len(segments))
	for _, segment := range segments {
		switch segment {
		case "", ".":
			continue
		case "..":
			return "", errors.New("远端路径包含非法的上级目录（..），已拒绝以免越出 WebDAV 目录")
		default:
			cleaned = append(cleaned, segment)
		}
	}
	if len(cleaned) == 0 {
		return "", errors.New("远端路径不能为空或根目录")
	}
	return strings.Join(cleaned, "/"), nil
}

// parentCollection returns the parent of a normalized relative file path, or
// an empty string when the file lives directly in the base collection.
func parentCollection(relative string) string {
	index := strings.LastIndex(relative, "/")
	if index < 0 {
		return ""
	}
	return relative[:index]
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func isSuccessStatus(status int) bool {
	return status >= 200 && status < 300
}

// statusError builds a Chinese error for a non-2xx WebDAV response. The status
// code is always included so users can search for it and support can identify
// server side problems quickly.
func statusError(action string, status int) error {
	return fmt.Errorf("%s失败：%s（HTTP %d）", action, statusReason(status), status)
}

func statusReason(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "请求格式无效"
	case http.StatusUnauthorized:
		return "认证失败，请检查 WebDAV 用户名和密码"
	case http.StatusForbidden:
		return "没有访问权限，请检查账号权限和远端路径"
	case http.StatusNotFound:
		return "远端资源不存在，请检查 WebDAV 地址和备份路径"
	case http.StatusMethodNotAllowed:
		return "服务器不允许该操作"
	case http.StatusConflict:
		return "远端目录不存在或资源冲突"
	case http.StatusPreconditionFailed:
		return "服务器拒绝了该请求，请检查 WebDAV 配置"
	case http.StatusRequestEntityTooLarge:
		return "文件超过服务器允许的大小"
	case http.StatusLocked:
		return "远端资源被锁定"
	case http.StatusTooManyRequests:
		return "服务器请求过于频繁，请稍后重试"
	case http.StatusInternalServerError:
		return "服务器内部错误"
	case http.StatusNotImplemented, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return "服务器暂时不可用，请稍后重试"
	case http.StatusInsufficientStorage:
		return "远端存储空间不足"
	default:
		return "WebDAV 请求未被服务器接受"
	}
}

// requestError wraps a transport error in a Chinese message. The request URL is
// redacted before being printed so credentials embedded in the base URL can
// never end up in the error shown to the user.
func requestError(err error) error {
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("WebDAV 请求已取消: %w", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("WebDAV 请求超时: %w", err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("WebDAV 请求超时: %w", err)
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("连接 WebDAV 服务器失败（%s %s）: %v", urlErr.Op, redactURL(urlErr.URL), urlErr.Err)
	}
	return fmt.Errorf("连接 WebDAV 服务器失败: %v", err)
}

// redactURL removes user information from a URL before it is logged.
func redactURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if parsed.User != nil {
		parsed.User = url.User("***")
	}
	return parsed.String()
}

// drainAndClose consumes a small amount of the body so the underlying
// connection can be reused, then closes it.
func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 64<<10))
	_ = body.Close()
}
