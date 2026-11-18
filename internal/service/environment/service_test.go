package environment

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MardukPrime/Fast-V/internal/store"
)

var (
	// errUnsupported mirrors what platform returns on non-Windows systems.
	errUnsupported = errors.New("仅支持 Windows")

	// checkedAt is the fixed clock used by every test.
	checkedAt = time.Date(2026, 11, 19, 5, 0, 0, 0, time.UTC)
)

// fakePlatform records the platform calls and simulates success or failure
// without touching the real registry.
type fakePlatform struct {
	protocolRegistered bool
	protocolCheckErr   error
	removeProtocolErr  error
	removeCalls        int

	autostartOn       bool
	autostartCheckErr error
	setAutostartErr   error
	autostartCalls    []bool
}

func (f *fakePlatform) URLSchemeRegistered(string) (bool, error) {
	return f.protocolRegistered, f.protocolCheckErr
}

func (f *fakePlatform) RemoveURLScheme(string) error {
	f.removeCalls++
	if f.removeProtocolErr != nil {
		return f.removeProtocolErr
	}
	f.protocolRegistered = false
	return nil
}

func (f *fakePlatform) AutostartEnabled() (bool, error) {
	return f.autostartOn, f.autostartCheckErr
}

func (f *fakePlatform) SetAutostart(enabled bool) error {
	f.autostartCalls = append(f.autostartCalls, enabled)
	if f.setAutostartErr != nil {
		return f.setAutostartErr
	}
	f.autostartOn = enabled
	return nil
}

// fakeFS simulates the filesystem with an in-memory file set.
type fakeFS struct {
	files       map[string]bool
	statErr     error
	removeError map[string]error
	removeCalls []string
}

func newFakeFS() *fakeFS {
	return &fakeFS{files: map[string]bool{}, removeError: map[string]error{}}
}

func (f *fakeFS) Stat(path string) error {
	if f.statErr != nil {
		return f.statErr
	}
	if f.files[path] {
		return nil
	}
	return fs.ErrNotExist
}

func (f *fakeFS) Remove(path string) error {
	f.removeCalls = append(f.removeCalls, path)
	if err := f.removeError[path]; err != nil {
		return err
	}
	if !f.files[path] {
		return fs.ErrNotExist
	}
	delete(f.files, path)
	return nil
}

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	return st
}

func newTestService(t *testing.T, ops platformOps, files fsOps) (*Service, *store.Store) {
	t.Helper()
	st := newStore(t)
	service := NewWithOps(st, ops, files)
	service.now = func() time.Time { return checkedAt }
	return service, st
}

func pacPathFor(st *store.Store) string {
	return filepath.Join(st.DataDir(), "pac", pacFileName)
}

func assertStrings(t *testing.T, label string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
}

func assertContains(t *testing.T, label, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("%s = %q, want it to contain %q", label, text, want)
		}
	}
}

func TestServiceName(t *testing.T) {
	if name := New(nil).ServiceName(); name != "EnvironmentService" {
		t.Fatalf("ServiceName() = %q, want %q", name, "EnvironmentService")
	}
}

func TestNewInjectsPlatform(t *testing.T) {
	st := newStore(t)
	service := New(st, &fakePlatform{protocolRegistered: true})
	service.now = func() time.Time { return checkedAt }

	report, err := service.Inspect()
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !report.ProtocolRegistered {
		t.Fatal("Inspect did not use the injected platform")
	}
}

func TestInspectNoResiduals(t *testing.T) {
	service, st := newTestService(t, &fakePlatform{}, newFakeFS())

	report, err := service.Inspect()
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if report.ProtocolRegistered || report.AutostartEnabled || report.PacFilePresent {
		t.Fatalf("Inspect reported residuals: %+v", report)
	}
	if report.DataDir != st.DataDir() {
		t.Fatalf("DataDir = %q, want %q", report.DataDir, st.DataDir())
	}
	if len(report.Residuals) != 0 {
		t.Fatalf("Residuals = %v, want empty", report.Residuals)
	}
	if want := checkedAt.Format(time.RFC3339); report.CheckedAt != want {
		t.Fatalf("CheckedAt = %q, want %q", report.CheckedAt, want)
	}
}

func TestInspectResiduals(t *testing.T) {
	ops := &fakePlatform{protocolRegistered: true, autostartOn: true}
	files := newFakeFS()
	service, st := newTestService(t, ops, files)
	pacPath := pacPathFor(st)
	files.files[pacPath] = true

	report, err := service.Inspect()
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !report.ProtocolRegistered || !report.AutostartEnabled || !report.PacFilePresent {
		t.Fatalf("Inspect missed residuals: %+v", report)
	}
	if len(report.Residuals) != 3 {
		t.Fatalf("Residuals = %v, want 3 entries", report.Residuals)
	}
	assertContains(t, "Residuals", strings.Join(report.Residuals, "\n"),
		protocolRegistryPath, autostartRegistryPath, pacPath)
}

