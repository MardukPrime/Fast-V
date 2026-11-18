package applog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
)

// openTestLogger opens a logger below a fresh temporary directory and makes sure
// it is closed when the test ends.
func openTestLogger(t *testing.T, dir string) *Logger {
	t.Helper()
	logger, err := Open(dir, 0, 0)
	if err != nil {
		t.Fatalf("open logger: %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	return logger
}

// readLines returns the physical lines of one log file.
func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Base(path), err)
	}
	if len(data) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// listLogFiles returns every log file present in dir, sorted by name.
func listLogFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read log dir: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == activeLogName || (strings.HasPrefix(name, "fastv.") && strings.HasSuffix(name, ".log")) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func TestWriteLineFormat(t *testing.T) {
	logger := openTestLogger(t, filepath.Join(t.TempDir(), "logs"))
	fixed := time.Date(2026, time.November, 19, 12, 34, 56, 0, time.FixedZone("JST", 9*60*60))
	logger.now = func() time.Time { return fixed }

	logger.WriteLine("info", "proxy", "hello world")
	logger.WriteLine("warning", "stats", "多行\n消息\r\n继续")
	logger.WriteEntry(model.LogEntry{
		ID:        "ignored-id",
		Timestamp: "ignored-timestamp",
		Level:     "error",
		Source:    "core",
		Message:   "boom",
	})
	logger.WriteLine("", "", "")
	if err := logger.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	lines := readLines(t, logger.Path())
	want := []string{
		"2026-11-19T12:34:56+09:00 [INFO] [proxy] hello world",
		"2026-11-19T12:34:56+09:00 [WARNING] [stats] 多行 消息 继续",
		"2026-11-19T12:34:56+09:00 [ERROR] [core] boom",
		"2026-11-19T12:34:56+09:00 [INFO] [-] ",
	}
	if len(lines) != len(want) {
		t.Fatalf("unexpected line count: got %d, want %d (%q)", len(lines), len(want), lines)
	}
	for index := range want {
		if lines[index] != want[index] {
			t.Errorf("line %d:\n got: %q\nwant: %q", index, lines[index], want[index])
		}
	}
}

func TestWriteEntryWritesOnePhysicalLinePerEntry(t *testing.T) {
	logger := openTestLogger(t, filepath.Join(t.TempDir(), "logs"))
	fixed := time.Date(2026, time.November, 19, 12, 34, 56, 0, time.FixedZone("JST", 9*60*60))
	logger.now = func() time.Time { return fixed }

	logger.WriteLine("info", "core", strings.Repeat("长", maxMessageRunes+100)+"\nsecond line")
	if err := logger.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	lines := readLines(t, logger.Path())
	if len(lines) != 1 {
		t.Fatalf("a message with line breaks must stay one line: got %d lines", len(lines))
	}
	marker := "] [core] "
	body := lines[0][strings.Index(lines[0], marker)+len(marker):]
	if count := len([]rune(body)); count != maxMessageRunes+1 {
		t.Fatalf("truncated body rune count: got %d, want %d", count, maxMessageRunes+1)
	}
	if !strings.HasSuffix(body, "…") {
		t.Fatalf("truncated message must end with an ellipsis: %q", body[len(body)-10:])
	}
}

func TestRotationKeepsNewestEntries(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	const (
		maxBytes = 128
		maxFiles = 3
		total    = 40
	)
	logger, err := Open(dir, maxBytes, maxFiles)
	if err != nil {
		t.Fatalf("open logger: %v", err)
	}
	fixed := time.Date(2026, time.November, 19, 12, 34, 56, 0, time.FixedZone("JST", 9*60*60))
	logger.now = func() time.Time { return fixed }
	for index := 0; index < total; index++ {
		logger.WriteLine("info", "rotate", fmt.Sprintf("entry-%03d", index))
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	files := listLogFiles(t, dir)
	if len(files) != maxFiles {
		t.Fatalf("file window: got %v, want exactly %d files", files, maxFiles)
	}
	for _, name := range []string{"fastv.log", "fastv.1.log", "fastv.2.log"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected rotated file %s: %v", name, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if info.Size() > maxBytes {
			t.Errorf("%s exceeds maxBytes: got %d, want <= %d", name, info.Size(), maxBytes)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "fastv.3.log")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("files beyond maxFiles must be removed, fastv.3.log still exists (err=%v)", err)
	}

	// Reconstruct the retained window chronologically: the higher rotated
	// suffix holds the older entries.
	indexPattern := regexp.MustCompile(`entry-(\d{3})$`)
	indices := make([]int, 0, total)
	for _, name := range []string{"fastv.2.log", "fastv.1.log", "fastv.log"} {
		for _, line := range readLines(t, filepath.Join(dir, name)) {
			match := indexPattern.FindStringSubmatch(line)
			if match == nil {
				t.Fatalf("unexpected log line in %s: %q", name, line)
			}
			index, convErr := strconv.Atoi(match[1])
			if convErr != nil {
				t.Fatalf("parse index from %q: %v", line, convErr)
			}
			indices = append(indices, index)
		}
	}
	if len(indices) < 2 {
		t.Fatalf("not enough retained entries to verify rotation: %v", indices)
	}
	for position := 1; position < len(indices); position++ {
		if indices[position] != indices[position-1]+1 {
			t.Fatalf("retained entries are not contiguous: %v", indices)
		}
	}
	if last := indices[len(indices)-1]; last != total-1 {
		t.Fatalf("newest entry missing: last retained index %d, want %d", last, total-1)
	}
	if indices[0] == 0 {
		t.Fatalf("oldest entries must be dropped once the window is full: %v", indices)
	}
	// Every entry older than the retained window must be gone from disk.
	for dropped := 0; dropped < indices[0]; dropped++ {
		token := fmt.Sprintf("entry-%03d", dropped)
		for _, name := range files {
			content, readErr := os.ReadFile(filepath.Join(dir, name))
			if readErr != nil {
				t.Fatalf("read %s: %v", name, readErr)
			}
			if strings.Contains(string(content), token) {
				t.Fatalf("%s still contains dropped entry %s", name, token)
			}
		}
	}
}

