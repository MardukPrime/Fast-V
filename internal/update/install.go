package update

// The installer half of the self-update flow: downloading a release asset,
// extracting it into the data directory and generating the batch script that
// replaces the running executable after FastV exits.
//
// Everything here is deliberately conservative:
//   - downloads and extracted contents are size-limited;
//   - archive entry names are validated before anything is written, so a
//     malicious release cannot escape the extraction directory (zip slip);
//   - the replacement script only ever receives quoted paths and refuses paths
//     that could break out of a cmd.exe argument.

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/MardukPrime/Fast-V/internal/buildinfo"
	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/platform"
)

// Update package limits. They are variables rather than constants so tests can
// lower them; the defaults are part of the public behavior.
var (
	// maxDownloadSize bounds one downloaded update package (300 MB).
	maxDownloadSize int64 = 300 << 20
	// maxExtractSize bounds the total uncompressed size of one update archive
	// (600 MB).
	maxExtractSize int64 = 600 << 20
)

// archiveDirName is the sub-directory of the destination directory the update
// package is extracted into.
const archiveDirName = "extracted"

// DownloadAndExtract downloads the update package referenced by info.AssetURL
// into destDir, extracts it and returns the absolute path of the executable to
// install.
//
// The caller controls cancellation and timeouts through ctx. A zip package is
// extracted to destDir/extracted; an executable package is copied there
// verbatim. The executable matching the running application name is preferred,
// otherwise the first .exe found in the extraction directory is returned.
func DownloadAndExtract(ctx context.Context, client *http.Client, info model.UpdateInfo, destDir string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	destDir = strings.TrimSpace(destDir)
	if destDir == "" {
		return "", errors.New("更新包目标目录为空")
	}
	assetURL := strings.TrimSpace(info.AssetURL)
	if assetURL == "" {
		return "", errors.New("更新包下载地址为空")
	}
	if info.AssetSize > maxDownloadSize {
		return "", packageTooLargeError()
	}

	fileName, err := assetFileName(info.AssetName, assetURL)
	if err != nil {
		return "", err
	}
	kind := packageKind(fileName)
	if kind == "" {
		return "", fmt.Errorf("不支持的更新包格式（仅支持 .zip 或 .exe）: %s", fileName)
	}

	absoluteDest, err := filepath.Abs(destDir)
	if err != nil {
		return "", fmt.Errorf("解析更新包目标目录失败: %w", err)
	}
	if err := os.MkdirAll(absoluteDest, 0o755); err != nil {
		return "", fmt.Errorf("创建更新包目录失败: %w", err)
	}

	archivePath := filepath.Join(absoluteDest, fileName)
	if err := downloadFile(ctx, client, assetURL, archivePath); err != nil {
		return "", err
	}

	extractedDir := filepath.Join(absoluteDest, archiveDirName)
	if err := os.RemoveAll(extractedDir); err != nil {
		return "", fmt.Errorf("清理旧解压目录失败: %w", err)
	}
	if err := os.MkdirAll(extractedDir, 0o755); err != nil {
		return "", fmt.Errorf("创建解压目录失败: %w", err)
	}

	switch kind {
	case packageKindZip:
		if err := extractZip(archivePath, extractedDir); err != nil {
			_ = os.RemoveAll(extractedDir)
			return "", err
		}
	case packageKindExe:
		if err := copyFile(archivePath, filepath.Join(extractedDir, fileName)); err != nil {
			_ = os.RemoveAll(extractedDir)
			return "", err
		}
	}

	executablePath, err := findExecutable(extractedDir)
	if err != nil {
		return "", err
	}
	return executablePath, nil
}

// Package kinds recognised by DownloadAndExtract.
const (
	packageKindZip = "zip"
	packageKindExe = "exe"
)

// packageKind classifies an update package by its file name extension.
func packageKind(fileName string) string {
	lower := strings.ToLower(strings.TrimSpace(fileName))
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return packageKindZip
	case strings.HasSuffix(lower, ".exe"):
		return packageKindExe
	default:
		return ""
	}
}

// assetFileName returns a safe local file name for a release asset. The name
// comes from a remote document, so it must not be able to escape destDir.
func assetFileName(assetName, assetURL string) (string, error) {
	name := strings.TrimSpace(assetName)
	if name == "" {
		parsed, err := url.Parse(assetURL)
		if err != nil {
			return "", fmt.Errorf("解析更新包下载地址失败: %w", err)
		}
		name = path.Base(parsed.Path)
	}
	if name == "" || name == "." || name == "/" {
		return "", errors.New("更新包文件名为空")
	}
	normalized := filepath.FromSlash(strings.ReplaceAll(name, "\\", "/"))
	if !filepath.IsLocal(normalized) || strings.ContainsAny(normalized, ":") ||
		strings.ContainsFunc(normalized, isControlRune) || filepath.Base(normalized) != normalized {
		return "", fmt.Errorf("更新包文件名非法: %q", assetName)
	}
	return normalized, nil
}

