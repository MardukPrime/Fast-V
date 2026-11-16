package update

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/MardukPrime/Fast-V/internal/model"
)

// zipEntry describes one file entry in a generated test archive.
type zipEntry struct {
	name    string
	content string
	symlink bool
}

// buildZip renders an in-memory zip archive.
func buildZip(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		if entry.symlink {
			header := &zip.FileHeader{Name: entry.name, Method: zip.Store}
			header.SetMode(os.ModeSymlink | 0o777)
			file, err := writer.CreateHeader(header)
			if err != nil {
				t.Fatalf("create symlink entry %q: %v", entry.name, err)
			}
			if _, err := file.Write([]byte(entry.content)); err != nil {
				t.Fatalf("write symlink entry %q: %v", entry.name, err)
			}
			continue
		}
		file, err := writer.Create(entry.name)
		if err != nil {
			t.Fatalf("create zip entry %q: %v", entry.name, err)
		}
		if _, err := file.Write([]byte(entry.content)); err != nil {
			t.Fatalf("write zip entry %q: %v", entry.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	return buffer.Bytes()
}

// newAssetServer serves body for every request.
func newAssetServer(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server
}

// testExecutableBase returns the file name of the running test binary.
func testExecutableBase(t *testing.T) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable(): %v", err)
	}
	return filepath.Base(executable)
}

// testUpdateInfo builds a complete UpdateInfo for an asset served by server.
func testUpdateInfo(server *httptest.Server, assetName string, size int64) model.UpdateInfo {
	return model.UpdateInfo{
		LatestVersion: "0.3.0",
		AssetName:     assetName,
		AssetURL:      server.URL + "/" + filepath.Base(assetName),
		AssetSize:     size,
	}
}

func TestUpdatePackageLimits(t *testing.T) {
	if maxDownloadSize != 300<<20 {
		t.Errorf("maxDownloadSize = %d, want %d (300 MB)", maxDownloadSize, int64(300<<20))
	}
	if maxExtractSize != 600<<20 {
		t.Errorf("maxExtractSize = %d, want %d (600 MB)", maxExtractSize, int64(600<<20))
	}
}

func TestDownloadAndExtractZipPrefersRunningExecutable(t *testing.T) {
	originalName := testExecutableBase(t)
	archive := buildZip(t, []zipEntry{
		{name: "docs/readme.txt", content: "FastV readme"},
		{name: "payload/aaa-first-alphabetically.exe", content: "not-the-one"},
		{name: "payload/" + originalName, content: "preferred-binary"},
	})
	server := newAssetServer(t, archive)
	destDir := t.TempDir()

	exePath, err := DownloadAndExtract(context.Background(), server.Client(), testUpdateInfo(server, "FastV-windows-amd64.zip", int64(len(archive))), destDir)
	if err != nil {
		t.Fatalf("DownloadAndExtract() error = %v, want nil", err)
	}
	if !filepath.IsAbs(exePath) {
		t.Errorf("executable path %q is not absolute", exePath)
	}
	if !strings.EqualFold(filepath.Base(exePath), originalName) {
		t.Errorf("executable base = %q, want the running executable name %q", filepath.Base(exePath), originalName)
	}
	content, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatalf("read extracted executable: %v", err)
	}
	if string(content) != "preferred-binary" {
		t.Errorf("extracted executable content = %q, want preferred-binary", content)
	}
	readme := filepath.Join(destDir, archiveDirName, "docs", "readme.txt")
	if data, err := os.ReadFile(readme); err != nil || string(data) != "FastV readme" {
		t.Errorf("nested extraction failed: data = %q, err = %v", data, err)
	}
	archivePath := filepath.Join(destDir, "FastV-windows-amd64.zip")
	if stat, err := os.Stat(archivePath); err != nil || stat.Size() != int64(len(archive)) {
		t.Errorf("downloaded archive stat = %v, err = %v", stat, err)
	}
}

