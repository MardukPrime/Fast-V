import { createSignal, onMount, Show } from "solid-js";

import { SettingsService } from "../../bindings/github.com/MardukPrime/Fast-V";
import type { Settings } from "../../bindings/github.com/MardukPrime/Fast-V";

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
    <div class="page-stack settings-page">
      <form class="panel" onSubmit={save}>
        <div class="panel-heading">
          <div>
            <h3>基础设置</h3>
            <span>这些设置保存在本机，不会上传</span>
          </div>
          <button class="btn ghost" type="button" onClick={reset} disabled={saving()}>恢复默认</button>
        </div>

        <Show when={!loading()} fallback={<div class="empty-state">正在读取设置...</div>}>
          <div class="settings-grid">
            <label>
              <span>界面主题</span>
              <select value={settings().theme} onChange={(event) => update("theme", event.currentTarget.value)}>
                <option value="dark">深色</option>
                <option value="light">浅色</option>
                <option value="system">跟随系统</option>
              </select>
              <small>主题切换会立即应用到界面。</small>
            </label>

            <label>
              <span>界面语言</span>
              <select value={settings().language} onChange={(event) => update("language", event.currentTarget.value)}>
                <option value="zh-CN">简体中文</option>
                <option value="en-US">English</option>
              </select>
              <small>语言资源将在后续版本接入。</small>
            </label>

            <label>
              <span>代理模式</span>
              <select value={settings().proxyMode} onChange={(event) => update("proxyMode", event.currentTarget.value)}>
                <option value="rule">规则分流</option>
                <option value="global">全局代理</option>
                <option value="direct">直连</option>
              </select>
              <small>代理内核接入后生效。</small>
            </label>

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
            <input
              type="checkbox"
              checked={settings().autoUpdate}
              onChange={(event) => update("autoUpdate", event.currentTarget.checked)}
            />
          </label>
        </Show>

        <Show when={message()}>
          <div class="alert success">{message()}</div>
        </Show>
        <Show when={error()}>
          <div class="alert error">{error()}</div>
        </Show>

        <div class="form-actions">
          <button class="btn primary" type="submit" disabled={saving() || loading()}>
            {saving() ? "保存中..." : "保存设置"}
          </button>
        </div>
      </form>

      <section class="panel">
        <div class="panel-heading">
          <h3>应用信息</h3>
          <span>ABOUT</span>
        </div>
        <dl class="info-list">
          <div><dt>应用</dt><dd>FastV</dd></div>
          <div><dt>版本</dt><dd>0.1.0</dd></div>
          <div><dt>界面</dt><dd>Wails v3 + SolidJS</dd></div>
          <div><dt>数据目录</dt><dd>%APPDATA%\FastV</dd></div>
        </dl>
      </section>
    </div>
  );
}