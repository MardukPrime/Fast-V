// Package applog persists the application log to a local file with size based
// rotation. It only depends on the standard library and the leaf model package,
// so both the bootstrap layer and the services can use it without creating an
// import cycle. The log file is intended for after-sales diagnostics: the
// in-memory proxy log and the diagnostics report only cover the current
// session, while this file survives restarts.
package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/MardukPrime/Fast-V/internal/model"
)

const (
	// DefaultMaxBytes is the per-file size limit used when Open receives a
	// non-positive maxBytes.
	DefaultMaxBytes int64 = 5 << 20 // 5 MiB
	// DefaultMaxFiles is the number of files kept (the active file plus the
	// rotated ones) when Open receives a non-positive maxFiles.
	DefaultMaxFiles = 3

	// activeLogName is the file every entry is appended to.
	activeLogName = "fastv.log"

	// maxMessageRunes bounds one message. A single runaway entry (for example
	// a whole core output dump) must never bloat the file or defeat rotation.
	maxMessageRunes = 2048

	dirMode  os.FileMode = 0o700
	fileMode os.FileMode = 0o600
)

// Logger appends timestamped lines to <dir>/fastv.log and rotates the file once
// it would grow past maxBytes. Rotation keeps at most maxFiles files: the
// active fastv.log plus fastv.1.log, fastv.2.log, ... up to fastv.<maxFiles-1>.log.
// The oldest file is dropped when the window is full.
//
// A Logger is safe for concurrent use by multiple goroutines. The zero value is
// not usable; call Open. A nil *Logger is safe: every method tolerates it.
type Logger struct {
	mu sync.Mutex

	dir  string
	path string
	file *os.File
	size int64

	maxBytes int64
	maxFiles int

	// now returns the timestamp written in front of every line. It is a field
	// so tests can pin the clock; nil means time.Now.
	now func() time.Time

	closed bool
}

// Open creates dir (0700 when it does not exist) and opens <dir>/fastv.log for
// appending, creating it with 0600 when missing. A non-positive maxBytes or
// maxFiles selects the corresponding default (5 MiB / 3 files).
//
// The returned logger must be closed by the caller to release the file handle.
func Open(dir string, maxBytes int64, maxFiles int) (*Logger, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("日志目录为空")
	}
	dir = filepath.Clean(dir)
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if maxFiles <= 0 {
		maxFiles = DefaultMaxFiles
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %w", err)
	}

	logger := &Logger{
		dir:      dir,
		path:     filepath.Join(dir, activeLogName),
		maxBytes: maxBytes,
		maxFiles: maxFiles,
		now:      time.Now,
	}
	if err := logger.openLocked(false); err != nil {
		return nil, err
	}
	return logger, nil
}

// WriteLine appends one formatted line:
//
//	2026-11-19T12:34:56+09:00 [LEVEL] [source] message
//
// The timestamp is RFC3339 in the local time zone. Level is upper-cased (empty
// becomes INFO), source defaults to "-", and message/level/source are collapsed
// to one line; message is truncated to maxMessageRunes. Write errors are
// silently ignored because logging must never fail the caller; rotation is
// triggered before the active file would pass maxBytes.
func (l *Logger) WriteLine(level, source, message string) {
	if l == nil {
		return
	}
	line := l.formatLine(level, source, message)

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	if l.file == nil {
		// A previous rotation could not recreate the file; retry lazily so the
		// logger recovers instead of staying silent forever.
		if err := l.openLocked(false); err != nil {
			return
		}
	}
	if l.size > 0 && l.size+int64(len(line)) > l.maxBytes {
		l.rotateLocked()
	}
	if l.file == nil {
		return
	}
	written, err := l.file.WriteString(line)
	if err == nil {
		l.size += int64(written)
	}
}

// WriteEntry appends a model.LogEntry as one line. The entry Timestamp and ID
// are ignored: the line is stamped with the logger clock, which makes the file
// ordering reflect the actual write order.
func (l *Logger) WriteEntry(entry model.LogEntry) {
	l.WriteLine(entry.Level, entry.Source, entry.Message)
}