func TestInspectPlatformUnavailable(t *testing.T) {
	ops := &fakePlatform{protocolCheckErr: errUnsupported, autostartCheckErr: errUnsupported}
	service, _ := newTestService(t, ops, newFakeFS())

	report, err := service.Inspect()
	if err != nil {
		t.Fatalf("Inspect must not fail when the platform is unavailable: %v", err)
	}
	if report.ProtocolRegistered || report.AutostartEnabled {
		t.Fatalf("Inspect reported a residual it could not check: %+v", report)
	}
	if len(report.Residuals) != 2 {
		t.Fatalf("Residuals = %v, want 2 explanations", report.Residuals)
	}
	text := strings.Join(report.Residuals, "\n")
	assertContains(t, "Residuals", text, "无法检测 fastv 协议注册", "无法检测开机自启", "仅支持 Windows")
}

func TestInspectPacStatError(t *testing.T) {
	files := newFakeFS()
	files.statErr = errors.New("权限不足")
	service, _ := newTestService(t, &fakePlatform{}, files)

	report, err := service.Inspect()
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if report.PacFilePresent {
		t.Fatal("PacFilePresent = true for an unreadable PAC file")
	}
	assertContains(t, "Residuals", strings.Join(report.Residuals, "\n"), "无法检测 PAC 文件", "权限不足")
}

func TestInspectNilStore(t *testing.T) {
	service := New(nil, &fakePlatform{})
	service.now = func() time.Time { return checkedAt }

	report, err := service.Inspect()
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if report.DataDir != "" {
		t.Fatalf("DataDir = %q, want empty", report.DataDir)
	}
	assertContains(t, "Residuals", strings.Join(report.Residuals, "\n"), "无法确定数据目录")
}

func TestCleanupRemovesAllResiduals(t *testing.T) {
	ops := &fakePlatform{protocolRegistered: true, autostartOn: true}
	files := newFakeFS()
	service, st := newTestService(t, ops, files)
	pacPath := pacPathFor(st)
	files.files[pacPath] = true

	result, err := service.Cleanup()
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	assertStrings(t, "Removed", result.Removed, []string{cleanupProtocol, cleanupAutostart, cleanupPACFile})
	assertStrings(t, "Failed", result.Failed, []string{})
	assertContains(t, "Message", result.Message,
		"已处理 3 项", st.DataDir(), "如需彻底卸载请手动删除该目录")
	if result.Report.ProtocolRegistered || result.Report.AutostartEnabled || result.Report.PacFilePresent {
		t.Fatalf("post-cleanup report still has residuals: %+v", result.Report)
	}
	if len(result.Report.Residuals) != 0 {
		t.Fatalf("post-cleanup Residuals = %v, want empty", result.Report.Residuals)
	}
	if ops.removeCalls != 1 {
		t.Fatalf("RemoveURLScheme calls = %d, want 1", ops.removeCalls)
	}
	if len(ops.autostartCalls) != 1 || ops.autostartCalls[0] != false {
		t.Fatalf("SetAutostart calls = %v, want [false]", ops.autostartCalls)
	}
	if len(files.files) != 0 {
		t.Fatalf("fake filesystem still has files: %v", files.files)
	}
}

func TestCleanupNothingToDo(t *testing.T) {
	ops := &fakePlatform{}
	files := newFakeFS()
	service, st := newTestService(t, ops, files)

	result, err := service.Cleanup()
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	assertStrings(t, "Removed", result.Removed, []string{})
	assertStrings(t, "Failed", result.Failed, []string{})
	assertContains(t, "Message", result.Message, "未发现可清理的环境残留", st.DataDir())
	if ops.removeCalls != 0 || len(ops.autostartCalls) != 0 {
		t.Fatalf("Cleanup touched the platform without residuals: %+v", ops)
	}
	if len(files.removeCalls) != 0 {
		t.Fatalf("Cleanup removed files without residual: %v", files.removeCalls)
	}
}

func TestCleanupPartialFailure(t *testing.T) {
	ops := &fakePlatform{
		protocolRegistered: true,
		autostartOn:        true,
		setAutostartErr:    errors.New("注册表写入被拒绝"),
	}
	files := newFakeFS()
	service, st := newTestService(t, ops, files)
	pacPath := pacPathFor(st)
	files.files[pacPath] = true
	files.removeError[pacPath] = errors.New("文件被占用")

	result, err := service.Cleanup()
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	assertStrings(t, "Removed", result.Removed, []string{cleanupProtocol})
	assertStrings(t, "Failed", result.Failed, []string{cleanupAutostart, cleanupPACFile})
	assertContains(t, "Message", result.Message, "已处理 1 项", "2 项失败", st.DataDir())
	if !result.Report.AutostartEnabled || !result.Report.PacFilePresent {
		t.Fatalf("post-cleanup report lost the failed residuals: %+v", result.Report)
	}
	if result.Report.ProtocolRegistered {
		t.Fatalf("post-cleanup report still has the removed protocol: %+v", result.Report)
	}
}