// isControlRune reports whether r is an ASCII control character or DEL. Release
// documents are remote input; control characters in file names are never valid
// here and make logs and error messages ambiguous.
func isControlRune(r rune) bool {
	return r < 0x20 || r == 0x7f
}

// downloadFile streams url into target, enforcing the download size limit and
// verifying that the transfer was complete.
func downloadFile(ctx context.Context, client *http.Client, rawURL, target string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("下载更新包失败: %w", err)
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", "FastV/"+normalizeVersion(buildinfo.Version))

	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("下载更新包失败: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("下载更新包失败: 服务器返回 HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxDownloadSize {
		return packageTooLargeError()
	}

	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("保存更新包失败: %w", err)
	}
	written, copyErr := io.Copy(file, io.LimitReader(response.Body, maxDownloadSize+1))
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(target)
		return fmt.Errorf("下载更新包失败: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(target)
		return fmt.Errorf("保存更新包失败: %w", closeErr)
	}
	if written > maxDownloadSize {
		_ = os.Remove(target)
		return packageTooLargeError()
	}
	if written <= 0 {
		_ = os.Remove(target)
		return errors.New("下载的更新包为空")
	}
	if response.ContentLength >= 0 && written != response.ContentLength {
		_ = os.Remove(target)
		return fmt.Errorf("更新包下载不完整: 期望 %d 字节，实际 %d 字节", response.ContentLength, written)
	}
	return nil
}

// packageTooLargeError reports a package above the download size limit.
func packageTooLargeError() error {
	return fmt.Errorf("更新包过大: 最大支持 %s", sizeLabel(maxDownloadSize))
}

// sizeLabel renders a byte limit for error messages.
func sizeLabel(size int64) string {
	const mebibyte = 1 << 20
	if size > 0 && size%mebibyte == 0 {
		return fmt.Sprintf("%d MB", size/mebibyte)
	}
	return fmt.Sprintf("%d 字节", size)
}

// extractZip unpacks archivePath into destDir. Every entry name is validated
// (no absolute paths, drive letters, ".." segments or stream names) and the
// total uncompressed size is capped.
func extractZip(archivePath, destDir string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("打开更新包失败: %w", err)
	}
	defer reader.Close()

	if len(reader.File) == 0 {
		return errors.New("更新包是空的")
	}

	var extracted int64
	for _, entry := range reader.File {
		target, err := archiveEntryPath(destDir, entry.Name)
		if err != nil {
			return err
		}
		if entry.FileInfo().IsDir() || strings.HasSuffix(entry.Name, "/") || strings.HasSuffix(entry.Name, "\\") {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("创建解压目录失败: %w", err)
			}
			continue
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("更新包中包含不支持的符号链接: %q", entry.Name)
		}
		entrySize := entry.UncompressedSize64
		if entrySize > uint64(maxExtractSize) {
			return extractTooLargeError()
		}
		if extracted > maxExtractSize-int64(entrySize) {
			return extractTooLargeError()
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("创建解压目录失败: %w", err)
		}
		remaining := maxExtractSize - extracted
		if err := extractZipEntry(entry, target, remaining); err != nil {
			return err
		}
		extracted += int64(entrySize)
	}
	return nil
}

// extractZipEntry writes one archive entry to target, refusing to write more
// than remaining bytes even when the archive header lies about the size.
func extractZipEntry(entry *zip.File, target string, remaining int64) error {
	source, err := entry.Open()
	if err != nil {
		return fmt.Errorf("读取更新包内容失败: %w", err)
	}
	defer source.Close()

	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("写出解压文件失败: %w", err)
	}
	written, copyErr := io.Copy(file, io.LimitReader(source, remaining+1))
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(target)
		return fmt.Errorf("解压更新包失败: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(target)
		return fmt.Errorf("写出解压文件失败: %w", closeErr)
	}
	if written > remaining {
		_ = os.Remove(target)
		return extractTooLargeError()
	}
	return nil
}

// extractTooLargeError reports an archive above the extraction size limit.
func extractTooLargeError() error {
	return fmt.Errorf("更新包解压后超过 %s 上限", sizeLabel(maxExtractSize))
}

