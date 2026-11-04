import { createEffect, createSignal, For, onCleanup, onMount, Show } from "solid-js";

import { Events, Window as AppWindow } from "@wailsio/runtime";

import { AppService } from "../bindings/github.com/MardukPrime/Fast-V/internal/service/app";
import { CoreService } from "../bindings/github.com/MardukPrime/Fast-V/internal/service/core";
import { ProxyService } from "../bindings/github.com/MardukPrime/Fast-V/internal/service/proxy";
import { SettingsService } from "../bindings/github.com/MardukPrime/Fast-V/internal/service/settings";
import type { AppInfo, CoreInfo, ProxyState, ProxyTestResult, Settings } from "../bindings/github.com/MardukPrime/Fast-V/internal/model";
import Icon from "./components/Icon";
import type { IconName } from "./components/Icon";
import DashboardPage from "./pages/DashboardPage";
import LogsPage from "./pages/LogsPage";
import NetworkPage from "./pages/NetworkPage";
import RoutingPage from "./pages/RoutingPage";
import ServersPage from "./pages/ServersPage";
import SettingsPage from "./pages/SettingsPage";
import SubscriptionsPage from "./pages/SubscriptionsPage";

type PageKey = "dashboard" | "servers" | "subscriptions" | "routing" | "speedtest" | "logs" | "settings";

const navigation: Array<{ key: PageKey; label: string; caption: string; icon: IconName }> = [
  { key: "dashboard", label: "概览", caption: "运行状态", icon: "dashboard" },
  { key: "servers", label: "服务器", caption: "节点列表", icon: "server" },
  { key: "subscriptions", label: "订阅", caption: "远程配置", icon: "subscription" },
  { key: "routing", label: "路由", caption: "分流规则", icon: "route" },
  { key: "speedtest", label: "网络诊断", caption: "IP 与质量", icon: "speed" },
  { key: "logs", label: "日志", caption: "运行记录", icon: "logs" },
  { key: "settings", label: "设置", caption: "应用偏好", icon: "settings" },
];

const dockNavigation = navigation;

const pageMeta: Record<PageKey, { title: string; subtitle: string }> = {
  dashboard: { title: "概览", subtitle: "FastV 当前状态与快捷操作" },
  servers: { title: "服务器", subtitle: "查看节点、切换当前节点和测试连通性" },
  subscriptions: { title: "订阅", subtitle: "导入、更新和查看订阅节点" },
  routing: { title: "路由", subtitle: "按顺序编辑域名、IP、端口和网络规则" },
  speedtest: { title: "网络诊断", subtitle: "检测出口 IP、归属地、延迟和下载速度" },
  logs: { title: "日志", subtitle: "查看、筛选和复制 Xray 运行日志" },
  settings: { title: "设置", subtitle: "客户端与内核参数分开管理" },
};

