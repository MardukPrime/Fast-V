import { createEffect, createSignal, For, onCleanup, onMount, Show } from "solid-js";

import { Events, Window as AppWindow } from "@wailsio/runtime";

import { AppService } from "../bindings/github.com/MardukPrime/Fast-V/internal/service/app";
import { CoreService } from "../bindings/github.com/MardukPrime/Fast-V/internal/service/core";
import { ProxyService } from "../bindings/github.com/MardukPrime/Fast-V/internal/service/proxy";
import { SettingsService } from "../bindings/github.com/MardukPrime/Fast-V/internal/service/settings";
import type { AppInfo, CoreInfo, ProxyState, ProxyTestResult, Settings } from "../bindings/github.com/MardukPrime/Fast-V/internal/model";
import Icon from "./components/Icon";
import type { IconName } from "./components/Icon";
import { androidVpnConnect, androidVpnDisconnect, androidVpnSupported, onAndroidVpnResult, socksPortFromLocalAddr } from "./androidVpn";
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

// Payload shared by the deeplink:imported / deeplink:failed events the backend
// broadcasts after handling a fastv:// link.
type DeepLinkPayload = { kind: string; name: string; message: string };

// One entry of the deep link toast stack. The tone selects the success/error
// styling and the auto-dismiss delay.
type DeepLinkToast = { id: number; tone: "success" | "error"; kind: string; name: string; message: string };

// readDeepLinkPayload keeps the toast render resilient to malformed payloads by
// keeping only the string fields of the event data.
function readDeepLinkPayload(data: unknown): DeepLinkPayload {
  if (!data || typeof data !== "object") return { kind: "", name: "", message: "" };
  const record = data as Record<string, unknown>;
  const text = (key: string) => (typeof record[key] === "string" ? (record[key] as string) : "");
  return { kind: text("kind"), name: text("name"), message: text("message") };
}

// The backend message already names the imported item, so it is shown on its
// own; the name is only a fallback when a message is missing.
function deepLinkToastDetail(toast: DeepLinkToast): string {
  return (toast.message || toast.name).trim();
}

// First-run onboarding storage marker. The guide shows up until the marker
// exists; a storage failure counts as "already seen" so a locked-down webview
// can never trap the window behind a guide that cannot be dismissed for good.
const ONBOARDING_STORAGE_KEY = "fastv.onboarding.v1";

// Code-only re-open hook: any future entry point (for example an About panel)
// can bring the guide back with
// window.dispatchEvent(new Event(ONBOARDING_OPEN_EVENT)) without new props.
const ONBOARDING_OPEN_EVENT = "fastv:onboarding:open";

function onboardingSeen(): boolean {
  try {
    return window.localStorage.getItem(ONBOARDING_STORAGE_KEY) !== null;
  } catch {
    return true;
  }
}

function markOnboardingSeen(): void {
  try {
    window.localStorage.setItem(ONBOARDING_STORAGE_KEY, "1");
  } catch {
    // Storage may be unavailable; the overlay still closes for this session.
  }
}

type OnboardingMode = { label: string; description: string };

type OnboardingAction = { label: string; page: PageKey };

// One of the three onboarding cards. Optional groups let each card keep its own
// content shape (modes / supported formats / safety features) without new
// components or per-card props.
type OnboardingCard = {
  index: number;
  icon: IconName;
  title: string;
  lead: string;
  modes?: OnboardingMode[];
  formats?: string[];
  features?: string[];
  note?: string;
  logLine?: string;
  action?: OnboardingAction;
};

// Accessor, not a constant: a zh/en switch must re-render every onboarding
// string (the same pattern as navigation/pageMeta above).
function onboardingCards(): OnboardingCard[] {
  return [
    {
      index: 1,
      icon: "route",
      title: t("shell.onboarding.welcomeTitle"),
      lead: t("shell.onboarding.welcomeLead"),
      modes: [
        { label: t("shell.onboarding.modeRule"), description: t("shell.onboarding.modeRuleDesc") },
        { label: t("shell.onboarding.modeGlobal"), description: t("shell.onboarding.modeGlobalDesc") },
        { label: t("shell.onboarding.modeDirect"), description: t("shell.onboarding.modeDirectDesc") },
      ],
      note: t("shell.onboarding.themeHint"),
    },
    {
      index: 2,
      icon: "subscription",
      title: t("shell.onboarding.importTitle"),
      lead: t("shell.onboarding.importLead"),
      formats: [
        t("shell.onboarding.formatShare"),
        t("shell.onboarding.formatBase64"),
        t("shell.onboarding.formatClash"),
      ],
      action: { label: t("shell.onboarding.goImport"), page: "subscriptions" },
    },
    {
      index: 3,
      icon: "check",
      title: t("shell.onboarding.safeTitle"),
      lead: t("shell.onboarding.safeLead"),
      features: [
        t("shell.onboarding.safeSnapshot"),
        t("shell.onboarding.safeRescue"),
        t("shell.onboarding.safeDiagnostics"),
      ],
      logLine: t("shell.onboarding.logPath", { dir: t("shell.onboarding.dataDir") }),
      action: { label: t("shell.onboarding.openSettings"), page: "settings" },
    },
  ];
}