func TestDownloadAndExtractPrefersProductExecutable(t *testing.T) {
	originalName := testExecutableBase(t)
	archive := buildZip(t, []zipEntry{
		{name: "Zzz-first.exe", content: "wrong"},
		{name: "FastV.exe", content: "product-binary"},
	})
	// Guard: the product executable is only the fallback when the running
	// executable name is not part of the archive.
	if strings.EqualFold(originalName, "FastV.exe") {
		t.Skip("test binary is FastV.exe; fallback ordering is not observable")
	}
	server := newAssetServer(t, archive)
	destDir := t.TempDir()

	exePath, err := DownloadAndExtract(context.Background(), server.Client(), testUpdateInfo(server, "FastV-windows.zip", int64(len(archive))), destDir)
	if err != nil {
		t.Fatalf("DownloadAndExtract() error = %v, want nil", err)
	}
	if base := filepath.Base(exePath); !strings.EqualFold(base, "FastV.exe") {
		t.Errorf("executable base = %q, want FastV.exe", base)
	}
}

func TestDownloadAndExtractExecutablePackage(t *testing.T) {
	binary := []byte("MZ fake FastV executable")
	server := newAssetServer(t, binary)
	destDir := t.TempDir()

	exePath, err := DownloadAndExtract(context.Background(), server.Client(), testUpdateInfo(server, "FastV.exe", int64(len(binary))), destDir)
	if err != nil {
		t.Fatalf("DownloadAndExtract() error = %v, want nil", err)
	}
	want := filepath.Join(destDir, archiveDirName, "FastV.exe")
	if filepath.Clean(exePath) != filepath.Clean(want) {
		t.Errorf("executable path = %q, want %q", exePath, want)
	}
	content, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatalf("read copied executable: %v", err)
	}
	if !bytes.Equal(content, binary) {
		t.Errorf("copied executable content = %q, want %q", content, binary)
	}
}

func TestDownloadAndExtractValidationErrors(t *testing.T) {
	unsupported := model.UpdateInfo{AssetName: "FastV.msi", AssetURL: "http://127.0.0.1:1/FastV.msi"}
	noURL := model.UpdateInfo{AssetName: "FastV-windows.zip"}
	unsafeName := model.UpdateInfo{AssetName: "../evil.zip", AssetURL: "http://127.0.0.1:1/evil.zip"}
	tooLarge := model.UpdateInfo{AssetName: "FastV-windows.zip", AssetURL: "http://127.0.0.1:1/huge.zip", AssetSize: maxDownloadSize + 1}
	unsafeNewline := model.UpdateInfo{AssetName: "FastV\n.zip", AssetURL: "http://127.0.0.1:1/x.zip"}

	cases := []struct {
		name    string
		info    model.UpdateInfo
		destDir string
		want    string
	}{
		{name: "unsupported format", info: unsupported, destDir: t.TempDir(), want: "不支持"},
		{name: "missing url", info: noURL, destDir: t.TempDir(), want: "下载地址为空"},
		{name: "unsafe asset name", info: unsafeName, destDir: t.TempDir(), want: "文件名非法"},
		{name: "asset too large", info: tooLarge, destDir: t.TempDir(), want: "过大"},
		{name: "empty dest dir", info: noURL, destDir: " ", want: "目标目录为空"},
		{name: "newline asset name", info: unsafeNewline, destDir: t.TempDir(), want: "文件名非法"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := DownloadAndExtract(context.Background(), nil, testCase.info, testCase.destDir)
			if err == nil {
				t.Fatal("DownloadAndExtract() error = nil, want a validation error")
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Errorf("DownloadAndExtract() error = %q, want it to contain %q", err, testCase.want)
			}
		})
	}
}

func TestDownloadAndExtractRejectsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	info := model.UpdateInfo{AssetName: "FastV-windows.zip", AssetURL: server.URL + "/missing.zip"}
	_, err := DownloadAndExtract(context.Background(), server.Client(), info, t.TempDir())
	if err == nil {
		t.Fatal("DownloadAndExtract() error = nil, want an HTTP error")
	}
	if !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("error = %q, want it to mention HTTP 404", err)
	}
}

func TestDownloadAndExtractRejectsEmptyPackage(t *testing.T) {
	server := newAssetServer(t, nil)
	destDir := t.TempDir()
	_, err := DownloadAndExtract(context.Background(), server.Client(), testUpdateInfo(server, "FastV-windows.zip", 0), destDir)
	if err == nil {
		t.Fatal("DownloadAndExtract() error = nil, want an empty-package error")
	}
	if !strings.Contains(err.Error(), "为空") {
		t.Errorf("error = %q, want it to report an empty package", err)
	}
}

