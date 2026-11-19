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

// CoreService is the Wails adapter for core lifecycle and updates. It owns one
// manager per supported core kind (Xray-core and sing-box); the settings'
// CoreType decides which manager is active.
type CoreService struct {
	store   *store.Store
	xray    *coreruntime.Manager
	singbox *coreruntime.Manager
	cancel  context.CancelFunc
}

func NewCoreService(st *store.Store) *CoreService {
	service := &CoreService{
		store:   st,
		xray:    coreruntime.NewManagerFor(coreruntime.KindXray, st.DataDir()),
		singbox: coreruntime.NewManagerFor(coreruntime.KindSingBox, st.DataDir()),
	}
	service.xray.SetOnChange(func(info model.CoreInfo) { service.emitFor(coreruntime.KindXray, info) })
	service.singbox.SetOnChange(func(info model.CoreInfo) { service.emitFor(coreruntime.KindSingBox, info) })
	if coremode.Enabled() {
		service.xray.SetAutoUpdate(false)
		service.singbox.SetAutoUpdate(false)
		return service
	}
	service.xray.SetAutoUpdate(st.Settings().CoreAutoUpdate)
	service.singbox.SetAutoUpdate(st.Settings().CoreAutoUpdate)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	service.xray.DetectInstalled(ctx)
	service.singbox.DetectInstalled(ctx)
	return service
}

func (s *CoreService) ServiceName() string {
	return "CoreService"
}

func (s *CoreService) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	serviceCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	if coremode.Enabled() {
		s.xray.SetAutoUpdate(false)
		s.singbox.SetAutoUpdate(false)
		return nil
	}
	s.xray.SetAutoUpdate(s.store.Settings().CoreAutoUpdate)
	s.singbox.SetAutoUpdate(s.store.Settings().CoreAutoUpdate)
	s.xray.DetectInstalled(serviceCtx)
	s.singbox.DetectInstalled(serviceCtx)

	if s.store.Settings().CoreAutoUpdate {
		go func() {
			select {
			case <-time.After(3 * time.Second):
				_, _ = s.activeManager().AutoUpdate(serviceCtx)
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

// activeKind returns the configured core kind (defaults to Xray).
func (s *CoreService) activeKind() coreruntime.Kind {
	if s.store.Settings().CoreType == string(coreruntime.KindSingBox) {
		return coreruntime.KindSingBox
	}
	return coreruntime.KindXray
}

func (s *CoreService) activeManager() *coreruntime.Manager {
	if s.activeKind() == coreruntime.KindSingBox {
		return s.singbox
	}
	return s.xray
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
	manager := s.activeManager()
	manager.SetAutoUpdate(s.store.Settings().CoreAutoUpdate)
	return manager.Info()
}

// XrayInfo reports the Xray-core manager regardless of the selected core. The
// URL latency tests spawn a temporary Xray process, so they need the Xray path
// even while sing-box handles normal proxy traffic.
func (s *CoreService) XrayInfo() model.CoreInfo {
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
	return s.xray.Info()
}

func (s *CoreService) GeoData() model.GeoDataInfo {
	if s.activeKind() == coreruntime.KindSingBox {
		return model.GeoDataInfo{Files: []model.GeoDataFile{}}
	}
	if coremode.Enabled() {
		return model.GeoDataInfo{Files: []model.GeoDataFile{}}
	}
	return s.xray.GeoDataInfo()
}

func (s *CoreService) UpdateGeoData() (model.GeoDataInfo, error) {
	if coremode.Enabled() {
		return model.GeoDataInfo{}, errors.New("内嵌内核暂不支持更新 Geo 数据")
	}
	if runtime.GOOS == "android" {
		return model.GeoDataInfo{}, errors.New("Android 预览版暂不支持更新 Geo 数据")
	}
	if s.activeKind() == coreruntime.KindSingBox {
		return model.GeoDataInfo{}, errors.New("sing-box 不使用 Xray GeoData 文件")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	return s.xray.UpdateGeoData(ctx)
}

func (s *CoreService) CheckLatest() (model.CoreInfo, error) {
	if coremode.Enabled() {
		return s.Info(), nil
	}
	manager := s.activeManager()
	manager.SetAutoUpdate(s.store.Settings().CoreAutoUpdate)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return manager.CheckLatest(ctx)
}

func (s *CoreService) DownloadLatest() (model.CoreInfo, error) {
	if coremode.Enabled() {
		return model.CoreInfo{}, errors.New("内嵌内核无需下载")
	}
	if runtime.GOOS == "android" {
		return model.CoreInfo{}, errors.New("Android 预览版暂不支持下载外部内核")
	}
	manager := s.activeManager()
	manager.SetAutoUpdate(s.store.Settings().CoreAutoUpdate)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	return manager.DownloadLatest(ctx)
}

func (s *CoreService) AutoUpdate() (model.CoreUpdateResult, error) {
	if coremode.Enabled() {
		return model.CoreUpdateResult{}, errors.New("内嵌内核无需更新")
	}
	manager := s.activeManager()
	manager.SetAutoUpdate(s.store.Settings().CoreAutoUpdate)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	return manager.AutoUpdate(ctx)
}

// emitFor only forwards state changes of the currently selected core, so the
// settings page never shows a stale version from the other core.
func (s *CoreService) emitFor(kind coreruntime.Kind, info model.CoreInfo) {
	if s.activeKind() != kind {
		return
	}
	s.emit(info)
}

func (s *CoreService) emit(info model.CoreInfo) {
	if app := application.Get(); app != nil {
		app.Event.Emit("core:state", info)
	}
}