func TestCleanupIdempotent(t *testing.T) {
	ops := &fakePlatform{protocolRegistered: true, autostartOn: true}
	files := newFakeFS()
	service, st := newTestService(t, ops, files)
	pacPath := pacPathFor(st)
	files.files[pacPath] = true

	first, err := service.Cleanup()
	if err != nil {
		t.Fatalf("first Cleanup: %v", err)
	}
	assertStrings(t, "first Removed", first.Removed, []string{cleanupProtocol, cleanupAutostart, cleanupPACFile})

	second, err := service.Cleanup()
	if err != nil {
		t.Fatalf("second Cleanup: %v", err)
	}
	assertStrings(t, "second Removed", second.Removed, []string{})
	assertStrings(t, "second Failed", second.Failed, []string{})
	assertContains(t, "second Message", second.Message, "未发现可清理的环境残留", st.DataDir())
	if ops.removeCalls != 1 || len(ops.autostartCalls) != 1 {
		t.Fatalf("second Cleanup repeated platform mutations: removeCalls=%d autostartCalls=%v",
			ops.removeCalls, ops.autostartCalls)
	}
}

func TestCleanupPlatformUnavailable(t *testing.T) {
	ops := &fakePlatform{
		protocolCheckErr:  errUnsupported,
		autostartCheckErr: errUnsupported,
		removeProtocolErr: errUnsupported,
		setAutostartErr:   errUnsupported,
	}
	files := newFakeFS()
	service, st := newTestService(t, ops, files)
	pacPath := pacPathFor(st)
	files.files[pacPath] = true

	result, err := service.Cleanup()
	if err != nil {
		t.Fatalf("Cleanup must not fail when the platform is unavailable: %v", err)
	}
	assertStrings(t, "Removed", result.Removed, []string{cleanupPACFile})
	assertStrings(t, "Failed", result.Failed, []string{cleanupProtocol, cleanupAutostart})
	assertContains(t, "Message", result.Message, "已处理 1 项", "2 项失败", st.DataDir())
	if result.Report.PacFilePresent {
		t.Fatalf("post-cleanup report still has the PAC file: %+v", result.Report)
	}
	assertContains(t, "post-cleanup Residuals", strings.Join(result.Report.Residuals, "\n"), "仅支持 Windows")
}

func TestCleanupDeletesPacFileOnDisk(t *testing.T) {
	st := newStore(t)
	pacPath := pacPathFor(st)
	if err := os.MkdirAll(filepath.Dir(pacPath), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(pacPath, []byte("function FindProxyForURL(){}"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	service := NewWithOps(st, &fakePlatform{}, nil) // nil filesystem -> real disk
	service.now = func() time.Time { return checkedAt }

	result, err := service.Cleanup()
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	assertStrings(t, "Removed", result.Removed, []string{cleanupPACFile})
	assertStrings(t, "Failed", result.Failed, []string{})
	if _, statErr := os.Stat(pacPath); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("PAC file still exists, Stat err = %v", statErr)
	}
	assertContains(t, "Message", result.Message, st.DataDir())
}

func TestCleanupPacStatError(t *testing.T) {
	t.Run("missing file is not a failure", func(t *testing.T) {
		files := newFakeFS()
		files.statErr = errors.New("权限不足")
		service, _ := newTestService(t, &fakePlatform{}, files)

		result, err := service.Cleanup()
		if err != nil {
			t.Fatalf("Cleanup: %v", err)
		}
		assertStrings(t, "Removed", result.Removed, []string{})
		assertStrings(t, "Failed", result.Failed, []string{})
		if len(files.removeCalls) != 1 {
			t.Fatalf("remove calls = %v, want one attempt", files.removeCalls)
		}
	})

	t.Run("failed removal is reported", func(t *testing.T) {
		files := newFakeFS()
		service, st := newTestService(t, &fakePlatform{}, files)
		pacPath := pacPathFor(st)
		files.statErr = errors.New("权限不足")
		files.removeError[pacPath] = errors.New("拒绝访问")

		result, err := service.Cleanup()
		if err != nil {
			t.Fatalf("Cleanup: %v", err)
		}
		assertStrings(t, "Removed", result.Removed, []string{})
		assertStrings(t, "Failed", result.Failed, []string{cleanupPACFile})
	})
}

func TestCleanupNilStore(t *testing.T) {
	service := New(nil, &fakePlatform{})
	service.now = func() time.Time { return checkedAt }

	result, err := service.Cleanup()
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	assertStrings(t, "Removed", result.Removed, []string{})
	assertStrings(t, "Failed", result.Failed, []string{})
	assertContains(t, "Message", result.Message, "用户数据未做修改")
}
