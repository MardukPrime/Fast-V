package bootstrap

import (
	"embed"
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"runtime/debug"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MardukPrime/Fast-V/internal/applog"
	"github.com/MardukPrime/Fast-V/internal/buildinfo"
	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/platform"
	"github.com/MardukPrime/Fast-V/internal/selfcheck"
	appservice "github.com/MardukPrime/Fast-V/internal/service/app"
	backupservice "github.com/MardukPrime/Fast-V/internal/service/backup"
	"github.com/MardukPrime/Fast-V/internal/service/core"
	"github.com/MardukPrime/Fast-V/internal/service/dashboard"
	diagnosticsservice "github.com/MardukPrime/Fast-V/internal/service/diagnostics"
	environment "github.com/MardukPrime/Fast-V/internal/service/environment"
	floatwinservice "github.com/MardukPrime/Fast-V/internal/service/floatwin"
	historyservice "github.com/MardukPrime/Fast-V/internal/service/history"
	"github.com/MardukPrime/Fast-V/internal/service/node"
	nodegroupservice "github.com/MardukPrime/Fast-V/internal/service/nodegroup"
	proxyservice "github.com/MardukPrime/Fast-V/internal/service/proxy"
	rescue "github.com/MardukPrime/Fast-V/internal/service/rescue"
	routingservice "github.com/MardukPrime/Fast-V/internal/service/routing"
	ruledebug "github.com/MardukPrime/Fast-V/internal/service/ruledebug"
	rulesetservice "github.com/MardukPrime/Fast-V/internal/service/ruleset"
	selfcheckservice "github.com/MardukPrime/Fast-V/internal/service/selfcheck"
	"github.com/MardukPrime/Fast-V/internal/service/settings"
	"github.com/MardukPrime/Fast-V/internal/service/subscription"
	"github.com/MardukPrime/Fast-V/internal/store"
)