export default function App() {
  const [active, setActive] = createSignal<PageKey>("dashboard");
  const [info, setInfo] = createSignal<AppInfo | null>(null);
  const [settings, setSettings] = createSignal<Settings | null>(null);
  const [core, setCore] = createSignal<CoreInfo | null>(null);
  const [proxy, setProxy] = createSignal<ProxyState>({ status: "stopped", nodeId: "", nodeName: "", nodeProtocol: "", nodeAddress: "", nodePort: 0, nodeSubscription: "", resolvedIp: "", proxyMode: "", inbound: "", localAddr: "", startedAt: "", error: "", pid: 0, degraded: false });
  const [proxyBusy, setProxyBusy] = createSignal(false);
  const [proxyError, setProxyError] = createSignal("");
  const [proxyTest, setProxyTest] = createSignal<ProxyTestResult | null>(null);
  const [proxyModeBusy, setProxyModeBusy] = createSignal(false);
  const [proxyModeError, setProxyModeError] = createSignal<ProxyModeError | null>(null);
  // Deep link feedback is a capped stack: at most three toasts, newest last.
  const [deeplinkToasts, setDeeplinkToasts] = createSignal<DeepLinkToast[]>([]);
  let deeplinkToastId = 0;
  const deeplinkToastTimers = new Map<number, number>();
  // First-run guide state: the overlay opens after the shell has mounted when
  // the storage marker is missing. Every dismissal path writes the marker.
  const [onboardingOpen, setOnboardingOpen] = createSignal(false);
  const [androidMenuOpen, setAndroidMenuOpen] = createSignal(false);
  let onboardingCloseButton: HTMLButtonElement | undefined;
  const [dockTransparency, setDockTransparency] = createSignal(28);
  const layoutMode = () => settings()?.layoutMode === "sidebar" ? "sidebar" : "dock";
  // Android builds are a preview: the desktop proxy/TUN/tray capabilities are
  // unavailable, so the shell advertises that clearly and the layout hides
  // desktop-only chrome.
  // `?android=1` forces the Android shell in a desktop build so the mobile
  // layout can be reviewed with Chrome device emulation during development.
  const isAndroidPlatform = () =>
    info()?.platform === "android" || new URLSearchParams(window.location.search).has("android");
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

  // Deep link toast helpers. Success notices disappear after 6s, failures
  // linger for 8s; both can be dismissed early with the close button.
  const clearDeeplinkToastTimer = (id: number) => {
    const timer = deeplinkToastTimers.get(id);
    if (timer === undefined) return;
    window.clearTimeout(timer);
    deeplinkToastTimers.delete(id);
  };

  const dismissDeeplinkToast = (id: number) => {
    clearDeeplinkToastTimer(id);
    setDeeplinkToasts((current) => current.filter((toast) => toast.id !== id));
  };

  const pushDeeplinkToast = (toast: Omit<DeepLinkToast, "id">) => {
    const id = ++deeplinkToastId;
    const next = [...deeplinkToasts(), { ...toast, id }];
    // Cap the stack at three entries: the oldest toast yields to the newest.
    const dropped = next.length > 3 ? next.splice(0, next.length - 3) : [];
    dropped.forEach((item) => clearDeeplinkToastTimer(item.id));
    setDeeplinkToasts(next);
    const timer = window.setTimeout(() => dismissDeeplinkToast(id), toast.tone === "error" ? 8000 : 6000);
    deeplinkToastTimers.set(id, timer);
  };

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
      const coreChanged = next.coreType !== settings()?.coreType;
      setSettings(next);
      if (coreChanged) void refreshCoreInfo();
      setDockTransparency(next.dockTransparency || 28);
      applyTheme(next.theme);
      applyAccent(next.accentColor);
      setLanguage(next.language);
    });
    // fastv:// deep links are imported by the backend, which broadcasts the
    // result here. The subscriptions page reloads itself from
    // subscriptions:changed, so the toast only has to offer the shortcut.
    const offDeepLinkImported = Events.On("deeplink:imported", (event) => {
      const payload = readDeepLinkPayload(event.data);
      pushDeeplinkToast({ tone: "success", kind: payload.kind, name: payload.name, message: payload.message });
    });
    const offDeepLinkFailed = Events.On("deeplink:failed", (event) => {
      const payload = readDeepLinkPayload(event.data);
      pushDeeplinkToast({ tone: "error", kind: payload.kind, name: payload.name, message: payload.message });
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
      offDeepLinkImported();
      offDeepLinkFailed();
      for (const timer of deeplinkToastTimers.values()) window.clearTimeout(timer);
      deeplinkToastTimers.clear();
      setDeeplinkToasts([]);
      systemTheme.removeEventListener("change", handleSystemTheme);
    });
  });

  // Any dismissal (Skip, Get started, close button, backdrop click, Escape or
  // closing the window from the titlebar) marks the guide as seen so it only
  // greets a user once.
  const dismissOnboarding = () => {
    markOnboardingSeen();
    setOnboardingOpen(false);
  };

  // The import/settings shortcuts both end the guide and jump to the target
  // page, which is the fastest first-run path.
  const handleOnboardingAction = (page: PageKey) => {
    dismissOnboarding();
    setActive(page);
  };

  onMount(() => {
    // The desktop guide is not auto-opened: the dashboard's empty states and
    // inline actions already make the first step obvious. It can still be
    // opened programmatically through ONBOARDING_OPEN_EVENT.
    const handleOpenRequest = () => setOnboardingOpen(true);
    window.addEventListener(ONBOARDING_OPEN_EVENT, handleOpenRequest);
    onCleanup(() => window.removeEventListener(ONBOARDING_OPEN_EVENT, handleOpenRequest));

    // Android reports the VPN consent/start outcome asynchronously, after the
    // system dialog closes.
    onCleanup(onAndroidVpnResult((ok, message) => {
      if (!ok && message) setProxyError(message);
    }));
  });

  // While the guide is open: focus its close button for keyboard users and let
  // Escape dismiss it. Cleanup runs on dismissal and on component teardown.
  createEffect(() => {
    if (!onboardingOpen()) return;
    const focusFrame = requestAnimationFrame(() => onboardingCloseButton?.focus());
    const handleKeydown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      dismissOnboarding();
    };
    document.addEventListener("keydown", handleKeydown);
    onCleanup(() => {
      cancelAnimationFrame(focusFrame);
      document.removeEventListener("keydown", handleKeydown);
    });
  });

  const handleStartProxy = async () => {
    setProxyBusy(true);
    setProxyError("");
    setProxyTest(null);
    try {
      const next = await ProxyService.Start();
      setProxy(next);
      // On Android the local SOCKS inbound is only useful once the tun captures
      // system traffic, so connect the system-wide proxy right after it is up.
      if (isAndroidPlatform() && androidVpnSupported()) {
        if (settings()?.inboundAuthEnabled) {
          // The dataplane speaks no-auth SOCKS only, so enabling inbound
          // authentication would build a tunnel that cannot forward anything.
          setProxyError(t("shell.androidAuthUnsupported"));
        } else {
          const port = socksPortFromLocalAddr(next.localAddr) || (settings()?.xrayLocalPort ?? 0);
          if (port > 0) androidVpnConnect(port);
        }
      }
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
    if (isAndroidPlatform()) androidVpnDisconnect();
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

  const refreshCoreInfo = async () => {
    try {
      setCore(await CoreService.Info());
    } catch {
      // Keep the previous snapshot; the core:state event will refresh it.
    }
  };

  const handleSettingsSaved = (next: Settings) => {
    const coreChanged = next.coreType !== settings()?.coreType;
    setSettings(next);
    if (coreChanged) void refreshCoreInfo();
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

  // Persisted UI preference for the live traffic waveform style. The dashboard
  // exposes it as a small segmented control; saving keeps it across restarts.
  const handleLiveWaveStyleChange = async (style: string) => {
    const current = settings();
    if (!current || current.liveWaveStyle === style) return;
    try {
      const saved = await SettingsService.Save({ ...current, liveWaveStyle: style });
      setSettings(saved);
    } catch {
      // The dashboard falls back to the previous style on failure.
    }
  };

  const toggleMaximise = async () => {
    if (await AppWindow.IsMaximised()) {
      await AppWindow.Restore();
    } else {
      await AppWindow.Maximise();
    }
  };

  return (
    <div classList={{ "app-root": true, "layout-sidebar": layoutMode() === "sidebar", "layout-dock": layoutMode() === "dock", "platform-android": isAndroidPlatform() }}>
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
          <button
            type="button"
            class="window-control close"
            aria-label={t("shell.windowClose")}
            onClick={() => {
              // Closing the window from the titlebar while the guide is open
              // also counts as read, so a first-run user is not greeted by the
              // same overlay after every restart.
              if (onboardingOpen()) markOnboardingSeen();
              AppWindow.Close();
            }}
          >
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

          {[
            { label: t("shell.workspace"), items: navigation().slice(0, 4) },
            { label: t("shell.groupTools"), items: navigation().slice(4, 6) },
            { label: t("shell.groupSystem"), items: navigation().slice(6) },
          ].map((group) => (
            <>
              <div class="nav-section-label">{group.label}</div>
              <nav class="nav">
                {group.items.map((item) => (
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
            </>
          ))}

          <div class="sidebar-footer">
            <div class="footer-status">
              <span classList={{ "status-dot": true, online: core()?.installed, idle: !core()?.installed }} />
              <div>
                <strong>{core()?.installed ? `${settings()?.coreType === "singbox" ? "sing-box" : "Xray"} ${core()?.version ?? ""}` : t("shell.coreMissing")}</strong>
                <small>{core()?.installed ? t("shell.coreReady") : t("shell.coreHint")}</small>
              </div>
            </div>
            <div class="footer-version">v{info()?.version ?? "0.1.0"}</div>
          </div>
        </aside>
        </Show>

        <main class="content">
          <Show when={isAndroidPlatform()}>
            <header class="android-appbar">
              <div class="android-appbar-title">
                <span class="android-appbar-mark" aria-hidden="true">F</span>
                <div>
                  <strong>{activeMeta().title}</strong>
                  <small>{core()?.installed ? `${settings()?.coreType === "singbox" ? "sing-box" : "Xray"} ${core()?.version ?? ""}` : t("shell.coreMissing")}</small>
                </div>
              </div>
              <div class="android-appbar-actions">
                <span classList={{ "android-live-dot": true, online: proxy().status === "running" }} aria-hidden="true" />
                <button type="button" class="android-icon-button" aria-label={t("shell.androidMore")} onClick={() => setAndroidMenuOpen(true)}>
                  <Icon name="more" size={20} />
                </button>
              </div>
            </header>

            <div class="android-mode-row">
              <For each={proxyModes()}>
                {(option) => (
                  <button
                    type="button"
                    classList={{ "android-mode-chip": true, active: currentProxyMode() === option.value }}
                    aria-pressed={currentProxyMode() === option.value}
                    disabled={proxyModeBusy() || !settings()}
                    onClick={() => { void handleProxyModeChange(option.value); }}
                  >
                    {option.label}
                  </button>
                )}
              </For>
              <span classList={{ "android-proxy-chip": true, running: proxy().status === "running", error: proxy().status === "error" }}>
                {proxy().status === "running" ? t("dash.proxyStatusRunning")
                  : proxy().status === "starting" ? t("dash.proxyStatusStarting")
                  : proxy().status === "error" ? t("dash.proxyStatusError")
                  : t("dash.proxyStatusStopped")}
              </span>
            </div>
          </Show>
          <header class="topbar">
            <div class="topbar-title">
              <h1>{activeMeta().title}</h1>
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
              <span classList={{ "status-chip": true, muted: !core()?.installed }}>
                <span classList={{ "status-dot": true, online: core()?.installed, idle: !core()?.installed }} />
                {core()?.installed ? `${settings()?.coreType === "singbox" ? "sing-box" : "Xray"} ${core()?.version ?? ""}` : t("shell.coreMissing")}
              </span>
              <span classList={{ "status-chip": true, "proxy-running": proxy().status === "running" }}>
                <span classList={{ "status-dot": true, online: proxy().status === "running", idle: proxy().status !== "running" }} />
                {proxy().status === "running" ? t("shell.proxyRunning", { addr: proxy().localAddr }) : proxy().status === "starting" ? t("shell.proxyStarting") : proxy().status === "error" ? t("shell.proxyError") : t("shell.proxyStopped")}
              </span>
              <Show when={proxy().degraded}>
                <span class="status-chip degraded-chip" title={t("shell.antiDpiDegradedTitle")}>
                  <Icon name="alert" size={11} /> {t("shell.antiDpiDegraded")}
                </span>
              </Show>
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
              onLiveWaveStyleChange={(style) => { void handleLiveWaveStyleChange(style); }}
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
            <LogsPage proxyState={proxy()} onNavigate={(page) => setActive(page as PageKey)} />
          </Show>
          <Show when={active() === "settings"}>
            <SettingsPage android={isAndroidPlatform()} onSaved={handleSettingsSaved} onPreviewTheme={applyTheme} onPreviewAccent={applyAccent} onPreviewLanguage={setLanguage} onPreviewDockTransparency={setDockTransparency} />
          </Show>

        </main>

        <Show when={isAndroidPlatform()}>
          <nav class="android-bottom-nav" aria-label={t("shell.dockNav")}>
            <For each={[navigation()[0], navigation()[1], navigation()[2], navigation()[3], navigation()[6]]}>
              {(item) => (
                <button
                  type="button"
                  classList={{ "android-nav-item": true, active: active() === item.key }}
                  onClick={() => { setActive(item.key); setAndroidMenuOpen(false); }}
                >
                  <span class="android-nav-indicator" aria-hidden="true" />
                  <Icon name={item.icon} size={22} />
                  <span>{item.label}</span>
                </button>
              )}
            </For>
          </nav>

          <button
            type="button"
            classList={{ "android-fab": true, running: proxy().status === "running" }}
            aria-label={proxy().status === "running" ? t("shell.androidStop") : t("shell.androidStart")}
            disabled={proxyBusy()}
            onClick={() => { void (proxy().status === "running" ? handleStopProxy() : handleStartProxy()); }}
          >
            <Icon name={proxy().status === "running" ? "stop" : "play"} size={24} />
          </button>

          <Show when={androidMenuOpen()}>
            <div class="android-sheet-backdrop" onClick={() => setAndroidMenuOpen(false)} />
            <div class="android-sheet" role="dialog" aria-modal="true" aria-label={t("shell.androidMore")}>
              <span class="android-sheet-handle" aria-hidden="true" />
              <strong class="android-sheet-title">{t("shell.androidMore")}</strong>
              <button type="button" class="android-sheet-item" onClick={() => { setActive("speedtest"); setAndroidMenuOpen(false); }}>
                <Icon name="speed" size={20} /> {t("shell.nav.speedtest")}
              </button>
              <button type="button" class="android-sheet-item" onClick={() => { setActive("logs"); setAndroidMenuOpen(false); }}>
                <Icon name="logs" size={20} /> {t("shell.nav.logs")}
              </button>
              <button type="button" class="android-sheet-item" onClick={() => { setActive("settings"); setAndroidMenuOpen(false); }}>
                <Icon name="settings" size={20} /> {t("shell.nav.settings")}
              </button>
              <button type="button" class="android-sheet-item" onClick={() => setAndroidMenuOpen(false)}>
                <Icon name="close" size={20} /> {t("shell.deeplink.close")}
              </button>
            </div>
          </Show>
        </Show>

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

      <Show when={deeplinkToasts().length > 0}>
        <div class="deeplink-toast-stack" role="region" aria-live="polite" aria-label={t("shell.deeplink.region")}>
          <For each={deeplinkToasts()}>
            {(toast) => (
              <div
                classList={{ "deeplink-toast": true, "is-success": toast.tone === "success", "is-error": toast.tone === "error" }}
                role={toast.tone === "error" ? "alert" : "status"}
              >
                <span class="deeplink-toast-icon" aria-hidden="true">
                  <Icon name={toast.tone === "error" ? "alert" : "check"} size={15} />
                </span>
                <div class="deeplink-toast-body">
                  <strong class="deeplink-toast-title">
                    {toast.tone === "error" ? t("shell.deeplink.failedTitle") : t("shell.deeplink.importedTitle")}
                  </strong>
                  <Show when={deepLinkToastDetail(toast)}>
                    {(detail) => <span class="deeplink-toast-text">{detail()}</span>}
                  </Show>
                  <Show when={toast.tone === "success" && toast.kind === "subscription"}>
                    <button
                      type="button"
                      class="deeplink-toast-action"
                      onClick={() => {
                        setActive("subscriptions");
                        dismissDeeplinkToast(toast.id);
                      }}
                    >
                      {t("shell.deeplink.viewSubscription")}
                    </button>
                  </Show>
                </div>
                <button type="button" class="deeplink-toast-close" aria-label={t("shell.deeplink.close")} onClick={() => dismissDeeplinkToast(toast.id)}>
                  <Icon name="close" size={11} />
                </button>
              </div>
            )}
          </For>
        </div>
      </Show>
      <Show when={onboardingOpen()}>
        <div
          class="onboarding-backdrop"
          role="dialog"
          aria-modal="true"
          aria-labelledby="onboarding-title"
          aria-describedby="onboarding-subtitle"
          onClick={(event) => {
            if (event.target === event.currentTarget) dismissOnboarding();
          }}
        >
          <section class="onboarding-panel">
            <header class="onboarding-header">
              <div class="onboarding-heading">
                <span class="onboarding-mark" aria-hidden="true">F</span>
                <div class="onboarding-heading-copy">
                  <h2 id="onboarding-title">{t("shell.onboarding.title")}</h2>
                  <p id="onboarding-subtitle">{t("shell.onboarding.subtitle")}</p>
                </div>
              </div>
              <button
                type="button"
                ref={(element) => (onboardingCloseButton = element)}
                class="onboarding-close"
                aria-label={t("shell.onboarding.close")}
                onClick={dismissOnboarding}
              >
                <Icon name="close" size={12} />
              </button>
            </header>

            <div class="onboarding-grid">
              <For each={onboardingCards()}>
                {(card) => (
                  <article class="onboarding-card">
                    <div class="onboarding-card-head">
                      <span class="onboarding-step">
                        <span class="onboarding-sr">{t("shell.onboarding.step", { index: card.index })}</span>
                        <span aria-hidden="true">{String(card.index).padStart(2, "0")}</span>
                      </span>
                      <span class="onboarding-icon" aria-hidden="true">
                        <Icon name={card.icon} size={16} />
                      </span>
                    </div>
                    <h3>{card.title}</h3>
                    <p class="onboarding-lead">{card.lead}</p>
                    <Show when={card.modes}>
                      {(modes) => (
                        <ul class="onboarding-modes">
                          <For each={modes()}>
                            {(mode) => (
                              <li class="onboarding-mode">
                                <strong>{mode.label}</strong>
                                <span>{mode.description}</span>
                              </li>
                            )}
                          </For>
                        </ul>
                      )}
                    </Show>
                    <Show when={card.formats}>
                      {(formats) => (
                        <ul class="onboarding-list">
                          <For each={formats()}>
                            {(item) => (
                              <li>
                                <Icon name="check" size={12} />
                                <span>{item}</span>
                              </li>
                            )}
                          </For>
                        </ul>
                      )}
                    </Show>
                    <Show when={card.features}>
                      {(features) => (
                        <ul class="onboarding-list">
                          <For each={features()}>
                            {(item) => (
                              <li>
                                <Icon name="check" size={12} />
                                <span>{item}</span>
                              </li>
                            )}
                          </For>
                        </ul>
                      )}
                    </Show>
                    <Show when={card.note}>
                      {(note) => <p class="onboarding-note">{note()}</p>}
                    </Show>
                    <Show when={card.logLine}>
                      {(line) => (
                        <p class="onboarding-log">
                          <Icon name="logs" size={12} />
                          <code>{line()}</code>
                        </p>
                      )}
                    </Show>
                    <Show when={card.action}>
                      {(action) => (
                        <button
                          type="button"
                          class="btn glass onboarding-action"
                          onClick={() => handleOnboardingAction(action().page)}
                        >
                          {action().label}
                          <Icon name="chevron" size={12} />
                        </button>
                      )}
                    </Show>
                  </article>
                )}
              </For>
            </div>

            <footer class="onboarding-footer">
              <p class="onboarding-once">{t("shell.onboarding.onceHint")}</p>
              <div class="onboarding-actions">
                <button type="button" class="btn ghost" onClick={dismissOnboarding}>
                  {t("shell.onboarding.skip")}
                </button>
                <button type="button" class="btn primary" onClick={dismissOnboarding}>
                  {t("shell.onboarding.start")}
                  <Icon name="play" size={12} />
                </button>
              </div>
            </footer>
          </section>
        </div>
      </Show>
    </div>
  );
}