func TestRotationHonorsMaxFiles(t *testing.T) {
	cases := []struct {
		name     string
		maxFiles int
	}{
		{name: "single file", maxFiles: 1},
		{name: "two files", maxFiles: 2},
		{name: "four files", maxFiles: 4},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "logs")
			logger, err := Open(dir, 100, testCase.maxFiles)
			if err != nil {
				t.Fatalf("open logger: %v", err)
			}
			fixed := time.Date(2026, time.November, 19, 12, 34, 56, 0, time.FixedZone("JST", 9*60*60))
			logger.now = func() time.Time { return fixed }
			for index := 0; index < 30; index++ {
				logger.WriteLine("info", "rotate", fmt.Sprintf("entry-%03d", index))
			}
			if err := logger.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}

			files := listLogFiles(t, dir)
			if len(files) != testCase.maxFiles {
				t.Fatalf("file window: got %v, want %d files", files, testCase.maxFiles)
			}
			if _, statErr := os.Stat(filepath.Join(dir, fmt.Sprintf("fastv.%d.log", testCase.maxFiles))); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("fastv.%d.log must not exist (err=%v)", testCase.maxFiles, statErr)
			}
			var all strings.Builder
			for _, name := range files {
				data, readErr := os.ReadFile(filepath.Join(dir, name))
				if readErr != nil {
					t.Fatalf("read %s: %v", name, readErr)
				}
				info, statErr := os.Stat(filepath.Join(dir, name))
				if statErr != nil {
					t.Fatalf("stat %s: %v", name, statErr)
				}
				if info.Size() > 100 {
					t.Errorf("%s exceeds maxBytes: %d", name, info.Size())
				}
				all.Write(data)
			}
			if !strings.Contains(all.String(), "entry-029") {
				t.Fatalf("newest entry missing from the retained files: %v", files)
			}
			if strings.Contains(all.String(), "entry-000") {
				t.Fatalf("oldest entry must be dropped: %v", files)
			}
		})
	}
}