func TestDownloadAndExtractRejectsOversizedDownload(t *testing.T) {
	previous := maxDownloadSize
	maxDownloadSize = 64
	t.Cleanup(func() { maxDownloadSize = previous })

	body := []byte(strings.Repeat("x", 100))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Flush between writes forces a chunked response without a
		// Content-Length, exercising the streaming limit instead of the
		// declared-size shortcut.
		_, _ = w.Write(body[:32])
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = w.Write(body[32:])
	}))
	t.Cleanup(server.Close)

	destDir := t.TempDir()
	info := testUpdateInfo(server, "FastV-windows.zip", 0)
	_, err := DownloadAndExtract(context.Background(), server.Client(), info, destDir)
	if err == nil {
		t.Fatal("DownloadAndExtract() error = nil, want a size-limit error")
	}
	if !strings.Contains(err.Error(), "过大") {
		t.Errorf("error = %q, want it to report a package that is too large", err)
	}
	if _, statErr := os.Stat(filepath.Join(destDir, "FastV-windows.zip")); !os.IsNotExist(statErr) {
		t.Errorf("oversized partial download was not removed (stat err = %v)", statErr)
	}
}

func TestDownloadAndExtractRejectsPathTraversal(t *testing.T) {
	names := []string{
		"../evil.txt",
		"..\\evil.txt",
		"sub/../../evil.txt",
		"/evil.txt",
		"C:\\evil.txt",
		"C:/evil.txt",
		"\\\\server\\share\\evil.txt",
		"a/b/../../../evil.txt",
		"evil.txt:stream",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			destDir := filepath.Join(root, "updates", "0.3.0")
			archive := buildZip(t, []zipEntry{
				{name: "readme.txt", content: "ok"},
				{name: name, content: "malicious"},
			})
			server := newAssetServer(t, archive)

			_, err := DownloadAndExtract(context.Background(), server.Client(), testUpdateInfo(server, "FastV-windows.zip", int64(len(archive))), destDir)
			if err == nil {
				t.Fatal("DownloadAndExtract() error = nil, want a path traversal rejection")
			}
			if !strings.Contains(err.Error(), "非法") {
				t.Errorf("error = %q, want a Chinese path rejection message", err)
			}
			// Only the downloaded archive may exist: nothing from the hostile
			// entry may be written anywhere below root.
			archivePath := filepath.Join(destDir, "FastV-windows.zip")
			_ = filepath.WalkDir(root, func(current string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil || entry.IsDir() {
					return nil
				}
				if filepath.Clean(current) != filepath.Clean(archivePath) {
					t.Errorf("unexpected file written by a hostile archive: %s", current)
				}
				return nil
			})
		})
	}
}

func TestDownloadAndExtractRejectsReservedNames(t *testing.T) {
	for _, name := range []string{"CON", "nul.txt", "aux"} {
		t.Run(name, func(t *testing.T) {
			archive := buildZip(t, []zipEntry{{name: name, content: "device"}})
			server := newAssetServer(t, archive)
			_, err := DownloadAndExtract(context.Background(), server.Client(), testUpdateInfo(server, "FastV-windows.zip", int64(len(archive))), t.TempDir())
			if err == nil {
				t.Fatal("DownloadAndExtract() error = nil, want a reserved-name rejection")
			}
			if !strings.Contains(err.Error(), "非法") {
				t.Errorf("error = %q, want a Chinese path rejection message", err)
			}
		})
	}
}

func TestDownloadAndExtractRejectsSymlinkEntry(t *testing.T) {
	archive := buildZip(t, []zipEntry{
		{name: "link.exe", content: "C:\\Windows\\System32\\cmd.exe", symlink: true},
	})
	server := newAssetServer(t, archive)
	_, err := DownloadAndExtract(context.Background(), server.Client(), testUpdateInfo(server, "FastV-windows.zip", int64(len(archive))), t.TempDir())
	if err == nil {
		t.Fatal("DownloadAndExtract() error = nil, want a symlink rejection")
	}
	if !strings.Contains(err.Error(), "符号链接") {
		t.Errorf("error = %q, want it to mention symlinks", err)
	}
}

