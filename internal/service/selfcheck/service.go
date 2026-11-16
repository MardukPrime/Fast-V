// Package selfcheck exposes the diagnostics self-check engine to the frontend.
//
// The service is a thin Wails adapter: it owns the run timeout and delegates the
// actual checks to the injected engine (internal/selfcheck.Checker in
// production, a fake in tests).
package selfcheck

import (
	"context"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
)

// selfCheckTimeout bounds one complete self-check run so a hanging external
// dependency (core validation, network probe) can never block the UI.
const selfCheckTimeout = 30 * time.Second

// Checker is the self-check engine contract. *selfcheck.Checker implements it.
type Checker interface {
	Run(ctx context.Context) []model.CheckResult
}

// Service is the Wails adapter for diagnostics self-check.
type Service struct {
	checker Checker
}

// New creates the service with the injected self-check engine. The checker may
// be nil; Run then returns an empty result list instead of panicking.
func New(checker Checker) *Service {
	return &Service{checker: checker}
}

// ServiceName identifies the service in the Wails bindings.
func (s *Service) ServiceName() string {
	return "SelfCheckService"
}

// Run executes all self-checks and returns exactly one result per check in the
// engine-defined fixed order. The 30s timeout keeps the UI responsive even when
// an external dependency hangs.
func (s *Service) Run() []model.CheckResult {
	ctx, cancel := context.WithTimeout(context.Background(), selfCheckTimeout)
	defer cancel()

	if s.checker == nil {
		return []model.CheckResult{}
	}
	return s.checker.Run(ctx)
}