func TestRotationWithOversizedLineStillBoundsFileCount(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	logger, err := Open(dir, 32, 2)
	if err != nil {
		t.Fatalf("open logger: %v", err)
	}
	for index := 0; index < 10; index++ {
		logger.WriteLine("info", "rotate", fmt.Sprintf("oversized-entry-%03d", index))
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	files := listLogFiles(t, dir)
	if len(files) > 2 {
		t.Fatalf("rotation must keep at most maxFiles files even when one line exceeds maxBytes: %v", files)
	}
	active, err := os.ReadFile(filepath.Join(dir, activeLogName))
	if err != nil {
		t.Fatalf("read active file: %v", err)
	}
	if !strings.Contains(string(active), "oversized-entry-009") {
		t.Fatalf("newest entry must stay in the active file: %q", active)
	}
}

func TestConcurrentWrites(t *testing.T) {
	logger := openTestLogger(t, filepath.Join(t.TempDir(), "logs"))
	const (
		writers   = 8
		perWriter = 50
	)

	var waitGroup sync.WaitGroup
	for writer := 0; writer < writers; writer++ {
		waitGroup.Add(1)
		go func(writer int) {
			defer waitGroup.Done()
			for entry := 0; entry < perWriter; entry++ {
				logger.WriteLine("info", "concurrent", fmt.Sprintf("writer-%02d-entry-%02d", writer, entry))
			}
		}(writer)
	}
	waitGroup.Wait()
	if err := logger.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	lines := readLines(t, logger.Path())
	if len(lines) != writers*perWriter {
		t.Fatalf("concurrent write count: got %d, want %d", len(lines), writers*perWriter)
	}
	linePattern := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:Z|[+-]\d{2}:\d{2}) \[INFO\] \[concurrent\] writer-(\d{2})-entry-(\d{2})$`)
	counts := make(map[string]int, writers*perWriter)
	for _, line := range lines {
		match := linePattern.FindStringSubmatch(line)
		if match == nil {
			t.Fatalf("line corrupted by concurrent writes: %q", line)
		}
		counts[match[1]+"-"+match[2]]++
	}
	if len(counts) != writers*perWriter {
		t.Fatalf("distinct messages: got %d, want %d", len(counts), writers*perWriter)
	}
	for key, count := range counts {
		if count != 1 {
			t.Fatalf("message %s written %d times, want 1", key, count)
		}
	}
}

func TestWritesAfterCloseAreIgnored(t *testing.T) {
	logger, err := Open(filepath.Join(t.TempDir(), "logs"), 0, 0)
	if err != nil {
		t.Fatalf("open logger: %v", err)
	}
	logger.WriteLine("info", "app", "before close")
	if err := logger.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("writing after Close must not panic: %v", recovered)
		}
	}()
	logger.WriteLine("info", "app", "after close")
	logger.WriteEntry(model.LogEntry{Level: "error", Source: "app", Message: "after close entry"})
	if err := logger.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}

	lines := readLines(t, logger.Path())
	if len(lines) != 1 || !strings.HasSuffix(lines[0], "before close") {
		t.Fatalf("writes after Close must be ignored: %q", lines)
	}

	var nilLogger *Logger
	nilLogger.WriteLine("info", "app", "nil logger")
	nilLogger.WriteEntry(model.LogEntry{Level: "error", Source: "app", Message: "nil logger"})
	if path := nilLogger.Path(); path != "" {
		t.Fatalf("nil logger path: got %q, want empty", path)
	}
	if err := nilLogger.Close(); err != nil {
		t.Fatalf("nil logger close: %v", err)
	}
}

func TestOpenCreatesDirectoryAndAppliesDefaults(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "logs")
	logger, err := Open(dir, 0, 0)
	if err != nil {
		t.Fatalf("open logger: %v", err)
	}
	defer func() { _ = logger.Close() }()

	if logger.maxBytes != DefaultMaxBytes {
		t.Errorf("default maxBytes: got %d, want %d", logger.maxBytes, DefaultMaxBytes)
	}
	if logger.maxFiles != DefaultMaxFiles {
		t.Errorf("default maxFiles: got %d, want %d", logger.maxFiles, DefaultMaxFiles)
	}
	wantPath := filepath.Join(dir, activeLogName)
	if logger.Path() != wantPath {
		t.Errorf("path: got %q, want %q", logger.Path(), wantPath)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat log dir: %v", err)
	}
	if !dirInfo.IsDir() {
		t.Fatalf("log dir is not a directory: %v", dir)
	}
	fileInfo, err := os.Stat(wantPath)
	if err != nil {
		t.Fatalf("stat active log file: %v", err)
	}
	if fileInfo.IsDir() {
		t.Fatalf("active log path is a directory: %v", wantPath)
	}
	if runtime.GOOS != "windows" {
		if mode := dirInfo.Mode().Perm(); mode != dirMode {
			t.Errorf("log dir mode: got %o, want %o", mode, dirMode)
		}
		if mode := fileInfo.Mode().Perm(); mode != fileMode {
			t.Errorf("log file mode: got %o, want %o", mode, fileMode)
		}
	}
}

func TestOpenAppendsToExistingFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	first, err := Open(dir, 0, 0)
	if err != nil {
		t.Fatalf("open first logger: %v", err)
	}
	first.WriteLine("info", "app", "first session")
	if err := first.Close(); err != nil {
		t.Fatalf("close first logger: %v", err)
	}

	second, err := Open(dir, 0, 0)
	if err != nil {
		t.Fatalf("open second logger: %v", err)
	}
	second.WriteLine("info", "app", "second session")
	if err := second.Close(); err != nil {
		t.Fatalf("close second logger: %v", err)
	}

	lines := readLines(t, filepath.Join(dir, activeLogName))
	if len(lines) != 2 {
		t.Fatalf("reopening must append: got %d lines, want 2 (%q)", len(lines), lines)
	}
	if !strings.HasSuffix(lines[0], "first session") || !strings.HasSuffix(lines[1], "second session") {
		t.Fatalf("unexpected appended lines: %q", lines)
	}
}

func TestOpenErrors(t *testing.T) {
	if _, err := Open("   ", 0, 0); err == nil {
		t.Fatal("Open must reject an empty directory")
	}

	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("prepare blocker file: %v", err)
	}
	if _, err := Open(filepath.Join(blocker, "logs"), 0, 0); err == nil {
		t.Fatal("Open must fail when the log directory cannot be created")
	}
}