// archiveEntryPath validates a zip entry name and maps it below destDir.
//
// zip files use forward slashes, but a hostile archive may mix in Windows
// separators; both are normalised so "../../evil.exe", "C:/evil.exe",
// "/etc/passwd" and "\\server\share\evil.exe" are all rejected.
func archiveEntryPath(destDir, entryName string) (string, error) {
	if strings.TrimSpace(entryName) == "" {
		return "", errors.New("更新包中包含空文件名")
	}
	if strings.ContainsRune(entryName, '\x00') {
		return "", fmt.Errorf("更新包中包含非法路径: %q", entryName)
	}
	if strings.ContainsFunc(entryName, isControlRune) {
		return "", unsafeArchivePathError(entryName)
	}
	normalized := strings.ReplaceAll(entryName, "\\", "/")
	if strings.Contains(normalized, ":") || strings.HasPrefix(normalized, "/") {
		return "", unsafeArchivePathError(entryName)
	}
	for _, segment := range strings.Split(normalized, "/") {
		if segment == ".." || isReservedWindowsName(segment) {
			return "", unsafeArchivePathError(entryName)
		}
	}
	trimmed := strings.TrimRight(normalized, "/")
	if trimmed == "" {
		return "", unsafeArchivePathError(entryName)
	}
	local := filepath.FromSlash(trimmed)
	if !filepath.IsLocal(local) {
		return "", unsafeArchivePathError(entryName)
	}
	target := filepath.Join(destDir, local)
	relative, err := filepath.Rel(destDir, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", unsafeArchivePathError(entryName)
	}
	return target, nil
}

// unsafeArchivePathError builds the standard zip-slip rejection error.
func unsafeArchivePathError(entryName string) error {
	return fmt.Errorf("更新包中包含非法路径（路径穿越或绝对路径），已拒绝: %q", entryName)
}

// windowsReservedNames lists device names that cannot be used as file names on
// Windows. Writing to them could redirect I/O to a device.
var windowsReservedNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// isReservedWindowsName reports whether a path segment resolves to a Windows
// reserved device name (also when a stream or extension is appended).
func isReservedWindowsName(segment string) bool {
	trimmed := strings.TrimRight(segment, " .")
	if trimmed == "" {
		return false
	}
	if index := strings.IndexByte(trimmed, '.'); index >= 0 {
		trimmed = trimmed[:index]
	}
	return windowsReservedNames[strings.ToLower(trimmed)]
}

// copyFile copies src to dst, enforcing the download size limit.
func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("复制更新包失败: %w", err)
	}
	defer source.Close()

	target, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("复制更新包失败: %w", err)
	}
	written, copyErr := io.Copy(target, io.LimitReader(source, maxDownloadSize))
	closeErr := target.Close()
	if copyErr != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("复制更新包失败: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("复制更新包失败: %w", closeErr)
	}
	if written <= 0 {
		_ = os.Remove(dst)
		return errors.New("更新包为空")
	}
	return nil
}

// findExecutable returns the absolute path of the executable to install. A file
// matching the running application name wins, otherwise the first .exe found.
func findExecutable(dir string) (string, error) {
	preferredNames := originalExecutableNames()
	var first, preferred string
	err := filepath.WalkDir(dir, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".exe") {
			return nil
		}
		absolute, err := filepath.Abs(currentPath)
		if err != nil {
			return err
		}
		if first == "" {
			first = absolute
		}
		if preferred == "" && containsFold(preferredNames, entry.Name()) {
			preferred = absolute
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("查找更新程序失败: %w", err)
	}
	if preferred != "" {
		return preferred, nil
	}
	if first != "" {
		return first, nil
	}
	return "", errors.New("更新包中没有找到可执行文件（.exe）")
}

// originalExecutableNames returns the lower-case file names the running
// application is known by, most specific first.
func originalExecutableNames() []string {
	names := make([]string, 0, 2)
	if executable, err := os.Executable(); err == nil && strings.TrimSpace(executable) != "" {
		names = append(names, strings.ToLower(filepath.Base(executable)))
	}
	if product := strings.TrimSpace(buildinfo.ProductName); product != "" {
		name := strings.ToLower(product)
		if !strings.HasSuffix(name, ".exe") {
			name += ".exe"
		}
		names = append(names, name)
	}
	return names
}

// containsFold reports whether names contains value, case-insensitively.
func containsFold(names []string, value string) bool {
	value = strings.ToLower(value)
	for _, name := range names {
		if strings.EqualFold(name, value) {
			return true
		}
	}
	return false
}

