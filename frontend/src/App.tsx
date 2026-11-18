import { createEffect, createSignal, For, onCleanup, onMount, Show } from "solid-js";

import { Events, Window as AppWindow } from "@wailsio/runtime";

import { AppService } from "../bindings/github.com/MardukPrime/Fast-V/internal/service/app";
import { CoreService } from "../bindings/github.com/MardukPrime/Fast-V/internal/service/core";
import { ProxyService } from "../bindings/github.com/MardukPrime/Fast-V/internal/service/proxy";
import { SettingsService } from "../bindings/github.com/MardukPrime/Fast-V/internal/service/settings";
import type { AppInfo, CoreInfo, ProxyState, ProxyTestResult, Settings } from "../bindings/github.com/MardukPrime/Fast-V/internal/model";
import Icon from "./components/Icon";
import type { IconName } from "./components/Icon";
import { language, setLanguage, t } from "./i18n";
import DashboardPage from "./pages/DashboardPage";
import LogsPage from "./pages/LogsPage";
import NetworkPage from "./pages/NetworkPage";
import RoutingPage from "./pages/RoutingPage";
import ServersPage from "./pages/ServersPage";
import SettingsPage from "./pages/SettingsPage";
import SubscriptionsPage from "./pages/SubscriptionsPage";

type PageKey = "dashboard" | "servers" | "subscriptions" | "routing" | "speedtest" | "logs" | "settings";

// Navigation and page metadata are accessors, not module-level constants, so a
// language switch immediately re-renders every translated label.
const navigation = (): Array<{ key: PageKey; label: string; caption: string; icon: IconName }> => [
  { key: "dashboard", label: t("shell.nav.dashboard"), caption: t("shell.nav.dashboardCaption"), icon: "dashboard" },
  { key: "servers", label: t("shell.nav.servers"), caption: t("shell.nav.serversCaption"), icon: "server" },
  { key: "subscriptions", label: t("shell.nav.subscriptions"), caption: t("shell.nav.subscriptionsCaption"), icon: "subscription" },
  { key: "routing", label: t("shell.nav.routing"), caption: t("shell.nav.routingCaption"), icon: "route" },
  { key: "speedtest", label: t("shell.nav.speedtest"), caption: t("shell.nav.speedtestCaption"), icon: "speed" },
  { key: "logs", label: t("shell.nav.logs"), caption: t("shell.nav.logsCaption"), icon: "logs" },
  { key: "settings", label: t("shell.nav.settings"), caption: t("shell.nav.settingsCaption"), icon: "settings" },
];

const dockNavigation = () => navigation();

const pageMeta = (): Record<PageKey, { title: string; subtitle: string }> => ({
  dashboard: { title: t("shell.nav.dashboard"), subtitle: t("shell.page.dashboardSubtitle") },
  servers: { title: t("shell.nav.servers"), subtitle: t("shell.page.serversSubtitle") },
  subscriptions: { title: t("shell.nav.subscriptions"), subtitle: t("shell.page.subscriptionsSubtitle") },
  routing: { title: t("shell.nav.routing"), subtitle: t("shell.page.routingSubtitle") },
  speedtest: { title: t("shell.nav.speedtest"), subtitle: t("shell.page.speedtestSubtitle") },
  logs: { title: t("shell.nav.logs"), subtitle: t("shell.page.logsSubtitle") },
  settings: { title: t("shell.nav.settings"), subtitle: t("shell.page.settingsSubtitle") },
});

