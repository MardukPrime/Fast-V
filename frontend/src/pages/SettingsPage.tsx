import { createSignal, onMount, Show } from "solid-js";

import { SettingsService } from "../../bindings/github.com/MardukPrime/Fast-V";
import type { Settings } from "../../bindings/github.com/MardukPrime/Fast-V";
import Icon from "../components/Icon";

type SettingsProps = {
  onSaved: (settings: Settings) => void;
};

const defaults: Settings = {
  theme: "dark",
  language: "zh-CN",
  autoUpdate: false,
  updateIntervalMinutes: 360,
  proxyMode: "rule",
};

const themes = [
  { value: "dark", label: "深色", hint: "低光环境" },
  { value: "light", label: "浅色", hint: "明亮环境" },
  { value: "system", label: "跟随系统", hint: "自动切换" },
];

const proxyModes = [
  { value: "rule", label: "规则分流", hint: "按规则选择直连或代理" },
  { value: "global", label: "全局代理", hint: "所有流量走代理" },
  { value: "direct", label: "直连", hint: "不经过代理" },
];

export default function SettingsPage(props: SettingsProps) {
  const [settings, setSettings] = createSignal<Settings>(defaults);
  const [loading, setLoading] = createSignal(true);
  const [saving, setSaving] = createSignal(false);
  const [message, setMessage] = createSignal("");
  const [error, setError] = createSignal("");

  const load = async () => {
    setLoading(true);
    try {
      setSettings(await SettingsService.Get());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  onMount(load);

  const update = <K extends keyof Settings>(key: K, value: Settings[K]) => {
    setSettings({ ...settings(), [key]: value });
  };

  const save = async (event: Event) => {
    event.preventDefault();
    setSaving(true);
    setError("");
    setMessage("");
    try {
      const saved = await SettingsService.Save(settings());
      setSettings(saved);
      props.onSaved(saved);
      setMessage("设置已保存。");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const reset = async () => {
    if (!window.confirm("确定恢复默认设置吗？")) return;
    setSaving(true);
    setError("");
    setMessage("");
    try {
      const resetSettings = await SettingsService.Reset();
      setSettings(resetSettings);
      props.onSaved(resetSettings);
      setMessage("已恢复默认设置。");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <form class="page-stack settings-page" onSubmit={save}>
      <Show when={!loading()} fallback={<div class="empty-state">正在读取设置...</div>}>
        <section class="panel">
          <div class="panel-heading">
            <div class="heading-with-icon">
              <span class="heading-icon indigo"><Icon name="dashboard" size={17} /></span>
              <div>
                <h3>外观</h3>
                <span>主题和语言</span>
              </div>
            </div>
          </div>

          <div class="setting-block">
            <div class="setting-label">
              <strong>界面主题</strong>
              <small>选择后立即预览，保存后应用到下次启动。</small>
            </div>
            <div class="theme-options">
              {themes.map((item) => (
                <button
                  type="button"
                  classList={{ "theme-option": true, active: settings().theme === item.value }}
                  onClick={() => update("theme", item.value)}
                >
                  <span classList={{ "theme-preview": true, [item.value]: true }}>
                    <span class="theme-preview-sidebar" />
                    <span class="theme-preview-content">
                      <span />
                      <span />
                    </span>
                  </span>
                  <span class="theme-option-copy">
                    <strong>{item.label}</strong>
                    <small>{item.hint}</small>
                  </span>
                  <Show when={settings().theme === item.value}>
                    <span class="theme-check"><Icon name="check" size={12} /></span>
                  </Show>
                </button>
              ))}
            </div>
          </div>

          <div class="settings-grid compact">
            <label>
              <span>界面语言</span>
              <select value={settings().language} onChange={(event) => update("language", event.currentTarget.value)}>
                <option value="zh-CN">简体中文</option>
                <option value="en-US">English</option>
              </select>
              <small>语言资源将在后续版本接入。</small>
            </label>
          </div>
        </section>

        <section class="panel">
          <div class="panel-heading">
            <div class="heading-with-icon">
              <span class="heading-icon cyan"><Icon name="route" size={17} /></span>
              <div>
                <h3>代理与订阅</h3>
                <span>模式与自动更新</span>
              </div>
            </div>
          </div>

          <div class="setting-block">
            <div class="setting-label">
              <strong>代理模式</strong>
              <small>代理内核接入后生效。</small>
            </div>
            <div class="mode-options">
              {proxyModes.map((item) => (
                <button
                  type="button"
                  classList={{ "mode-option": true, active: settings().proxyMode === item.value }}
                  onClick={() => update("proxyMode", item.value)}
                >
                  <strong>{item.label}</strong>
                  <small>{item.hint}</small>
                </button>
              ))}
            </div>
          </div>

          <div class="settings-grid">
            <label>
              <span>订阅更新间隔（分钟）</span>
              <input
                type="number"
                min="15"
                max="1440"
                value={settings().updateIntervalMinutes}
                onInput={(event) => update("updateIntervalMinutes", Number(event.currentTarget.value))}
              />
              <small>允许范围 15-1440 分钟。</small>
            </label>
          </div>

          <label class="switch-row">
            <span>
              <strong>自动更新订阅</strong>
              <small>应用启动后按更新间隔检查订阅。</small>
            </span>
            <span classList={{ "switch": true, on: settings().autoUpdate }}>
              <input
                type="checkbox"
                checked={settings().autoUpdate}
                onChange={(event) => update("autoUpdate", event.currentTarget.checked)}
              />
              <span class="switch-track"><span class="switch-thumb" /></span>
            </span>
          </label>
        </section>

        <Show when={message()}>
          <div class="alert success"><span class="alert-message"><Icon name="check" size={15} /> {message()}</span></div>
        </Show>
        <Show when={error()}>
          <div class="alert error"><span class="alert-message"><Icon name="alert" size={15} /> {error()}</span></div>
        </Show>

        <div class="settings-actions">
          <button class="btn ghost" type="button" onClick={reset} disabled={saving()}>恢复默认</button>
          <button class="btn primary" type="submit" disabled={saving()}>
            <Icon name="check" size={15} /> {saving() ? "保存中..." : "保存设置"}
          </button>
        </div>

        <section class="panel about-panel">
          <div class="panel-heading">
            <div>
              <h3>应用信息</h3>
              <span>ABOUT</span>
            </div>
          </div>
          <div class="about-grid">
            <div><span>应用</span><strong>FastV</strong></div>
            <div><span>版本</span><strong>0.1.0</strong></div>
            <div><span>界面</span><strong>Wails v3 + SolidJS</strong></div>
            <div><span>数据目录</span><strong>%APPDATA%\FastV</strong></div>
          </div>
        </section>
      </Show>
    </form>
  );
}