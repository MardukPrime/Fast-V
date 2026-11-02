import { createSignal, onMount, Show } from "solid-js";

import { AppService, SettingsService } from "../bindings/github.com/MardukPrime/Fast-V";
import type { AppInfo, Settings } from "../bindings/github.com/MardukPrime/Fast-V";
import DashboardPage from "./pages/DashboardPage";
import PlaceholderPage from "./pages/PlaceholderPage";
import SettingsPage from "./pages/SettingsPage";
import SubscriptionsPage from "./pages/SubscriptionsPage";

type PageKey = "dashboard" | "servers" | "subscriptions" | "routing" | "speedtest" | "logs" | "settings";

const navigation: Array<{ key: PageKey; label: string; caption: string }> = [
  { key: "dashboard", label: "概览", caption: "运行状态" },
  { key: "servers", label: "服务器", caption: "节点列表" },
  { key: "subscriptions", label: "订阅", caption: "远程配置" },
  { key: "routing", label: "路由", caption: "分流规则" },
  { key: "speedtest", label: "测速", caption: "延迟与速度" },
  { key: "logs", label: "日志", caption: "运行记录" },
  { key: "settings", label: "设置", caption: "应用偏好" },
];

const pageMeta: Record<PageKey, { title: string; subtitle: string }> = {
  dashboard: { title: "概览", subtitle: "FastV 当前状态与快捷操作" },
  servers: { title: "服务器", subtitle: "节点管理将在后续版本接入" },
  subscriptions: { title: "订阅", subtitle: "导入、更新和查看订阅节点" },
  routing: { title: "路由", subtitle: "规则编辑器将在后续版本接入" },
  speedtest: { title: "测速", subtitle: "节点测速将在后续版本接入" },
  logs: { title: "日志", subtitle: "内核日志将在后续版本接入" },
  settings: { title: "设置", subtitle: "应用基础偏好设置" },
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

  onMount(async () => {
    try {
      const [appInfo, loadedSettings] = await Promise.all([
        AppService.Info(),
        SettingsService.Get(),
      ]);
      setInfo(appInfo);
      setSettings(loadedSettings);
      applyTheme(loadedSettings.theme);
    } catch (error) {
      console.error("Failed to load application state", error);
    }
  });

  const handleSettingsSaved = (next: Settings) => {
    setSettings(next);
    applyTheme(next.theme);
  };

  return (
    <div class="app-shell">
      <aside class="sidebar">
        <div class="brand">
          <div class="brand-mark">F</div>
          <div>
            <div class="brand-name">FastV</div>
            <div class="brand-caption">Proxy Client</div>
          </div>
        </div>

        <nav class="nav">
          {navigation.map((item) => (
            <button
              classList={{ "nav-item": true, active: active() === item.key }}
              onClick={() => setActive(item.key)}
            >
              <span class="nav-label">{item.label}</span>
              <span class="nav-caption">{item.caption}</span>
            </button>
          ))}
        </nav>

        <div class="sidebar-footer">
          <span class="status-dot" />
          <div>
            <div>代理内核未启动</div>
            <small>Xray-core 尚未接入</small>
          </div>
        </div>
      </aside>

      <main class="content">
        <header class="topbar">
          <div>
            <h1>{pageMeta[active()].title}</h1>
            <p>{pageMeta[active()].subtitle}</p>
          </div>
          <div class="topbar-actions">
            <span class="pill">Wails v{info()?.wailsVersion ?? "3.1.0"}</span>
            <span class="pill">v{info()?.version ?? "0.1.0"}</span>
          </div>
        </header>

        <Show when={active() === "dashboard"}>
          <DashboardPage
            info={info()}
            settings={settings()}
            onNavigate={(page) => setActive(page as PageKey)}
          />
        </Show>
        <Show when={active() === "subscriptions"}>
          <SubscriptionsPage />
        </Show>
        <Show when={active() === "settings"}>
          <SettingsPage onSaved={handleSettingsSaved} />
        </Show>
        <Show when={active() !== "dashboard" && active() !== "subscriptions" && active() !== "settings"}>
          <PlaceholderPage
            title={pageMeta[active()].title}
            description={pageMeta[active()].subtitle}
          />
        </Show>
      </main>
    </div>
  );
}