function applyTheme(theme: string) {
  const resolved = theme === "system"
    ? (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light")
    : theme;
  document.documentElement.dataset.theme = resolved;
}

// Accent presets must stay in sync with the backend validation in
// internal/service/settings. Anything outside the whitelist falls back to the
// default sky palette so the stylesheet always has a matching theme layer.
const accentColors = ["sky", "violet", "emerald", "amber", "rose"];

function applyAccent(accent: string | undefined) {
  document.documentElement.dataset.accent = accent && accentColors.includes(accent) ? accent : "sky";
}

// Proxy mode quick switch in the global topbar. Values must stay in sync with
// the backend validation used by the settings page.
const proxyModes = (): Array<{ value: string; label: string; title: string }> => [
  { value: "rule", label: t("shell.proxyModeRule"), title: t("shell.proxyModeRuleTitle") },
  { value: "global", label: t("shell.proxyModeGlobal"), title: t("shell.proxyModeGlobalTitle") },
  { value: "direct", label: t("shell.proxyModeDirect"), title: t("shell.proxyModeDirectTitle") },
];

function normalizeProxyMode(mode: string | undefined) {
  return mode === "global" || mode === "direct" ? mode : "rule";
}

function proxyModeTitle(mode: string) {
  return proxyModes().find((item) => item.value === mode)?.title ?? t("shell.proxyMode");
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

// Keep the untranslated backend error separate from the translation key so the
// banner re-renders correctly after a language switch.
type ProxyModeError = { kind: "restart" | "switch"; detail: string };

export default function App() {
  const [active, setActive] = createSignal<PageKey>("dashboard");
  const [info, setInfo] = createSignal<AppInfo | null>(null);
  const [settings, setSettings] = createSignal<Settings | null>(null);
  const [core, setCore] = createSignal<CoreInfo | null>(null);
  const [proxy, setProxy] = createSignal<ProxyState>({ status: "stopped", nodeId: "", nodeName: "", nodeProtocol: "", nodeAddress: "", nodePort: 0, nodeSubscription: "", resolvedIp: "", proxyMode: "", inbound: "", localAddr: "", startedAt: "", error: "", pid: 0 });
  const [proxyBusy, setProxyBusy] = createSignal(false);
  const [proxyError, setProxyError] = createSignal("");
  const [proxyTest, setProxyTest] = createSignal<ProxyTestResult | null>(null);
  const [proxyModeBusy, setProxyModeBusy] = createSignal(false);
  const [proxyModeError, setProxyModeError] = createSignal<ProxyModeError | null>(null);
  const [dockTransparency, setDockTransparency] = createSignal(28);
  const layoutMode = () => settings()?.layoutMode === "sidebar" ? "sidebar" : "dock";
  const activeMeta = () => pageMeta()[active()];
  let dockNavElement: HTMLDivElement | undefined;
  const [dockIndicator, setDockIndicator] = createSignal({ left: 0, width: 0, visible: false });

  const updateDockIndicator = () => {
    if (!dockNavElement) {
      setDockIndicator((current) => ({ ...current, visible: false }));
      return;
    }
    const buttons = dockNavElement.querySelectorAll<HTMLButtonElement>(".liquid-nav-item");
    const index = dockNavigation().findIndex((item) => item.key === active());
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
    // Dock labels change width with the language, so re-measure the indicator.
    language();
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
      setDockTransparency(loadedSettings.dockTransparency || 28);
      applyTheme(loadedSettings.theme);
      applyAccent(loadedSettings.accentColor);
      setLanguage(loadedSettings.language);
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
    // Settings can also change outside this window: the tray menu and the
    // global shortcuts save through the same service and broadcast the result.
    // Re-apply the visual tokens so accent/theme/layout never drift.
    const offSettings = Events.On("settings:changed", (event) => {
      const next = event.data as Settings;
      if (!next) return;
      setSettings(next);
      setDockTransparency(next.dockTransparency || 28);
      applyTheme(next.theme);
      applyAccent(next.accentColor);
      setLanguage(next.language);
    });
    const systemTheme = window.matchMedia("(prefers-color-scheme: dark)");
    const handleSystemTheme = () => {
      if (settings()?.theme === "system") applyTheme("system");
    };
    systemTheme.addEventListener("change", handleSystemTheme);
    onCleanup(() => {
      offCore();
      offProxy();
      offSettings();
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
  const currentProxyMode = () => normalizeProxyMode(settings()?.proxyMode);

  // Switching mode saves the full settings object (the service expects a
  // complete payload) and restarts a running proxy so the new mode takes effect
  // immediately. Errors surface as a lightweight bar right under the topbar.
  const handleProxyModeChange = async (mode: string) => {
    const current = settings();
    if (!current || current.proxyMode === mode || proxyModeBusy()) return;
    setProxyModeBusy(true);
    setProxyModeError(null);
    try {
      const saved = await SettingsService.Save({ ...current, proxyMode: mode });
      setSettings(saved);
      if (proxy().status === "running") {
        try {
          setProxy(await ProxyService.Restart());
        } catch (error) {
          setProxyModeError({ kind: "restart", detail: errorMessage(error) });
        }
      }
    } catch (error) {
      setProxyModeError({ kind: "switch", detail: errorMessage(error) });
    } finally {
      setProxyModeBusy(false);
    }
  };

  const handleSettingsSaved = (next: Settings) => {
    setSettings(next);
    setDockTransparency(next.dockTransparency || 28);
    applyTheme(next.theme);
    applyAccent(next.accentColor);
    setLanguage(next.language);
    // A successful full save makes any earlier quick-switch error stale.
    setProxyModeError(null);
  };

  const dockGlassStyle = () => {
    const alpha = Math.max(0.1, Math.min(1, 1 - dockTransparency() / 100));
    return [
      `--dock-a1:${(0.21 * alpha).toFixed(3)}`,
      `--dock-a2:${(0.15 * alpha).toFixed(3)}`,
      `--dock-a3:${(0.035 * alpha).toFixed(3)}`,
      `--dock-a4:${(0.07 * alpha).toFixed(3)}`,
      `--dock-l1:${(0.8 * alpha).toFixed(3)}`,
      `--dock-l2:${(0.72 * alpha).toFixed(3)}`,
      `--dock-l3:${(0.42 * alpha).toFixed(3)}`,
    ].join(";");
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
          <button type="button" class="window-control" aria-label={t("shell.windowMinimize")} onClick={() => AppWindow.Minimise()}>
            <Icon name="minimize" size={11} />
          </button>
          <button type="button" class="window-control" aria-label={t("shell.windowMaximizeOrRestore")} onClick={toggleMaximise}>
            <Icon name="maximize" size={10} />
          </button>
          <button type="button" class="window-control close" aria-label={t("shell.windowClose")} onClick={() => AppWindow.Close()}>
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
              <div class="brand-caption">{t("shell.brandTagline")}</div>
            </div>
          </div>

          <div class="nav-section-label">{t("shell.workspace")}</div>
          <nav class="nav">
            {navigation().map((item) => (
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
                <strong>{core()?.installed ? t("shell.xrayVersion", { version: core()?.version ?? "" }) : t("shell.coreMissing")}</strong>
                <small>{core()?.installed ? t("shell.coreReady") : t("shell.coreHint")}</small>
              </div>
            </div>
            <div class="footer-version">v{info()?.version ?? "0.1.0"}</div>
          </div>
        </aside>
        </Show>

        <main class="content">
          <header class="topbar">
            <div class="topbar-title">
              <span class="breadcrumb">FastV / {activeMeta().title}</span>
              <h1>{activeMeta().title}</h1>
              <p>{activeMeta().subtitle}</p>
            </div>
            <div class="topbar-actions">
              <div classList={{ "topbar-mode": true, switching: proxyModeBusy() }} role="group" aria-label={t("shell.proxyMode")}>
                <div class="topbar-mode-switch">
                  <For each={proxyModes()}>
                    {(option) => (
                      <button
                        type="button"
                        aria-pressed={currentProxyMode() === option.value}
                        title={option.title}
                        classList={{ "topbar-mode-option": true, active: currentProxyMode() === option.value }}
                        disabled={proxyModeBusy() || !settings()}
                        onClick={() => { void handleProxyModeChange(option.value); }}
                      >
                        <span class="topbar-mode-dot" aria-hidden="true" />
                        {option.label}
                      </button>
                    )}
                  </For>
                </div>
                <select
                  class="topbar-mode-select"
                  aria-label={t("shell.proxyMode")}
                  title={proxyModeTitle(currentProxyMode())}
                  value={currentProxyMode()}
                  disabled={proxyModeBusy() || !settings()}
                  onChange={(event) => { void handleProxyModeChange(event.currentTarget.value); }}
                >
                  <For each={proxyModes()}>{(option) => <option value={option.value}>{option.label}</option>}</For>
                </select>
              </div>
              <span class="status-chip"><span class="status-dot online" />{t("shell.localDataReady")}</span>
              <span classList={{ "status-chip": true, muted: !core()?.installed }}>
                <span classList={{ "status-dot": true, online: core()?.installed, idle: !core()?.installed }} />
                {core()?.installed ? t("shell.xrayVersion", { version: core()?.version ?? "" }) : t("shell.coreMissing")}
              </span>
              <span classList={{ "status-chip": true, "proxy-running": proxy().status === "running" }}>
                <span classList={{ "status-dot": true, online: proxy().status === "running", idle: proxy().status !== "running" }} />
                {proxy().status === "running" ? t("shell.proxyRunning", { addr: proxy().localAddr }) : proxy().status === "starting" ? t("shell.proxyStarting") : proxy().status === "error" ? t("shell.proxyError") : t("shell.proxyStopped")}
              </span>
            </div>
          </header>

          <Show when={proxyModeError()}>
            {(modeError) => (
              <div class="topbar-mode-error" role="alert">
                <Icon name="alert" size={13} />
                <span>{modeError().kind === "restart"
                  ? t("shell.error.restartAfterMode", { error: modeError().detail })
                  : t("shell.error.switchMode", { error: modeError().detail })}</span>
                <button type="button" class="topbar-mode-error-dismiss" aria-label={t("shell.dismissNotice")} onClick={() => setProxyModeError(null)}>
                  <Icon name="close" size={11} />
                </button>
              </div>
            )}
          </Show>

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
            <SettingsPage onSaved={handleSettingsSaved} onPreviewTheme={applyTheme} onPreviewAccent={applyAccent} onPreviewLanguage={setLanguage} onPreviewDockTransparency={setDockTransparency} />
          </Show>

        </main>

          <Show when={layoutMode() === "dock"}>
          <nav
            class="liquid-dock"
            style={dockGlassStyle()}
            aria-label={t("shell.dockNav")}
            onPointerMove={(event) => {
              const rect = event.currentTarget.getBoundingClientRect();
              event.currentTarget.style.setProperty("--liquid-x", `${event.clientX - rect.left}px`);
              event.currentTarget.style.setProperty("--liquid-y", `${event.clientY - rect.top}px`);
            }}
          >
            <div class="liquid-dock-nav" ref={(element) => (dockNavElement = element)}>
              <span class="liquid-nav-indicator" aria-hidden="true" style={{ transform: `translateX(${dockIndicator().left}px)`, width: `${dockIndicator().width}px`, opacity: dockIndicator().visible ? 1 : 0 }} />
              <For each={dockNavigation()}>
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
                <strong>{proxy().status === "running" ? t("shell.dockRunning") : proxy().status === "starting" ? t("shell.dockStarting") : t("shell.dockStopped")}</strong>
                <small>{proxy().status === "running" ? proxy().localAddr : t("shell.dockEntry")}</small>
              </div>
            </div>

            <div class="liquid-dock-node">
              <span>{t("shell.currentNode")}</span>
              <strong title={proxy().nodeName || t("shell.noNode")}>{proxy().nodeName || t("shell.noNode")}</strong>
            </div>

            <div class="liquid-dock-actions">
              <button class="liquid-action" type="button" onClick={handleTestProxy} disabled={proxyBusy() || proxy().status !== "running"}>
                <Icon name="speed" size={14} /> {t("shell.test")}
              </button>
              <button classList={{ "liquid-action": true, primary: proxy().status !== "running" }} type="button" onClick={proxy().status === "running" ? handleStopProxy : handleStartProxy} disabled={proxyBusy() || proxy().status === "starting" || proxy().status === "stopping"}>
                <Icon name={proxy().status === "running" ? "stop" : "play"} size={14} />
                {proxyBusy() ? t("shell.working") : proxy().status === "running" ? t("shell.stop") : t("shell.start")}
              </button>
            </div>
          </nav>
          </Show>
      </div>
    </div>
  );
}