// Path returns the active log file path.
func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Close flushes nothing (every line is written immediately) but releases the
// file handle. Close is idempotent and safe to call from any goroutine; writes
// after Close are ignored instead of panicking.
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

// formatLine renders one log line. It does not touch shared state besides
// reading the immutable clock function.
func (l *Logger) formatLine(level, source, message string) string {
	timestamp := time.Now()
	if l.now != nil {
		timestamp = l.now()
	}
	return fmt.Sprintf("%s [%s] [%s] %s\n",
		timestamp.Format(time.RFC3339),
		normalizeLevel(level),
		normalizeSource(source),
		normalizeMessage(message))
}

// openLocked opens the active log file. truncate yields a fresh empty file and
// is used after rotation; otherwise the existing content is appended to and its
// size becomes the rotation watermark. The caller must hold l.mu.
func (l *Logger) openLocked(truncate bool) error {
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if truncate {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(l.path, flags, fileMode)
	if err != nil {
		l.file = nil
		l.size = 0
		return fmt.Errorf("打开日志文件失败: %w", err)
	}
	size := int64(0)
	if !truncate {
		if info, statErr := file.Stat(); statErr == nil {
			size = info.Size()
		}
	}
	l.file = file
	l.size = size
	return nil
}

// rotateLocked moves the active file to fastv.1.log, shifts the older files up
// by one and drops the file that falls out of the window. With maxFiles == 1 the
// active file is truncated in place because no rotated file may exist. All file
// operations are best effort: if a rename fails, the active file is recreated
// truncated so logging keeps working and stays bounded. The caller must hold
// l.mu.
func (l *Logger) rotateLocked() {
	if l.file == nil {
		return
	}
	if l.maxFiles <= 1 {
		// Only the active file may be kept. Windows opens O_APPEND files with
		// FILE_APPEND_DATA, which cannot truncate the file in place, so the file
		// is closed and reopened with O_TRUNC instead.
		_ = l.file.Close()
		l.file = nil
		l.size = 0
		if err := l.openLocked(true); err != nil {
			l.file = nil
		}
		return
	}

	_ = l.file.Close()
	l.file = nil
	l.size = 0

	// Drop the oldest file first: this also frees the rename destination on
	// Windows, where os.Rename refuses to overwrite an existing file.
	_ = os.Remove(l.rotatedPath(l.maxFiles - 1))
	for index := l.maxFiles - 2; index >= 1; index-- {
		_ = os.Rename(l.rotatedPath(index), l.rotatedPath(index+1))
	}
	_ = os.Rename(l.path, l.rotatedPath(1))

	// Recreate the active file. O_TRUNC is a safety net: if a rename above
	// failed, the stale content is discarded instead of growing forever.
	if err := l.openLocked(true); err != nil {
		l.file = nil
	}
}

// rotatedPath returns the path of the index-th rotated file (1 is the newest).
func (l *Logger) rotatedPath(index int) string {
	return filepath.Join(l.dir, fmt.Sprintf("fastv.%d.log", index))
}

// normalizeLevel upper-cases the level and defaults an empty one to INFO.
func normalizeLevel(level string) string {
	level = strings.ToUpper(sanitizeSingleLine(level))
	if level == "" {
		return "INFO"
	}
	return level
}

// normalizeSource keeps the source on one line and defaults an empty one to "-".
func normalizeSource(source string) string {
	source = sanitizeSingleLine(source)
	if source == "" {
		return "-"
	}
	return source
}

// normalizeMessage collapses a message to a single trimmed line and bounds its
// length so one entry cannot dominate the log file.
func normalizeMessage(message string) string {
	message = sanitizeSingleLine(message)
	if utf8.RuneCountInString(message) <= maxMessageRunes {
		return message
	}
	runes := []rune(message)
	return string(runes[:maxMessageRunes]) + "…"
}

// sanitizeSingleLine replaces line breaks and tabs with spaces and trims the
// result, guaranteeing one physical line per log entry.
func sanitizeSingleLine(value string) string {
	replacer := strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ", "\t", " ")
	return strings.TrimSpace(replacer.Replace(value))
}
