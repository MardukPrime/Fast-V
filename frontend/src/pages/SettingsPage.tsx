import { createSignal, onCleanup, onMount, Show } from "solid-js";

import { Events } from "@wailsio/runtime";

import { CoreService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/core";
import { SettingsService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/settings";
import type { CoreInfo, GeoDataInfo, Settings } from "../../bindings/github.com/MardukPrime/Fast-V/internal/model";
import Icon from "../components/Icon";

type SettingsProps = {
  onSaved: (settings: Settings) => void;
  onPreviewTheme: (theme: string) => void;
};

const defaults: Settings = {
  theme: "dark",
  language: "zh-CN",
  autoUpdate: false,
  updateIntervalMinutes: 360,
  proxyMode: "rule",
  coreAutoUpdate: true,
  coreType: "xray",
  xrayLocalPort: 10808,
  xrayLogLevel: "warning",
  xrayMuxEnabled: false,
  xrayMuxConcurrency: 8,
  xraySniffing: true,
  tunMode: false,
  testUrl: "https://www.gstatic.com/generate_204",
  xrayApiEnabled: false,
  xrayApiPort: 10085,
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

const coreTypes = [
  { value: "xray", label: "Xray-core", hint: "已接入，支持版本检测和自动更新" },
  { value: "singbox", label: "sing-box", hint: "规划中，参数会在接入后开放" },
];

const logLevels = [
  { value: "warning", label: "warning" },
  { value: "info", label: "info" },
  { value: "debug", label: "debug" },
  { value: "error", label: "error" },
  { value: "none", label: "none" },
];

function coreStatusLabel(status: string) {
  switch (status) {
    case "ready": return "已就绪";
    case "up_to_date": return "已是最新";
    case "update_available": return "可更新";
    case "checking": return "检查中";
    case "downloading": return "下载中";
    case "not_installed": return "未安装";
    case "error": return "异常";
    default: return status || "未安装";
  }
}

function formatBytes(size: number) {
  if (!size) return "0 B";
  if (size >= 1024 * 1024) return `${(size / (1024 * 1024)).toFixed(1)} MB`;
  if (size >= 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${size} B`;
}

function readSectionState(key: string, fallback: boolean) {
  try {
    const value = localStorage.getItem(key);
    return value === null ? fallback : value === "true";
  } catch {
    return fallback;
  }
}

function writeSectionState(key: string, value: boolean) {
  try {
    localStorage.setItem(key, String(value));
  } catch {
    // Ignore storage failures; collapse state is non-critical.
  }
}
export default function SettingsPage(props: SettingsProps) {
  const [settings, setSettings] = createSignal<Settings>(defaults);
  const [core, setCore] = createSignal<CoreInfo | null>(null);
  const [loading, setLoading] = createSignal(true);
  const [saving, setSaving] = createSignal(false);
  const [coreBusy, setCoreBusy] = createSignal("");
  const [geoBusy, setGeoBusy] = createSignal("");
  const [geoData, setGeoData] = createSignal<GeoDataInfo | null>(null);
  const [message, setMessage] = createSignal("");
  const [error, setError] = createSignal("");
  const [clientExpanded, setClientExpanded] = createSignal(readSectionState("fastv.settings.clientOpen", false));
  const [coreExpanded, setCoreExpanded] = createSignal(readSectionState("fastv.settings.coreOpen", false));

  const load = async () => {
    setLoading(true);
    try {
      const [loadedSettings, loadedCore, loadedGeoData] = await Promise.all([
        SettingsService.Get(),
        CoreService.Info(),
        CoreService.GeoData(),
      ]);
      setSettings(loadedSettings);
      setCore(loadedCore);
      setGeoData(loadedGeoData);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  onMount(load);

  onMount(() => {
    const off = Events.On("core:state", (event) => {
      const info = event.data as CoreInfo;
      setCore(info);
      if (info.status === "checking") setCoreBusy("checking");
      else if (info.status === "downloading") setCoreBusy("downloading");
      else setCoreBusy("");
    });
    onCleanup(off);
  });

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

  const refreshGeoData = async () => {
    setGeoBusy("refresh");
    setError("");
    setMessage("");
    try {
      setGeoData(await CoreService.GeoData());
      setMessage("GeoData 状态已刷新。");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setGeoBusy("");
    }
  };

  const updateGeoData = async () => {
    setGeoBusy("update");
    setError("");
    setMessage("");
    try {
      const info = await CoreService.UpdateGeoData();
      setGeoData(info);
      setMessage("GeoData 已更新。");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setGeoBusy("");
    }
  };
  const checkCore = async () => {
    setCoreBusy("checking");
    setError("");
    setMessage("");
    try {
      const info = await CoreService.CheckLatest();
      setCore(info);
      setMessage(info.message);
    } catch (err) {
      setCore(await CoreService.Info());
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setCoreBusy("");
    }
  };

  const downloadCore = async () => {
    setCoreBusy("downloading");
    setError("");
    setMessage("");
    try {
      const info = await CoreService.DownloadLatest();
      setCore(info);
      setMessage(info.message);
    } catch (err) {
      setCore(await CoreService.Info());
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setCoreBusy("");
    }
  };

  return (
    <form class="page-stack settings-page" onSubmit={save}>
      <Show when={!loading()} fallback={<div class="empty-state">正在读取设置...</div>}>
        <details
          class="panel settings-section"
          open={clientExpanded()}
          onToggle={(event) => {
            const open = event.currentTarget.open;
            setClientExpanded(open);
            writeSectionState("fastv.settings.clientOpen", open);
          }}
        >
          <summary class="panel-heading settings-section-summary">
            <div class="heading-with-icon">
              <span class="heading-icon indigo"><Icon name="settings" size={17} /></span>
              <div>
                <h3>客户端设置</h3>
                <span>只影响 FastV 的界面和使用体验</span>
              </div>
            </div>
            <span class="settings-section-chevron"><Icon name="chevron" size={14} /></span>
          </summary>
          <div class="settings-section-body">
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
                  onClick={() => {
                    update("theme", item.value);
                    props.onPreviewTheme(item.value);
                  }}
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

          <div class="setting-block">
            <div class="setting-label">
              <strong>默认代理模式</strong>
              <small>生成 Xray 路由规则时使用：规则分流、全局代理或全部直连。</small>
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
            <div class="settings-grid compact">
              <label>
                <span>代理测试地址</span>
                <input
                  value={settings().testUrl}
                  onInput={(event) => update("testUrl", event.currentTarget.value)}
                  placeholder="https://www.gstatic.com/generate_204"
                />
                <small>启动代理后使用本地 SOCKS/HTTP 入站访问此地址。</small>
              </label>
            </div>
          </div>

          <div class="setting-block">
            <div class="setting-label">
              <strong>网络接管</strong>
              <small>控制 FastV 如何接管系统流量。</small>
            </div>
            <label class="switch-row tun-mode-row">
              <span>
                <strong>TUN 模式</strong>
                <small>全局接管系统流量。Xray-core 已支持 TUN，需要管理员权限、wintun.dll 和系统路由接管。</small>
              </span>
              <span classList={{ "switch": true, on: settings().tunMode }}>
                <input
                  type="checkbox"
                  checked={settings().tunMode}
                  onChange={(event) => update("tunMode", event.currentTarget.checked)}
                />
                <span class="switch-track"><span class="switch-thumb" /></span>
              </span>
            </label>
            <Show when={settings().tunMode}>
              <div class="tun-warning">
                <Icon name="alert" size={14} />
                <span>TUN 模式需要以管理员身份运行 FastV，并确保 wintun.dll 位于 xray.exe 同目录。启动时 FastV 会创建 FastV-TUN 适配器并接管默认路由。</span>
              </div>
            </Show>
          </div>

          <div class="setting-block">
            <div class="setting-label">
              <strong>订阅更新</strong>
              <small>控制订阅刷新间隔和启动时自动更新。</small>
            </div>
            <div class="settings-grid">
              <label>
                <span>更新间隔（分钟）</span>
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
          </div>
          </div>
        </details>

        <details
          class="panel settings-section"
          open={coreExpanded()}
          onToggle={(event) => {
            const open = event.currentTarget.open;
            setCoreExpanded(open);
            writeSectionState("fastv.settings.coreOpen", open);
          }}
        >
          <summary class="panel-heading settings-section-summary">
            <div class="heading-with-icon">
              <span class="heading-icon cyan"><Icon name="server" size={17} /></span>
              <div>
                <h3>内核设置</h3>
                <span>不同内核只展示各自的参数</span>
              </div>
            </div>
            <span class="settings-section-meta">
              <span classList={{ "tag": true, success: core()?.status === "ready" || core()?.status === "up_to_date", neutral: !core() || (core()?.status !== "ready" && core()?.status !== "up_to_date"), danger: core()?.status === "error" }}>
                {coreStatusLabel(core()?.status ?? "")}
              </span>
              <span class="settings-section-chevron"><Icon name="chevron" size={14} /></span>
            </span>
          </summary>
          <div class="settings-section-body">
          <div class="setting-block">
            <div class="setting-label">
              <strong>代理内核</strong>
              <small>选择用于处理代理流量的内核。</small>
            </div>
            <div class="core-type-options">
              {coreTypes.map((item) => (
                <button
                  type="button"
                  classList={{ "core-type-option": true, active: settings().coreType === item.value }}
                  onClick={() => update("coreType", item.value)}
                >
                  <span class="core-type-icon"><Icon name={item.value === "xray" ? "server" : "route"} size={16} /></span>
                  <span class="core-type-copy">
                    <strong>{item.label}</strong>
                    <small>{item.hint}</small>
                  </span>
                  <Show when={settings().coreType === item.value}>
                    <span class="theme-check"><Icon name="check" size={12} /></span>
                  </Show>
                </button>
              ))}
            </div>
          </div>

          <Show when={settings().coreType === "xray"} fallback={
            <div class="core-placeholder">
              <div class="empty-icon"><Icon name="route" size={22} /></div>
              <strong>sing-box 内核尚未接入</strong>
              <span>接入后会在这里显示 sing-box 专属参数，不会和客户端设置混在一起。</span>
            </div>
          }>
            <div class="core-summary">
              <div class="core-version">
                <span>当前版本</span>
                <strong>{core()?.version || "未安装"}</strong>
              </div>
              <div class="core-version">
                <span>最新版本</span>
                <strong>{core()?.latestVersion || "未检查"}</strong>
              </div>
              <div class="core-version">
                <span>更新方式</span>
                <strong>{settings().coreAutoUpdate ? "自动检测" : "手动检查"}</strong>
              </div>
            </div>

            <p class="core-message">{core()?.message || "可以检查最新 Xray-core 版本。"}</p>

            <div class="core-actions">
              <button class="btn secondary" type="button" onClick={checkCore} disabled={coreBusy() !== ""}>
                <Icon name="refresh" size={14} /> {coreBusy() === "checking" ? "检查中..." : "检查更新"}
              </button>
              <button class="btn primary" type="button" onClick={downloadCore} disabled={coreBusy() !== ""}>
                <Icon name="external" size={14} /> {coreBusy() === "downloading" ? "下载中..." : "下载最新版"}
              </button>
            </div>

            <div class="core-params">
              <div class="setting-label">
                <strong>Xray 运行参数</strong>
                <small>这些参数只属于 Xray-core，修改后重新启动代理即可生效。</small>
              </div>
              <div class="core-param-grid">
                <label>
                  <span>本地 SOCKS/HTTP 端口</span>
                  <input
                    type="number"
                    min="1"
                    max="65535"
                    value={settings().xrayLocalPort}
                    onInput={(event) => update("xrayLocalPort", Number(event.currentTarget.value))}
                  />
                  <small>启动代理时监听 127.0.0.1 的 SOCKS/HTTP 端口。</small>
                </label>
                <label>
                  <span>日志级别</span>
                  <select value={settings().xrayLogLevel} onChange={(event) => update("xrayLogLevel", event.currentTarget.value)}>
                    {logLevels.map((level) => <option value={level.value}>{level.label}</option>)}
                  </select>
                  <small>写入 Xray 配置的 log.loglevel。</small>
                </label>
                <label>
                  <span>Mux 并发数</span>
                  <input
                    type="number"
                    min="1"
                    max="128"
                    value={settings().xrayMuxConcurrency}
                    disabled={!settings().xrayMuxEnabled}
                    onInput={(event) => update("xrayMuxConcurrency", Number(event.currentTarget.value))}
                  />
                  <small>写入 outbound.mux.concurrency，仅在 Mux 开启时生效。</small>
                </label>
                <label>
                  <span>API / 统计端口</span>
                  <input
                    type="number"
                    min="1"
                    max="65535"
                    value={settings().xrayApiPort}
                    disabled={!settings().xrayApiEnabled}
                    onInput={(event) => update("xrayApiPort", Number(event.currentTarget.value))}
                  />
                  <small>仅监听 127.0.0.1，不能和本地 SOCKS/HTTP 端口相同。</small>
                </label>
              </div>

              <div class="core-switch-grid">
                <label class="switch-row">
                  <span>
                    <strong>启用 Mux</strong>
                    <small>减少握手次数，但可能影响大流量吞吐。</small>
                  </span>
                  <span classList={{ "switch": true, on: settings().xrayMuxEnabled }}>
                    <input
                      type="checkbox"
                      checked={settings().xrayMuxEnabled}
                      onChange={(event) => update("xrayMuxEnabled", event.currentTarget.checked)}
                    />
                    <span class="switch-track"><span class="switch-thumb" /></span>
                  </span>
                </label>
                <label class="switch-row">
                  <span>
                    <strong>启用流量嗅探</strong>
                    <small>写入 inbound.sniffing，用于域名分流和协议识别。</small>
                  </span>
                  <span classList={{ "switch": true, on: settings().xraySniffing }}>
                    <input
                      type="checkbox"
                      checked={settings().xraySniffing}
                      onChange={(event) => update("xraySniffing", event.currentTarget.checked)}
                    />
                    <span class="switch-track"><span class="switch-thumb" /></span>
                  </span>
                </label>
                <label class="switch-row">
                  <span>
                    <strong>启用 Xray API 与本地统计</strong>
                    <small>启动本地 API、StatsService 和统计策略，为流量统计和内核管理功能提供支持。</small>
                  </span>
                  <span classList={{ "switch": true, on: settings().xrayApiEnabled }}>
                    <input
                      type="checkbox"
                      checked={settings().xrayApiEnabled}
                      onChange={(event) => update("xrayApiEnabled", event.currentTarget.checked)}
                    />
                    <span class="switch-track"><span class="switch-thumb" /></span>
                  </span>
                </label>
              </div>
            </div>

            <div class="geodata-block">
              <div class="setting-label">
                <strong>GeoData 资源</strong>
                <small>路由中的 geoip/geosite 规则依赖这些资源。FastV 会通过 XRAY_LOCATION_ASSET 指向内核目录。</small>
              </div>
              <div class="geodata-files">
                <div class="geodata-file">
                  <span>GeoIP</span>
                  <strong>{geoData()?.geoIp ? "已安装" : "缺失"}</strong>
                  <small>{geoData()?.geoIp ? `${formatBytes(geoData()!.geoIp!.size)} · ${geoData()!.geoIp!.name}` : "geoip.dat"}</small>
                </div>
                <div class="geodata-file">
                  <span>GeoSite</span>
                  <strong>{geoData()?.geoSite ? "已安装" : "缺失"}</strong>
                  <small>{geoData()?.geoSite ? `${formatBytes(geoData()!.geoSite!.size)} · ${geoData()!.geoSite!.name}` : "geosite.dat"}</small>
                </div>
                <div class="geodata-file">
                  <span>资源文件</span>
                  <strong>{geoData()?.files?.length ?? 0}</strong>
                  <small>{geoData()?.directory ?? "内核目录"}</small>
                </div>
              </div>
              <div class="core-actions">
                <button class="btn secondary" type="button" onClick={refreshGeoData} disabled={geoBusy() !== ""}>
                  <Icon name="refresh" size={14} /> {geoBusy() === "refresh" ? "刷新中..." : "刷新状态"}
                </button>
                <button class="btn primary" type="button" onClick={updateGeoData} disabled={geoBusy() !== ""}>
                  <Icon name="external" size={14} /> {geoBusy() === "update" ? "更新中..." : "下载/更新 GeoData"}
                </button>
              </div>
              <p class="core-message">支持 geoip.dat、geosite.dat 以及额外的 .dat 资源文件；更新 GeoData 不会替换 Xray 可执行文件。</p>
            </div>
            <label class="switch-row">
              <span>
                <strong>自动检查并下载 Xray 内核更新</strong>
                <small>启动后检查新版本，并在可用时自动下载安装。</small>
              </span>
              <span classList={{ "switch": true, on: settings().coreAutoUpdate }}>
                <input
                  type="checkbox"
                  checked={settings().coreAutoUpdate}
                  onChange={(event) => update("coreAutoUpdate", event.currentTarget.checked)}
                />
                <span class="switch-track"><span class="switch-thumb" /></span>
              </span>
            </label>
          </Show>
          </div>
        </details>

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