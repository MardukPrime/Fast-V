package webdav

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// testDAVServer is a deliberately small WebDAV implementation used by the
// tests. It implements exactly the methods the client relies on, validates
// Basic authentication, enforces parent collections for PUT/MKCOL and keeps a
// log of every request so tests can verify the wire protocol.
type testDAVServer struct {
	mu          sync.Mutex
	username    string
	password    string
	requireAuth bool

	files       map[string][]byte
	collections map[string]bool

	methods     []string
	mkcolPaths  []string
	putPaths    []string
	getPaths    []string
	deletePaths []string

	lastPutContentType string
	lastPutBody        []byte
	lastUser           string
	lastPass           string
	authSeen           bool
}

func newTestDAVServer(t *testing.T, username, password string) (*testDAVServer, *httptest.Server) {
	t.Helper()

	dav := &testDAVServer{
		username:    username,
		password:    password,
		requireAuth: true,
		files:       map[string][]byte{},
		collections: map[string]bool{"/": true},
	}
	server := httptest.NewServer(http.HandlerFunc(dav.handle))
	t.Cleanup(server.Close)
	return dav, server
}

func (d *testDAVServer) handle(w http.ResponseWriter, r *http.Request) {
	user, pass, hasAuth := r.BasicAuth()

	d.mu.Lock()
	d.methods = append(d.methods, r.Method+" "+r.URL.Path)
	d.lastUser, d.lastPass, d.authSeen = user, pass, hasAuth
	d.mu.Unlock()

	if d.requireAuth && (!hasAuth || user != d.username || pass != d.password) {
		w.Header().Set("WWW-Authenticate", `Basic realm="fastv-test"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case "MKCOL":
		d.handleMKCOL(w, r)
	case http.MethodPut:
		d.handlePut(w, r)
	case http.MethodGet:
		d.handleGet(w, r)
	case http.MethodHead:
		d.handleHead(w, r)
	case http.MethodDelete:
		d.handleDelete(w, r)
	case "PROPFIND":
		d.handlePropfind(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (d *testDAVServer) handleMKCOL(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	d.mkcolPaths = append(d.mkcolPaths, r.URL.Path)
	trailingSlash := strings.HasSuffix(r.URL.Path, "/")
	exists := d.collections[r.URL.Path]
	parentOK := d.parentExistsLocked(r.URL.Path)
	if !exists && parentOK && trailingSlash {
		d.collections[r.URL.Path] = true
	}
	d.mu.Unlock()

	switch {
	case !trailingSlash:
		w.WriteHeader(http.StatusBadRequest)
	case exists:
		w.WriteHeader(http.StatusMethodNotAllowed)
	case !parentOK:
		w.WriteHeader(http.StatusConflict)
	default:
		w.WriteHeader(http.StatusCreated)
	}
}

func (d *testDAVServer) handlePut(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxUploadSize+1))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	d.mu.Lock()
	parentOK := d.parentExistsLocked(r.URL.Path)
	d.lastPutContentType = r.Header.Get("Content-Type")
	d.lastPutBody = append([]byte(nil), body...)
	if parentOK {
		d.files[r.URL.Path] = append([]byte(nil), body...)
		d.putPaths = append(d.putPaths, r.URL.Path)
	}
	d.mu.Unlock()

	if !parentOK {
		w.WriteHeader(http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (d *testDAVServer) handleGet(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	data, ok := d.files[r.URL.Path]
	d.getPaths = append(d.getPaths, r.URL.Path)
	d.mu.Unlock()

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (d *testDAVServer) handleHead(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	_, ok := d.files[r.URL.Path]
	d.mu.Unlock()

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (d *testDAVServer) handleDelete(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	_, fileOK := d.files[r.URL.Path]
	if fileOK {
		delete(d.files, r.URL.Path)
		d.deletePaths = append(d.deletePaths, r.URL.Path)
	}
	d.mu.Unlock()

	if !fileOK {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *testDAVServer) handlePropfind(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	_, fileOK := d.files[r.URL.Path]
	collectionKey := strings.TrimSuffix(r.URL.Path, "/") + "/"
	collectionOK := d.collections[collectionKey]
	d.mu.Unlock()

	if !fileOK && !collectionOK {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusMultiStatus)
}

func (d *testDAVServer) parentExistsLocked(p string) bool {
	trimmed := strings.TrimSuffix(p, "/")
	parent := path.Dir(trimmed)
	if parent == "/" || parent == "." {
		return true
	}
	return d.collections[parent] || d.collections[parent+"/"]
}

func (d *testDAVServer) methodsLog() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.methods...)
}

func (d *testDAVServer) mkcolLog() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.mkcolPaths...)
}

func (d *testDAVServer) fileAt(remotePath string) ([]byte, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	data, ok := d.files[remotePath]
	return append([]byte(nil), data...), ok
}

func (d *testDAVServer) lastPut() (string, []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastPutContentType, append([]byte(nil), d.lastPutBody...)
}

func (d *testDAVServer) credentialsSeen() (string, string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastUser, d.lastPass, d.authSeen
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestNewTrimsBaseURLAndResolvesRemoteURL(t *testing.T) {
	client := New("  https://dav.example.com/backups/  ", "alice", "secret")
	if client.BaseURL != "https://dav.example.com/backups" {
		t.Fatalf("unexpected base URL: %q", client.BaseURL)
	}

	for _, remotePath := range []string{"/FastV/fastv-backup.json", "FastV/fastv-backup.json", "/FastV//fastv-backup.json"} {
		got, err := client.RemoteURL(remotePath)
		if err != nil {
			t.Fatalf("RemoteURL(%q): %v", remotePath, err)
		}
		want := "https://dav.example.com/backups/FastV/fastv-backup.json"
		if got != want {
			t.Fatalf("RemoteURL(%q) = %q, want %q", remotePath, got, want)
		}
	}

	if _, err := client.RemoteURL("../escape.json"); err == nil || !strings.Contains(err.Error(), "上级目录") {
		t.Fatalf("expected traversal rejection, got %v", err)
	}
}

func TestInvalidBaseURLIsRejected(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		{"空地址", "", "WebDAV 地址"},
		{"不支持的协议", "ftp://dav.example.com", "http:// 或 https://"},
		{"缺少协议", "dav.example.com", "http:// 或 https://"},
		{"缺少主机名", "http://", "WebDAV 地址"},
		{"文件地址", "file:///tmp/dav", "http:// 或 https://"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := New(tc.url, "alice", "secret").Test(context.Background())
			if err == nil {
				t.Fatal("expected an error for an invalid base URL")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestTestUploadsAndDeletesProbe(t *testing.T) {
	dav, server := newTestDAVServer(t, "alice", "secret")
	client := New(server.URL+"/", "alice", "secret")

	if err := client.Test(context.Background()); err != nil {
		t.Fatalf("Test: %v", err)
	}

	if _, ok := dav.fileAt("/.fastv-probe"); ok {
		t.Fatal("probe file was not removed after a successful test")
	}
	if wants := []string{"PUT /.fastv-probe", "DELETE /.fastv-probe"}; !equalStrings(dav.methodsLog(), wants) {
		t.Fatalf("unexpected request sequence: %v", dav.methodsLog())
	}
	if len(dav.mkcolLog()) != 0 {
		t.Fatalf("Test should not create collections, got MKCOL %v", dav.mkcolLog())
	}
	if contentType, _ := dav.lastPut(); contentType != probeContentType {
		t.Fatalf("probe content type = %q, want %q", contentType, probeContentType)
	}
	if user, pass, ok := dav.credentialsSeen(); !ok || user != "alice" || pass != "secret" {
		t.Fatalf("Basic auth was not sent correctly: user=%q pass=%q ok=%v", user, pass, ok)
	}
}

func TestTestPathCreatesConfiguredDirectory(t *testing.T) {
	dav, server := newTestDAVServer(t, "alice", "secret")
	client := New(server.URL, "alice", "secret")

	if err := client.TestPath(context.Background(), "/FastV/fastv-backup.json"); err != nil {
		t.Fatalf("TestPath: %v", err)
	}

	if wants := []string{"/FastV/"}; !equalStrings(dav.mkcolLog(), wants) {
		t.Fatalf("MKCOL log = %v, want %v", dav.mkcolLog(), wants)
	}
	if wants := []string{"MKCOL /FastV/", "PUT /FastV/.fastv-probe", "DELETE /FastV/.fastv-probe"}; !equalStrings(dav.methodsLog(), wants) {
		t.Fatalf("unexpected request sequence: %v", dav.methodsLog())
	}
	if _, ok := dav.fileAt("/FastV/.fastv-probe"); ok {
		t.Fatal("probe file was not removed after a successful test")
	}
}

func TestTestReportsAuthenticationFailureWithoutLeakingPassword(t *testing.T) {
	_, server := newTestDAVServer(t, "alice", "s3cret-server-password")
	client := New(server.URL, "alice", "wrong-client-password")

	err := client.Test(context.Background())
	if err == nil {
		t.Fatal("expected 401 to fail the connection test")
	}
	message := err.Error()
	for _, want := range []string{"认证", "401"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error %q does not contain %q", message, want)
		}
	}
	for _, secret := range []string{"s3cret-server-password", "wrong-client-password"} {
		if strings.Contains(message, secret) {
			t.Fatalf("error leaks a password: %q", message)
		}
	}
}

func TestTestReportsForbiddenWithStatusCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	err := New(server.URL, "alice", "secret").Test(context.Background())
	if err == nil {
		t.Fatal("expected 403 to fail the connection test")
	}
	for _, want := range []string{"权限", "403"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err.Error(), want)
		}
	}
}

func TestStatusErrorsAreChineseAndIncludeStatusCode(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{http.StatusBadRequest, "请求格式无效"},
		{http.StatusUnauthorized, "认证失败"},
		{http.StatusForbidden, "没有访问权限"},
		{http.StatusNotFound, "远端资源不存在"},
		{http.StatusConflict, "冲突"},
		{http.StatusRequestEntityTooLarge, "超过服务器允许的大小"},
		{http.StatusLocked, "锁定"},
		{http.StatusTooManyRequests, "频繁"},
		{http.StatusInternalServerError, "内部错误"},
		{http.StatusServiceUnavailable, "暂时不可用"},
		{http.StatusInsufficientStorage, "空间不足"},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			status := tc.status
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			t.Cleanup(server.Close)

			err := New(server.URL, "alice", "secret").Test(context.Background())
			if err == nil {
				t.Fatal("expected a status error")
			}
			message := err.Error()
			if !strings.Contains(message, tc.want) {
				t.Fatalf("error %q does not contain %q", message, tc.want)
			}
			if !strings.Contains(message, strconv.Itoa(status)) {
				t.Fatalf("error %q does not contain status %d", message, status)
			}
		})
	}
}

func TestUploadCreatesParentCollectionsAndUploadsJSON(t *testing.T) {
	dav, server := newTestDAVServer(t, "alice", "secret")
	client := New(server.URL, "alice", "secret")
	payload := []byte("{\n  \"version\": 1,\n  \"settings\": {}\n}")

	if err := client.Upload(context.Background(), "/FastV/sub/fastv-backup.json", payload); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	if wants := []string{"/FastV/", "/FastV/sub/"}; !equalStrings(dav.mkcolLog(), wants) {
		t.Fatalf("MKCOL log = %v, want %v", dav.mkcolLog(), wants)
	}
	if contentType, body := dav.lastPut(); contentType != backupContentType || !bytes.Equal(body, payload) {
		t.Fatalf("PUT content type = %q, body equal = %v", contentType, bytes.Equal(body, payload))
	}
	stored, ok := dav.fileAt("/FastV/sub/fastv-backup.json")
	if !ok || !bytes.Equal(stored, payload) {
		t.Fatalf("uploaded file not stored correctly: ok=%v", ok)
	}

	// A second upload hits existing collections: the server answers 405 to
	// MKCOL, which must be tolerated.
	if err := client.Upload(context.Background(), "/FastV/sub/fastv-backup.json", payload); err != nil {
		t.Fatalf("second upload should tolerate existing collections: %v", err)
	}
}

func TestUploadRejectsUnsafeOrInvalidPaths(t *testing.T) {
	dav, server := newTestDAVServer(t, "alice", "secret")
	client := New(server.URL, "alice", "secret")

	cases := []struct {
		name string
		path string
		want string
	}{
		{"上级目录", "../outside.json", "上级目录"},
		{"中间上级目录", "FastV/../../outside.json", "上级目录"},
		{"绝对 URL", "https://evil.example/backup.json", "相对路径"},
		{"协议相对地址", "//evil.example/backup.json", "相对路径"},
		{"Windows 盘符", `C:\Windows\backup.json`, "盘符"},
		{"空路径", "", "不能为空"},
		{"目录路径", "/FastV/", "文件"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := client.Upload(context.Background(), tc.path, []byte("{}"))
			if err == nil {
				t.Fatalf("expected Upload(%q) to fail", tc.path)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
			if requests := dav.methodsLog(); len(requests) != 0 {
				t.Fatalf("unsafe path reached the server: %v", requests)
			}
		})
	}
}

func TestDownloadReturnsNotFoundMessage(t *testing.T) {
	_, server := newTestDAVServer(t, "alice", "secret")
	client := New(server.URL, "alice", "secret")

	_, err := client.Download(context.Background(), "/FastV/missing.json")
	if err == nil {
		t.Fatal("expected a missing file to fail the download")
	}
	for _, want := range []string{"远端没有备份文件", "404"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err.Error(), want)
		}
	}
}

func TestDownloadRejectsOversizedBackup(t *testing.T) {
	oversized := bytes.Repeat([]byte("a"), int(MaxDownloadSize)+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(oversized)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(oversized)
	}))
	t.Cleanup(server.Close)

	_, err := New(server.URL, "alice", "secret").Download(context.Background(), "/huge.json")
	if err == nil {
		t.Fatal("expected an oversized download to fail")
	}
	if !strings.Contains(err.Error(), "过大") {
		t.Fatalf("error %q does not mention the size limit", err.Error())
	}
}

func TestDownloadRejectsUnsafePaths(t *testing.T) {
	dav, server := newTestDAVServer(t, "alice", "secret")
	client := New(server.URL, "alice", "secret")

	for _, unsafe := range []string{"../secret.json", `C:\backup.json`, "https://evil.example/x.json"} {
		if _, err := client.Download(context.Background(), unsafe); err == nil {
			t.Fatalf("expected Download(%q) to fail", unsafe)
		}
	}
	if requests := dav.methodsLog(); len(requests) != 0 {
		t.Fatalf("unsafe path reached the server: %v", requests)
	}
}

func TestUploadDownloadRoundTripAndExists(t *testing.T) {
	dav, server := newTestDAVServer(t, "alice", "secret")
	client := New(server.URL, "alice", "secret")
	payload := []byte(`{"version":1}`)

	if exists, err := client.Exists(context.Background(), "/FastV/fastv-backup.json"); err != nil || exists {
		t.Fatalf("Exists before upload = %v, %v", exists, err)
	}
	if err := client.Upload(context.Background(), "/FastV/fastv-backup.json", payload); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if exists, err := client.Exists(context.Background(), "/FastV/fastv-backup.json"); err != nil || !exists {
		t.Fatalf("Exists after upload = %v, %v", exists, err)
	}
	got, err := client.Download(context.Background(), "/FastV/fastv-backup.json")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("downloaded payload differs: %q", got)
	}
	if _, ok := dav.fileAt("/FastV/fastv-backup.json"); !ok {
		t.Fatal("server does not hold the uploaded file")
	}
}

func TestExistsFallsBackToPropfind(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.WriteHeader(http.StatusMethodNotAllowed)
		case "PROPFIND":
			if r.Header.Get("Depth") != "0" {
				t.Errorf("PROPFIND Depth = %q, want 0", r.Header.Get("Depth"))
			}
			w.WriteHeader(http.StatusMultiStatus)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	exists, err := New(server.URL, "alice", "secret").Exists(context.Background(), "/FastV/fastv-backup.json")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Fatal("expected PROPFIND fallback to report the file as existing")
	}
}

func TestAnonymousServerWithoutCredentials(t *testing.T) {
	dav, server := newTestDAVServer(t, "", "")
	dav.mu.Lock()
	dav.requireAuth = false
	dav.mu.Unlock()

	if err := New(server.URL, "", "").Test(context.Background()); err != nil {
		t.Fatalf("anonymous Test: %v", err)
	}
	if _, _, hasAuth := dav.credentialsSeen(); hasAuth {
		t.Fatal("no Authorization header should be sent when no credentials are configured")
	}
}

func TestNetworkErrorIsWrappedInChinese(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	serverURL := server.URL
	server.Close()

	err := New(serverURL, "alice", "n3ver-leak-me").Test(context.Background())
	if err == nil {
		t.Fatal("expected a connection error")
	}
	if !strings.Contains(err.Error(), "连接 WebDAV 服务器失败") {
		t.Fatalf("error %q is not a wrapped Chinese network error", err.Error())
	}
	if strings.Contains(err.Error(), "n3ver-leak-me") {
		t.Fatalf("error leaks the password: %q", err.Error())
	}
}

func TestCredentialsInBaseURLAreRedacted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	serverURL := server.URL
	server.Close()

	withCredentials := strings.Replace(serverURL, "http://", "http://user:topsecret@", 1)
	err := New(withCredentials, "", "").Test(context.Background())
	if err == nil {
		t.Fatal("expected a connection error")
	}
	if strings.Contains(err.Error(), "topsecret") {
		t.Fatalf("error leaks the base URL password: %q", err.Error())
	}
}

func TestCanceledContextIsReported(t *testing.T) {
	_, server := newTestDAVServer(t, "alice", "secret")
	client := New(server.URL, "alice", "secret")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := client.Test(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