// CommanderScript renders the Windows batch script that replaces a running
// FastV executable.
//
// The script polls tasklist until the given PID is gone (at most 120 one-second
// iterations), overwrites currentExe with newExe, starts the updated executable
// and finally deletes itself. Both paths are quoted; paths containing double
// quotes, newlines or percent signs are rejected because they could escape the
// quoting in a cmd.exe command line.
//
// It is exported and pure so the generated content can be tested without
// running it.
func CommanderScript(currentExe, newExe string, pid int) (string, error) {
	current, err := quoteScriptPath(currentExe, "当前程序路径")
	if err != nil {
		return "", err
	}
	replacement, err := quoteScriptPath(newExe, "新版本程序路径")
	if err != nil {
		return "", err
	}
	if pid <= 0 {
		return "", fmt.Errorf("进程 PID 无效: %d", pid)
	}

	content := commanderScriptTemplate
	content = strings.ReplaceAll(content, "{{PID}}", fmt.Sprintf("%d", pid))
	content = strings.ReplaceAll(content, "{{CURRENT_EXE}}", current)
	content = strings.ReplaceAll(content, "{{NEW_EXE}}", replacement)
	return content, nil
}

// commanderScriptTemplate is the replacement script. Only ASCII is used so the
// script parses the same way under every Windows OEM code page. Placeholders
// are substituted by CommanderScript.
const commanderScriptTemplate = `@echo off
setlocal EnableExtensions
rem FastV self-update: wait for the old process to exit, replace the executable
rem and start the updated version. This file deletes itself at the end.

set "FASTV_PID={{PID}}"
set "FASTV_TRIES=0"

:waitloop
tasklist /FI "PID eq %FASTV_PID%" /FO CSV /NH 2>nul | findstr /C:"%FASTV_PID%" >nul
if errorlevel 1 goto replace
set /a FASTV_TRIES+=1
if %FASTV_TRIES% GEQ 120 goto replace
timeout /t 1 /nobreak >nul 2>&1 || ping -n 2 127.0.0.1 >nul
goto waitloop

:replace
copy /Y {{NEW_EXE}} {{CURRENT_EXE}} >nul 2>&1
start "" {{CURRENT_EXE}}
del "%~f0" >nul 2>&1
`

// quoteScriptPath validates a path for the replacement script and returns it
// surrounded by double quotes.
func quoteScriptPath(value, label string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s为空", label)
	}
	if strings.ContainsAny(value, "\"\r\n%\x00") {
		return "", fmt.Errorf("%s包含非法字符（引号、换行、百分号或空字符）: %q", label, value)
	}
	return `"` + value + `"`, nil
}

// WriteReplaceScript writes a replacement script for the current process to
// scriptPath. The script itself is generated by CommanderScript.
func WriteReplaceScript(currentExe, newExe, scriptPath string) error {
	content, err := CommanderScript(currentExe, newExe, os.Getpid())
	if err != nil {
		return err
	}
	scriptPath = strings.TrimSpace(scriptPath)
	if scriptPath == "" {
		return errors.New("更新脚本路径为空")
	}
	if directory := filepath.Dir(scriptPath); directory != "" {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("创建更新脚本目录失败: %w", err)
		}
	}
	if err := os.WriteFile(scriptPath, []byte(content), 0o600); err != nil {
		return fmt.Errorf("写入更新脚本失败: %w", err)
	}
	return nil
}

// NewReplaceScriptPath returns a unique .cmd path in the user's temporary
// directory (%TEMP% on Windows).
func NewReplaceScriptPath() (string, error) {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("生成更新脚本名失败: %w", err)
	}
	name := "fastv-update-" + hex.EncodeToString(buffer) + ".cmd"
	return filepath.Join(os.TempDir(), name), nil
}

// StartReplaceScript launches scriptPath detached through `cmd /c start` so the
// script survives the FastV process it is about to replace.
func StartReplaceScript(scriptPath string) error {
	scriptPath = strings.TrimSpace(scriptPath)
	if scriptPath == "" {
		return errors.New("更新脚本路径为空")
	}
	command := exec.Command("cmd", "/c", "start", "", "/min", scriptPath)
	// Keep the transient cmd.exe window out of the way; the replacement script
	// itself runs in its own minimized window.
	platform.ConfigureHiddenProcess(command)
	if err := command.Run(); err != nil {
		return fmt.Errorf("启动更新脚本失败: %w", err)
	}
	return nil
}
