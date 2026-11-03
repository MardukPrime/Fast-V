package diagnostics

import (
	"context"
	"errors"
	"strings"
	"time"

	diag "github.com/MardukPrime/Fast-V/internal/diagnostics"
	"github.com/MardukPrime/Fast-V/internal/model"
)

type ProxyStateProvider interface {
	State() model.ProxyState
}

// DiagnosticsService exposes direct and proxied network probes to the frontend.
type DiagnosticsService struct {
	checker *diag.Checker
	proxy   ProxyStateProvider
}

func New(proxyProvider ProxyStateProvider) *DiagnosticsService {
	return &DiagnosticsService{
		checker: diag.NewChecker(),
		proxy:   proxyProvider,
	}
}

func (s *DiagnosticsService) ServiceName() string {
	return "DiagnosticsService"
}

func (s *DiagnosticsService) Check(mode string, includeSpeed bool) (model.NetworkProbe, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "direct" && mode != "proxy" {
		return model.NetworkProbe{}, errors.New("mode must be direct or proxy")
	}
	proxyAddr := ""
	if mode == "proxy" {
		state := s.proxy.State()
		if state.Status != "running" || state.LocalAddr == "" {
			return model.NetworkProbe{}, errors.New("local proxy is not running")
		}
		proxyAddr = state.LocalAddr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return s.checker.Probe(ctx, mode, proxyAddr, includeSpeed)
}