func TestDownloadAndExtractRejectsExtractionSizeLimit(t *testing.T) {
	previous := maxExtractSize
	maxExtractSize = 32
	t.Cleanup(func() { maxExtractSize = previous })

	archive := buildZip(t, []zipEntry{
		{name: "first.txt", content: strings.Repeat("a", 20)},
		{name: "second.txt", content: strings.Repeat("b", 20)},
	})
	server := newAssetServer(t, archive)
	destDir := t.TempDir()
	_, err := DownloadAndExtract(context.Background(), server.Client(), testUpdateInfo(server, "FastV-windows.zip", int64(len(archive))), destDir)
	if err == nil {
		t.Fatal("DownloadAndExtract() error = nil, want an extraction size-limit error")
	}
	if !strings.Contains(err.Error(), "上限") {
		t.Errorf("error = %q, want it to mention the extraction size limit", err)
	}
}

func TestDownloadAndExtractRequiresExecutable(t *testing.T) {
	archive := buildZip(t, []zipEntry{{name: "readme.txt", content: "no binary here"}})
	server := newAssetServer(t, archive)
	_, err := DownloadAndExtract(context.Background(), server.Client(), testUpdateInfo(server, "FastV-windows.zip", int64(len(archive))), t.TempDir())
	if err == nil {
		t.Fatal("DownloadAndExtract() error = nil, want a missing-executable error")
	}
	if !strings.Contains(err.Error(), "没有找到可执行文件") {
		t.Errorf("error = %q, want it to report the missing executable", err)
	}
}

func TestDownloadAndExtractContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	info := model.UpdateInfo{AssetName: "FastV-windows.zip", AssetURL: server.URL + "/pkg.zip"}
	if _, err := DownloadAndExtract(ctx, server.Client(), info, t.TempDir()); err == nil {
		t.Fatal("DownloadAndExtract() error = nil, want a context cancellation error")
	}
}

func TestAssetFileName(t *testing.T) {
	cases := []struct {
		name    string
		asset   string
		url     string
		want    string
		wantErr bool
	}{
		{name: "plain", asset: "FastV-windows-amd64.zip", url: "https://example.com/x", want: "FastV-windows-amd64.zip"},
		{name: "derived from url", url: "https://example.com/dl/FastV.zip", want: "FastV.zip"},
		{name: "trimmed", asset: "  FastV.exe ", url: "https://example.com/x", want: "FastV.exe"},
		{name: "traversal", asset: "../evil.zip", url: "https://example.com/x", wantErr: true},
		{name: "backslash traversal", asset: "..\\evil.zip", url: "https://example.com/x", wantErr: true},
		{name: "drive letter", asset: "C:\\evil.zip", url: "https://example.com/x", wantErr: true},
		{name: "subdirectory", asset: "sub/evil.zip", url: "https://example.com/x", wantErr: true},
		{name: "empty url path", asset: "", url: "https://example.com/", wantErr: true},
		{name: "empty both", asset: " ", url: "", wantErr: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := assetFileName(testCase.asset, testCase.url)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("assetFileName(%q, %q) error = nil, want an error", testCase.asset, testCase.url)
				}
				return
			}
			if err != nil {
				t.Fatalf("assetFileName(%q, %q) error = %v, want nil", testCase.asset, testCase.url, err)
			}
			if got != testCase.want {
				t.Errorf("assetFileName(%q, %q) = %q, want %q", testCase.asset, testCase.url, got, testCase.want)
			}
		})
	}
}

