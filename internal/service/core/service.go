package core

import (
	"context"
	"errors"
	"runtime"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	coreruntime "github.com/MardukPrime/Fast-V/internal/core"
	"github.com/MardukPrime/Fast-V/internal/coremode"
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
	if coremode.Enabled() {
		// The core is linked into the app; there is nothing to detect/download.
		manager.SetAutoUpdate(false)
		return service
	}
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
	if coremode.Enabled() {
		s.manager.SetAutoUpdate(false)
		return nil
	}
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
	if coremode.Enabled() {
		return model.CoreInfo{
			Installed: true,
			Version:   coremode.Version(),
			Path:      "embedded",
			Status:    "ready",
			Message:   "内嵌内核（随应用运行）",
			CheckedAt: model.NowRFC3339(),
		}
	}
	s.manager.SetAutoUpdate(s.store.Settings().CoreAutoUpdate)
	return s.manager.Info()
}

func (s *CoreService) GeoData() model.GeoDataInfo {
	return s.manager.GeoDataInfo()
}

func (s *CoreService) UpdateGeoData() (model.GeoDataInfo, error) {
	if coremode.Enabled() {
		return model.GeoDataInfo{}, errors.New("内嵌内核暂不支持更新 Geo 数据")
	}
	if runtime.GOOS == "android" {
		return model.GeoDataInfo{}, errors.New("Android 预览版暂不支持更新 Geo 数据")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	return s.manager.UpdateGeoData(ctx)
}
func (s *CoreService) CheckLatest() (model.CoreInfo, error) {
	if coremode.Enabled() {
		return s.Info(), nil
	}
	s.manager.SetAutoUpdate(s.store.Settings().CoreAutoUpdate)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return s.manager.CheckLatest(ctx)
}

func (s *CoreService) DownloadLatest() (model.CoreInfo, error) {
	if coremode.Enabled() {
		return model.CoreInfo{}, errors.New("内嵌内核无需下载")
	}
	if runtime.GOOS == "android" {
		return model.CoreInfo{}, errors.New("Android 预览版暂不支持下载外部内核")
	}
	s.manager.SetAutoUpdate(s.store.Settings().CoreAutoUpdate)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	return s.manager.DownloadLatest(ctx)
}

func (s *CoreService) AutoUpdate() (model.CoreUpdateResult, error) {
	if coremode.Enabled() {
		return model.CoreUpdateResult{}, errors.New("内嵌内核无需更新")
	}
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
