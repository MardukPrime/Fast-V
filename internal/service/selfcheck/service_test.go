package selfcheck

import (
	"context"
	"testing"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
)

// fakeChecker records the context it received and returns fixed results so the
// service can be tested without the real self-check engine.
type fakeChecker struct {
	calls       int
	hasDeadline bool
	deadline    time.Time
	results     []model.CheckResult
}

func (f *fakeChecker) Run(ctx context.Context) []model.CheckResult {
	f.calls++
	f.deadline, f.hasDeadline = ctx.Deadline()
	return f.results
}

func TestServiceName(t *testing.T) {
	if name := New(nil).ServiceName(); name != "SelfCheckService" {
		t.Fatalf("unexpected service name: %q", name)
	}
}

func TestRunAppliesTimeoutAndReturnsResultsInOrder(t *testing.T) {
	engine := &fakeChecker{results: []model.CheckResult{
		{ID: "core-installed", Name: "Xray-core 安装", Status: "ok", Detail: "ready"},
		{ID: "geodata", Name: "Geo 数据文件", Status: "warn", Detail: "missing"},
	}}
	service := New(engine)

	results := service.Run()

	if engine.calls != 1 {
		t.Fatalf("checker called %d times, want 1", engine.calls)
	}
	if !engine.hasDeadline {
		t.Fatal("self-check context has no deadline")
	}
	remaining := time.Until(engine.deadline)
	if remaining <= 20*time.Second || remaining > selfCheckTimeout {
		t.Fatalf("unexpected timeout remaining: %v (want close to %v)", remaining, selfCheckTimeout)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].ID != "core-installed" || results[0].Status != "ok" {
		t.Fatalf("unexpected first result: %+v", results[0])
	}
	if results[1].ID != "geodata" || results[1].Status != "warn" {
		t.Fatalf("unexpected second result: %+v", results[1])
	}
}

func TestRunNilCheckerReturnsEmptyList(t *testing.T) {
	results := New(nil).Run()
	if results == nil {
		t.Fatal("expected a non-nil empty result list")
	}
	if len(results) != 0 {
		t.Fatalf("got %d results, want 0", len(results))
	}
}