func TestCommanderScriptContent(t *testing.T) {
	currentExe := `C:\Program Files\FastV\FastV.exe`
	newExe := `C:\Users\me\AppData\Local\FastV\updates\0.3.0\extracted\FastV.exe`
	script, err := CommanderScript(currentExe, newExe, 4321)
	if err != nil {
		t.Fatalf("CommanderScript() error = %v, want nil", err)
	}

	quotedCurrent := `"` + currentExe + `"`
	quotedNew := `"` + newExe + `"`
	expectations := []string{
		`set "FASTV_PID=4321"`,
		"tasklist",
		"findstr",
		"GEQ 120",
		"copy /Y " + quotedNew + " " + quotedCurrent,
		`start "" ` + quotedCurrent,
		`del "%~f0"`,
	}
	for _, want := range expectations {
		if !strings.Contains(script, want) {
			t.Errorf("script does not contain %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "{{") {
		t.Errorf("script still contains template placeholders:\n%s", script)
	}
	if !strings.HasSuffix(script, "\n") {
		t.Error("script should end with a newline")
	}
}

func TestCommanderScriptRejectsUnsafePaths(t *testing.T) {
	cases := []struct {
		name       string
		currentExe string
		newExe     string
		pid        int
		want       string
	}{
		{name: "quote in current path", currentExe: `C:\bad"name\FastV.exe`, newExe: `C:\new\FastV.exe`, pid: 1, want: "非法字符"},
		{name: "quote in new path", currentExe: `C:\FastV.exe`, newExe: `C:\bad"name\FastV.exe`, pid: 1, want: "非法字符"},
		{name: "newline in current path", currentExe: "C:\\bad\nname\\FastV.exe", newExe: `C:\new\FastV.exe`, pid: 1, want: "非法字符"},
		{name: "carriage return", currentExe: "C:\\bad\rname\\FastV.exe", newExe: `C:\new\FastV.exe`, pid: 1, want: "非法字符"},
		{name: "percent in current path", currentExe: `C:\%TEMP%\FastV.exe`, newExe: `C:\new\FastV.exe`, pid: 1, want: "非法字符"},
		{name: "percent in new path", currentExe: `C:\FastV.exe`, newExe: `C:\%TEMP%\FastV.exe`, pid: 1, want: "非法字符"},
		{name: "empty current", currentExe: "  ", newExe: `C:\new\FastV.exe`, pid: 1, want: "为空"},
		{name: "empty new", currentExe: `C:\FastV.exe`, newExe: "", pid: 1, want: "为空"},
		{name: "zero pid", currentExe: `C:\FastV.exe`, newExe: `C:\new\FastV.exe`, pid: 0, want: "PID"},
		{name: "negative pid", currentExe: `C:\FastV.exe`, newExe: `C:\new\FastV.exe`, pid: -5, want: "PID"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			script, err := CommanderScript(testCase.currentExe, testCase.newExe, testCase.pid)
			if err == nil {
				t.Fatalf("CommanderScript() error = nil, want an error; script:\n%s", script)
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Errorf("error = %q, want it to contain %q", err, testCase.want)
			}
		})
	}
}

func TestWriteReplaceScript(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "nested", "fastv-update.cmd")
	currentExe := `C:\Apps\FastV\FastV.exe`
	newExe := `C:\Updates\FastV.exe`
	if err := WriteReplaceScript(currentExe, newExe, scriptPath); err != nil {
		t.Fatalf("WriteReplaceScript() error = %v, want nil", err)
	}
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read generated script: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, `"`+currentExe+`"`) {
		t.Errorf("script does not quote the current executable:\n%s", text)
	}
	if !strings.Contains(text, `"`+newExe+`"`) {
		t.Errorf("script does not quote the new executable:\n%s", text)
	}
	if !strings.Contains(text, strconv.Itoa(os.Getpid())) {
		t.Errorf("script does not contain the current PID %d:\n%s", os.Getpid(), text)
	}
}

func TestWriteReplaceScriptRejectsUnsafePath(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "fastv-update.cmd")
	err := WriteReplaceScript(`C:\bad"%\FastV.exe`, `C:\new\FastV.exe`, scriptPath)
	if err == nil {
		t.Fatal("WriteReplaceScript() error = nil, want a path validation error")
	}
	if _, statErr := os.Stat(scriptPath); !os.IsNotExist(statErr) {
		t.Errorf("invalid script was still written (stat err = %v)", statErr)
	}
}

func TestNewReplaceScriptPath(t *testing.T) {
	first, err := NewReplaceScriptPath()
	if err != nil {
		t.Fatalf("NewReplaceScriptPath() error = %v, want nil", err)
	}
	second, err := NewReplaceScriptPath()
	if err != nil {
		t.Fatalf("NewReplaceScriptPath() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(first)
		_ = os.Remove(second)
	})
	if filepath.Ext(first) != ".cmd" {
		t.Errorf("script path %q does not end in .cmd", first)
	}
	if !strings.EqualFold(filepath.Dir(first), filepath.Clean(os.TempDir())) {
		t.Errorf("script directory = %q, want the temp directory %q", filepath.Dir(first), os.TempDir())
	}
	if first == second {
		t.Error("NewReplaceScriptPath() returned the same path twice")
	}
}

func TestStartReplaceScriptRejectsEmptyPath(t *testing.T) {
	if err := StartReplaceScript(" "); err == nil {
		t.Fatal("StartReplaceScript(\" \") error = nil, want a validation error")
	}
}
