package proxy

import (
	"context"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MardukPrime/Fast-V/internal/model"
	proxyruntime "github.com/MardukPrime/Fast-V/internal/proxy"
	"github.com/MardukPrime/Fast-V/internal/store"
)

// ProxyService is the Wails adapter for local proxy start, stop and testing.
type ProxyService struct {
	store   *store.Store
	manager *proxyruntime.Manager
}

func New(st *store.Store, coreProvider proxyruntime.CoreInfoProvider) *ProxyService {
	manager := proxyruntime.NewManager(st, coreProvider)
	service := &ProxyService{store: st, manager: manager}
	manager.SetOnStateChange(service.emit)
	return service
}

func (s *ProxyService) ServiceName() string {
	return "ProxyService"
}

func (s *ProxyService) Start() (model.ProxyState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return s.manager.Start(ctx)
}

func (s *ProxyService) SwitchNode(id string) (model.ProxyState, error) {
	if _, err := s.store.SetActiveNode(id); err != nil {
		return s.manager.State(), err
	}
	state := s.manager.State()
	if state.Status != "running" && state.Status != "starting" {
		return state, nil
	}
	if _, err := s.manager.Stop(); err != nil {
		return s.manager.State(), err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return s.manager.Start(ctx)
}
func (s *ProxyService) Stop() (model.ProxyState, error) {
	return s.manager.Stop()
}

func (s *ProxyService) State() model.ProxyState {
	return s.manager.State()
}

func (s *ProxyService) Test() (model.ProxyTestResult, error) {
	return s.manager.Test(s.store.Settings())
}

func (s *ProxyService) emit(state model.ProxyState) {
	if app := application.Get(); app != nil {
		app.Event.Emit("proxy:state", state)
	}
}
