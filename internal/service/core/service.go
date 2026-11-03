package core

import (
	"context"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	coreruntime "github.com/MardukPrime/Fast-V/internal/core"
	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
)

// CoreService is the Wails adapter for Xray-core lifecycle and updates.
type CoreService struct {
	store   *store.Store
	manager *coreruntime.Manager
	cancel  context.CancelFunc
}

func NewCoreService(st *store.Store) *CoreService {
	manager := coreruntime.NewManager(st.DataDir())
	service := &CoreService{store: st, manager: manager}
	manager.SetOnChange(service.emit)
	manager.SetAutoUpdate(st.Settings().CoreAutoUpdate)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	manager.DetectInstalled(ctx)
	return service
}

func (s *CoreService) ServiceName() string {
	return "CoreService"
}

func (s *CoreService) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	serviceCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.manager.SetAutoUpdate(s.store.Settings().CoreAutoUpdate)
	s.manager.DetectInstalled(serviceCtx)

	if s.store.Settings().CoreAutoUpdate {
		go func() {
			select {
			case <-time.After(3 * time.Second):
				_, _ = s.manager.AutoUpdate(serviceCtx)
			case <-serviceCtx.Done():
				return
			}
		}()
	}
	return nil
}

func (s *CoreService) ServiceShutdown() error {
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

func (s *CoreService) Info() model.CoreInfo {
	s.manager.SetAutoUpdate(s.store.Settings().CoreAutoUpdate)
	return s.manager.Info()
}

func (s *CoreService) CheckLatest() (model.CoreInfo, error) {
	s.manager.SetAutoUpdate(s.store.Settings().CoreAutoUpdate)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return s.manager.CheckLatest(ctx)
}

func (s *CoreService) DownloadLatest() (model.CoreInfo, error) {
	s.manager.SetAutoUpdate(s.store.Settings().CoreAutoUpdate)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	return s.manager.DownloadLatest(ctx)
}

func (s *CoreService) AutoUpdate() (model.CoreUpdateResult, error) {
	s.manager.SetAutoUpdate(s.store.Settings().CoreAutoUpdate)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	return s.manager.AutoUpdate(ctx)
}

func (s *CoreService) emit(info model.CoreInfo) {
	if app := application.Get(); app != nil {
		app.Event.Emit("core:state", info)
	}
}