// Run assembles the application, services and window, then starts the Wails event loop.
func Run(assets embed.FS) (runErr error) {
	// Android: resolve the app-private data dir from the Java bridge before the
	// store picks its location.
	prepareMobileDataDir()
	// The file logger is created after the services so it can receive their log
	// sink. Every exit path (normal return, early return, panic) runs this
	// cleanup: the exit reason is written, the file is closed, and a recovered
	// panic is re-raised so the crash behavior of the application is unchanged.
	var logger *applog.Logger
	defer func() {
		recovered := recover()
		if recovered != nil {
			if logger != nil {
				logger.WriteLine("error", "panic", fmt.Sprintf("应用崩溃: %v | 堆栈: %s", recovered, debug.Stack()))
			} else {
				log.Printf("应用崩溃: %v\n%s", recovered, debug.Stack())
			}
		} else if logger != nil {
			if runErr != nil {
				logger.WriteLine("error", "app", "FastV 退出，错误: "+runErr.Error())
			} else {
				logger.WriteLine("info", "app", "FastV 已退出")
			}
		}
		if logger != nil {
			if closeErr := logger.Close(); closeErr != nil {
				log.Printf("applog: 关闭日志文件失败: %v", closeErr)
			}
		}
		if recovered != nil {
			panic(recovered)
		}
	}()

	st, err := store.NewStore()
	if err != nil {
		return err
	}

	if st.Settings().TunMode && !platform.IsElevated() {
		if err := platform.LaunchElevatedRestart(); err == nil {
			return nil
		}
	}

	appSvc := appservice.New(st)
	coreSvc := core.NewCoreService(st)
	proxySvc := proxyservice.New(st, coreSvc)
	dashboardSvc := dashboard.New(st, coreSvc)
	subSvc := subscription.New(st, proxySvc)
	nodeSvc := node.New(st, coreSvc)
	settingsSvc := settings.New(st)
	routingSvc := routingservice.New(st)
	backupSvc := backupservice.New(st)
	historySvc := historyservice.New(st)
	selfcheckSvc := selfcheckservice.New(selfcheck.NewChecker(st, coreSvc, proxySvc))
	diagnosticsSvc := diagnosticsservice.New(proxySvc, diagnosticsservice.ReportSources{
		Store:     st,
		CoreInfo:  coreSvc.Info,
		AppInfo:   appSvc.Info,
		SelfCheck: diagnosticsservice.SelfCheckFunc(selfcheckSvc.Run),
	})
	nodeGroupSvc := nodegroupservice.New(st, proxySvc)
	ruleSetSvc := rulesetservice.New(st)
	rescueSvc := rescue.New(st, proxySvc)
	ruleDebugSvc := ruledebug.New(st, ruleSetSvc.ExpandedRules)
	environmentSvc := environment.New(st)
	floatWinSvc := floatwinservice.New(st)
	// Rule-set subscriptions are expanded into routing rules at configuration
	// build time; the provider keeps the generated config independent of which
	// sets happened to download successfully.
	proxySvc.SetExtraRulesProvider(ruleSetSvc.ExpandedRules)

	// Persist the runtime log next to the user data so after-sales diagnostics
	// survive restarts; the in-memory proxy log and the diagnostics report only
	// cover the current session. A logging failure is reported but must never
	// block startup.
	logger, err = applog.Open(filepath.Join(st.DataDir(), "logs"), 5<<20, 3)
	if err != nil {
		log.Printf("applog: 初始化文件日志失败: %v", err)
		logger = nil
	} else {
		logger.WriteLine("info", "app", fmt.Sprintf(
			"FastV 启动: 版本=%s commit=%s 构建时间=%s 平台=%s/%s 数据目录=%s",
			buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate, runtime.GOOS, runtime.GOARCH, st.DataDir()))
		proxySvc.SetLogSink(logger.WriteEntry)
	}

	// Deep links arrive from the fastv:// protocol handler (first launch and
	// second instance launches) and from share links passed on the command line.
	// Window-level settings (currently the floating traffic ball) are applied
	// after the settings service persists and broadcasts a change.
	settingsSvc.SetOnSaved(func(settings model.Settings) {
		if desktopIntegrationSupported() {
			floatWinSvc.Sync(settings.FloatWindowEnabled)
		}
	})

	deepLinks := newDeepLinkIntegration(subSvc, nodeSvc)
	deepLinks.registerURLScheme()

	app := application.New(application.Options{
		Name:        buildinfo.ProductName,
		Description: "A modern proxy client built with Wails v3",
		Services: []application.Service{
			application.NewService(appSvc),
			application.NewService(coreSvc),
			application.NewService(proxySvc),
			application.NewService(diagnosticsSvc),
			application.NewService(dashboardSvc),
			application.NewService(subSvc),
			application.NewService(nodeSvc),
			application.NewService(settingsSvc),
			application.NewService(routingSvc),
			application.NewService(backupSvc),
			application.NewService(historySvc),
			application.NewService(selfcheckSvc),
			application.NewService(nodeGroupSvc),
			application.NewService(ruleSetSvc),
			application.NewService(rescueSvc),
			application.NewService(ruleDebugSvc),
			application.NewService(environmentSvc),
			application.NewService(floatWinSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:               buildinfo.ProductID,
			OnSecondInstanceLaunch: deepLinks.onSecondInstanceLaunch,
		},
		Windows: application.WindowsOptions{
			WebviewUserDataPath: filepath.Join(st.DataDir(), "webview2"),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            buildinfo.ProductName,
		Width:            1280,
		Height:           800,
		MinWidth:         960,
		MinHeight:        640,
		Frameless:        true,
		BackgroundColour: application.NewRGB(238, 242, 247),
		URL:              "/",
	})

	deepLinks.setWindow(window)
	deepLinks.handleStartupArgs()

	// The tray and the close/minimize-to-tray behavior are wired after the
	// window exists and before the event loop starts; Wails creates the tray
	// icon itself once the application is running. The global hotkeys are
	// registered afterwards so a shortcut can drive the proxy and the tray at
	// the same time.
	if desktopIntegrationSupported() {
		newTray(app, st, window, proxySvc, settingsSvc, appSvc)
		startGlobalHotkeys(st, settingsSvc, proxySvc)
	}

	if desktopIntegrationSupported() {
		// The floating ball opens the main window on click and follows the
		// persisted toggle; enabling it must survive restarts.
		floatWinSvc.OpenMainHandler = func() {
			window.UnMinimise()
			window.Show()
			window.Focus()
		}
		floatWinSvc.Sync(st.Settings().FloatWindowEnabled)
	}

	return app.Run()
}
