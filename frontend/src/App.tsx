import { createSignal, onCleanup, onMount, Show } from "solid-js";

import { Events, Window as AppWindow } from "@wailsio/runtime";

import { AppService } from "../bindings/github.com/MardukPrime/Fast-V/internal/service/app";
import { CoreService } from "../bindings/github.com/MardukPrime/Fast-V/internal/service/core";
import { ProxyService } from "../bindings/github.com/MardukPrime/Fast-V/internal/service/proxy";
import { SettingsService } from "../bindings/github.com/MardukPrime/Fast-V/internal/service/settings";
import type { AppInfo, CoreInfo, ProxyState, ProxyTestResult, Settings } from "../bindings/github.com/MardukPrime/Fast-V/internal/model";
import Icon from "./components/Icon";
import type { IconName } from "./components/Icon";
import DashboardPage from "./pages/DashboardPage";
import PlaceholderPage from "./pages/PlaceholderPage";
import ServersPage from "./pages/ServersPage";
import SettingsPage from "./pages/SettingsPage";
import SubscriptionsPage from "./pages/SubscriptionsPage";

type PageKey = "dashboard" | "servers" | "subscriptions" | "routing" | "speedtest" | "logs" | "settings";

const navigation: Array<{ key: PageKey; label: string; caption: string; icon: IconName }> = [
  { key: "dashboard", label: "概览", caption: "运行状态", icon: "dashboard" },
  { key: "servers", label: "服务器", caption: "节点列表", icon: "server" },
  { key: "subscriptions", label: "订阅", caption: "远程配置", icon: "subscription" },
  { key: "routing", label: "路由", caption: "分流规则", icon: "route" },
  { key: "speedtest", label: "测速", caption: "延迟与速度", icon: "speed" },
  { key: "logs", label: "日志", caption: "运行记录", icon: "logs" },
  { key: "settings", label: "设置", caption: "应用偏好", icon: "settings" },
];

const pageMeta: Record<PageKey, { title: string; subtitle: string }> = {
  dashboard: { title: "概览", subtitle: "FastV 当前状态与快捷操作" },
  servers: { title: "服务器", subtitle: "查看节点、切换当前节点和测试连通性" },
  subscriptions: { title: "订阅", subtitle: "导入、更新和查看订阅节点" },
  routing: { title: "路由", subtitle: "规则编辑器将在后续版本接入" },
  speedtest: { title: "测速", subtitle: "节点测速将在后续版本接入" },
  logs: { title: "日志", subtitle: "内核日志将在后续版本接入" },
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
    onCleanup(() => {
      offCore();
      offProxy();
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
    <div class="app-root">
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
          <Show when={active() === "settings"}>
            <SettingsPage onSaved={handleSettingsSaved} />
          </Show>
          <Show when={active() !== "dashboard" && active() !== "servers" && active() !== "subscriptions" && active() !== "settings"}>
            <PlaceholderPage
              title={pageMeta[active()].title}
              description={pageMeta[active()].subtitle}
            />
          </Show>
        </main>
      </div>
    </div>
  );
}