function applyTheme(theme: string) {
  const resolved = theme === "system"
    ? (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light")
    : theme;
  document.documentElement.dataset.theme = resolved;
}

export default function App() {
  const [active, setActive] = createSignal<PageKey>("dashboard");
  const [info, setInfo] = createSignal<AppInfo | null>(null);
  const [settings, setSettings] = createSignal<Settings | null>(null);
  const [core, setCore] = createSignal<CoreInfo | null>(null);
  const [proxy, setProxy] = createSignal<ProxyState>({ status: "stopped", nodeId: "", nodeName: "", nodeProtocol: "", nodeAddress: "", nodePort: 0, nodeSubscription: "", resolvedIp: "", proxyMode: "", inbound: "", localAddr: "", startedAt: "", error: "", pid: 0 });
  const [proxyBusy, setProxyBusy] = createSignal(false);
  const [proxyError, setProxyError] = createSignal("");
  const [proxyTest, setProxyTest] = createSignal<ProxyTestResult | null>(null);
  const layoutMode = () => settings()?.layoutMode === "dock" ? "dock" : "sidebar";
  let dockNavElement: HTMLDivElement | undefined;
  const [dockIndicator, setDockIndicator] = createSignal({ left: 0, width: 0, visible: false });

  const updateDockIndicator = () => {
    if (!dockNavElement) {
      setDockIndicator((current) => ({ ...current, visible: false }));
      return;
    }
    const buttons = dockNavElement.querySelectorAll<HTMLButtonElement>(".liquid-nav-item");
    const index = dockNavigation.findIndex((item) => item.key === active());
    const button = buttons[index];
    if (!button) {
      setDockIndicator((current) => ({ ...current, visible: false }));
      return;
    }
    setDockIndicator({ left: button.offsetLeft, width: button.offsetWidth, visible: true });
  };

  createEffect(() => {
    active();
    layoutMode();
    requestAnimationFrame(updateDockIndicator);
  });

  onMount(() => {
    const handleResize = () => updateDockIndicator();
    window.addEventListener("resize", handleResize);
    onCleanup(() => window.removeEventListener("resize", handleResize));
  });

  onMount(() => {
    let frame = 0;
    let lastEvent: PointerEvent | null = null;
    const glassSelector = ".panel, .glass-panel, .sidebar, .window-titlebar, .mini-stat, .quick-action, .settings-section, .core-summary, .status-chip, .theme-option, .mode-option, .core-type-option, .liquid-dock";
    const updateGlass = () => {
      frame = 0;
      if (!lastEvent) return;
      const target = lastEvent.target instanceof Element ? lastEvent.target.closest<HTMLElement>(glassSelector) : null;
      if (!target) return;
      const rect = target.getBoundingClientRect();
      target.style.setProperty("--glass-x", `${lastEvent.clientX - rect.left}px`);
      target.style.setProperty("--glass-y", `${lastEvent.clientY - rect.top}px`);
    };
    const handlePointerMove = (event: PointerEvent) => {
      lastEvent = event;
      if (!frame) frame = requestAnimationFrame(updateGlass);
    };
    document.addEventListener("pointermove", handlePointerMove, { passive: true });
    onCleanup(() => {
      document.removeEventListener("pointermove", handlePointerMove);
      if (frame) cancelAnimationFrame(frame);
    });
  });

  onMount(async () => {
    try {
      const [appInfo, loadedSettings, coreInfo, proxyState] = await Promise.all([
        AppService.Info(),
        SettingsService.Get(),
        CoreService.Info(),
        ProxyService.State(),
      ]);
      setInfo(appInfo);
      setSettings(loadedSettings);
      setCore(coreInfo);
      setProxy(proxyState);
      applyTheme(loadedSettings.theme);
    } catch (error) {
      console.error("Failed to load application state", error);
    }
  });

  onMount(() => {
    const offCore = Events.On("core:state", (event) => {
      setCore(event.data as CoreInfo);
    });
    const offProxy = Events.On("proxy:state", (event) => {
      setProxy(event.data as ProxyState);
    });
    const systemTheme = window.matchMedia("(prefers-color-scheme: dark)");
    const handleSystemTheme = () => {
      if (settings()?.theme === "system") applyTheme("system");
    };
    systemTheme.addEventListener("change", handleSystemTheme);
    onCleanup(() => {
      offCore();
      offProxy();
      systemTheme.removeEventListener("change", handleSystemTheme);
    });
  });

  const handleStartProxy = async () => {
    setProxyBusy(true);
    setProxyError("");
    setProxyTest(null);
    try {
      setProxy(await ProxyService.Start());
    } catch (error) {
      setProxyError(error instanceof Error ? error.message : String(error));
    } finally {
      setProxyBusy(false);
    }
  };

  const handleStopProxy = async () => {
    setProxyBusy(true);
    setProxyError("");
    setProxyTest(null);
    try {
      setProxy(await ProxyService.Stop());
    } catch (error) {
      setProxyError(error instanceof Error ? error.message : String(error));
    } finally {
      setProxyBusy(false);
    }
  };

  const handleTestProxy = async () => {
    setProxyBusy(true);
    setProxyError("");
    try {
      const result = await ProxyService.Test();
      setProxyTest(result);
      if (!result.success) setProxyError(result.error);
    } catch (error) {
      setProxyError(error instanceof Error ? error.message : String(error));
    } finally {
      setProxyBusy(false);
    }
  };
  const handleSettingsSaved = (next: Settings) => {
    setSettings(next);
    applyTheme(next.theme);
  };

  const toggleMaximise = async () => {
    if (await AppWindow.IsMaximised()) {
      await AppWindow.Restore();
    } else {
      await AppWindow.Maximise();
    }
  };

  return (
    <div classList={{ "app-root": true, "layout-sidebar": layoutMode() === "sidebar", "layout-dock": layoutMode() === "dock" }}>
      <svg class="liquid-glass-defs" aria-hidden="true">
        <defs>
          <filter id="liquid-glass-refraction" x="-20%" y="-20%" width="140%" height="140%" color-interpolation-filters="sRGB">
            <feTurbulence type="fractalNoise" baseFrequency="0.008 0.012" numOctaves="2" seed="11" result="noise" />
            <feGaussianBlur in="noise" stdDeviation="1.2" result="softNoise" />
            <feDisplacementMap in="SourceGraphic" in2="softNoise" scale="7" xChannelSelector="R" yChannelSelector="G" />
          </filter>
        </defs>
      </svg>
      <header class="window-titlebar" onDblClick={toggleMaximise}>
        <div class="titlebar-brand">
          <span class="titlebar-mark">F</span>
          <span>FastV</span>
        </div>
        <div class="titlebar-drag" />
        <div class="window-controls">
          <button type="button" class="window-control" aria-label="最小化" onClick={() => AppWindow.Minimise()}>
            <Icon name="minimize" size={11} />
          </button>
          <button type="button" class="window-control" aria-label="最大化或还原" onClick={toggleMaximise}>
            <Icon name="maximize" size={10} />
          </button>
          <button type="button" class="window-control close" aria-label="关闭" onClick={() => AppWindow.Close()}>
            <Icon name="close" size={11} />
          </button>
        </div>
      </header>

      <div class="app-shell">
        <Show when={layoutMode() === "sidebar"}>
        <aside class="sidebar">
          <div class="brand">
            <div class="brand-mark">F</div>
            <div>
              <div class="brand-name">FastV</div>
              <div class="brand-caption">Proxy Client</div>
            </div>
          </div>

          <div class="nav-section-label">工作区</div>
          <nav class="nav">
            {navigation.map((item) => (
              <button
                classList={{ "nav-item": true, active: active() === item.key }}
                onClick={() => setActive(item.key)}
              >
                <span class="nav-icon"><Icon name={item.icon} size={17} /></span>
                <span class="nav-copy">
                  <span class="nav-label">{item.label}</span>
                  <span class="nav-caption">{item.caption}</span>
                </span>
                <Show when={active() === item.key}>
                  <span class="nav-active-dot" />
                </Show>
              </button>
            ))}
          </nav>

          <div class="sidebar-footer">
            <div class="footer-status">
              <span classList={{ "status-dot": true, online: core()?.installed, idle: !core()?.installed }} />
              <div>
                <strong>{core()?.installed ? `Xray ${core()?.version}` : "Xray 未安装"}</strong>
                <small>{core()?.installed ? "内核已就绪" : "可在设置中下载"}</small>
              </div>
            </div>
            <div class="footer-version">v{info()?.version ?? "0.1.0"}</div>
          </div>
        </aside>
        </Show>

        <main class="content">
          <header class="topbar">
            <div class="topbar-title">
              <span class="breadcrumb">FastV / {pageMeta[active()].title}</span>
              <h1>{pageMeta[active()].title}</h1>
              <p>{pageMeta[active()].subtitle}</p>
            </div>
            <div class="topbar-actions">
              <span class="status-chip"><span class="status-dot online" />本地数据已就绪</span>
              <span classList={{ "status-chip": true, muted: !core()?.installed }}>
                <span classList={{ "status-dot": true, online: core()?.installed, idle: !core()?.installed }} />
                {core()?.installed ? `Xray ${core()?.version}` : "Xray 未安装"}
              </span>
              <span classList={{ "status-chip": true, "proxy-running": proxy().status === "running" }}>
                <span classList={{ "status-dot": true, online: proxy().status === "running", idle: proxy().status !== "running" }} />
                {proxy().status === "running" ? `代理 ${proxy().localAddr}` : proxy().status === "starting" ? "代理启动中" : proxy().status === "error" ? "代理异常" : "代理未启动"}
              </span>
            </div>
          </header>

          <Show when={active() === "dashboard"}>
            <DashboardPage
              info={info()}
              settings={settings()}
              proxyState={proxy()}
              proxyBusy={proxyBusy()}
              proxyError={proxyError()}
              proxyTest={proxyTest()}
              onStartProxy={handleStartProxy}
              onStopProxy={handleStopProxy}
              onTestProxy={handleTestProxy}
              onNavigate={(page) => setActive(page as PageKey)}
            />
          </Show>
          <Show when={active() === "servers"}>
            <ServersPage onNavigate={(page) => setActive(page as PageKey)} />
          </Show>
          <Show when={active() === "subscriptions"}>
            <SubscriptionsPage />
          </Show>
          <Show when={active() === "speedtest"}>
            <NetworkPage proxyState={proxy()} />
          </Show>
          <Show when={active() === "routing"}>
            <RoutingPage proxyState={proxy()} proxyMode={settings()?.proxyMode ?? "rule"} />
          </Show>
          <Show when={active() === "logs"}>
            <LogsPage proxyState={proxy()} />
          </Show>
          <Show when={active() === "settings"}>
            <SettingsPage onSaved={handleSettingsSaved} onPreviewTheme={applyTheme} />
          </Show>

        </main>

          <Show when={layoutMode() === "dock"}>
          <nav
            class="liquid-dock"
            aria-label="底部快捷导航"
            onPointerMove={(event) => {
              const rect = event.currentTarget.getBoundingClientRect();
              event.currentTarget.style.setProperty("--liquid-x", `${event.clientX - rect.left}px`);
              event.currentTarget.style.setProperty("--liquid-y", `${event.clientY - rect.top}px`);
            }}
          >
            <div class="liquid-dock-nav" ref={(element) => (dockNavElement = element)}>
              <span class="liquid-nav-indicator" aria-hidden="true" style={{ transform: `translateX(${dockIndicator().left}px)`, width: `${dockIndicator().width}px`, opacity: dockIndicator().visible ? 1 : 0 }} />
              <For each={dockNavigation}>
                {(item) => (
                  <button type="button" classList={{ "liquid-nav-item": true, active: active() === item.key }} onClick={() => setActive(item.key)}>
                    <Icon name={item.icon} size={16} />
                    <span>{item.label}</span>
                  </button>
                )}
              </For>
            </div>

            <div class="liquid-dock-divider" />

            <div class="liquid-dock-status">
              <span classList={{ "status-dot": true, online: proxy().status === "running", idle: proxy().status !== "running" }} />
              <div>
                <strong>{proxy().status === "running" ? "代理运行中" : proxy().status === "starting" ? "正在启动" : "代理未启动"}</strong>
                <small>{proxy().status === "running" ? proxy().localAddr : "本地 SOCKS/HTTP 入口"}</small>
              </div>
            </div>

            <div class="liquid-dock-node">
              <span>当前节点</span>
              <strong title={proxy().nodeName || "未选择节点"}>{proxy().nodeName || "未选择节点"}</strong>
            </div>

            <div class="liquid-dock-actions">
              <button class="liquid-action" type="button" onClick={handleTestProxy} disabled={proxyBusy() || proxy().status !== "running"}>
                <Icon name="speed" size={14} /> 测试
              </button>
              <button classList={{ "liquid-action": true, primary: proxy().status !== "running" }} type="button" onClick={proxy().status === "running" ? handleStopProxy : handleStartProxy} disabled={proxyBusy() || proxy().status === "starting" || proxy().status === "stopping"}>
                <Icon name={proxy().status === "running" ? "stop" : "play"} size={14} />
                {proxyBusy() ? "处理中" : proxy().status === "running" ? "停止" : "启动"}
              </button>
            </div>
          </nav>
          </Show>
      </div>
    </div>
  );
}
