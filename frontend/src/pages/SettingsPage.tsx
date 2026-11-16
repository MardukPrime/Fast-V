import { createSignal, onCleanup, onMount, Show } from "solid-js";

import { Browser, Events } from "@wailsio/runtime";

import { AppService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/app";
import { Service as BackupService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/backup";
import { CoreService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/core";
import { ProxyService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/proxy";
import { SettingsService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/settings";
import type { BackupResult, CoreInfo, GeoDataInfo, ProxyState, Settings, UpdateInfo } from "../../bindings/github.com/MardukPrime/Fast-V/internal/model";
import Icon from "../components/Icon";

type SettingsProps = {
  onSaved: (settings: Settings) => void;
  onPreviewTheme: (theme: string) => void;
  onPreviewDockTransparency: (value: number) => void;
};

const defaults: Settings = {
  theme: "light",
  language: "zh-CN",
  layoutMode: "sidebar",
  systemProxyMode: "system",
  dockTransparency: 28,
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
  xrayDnsStrategy: "UseIP",
  xrayDnsServers: "1.1.1.1,8.8.8.8",
  xrayDomainStrategy: "IPIfNonMatch",
  xrayTunMtu: 1500,
  xraySockoptTfo: false,
  xraySockoptMptcp: false,
  xraySockoptKeepIdle: 0,
  xraySockoptKeepIntvl: 0,
  xraySockoptUserTimeout: 0,
  xraySockoptCongestion: "",
  xraySockoptDomainStrategy: "",
  xraySockoptJson: "",
  xrayFinalMaskJson: "",
  tunMode: false,
  allowLan: false,
  inboundAuthEnabled: false,
  inboundUsername: "",
  inboundPassword: "",
  closeToTray: false,
  minimizeToTray: false,
  autoCheckUpdate: true,
  statsEnabled: true,
  autoSelectEnabled: false,
  autoSelectIntervalMinutes: 10,
  autoSelectToleranceMs: 80,
  failoverEnabled: false,
  loadBalanceEnabled: false,
  loadBalanceMode: "leastPing",
  testUrl: "https://www.gstatic.com/generate_204",
  xrayApiEnabled: false,
  xrayApiPort: 10085,
};

const themes = [
  { value: "dark", label: "深色", hint: "低光环境" },
  { value: "light", label: "浅色", hint: "明亮环境" },
  { value: "system", label: "跟随系统", hint: "自动切换" },
];

const layoutModes = [
  { value: "sidebar", label: "侧边模式", hint: "保留左侧导航，关闭底栏" },
  { value: "dock", label: "底栏模式", hint: "保留液态玻璃底栏，关闭侧边" },
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

const sockoptDomainStrategies = [
  { value: "", label: "默认" },
  { value: "AsIs", label: "AsIs" },
  { value: "UseIP", label: "UseIP" },
  { value: "UseIPv4", label: "UseIPv4" },
  { value: "UseIPv6", label: "UseIPv6" },
];

const finalMaskTemplate = `{
  "tcp": [
    {
      "type": "fragment",
      "settings": {
        "packets": "tlshello",
        "length": "100-200",
        "delay": "10-20"
      }
    }
  ]
}`;

const dnsStrategies = [
  { value: "UseIP", label: "UseIP", hint: "优先解析 A/AAAA 记录" },
  { value: "UseIPv4", label: "UseIPv4", hint: "只使用 IPv4 解析结果" },
  { value: "UseIPv6", label: "UseIPv6", hint: "只使用 IPv6 解析结果" },
  { value: "AsIs", label: "AsIs", hint: "保持原始解析行为" },
];

const domainStrategies = [
  { value: "IPIfNonMatch", label: "IPIfNonMatch", hint: "域名规则未命中时再匹配 IP" },
  { value: "IPOnDemand", label: "IPOnDemand", hint: "需要 IP 信息时按需解析" },
  { value: "AsIs", label: "AsIs", hint: "只使用原始域名匹配" },
];

const logLevels = [
  { value: "warning", label: "warning" },
  { value: "info", label: "info" },
  { value: "debug", label: "debug" },
  { value: "error", label: "error" },
  { value: "none", label: "none" },
];

// Keep in sync with the backend validation in internal/service/settings.
const loadBalanceModes = [
  { value: "leastPing", label: "延迟优先", hint: "按 URL 延迟测速选择最快节点" },
  { value: "leastLoad", label: "负载优先", hint: "优先选择当前负载较低的节点" },
  { value: "roundRobin", label: "轮询", hint: "按顺序轮流使用全部启用节点" },
];

const loadBalanceModeValues = ["leastPing", "leastLoad", "roundRobin"];

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

function errorMessage(err: unknown) {
  return err instanceof Error ? err.message : String(err);
}

function formatBackupTimestamp(date: Date) {
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}${pad(date.getMonth() + 1)}${pad(date.getDate())}-${pad(date.getHours())}${pad(date.getMinutes())}`;
}

function formatCheckedAt(value: string) {
  if (!value) return "未知";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

// The self-update panel reports the release asset size in MB; the backend
// returns 0 when GitHub does not expose a size for the asset.
function formatAssetSize(size: number) {
  if (!Number.isFinite(size) || size <= 0) return "大小未知";
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}

function updateStatusLabel(info: UpdateInfo) {
  if (info.downloadedPath) return "更新已下载";
  if (info.error) return "检查失败";
  if (info.available) return info.assetUrl ? "有可用更新" : "有可用更新（需手动下载）";
  return "已是最新";
}

function updateHint(info: UpdateInfo) {
  if (info.downloadedPath) return " · 更新包已就绪，可立即重启并安装。";
  if (info.error) return "";
  if (!info.available) return " · 无需更新。";
  return info.assetUrl ? " · 可在下方下载安装包并自动安装。" : " · 请前往发布页面手动下载。";
}

const autoSelectIntervalRange = { min: 1, max: 1440 };
const autoSelectToleranceRange = { min: 0, max: 5000 };

export default function SettingsPage(props: SettingsProps) {
  const [settings, setSettings] = createSignal<Settings>(defaults);
  const [core, setCore] = createSignal<CoreInfo | null>(null);
  const [loading, setLoading] = createSignal(true);
  const [saving, setSaving] = createSignal(false);
  const [coreBusy, setCoreBusy] = createSignal("");
  const [geoBusy, setGeoBusy] = createSignal("");
  const [geoData, setGeoData] = createSignal<GeoDataInfo | null>(null);
  const [elevated, setElevated] = createSignal(false);
  const [message, setMessage] = createSignal("");
  const [error, setError] = createSignal("");
  const [clientExpanded, setClientExpanded] = createSignal(readSectionState("fastv.settings.clientOpen", false));
  const [coreExpanded, setCoreExpanded] = createSignal(readSectionState("fastv.settings.coreOpen", false));
  const [autoSelectExpanded, setAutoSelectExpanded] = createSignal(readSectionState("fastv.settings.autoSelectOpen", false));
  const [statsExpanded, setStatsExpanded] = createSignal(readSectionState("fastv.settings.statsOpen", false));
  const [integrationExpanded, setIntegrationExpanded] = createSignal(readSectionState("fastv.settings.integrationOpen", false));
  const [backupExpanded, setBackupExpanded] = createSignal(readSectionState("fastv.settings.backupOpen", false));
  const [autostartEnabled, setAutostartEnabled] = createSignal(false);
  const [autostartBusy, setAutostartBusy] = createSignal(false);
  const [autostartError, setAutostartError] = createSignal("");
  const [backupBusy, setBackupBusy] = createSignal("");
  const [backupMessage, setBackupMessage] = createSignal("");
  const [backupError, setBackupError] = createSignal("");
  const [importResult, setImportResult] = createSignal<BackupResult | null>(null);
  const [updateBusy, setUpdateBusy] = createSignal(false);
  const [updateInfo, setUpdateInfo] = createSignal<UpdateInfo | null>(null);
  const [updateError, setUpdateError] = createSignal("");
  const [updateDownloadBusy, setUpdateDownloadBusy] = createSignal(false);
  const [updateInstallBusy, setUpdateInstallBusy] = createSignal(false);
  const [updateNotice, setUpdateNotice] = createSignal("");
  const [proxyRunning, setProxyRunning] = createSignal(false);
  let backupFileInput: HTMLInputElement | undefined;

  // CheckUpdate never returns the downloaded package path, so a fresh check
  // after a successful download would hide the "install now" state. Keep the
  // downloaded package when the reported version is still the same.
  const mergeCheckedUpdate = (info: UpdateInfo): UpdateInfo => {
    const previous = updateInfo();
    if (!previous?.downloadedPath || previous.latestVersion !== info.latestVersion) {
      return info;
    }
    return {
      ...info,
      assetName: info.assetName || previous.assetName,
      assetSize: info.assetSize || previous.assetSize,
      downloadedPath: previous.downloadedPath,
    };
  };

  const load = async () => {
    setLoading(true);
    try {
      const [loadedSettings, loadedCore, loadedGeoData, isElevated] = await Promise.all([
        SettingsService.Get(),
        CoreService.Info(),
        CoreService.GeoData(),
        AppService.IsElevated(),
      ]);
      setSettings(loadedSettings);
      setCore(loadedCore);
      setGeoData(loadedGeoData);
      setElevated(isElevated);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  onMount(load);

  onMount(() => {
    void (async () => {
      try {
        setAutostartEnabled(await AppService.AutostartEnabled());
      } catch (err) {
        setAutostartError(`读取开机自启状态失败：${errorMessage(err)}`);
      }
    })();
  });

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

  onMount(() => {
    const off = Events.On("app:update", (event) => {
      const info = mergeCheckedUpdate(event.data as UpdateInfo);
      setUpdateInfo(info);
      setUpdateError(info.error || "");
      setUpdateNotice("");
    });
    onCleanup(off);
  });

  // The proxy state is only used for an advisory hint before installing an
  // update: replacing the executable is safest while the proxy is stopped.
  onMount(() => {
    void (async () => {
      try {
        const state = await ProxyService.State();
        setProxyRunning(state.status === "running" || state.status === "starting");
      } catch {
        // The hint is optional; ignore failures so the panel keeps working.
      }
    })();
    const off = Events.On("proxy:state", (event) => {
      const state = event.data as ProxyState;
      setProxyRunning(state.status === "running" || state.status === "starting");
    });
    onCleanup(off);
  });

  const update = <K extends keyof Settings>(key: K, value: Settings[K]) => {
    setSettings({ ...settings(), [key]: value });
  };

  const validateAutoSelect = () => {
    const current = settings();
    const interval = current.autoSelectIntervalMinutes;
    const tolerance = current.autoSelectToleranceMs;
    if (!Number.isFinite(interval) || interval < autoSelectIntervalRange.min || interval > autoSelectIntervalRange.max) {
      return `自动选路间隔必须在 ${autoSelectIntervalRange.min}-${autoSelectIntervalRange.max} 分钟之间。`;
    }
    if (!Number.isFinite(tolerance) || tolerance < autoSelectToleranceRange.min || tolerance > autoSelectToleranceRange.max) {
      return `切换容差必须在 ${autoSelectToleranceRange.min}-${autoSelectToleranceRange.max} 毫秒之间。`;
    }
    return "";
  };

  const validateInboundAuth = () => {
    const current = settings();
    if (!current.inboundAuthEnabled) return "";
    const username = current.inboundUsername;
    if (!username) return "开启入站认证时必须填写用户名。";
    if (/\s/.test(username)) return "入站认证用户名不能包含空白字符。";
    const password = current.inboundPassword;
    if (!password) return "开启入站认证时必须填写密码。";
    if ([...password].length < 6) return "入站认证密码长度不能少于 6 位。";
    return "";
  };

  // The backend only accepts the three Xray balancer strategies; block anything
  // else before it reaches SettingsService.Save. An empty value keeps the
  // leastPing default, mirroring the backend behaviour.
  const validateLoadBalance = () => {
    const mode = settings().loadBalanceMode.trim();
    if (mode !== "" && !loadBalanceModeValues.includes(mode)) {
      return "负载均衡策略必须是延迟优先（leastPing）、负载优先（leastLoad）或轮询（roundRobin）。";
    }
    return "";
  };

  const save = async (event: Event) => {
    event.preventDefault();
    const autoSelectError = validateAutoSelect();
    const inboundError = validateInboundAuth();
    const loadBalanceError = validateLoadBalance();
    const validationError = autoSelectError || inboundError || loadBalanceError;
    if (validationError) {
      if (inboundError) {
        setClientExpanded(true);
        writeSectionState("fastv.settings.clientOpen", true);
      }
      if (autoSelectError || loadBalanceError) {
        setAutoSelectExpanded(true);
        writeSectionState("fastv.settings.autoSelectOpen", true);
      }
      setMessage("");
      setError(validationError);
      return;
    }
    setSaving(true);
    setError("");
    setMessage("");
    try {
      const saved = await SettingsService.Save(settings());
      setSettings(saved);
      props.onSaved(saved);
      setMessage("设置已保存。");
      if (saved.tunMode && !elevated()) {
        setMessage("TUN 模式需要管理员权限，正在请求 UAC 提权并重启 FastV...");
        await AppService.RestartAsAdmin();
      }
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

  const toggleAutostart = async (enabled: boolean) => {
    if (autostartBusy()) return;
    const previous = autostartEnabled();
    setAutostartError("");
    setAutostartEnabled(enabled);
    setAutostartBusy(true);
    try {
      setAutostartEnabled(await AppService.SetAutostart(enabled));
      setMessage(enabled ? "已开启开机自启。" : "已关闭开机自启。");
    } catch (err) {
      setAutostartEnabled(previous);
      setAutostartError(`设置开机自启失败：${errorMessage(err)}`);
    } finally {
      setAutostartBusy(false);
    }
  };

  const exportBackup = async () => {
    setBackupBusy("export");
    setBackupMessage("");
    setBackupError("");
    try {
      const data = await BackupService.Export();
      const blob = new Blob([data], { type: "application/json;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = `fastv-backup-${formatBackupTimestamp(new Date())}.json`;
      document.body.appendChild(link);
      link.click();
      link.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 1000);
      setBackupMessage(`备份已导出为 fastv-backup-${formatBackupTimestamp(new Date())}.json。`);
    } catch (err) {
      setBackupError(`导出备份失败：${errorMessage(err)}`);
    } finally {
      setBackupBusy("");
    }
  };

  const importBackup = async (file: File | null) => {
    if (!file) return;
    setBackupBusy("import");
    setBackupMessage("");
    setBackupError("");
    setImportResult(null);
    try {
      const text = await file.text();
      const result = await BackupService.Import(text);
      setImportResult(result);
      setBackupMessage("备份已导入，请重启代理后生效。");
      try {
        const restored = await SettingsService.Get();
        setSettings(restored);
        props.onSaved(restored);
      } catch {
        // The restore already succeeded; a stale form is better than a false error.
      }
    } catch (err) {
      setBackupError(`导入备份失败：${errorMessage(err)}`);
    } finally {
      setBackupBusy("");
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

  const checkUpdate = async () => {
    if (updateBusy() || updateDownloadBusy() || updateInstallBusy()) return;
    setUpdateBusy(true);
    setMessage("");
    setUpdateError("");
    setUpdateNotice("");
    try {
      const info = mergeCheckedUpdate(await AppService.CheckUpdate());
      setUpdateInfo(info);
      setUpdateError(info.error || "");
      if (!info.error) {
        setMessage(info.available ? `发现新版本 v${info.latestVersion}。` : "当前已是最新版本。");
      }
    } catch (err) {
      setUpdateError(`检查更新失败：${errorMessage(err)}`);
    } finally {
      setUpdateBusy(false);
    }
  };

  const downloadUpdate = async () => {
    if (updateBusy() || updateDownloadBusy() || updateInstallBusy()) return;
    setUpdateDownloadBusy(true);
    setMessage("");
    setUpdateError("");
    setUpdateNotice("");
    try {
      const info = await AppService.DownloadUpdate();
      setUpdateInfo(info);
      if (info.error) {
        setUpdateError(`下载更新失败：${info.error}`);
      } else {
        setUpdateNotice(
          `更新包 ${info.assetName || "安装包"}（${formatAssetSize(info.assetSize)}）已下载；确认后即可重启安装。`,
        );
      }
    } catch (err) {
      setUpdateError(`下载更新失败：${errorMessage(err)}`);
    } finally {
      setUpdateDownloadBusy(false);
    }
  };

  const installUpdate = async () => {
    if (updateBusy() || updateDownloadBusy() || updateInstallBusy()) return;
    const info = updateInfo();
    if (!info?.downloadedPath) {
      setUpdateError("没有已下载的更新包，请先下载更新。");
      return;
    }
    const lines = [
      `确定立即重启并安装 FastV v${info.latestVersion || "新版本"} 吗？`,
      "",
      "FastV 将退出，自动替换程序文件后重新启动。",
    ];
    if (proxyRunning()) {
      lines.push("", "当前代理正在运行，安装前建议先停止代理；安装过程中网络会短暂中断。");
    }
    if (!window.confirm(lines.join("\n"))) return;
    setUpdateInstallBusy(true);
    setMessage("");
    setUpdateError("");
    setUpdateNotice("");
    try {
      await AppService.ApplyUpdate();
      // The backend starts the replacement script and asks the app to quit.
      // A successful call is expected to terminate FastV, so keep the button
      // disabled and only surface the restart hint.
      setUpdateNotice("正在重启并安装更新，FastV 将自动重新启动…");
    } catch (err) {
      setUpdateError(`安装更新失败：${errorMessage(err)}`);
      setUpdateInstallBusy(false);
    }
  };

  const openReleasePage = async (url: string) => {
    if (!url) return;
    try {
      await Browser.OpenURL(url);
    } catch {
      window.open(url, "_blank");
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

          <div class="setting-block">
            <div class="setting-label">
              <strong>导航布局</strong>
              <small>侧边模式保留左侧导航；底栏模式隐藏侧边栏，只保留液态玻璃底栏。</small>
            </div>
            <div class="mode-options">
              {layoutModes.map((item) => (
                <button type="button" classList={{ "mode-option": true, active: settings().layoutMode === item.value }} onClick={() => update("layoutMode", item.value)}>
                  <strong>{item.label}</strong>
                  <small>{item.hint}</small>
                </button>
              ))}
            </div>
          </div>

          <Show when={settings().layoutMode === "dock"}>
            <div class="setting-block">
              <div class="setting-label">
                <strong>底栏透明度</strong>
                <small>只调整底栏玻璃背景的透明度，文字、图标和操作按钮保持不透明。</small>
              </div>
              <div class="dock-transparency-row">
                <input type="range" min="5" max="90" value={settings().dockTransparency} onInput={(event) => { const value = Number(event.currentTarget.value); update("dockTransparency", value); props.onPreviewDockTransparency(value); }} />
                <strong>{settings().dockTransparency}%</strong>
              </div>
              <div class="dock-transparency-scale"><span>更实</span><span>更透明</span></div>
            </div>
          </Show>

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
              <strong>Windows 系统代理</strong>
              <small>启动代理时自动设置 Windows 系统代理，停止代理或退出 FastV 时恢复原设置。TUN 模式会自动跳过。</small>
            </div>
            <label class="switch-row">
              <span>
                <strong>自动设置系统代理</strong>
                <small>{settings().tunMode ? "当前已启用 TUN 模式，将跳过系统代理设置。" : `系统代理将指向 127.0.0.1:${settings().xrayLocalPort}`}</small>
              </span>
              <span classList={{ "switch": true, on: settings().systemProxyMode === "system" }}>
                <input type="checkbox" checked={settings().systemProxyMode === "system"} onChange={(event) => update("systemProxyMode", event.currentTarget.checked ? "system" : "off")} />
                <span class="switch-track"><span class="switch-thumb" /></span>
              </span>
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
                <span>TUN 模式需要管理员权限。保存设置后 FastV 会自动请求 UAC 提权并重启；也可以直接启动代理触发提权。请确保 wintun.dll 位于 xray.exe 同目录。当前状态：{elevated() ? "已获得管理员权限" : "普通权限，启动 TUN 时将自动申请提权"}。</span>
              </div>
            </Show>

            <label class="switch-row">
              <span>
                <strong>允许局域网连接</strong>
                <small>{settings().allowLan ? `本地 SOCKS 入站监听 0.0.0.0:${settings().xrayLocalPort}，同网段设备可以把代理指向本机 IP。` : "本地 SOCKS 入站仅监听 127.0.0.1，只有本机可以使用本地代理。"}</small>
              </span>
              <span classList={{ "switch": true, on: settings().allowLan }}>
                <input
                  type="checkbox"
                  checked={settings().allowLan}
                  onChange={(event) => update("allowLan", event.currentTarget.checked)}
                />
                <span class="switch-track"><span class="switch-thumb" /></span>
              </span>
            </label>
            <Show when={settings().allowLan}>
              <div class="settings-inline-warning">
                <Icon name="alert" size={13} />
                <span>监听 0.0.0.0，同网段设备可用本机代理；请确认防火墙设置，并建议同时开启入站认证，避免同网段设备无需密码即可使用代理。</span>
              </div>
            </Show>

            <label class="switch-row">
              <span>
                <strong>入站认证</strong>
                <small>为本地 SOCKS 入站启用用户名/密码认证；关闭时使用 noauth，本机应用无需填写密码。开启后访问本机代理的应用需要配置对应账号。</small>
              </span>
              <span classList={{ "switch": true, on: settings().inboundAuthEnabled }}>
                <input
                  type="checkbox"
                  checked={settings().inboundAuthEnabled}
                  onChange={(event) => update("inboundAuthEnabled", event.currentTarget.checked)}
                />
                <span class="switch-track"><span class="switch-thumb" /></span>
              </span>
            </label>
            <Show when={settings().inboundAuthEnabled}>
              <div class="settings-grid">
                <label>
                  <span>入站用户名</span>
                  <input
                    value={settings().inboundUsername}
                    onInput={(event) => update("inboundUsername", event.currentTarget.value)}
                    placeholder="例如 fastv"
                  />
                  <small>不能为空，且不能包含空白字符。</small>
                </label>
                <label>
                  <span>入站密码</span>
                  <input
                    type="password"
                    value={settings().inboundPassword}
                    onInput={(event) => update("inboundPassword", event.currentTarget.value)}
                    placeholder="至少 6 位"
                  />
                  <small>至少 6 位；保存并重启代理后生效。</small>
                </label>
              </div>
              <div class="settings-inline-warning">
                <Icon name="alert" size={13} />
                <span>入站认证只作用于本地 SOCKS 入站，TUN 入站不受影响；修改账号密码后需要重启代理才会生效。</span>
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

            <div class="setting-block core-advanced-block">
              <div class="setting-label">
                <strong>Xray 高级网络参数</strong>
                <small>直接写入 Xray 的 DNS、路由和 TUN 配置，修改后重新启动代理生效。</small>
              </div>
              <div class="core-param-grid">
                <label>
                  <span>DNS 服务器</span>
                  <input value={settings().xrayDnsServers} onInput={(event) => update("xrayDnsServers", event.currentTarget.value)} placeholder="1.1.1.1,8.8.8.8" />
                  <small>逗号或换行分隔，写入 dns.servers。</small>
                </label>
                <label>
                  <span>DNS 查询策略</span>
                  <select value={settings().xrayDnsStrategy} onChange={(event) => update("xrayDnsStrategy", event.currentTarget.value)}>
                    {dnsStrategies.map((item) => <option value={item.value}>{item.label} · {item.hint}</option>)}
                  </select>
                  <small>写入 dns.queryStrategy。</small>
                </label>
                <label>
                  <span>路由域名策略</span>
                  <select value={settings().xrayDomainStrategy} onChange={(event) => update("xrayDomainStrategy", event.currentTarget.value)}>
                    {domainStrategies.map((item) => <option value={item.value}>{item.label} · {item.hint}</option>)}
                  </select>
                  <small>写入 routing.domainStrategy。</small>
                </label>
                <label>
                  <span>TUN MTU</span>
                  <input type="number" min="576" max="9000" value={settings().xrayTunMtu} onInput={(event) => update("xrayTunMtu", Number(event.currentTarget.value))} />
                  <small>写入 TUN inbound 的 MTU，默认 1500。</small>
                </label>
              </div>
              <div class="core-path-row">
                <span>Xray 内核路径</span>
                <code>{core()?.path || "未安装"}</code>
              </div>
            </div>
            <div class="setting-block transport-advanced-block">
              <div class="setting-label">
                <strong>传输层 FinalMask / Sockopt</strong>
                <small>写入当前 outbound 的 streamSettings。Sockopt 高级 JSON 会覆盖上方同名参数；FinalMask 为空时不生成。</small>
              </div>

              <div class="core-switch-grid">
                <label class="switch-row">
                  <span><strong>TCP Fast Open</strong><small>写入 sockopt.tcpFastOpen。</small></span>
                  <span classList={{ "switch": true, on: settings().xraySockoptTfo }}>
                    <input type="checkbox" checked={settings().xraySockoptTfo} onChange={(event) => update("xraySockoptTfo", event.currentTarget.checked)} />
                    <span class="switch-track"><span class="switch-thumb" /></span>
                  </span>
                </label>
                <label class="switch-row">
                  <span><strong>TCP MPTCP</strong><small>写入 sockopt.tcpMptcp，系统支持时启用。</small></span>
                  <span classList={{ "switch": true, on: settings().xraySockoptMptcp }}>
                    <input type="checkbox" checked={settings().xraySockoptMptcp} onChange={(event) => update("xraySockoptMptcp", event.currentTarget.checked)} />
                    <span class="switch-track"><span class="switch-thumb" /></span>
                  </span>
                </label>
              </div>

              <div class="core-param-grid">
                <label>
                  <span>KeepAlive Idle</span>
                  <input type="number" min="0" value={settings().xraySockoptKeepIdle} onInput={(event) => update("xraySockoptKeepIdle", Number(event.currentTarget.value))} />
                  <small>秒，0 表示不写入。</small>
                </label>
                <label>
                  <span>KeepAlive Interval</span>
                  <input type="number" min="0" value={settings().xraySockoptKeepIntvl} onInput={(event) => update("xraySockoptKeepIntvl", Number(event.currentTarget.value))} />
                  <small>秒，0 表示不写入。</small>
                </label>
                <label>
                  <span>TCP User Timeout</span>
                  <input type="number" min="0" value={settings().xraySockoptUserTimeout} onInput={(event) => update("xraySockoptUserTimeout", Number(event.currentTarget.value))} />
                  <small>毫秒，0 表示不写入。</small>
                </label>
                <label>
                  <span>TCP Congestion</span>
                  <input value={settings().xraySockoptCongestion} onInput={(event) => update("xraySockoptCongestion", event.currentTarget.value)} placeholder="例如 bbr / cubic" />
                  <small>写入 sockopt.tcpCongestion。</small>
                </label>
                <label>
                  <span>Sockopt Domain Strategy</span>
                  <select value={settings().xraySockoptDomainStrategy} onChange={(event) => update("xraySockoptDomainStrategy", event.currentTarget.value)}>
                    {sockoptDomainStrategies.map((item) => <option value={item.value}>{item.label}</option>)}
                  </select>
                  <small>写入 sockopt.domainStrategy。</small>
                </label>
              </div>

              <div class="advanced-json-grid">
                <label>
                  <span>Sockopt 高级 JSON</span>
                  <textarea value={settings().xraySockoptJson} onInput={(event) => update("xraySockoptJson", event.currentTarget.value)} spellcheck={false} placeholder={'{\n  "acceptProxyProtocol": false\n}'} />
                  <small>与上方 Sockopt 字段合并，同名键以 JSON 为准。</small>
                </label>
                <label>
                  <span>FinalMask JSON</span>
                  <textarea value={settings().xrayFinalMaskJson} onInput={(event) => update("xrayFinalMaskJson", event.currentTarget.value)} spellcheck={false} placeholder={'{\n  "tcp": [\n    { "type": "fragment", "settings": { "packets": "tlshello" } }\n  ]\n}'} />
                  <small>完整 finalmask 对象，写入 streamSettings.finalmask。</small>
                </label>
              </div>

              <div class="core-actions">
                <button class="btn secondary" type="button" onClick={() => update("xrayFinalMaskJson", finalMaskTemplate)}>
                  <Icon name="plus" size={14} /> 插入 Fragment 模板
                </button>
                <button class="btn ghost" type="button" onClick={() => update("xrayFinalMaskJson", "")}>
                  清空 FinalMask
                </button>
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

        <details
          class="panel settings-section"
          open={autoSelectExpanded()}
          onToggle={(event) => {
            const open = event.currentTarget.open;
            setAutoSelectExpanded(open);
            writeSectionState("fastv.settings.autoSelectOpen", open);
          }}
        >
          <summary class="panel-heading settings-section-summary">
            <div class="heading-with-icon">
              <span class="heading-icon"><Icon name="route" size={17} /></span>
              <div>
                <h3>智能选路</h3>
                <span>自动选路、故障切换与多节点负载均衡</span>
              </div>
            </div>
            <span class="settings-section-meta">
              <Show when={settings().loadBalanceEnabled}>
                <span class="tag success">负载均衡已开启</span>
              </Show>
              <span classList={{ "tag": true, success: settings().autoSelectEnabled, neutral: !settings().autoSelectEnabled }}>
                {settings().autoSelectEnabled ? "自动选路已开启" : "自动选路已关闭"}
              </span>
              <span class="settings-section-chevron"><Icon name="chevron" size={14} /></span>
            </span>
          </summary>
          <div class="settings-section-body">
            <label class="switch-row">
              <span>
                <strong>自动选择最优节点</strong>
                <small>按选路间隔对所有节点做 URL 延迟测速，自动切换到延迟最低的可用节点。</small>
              </span>
              <span classList={{ "switch": true, on: settings().autoSelectEnabled }}>
                <input
                  type="checkbox"
                  checked={settings().autoSelectEnabled}
                  onChange={(event) => update("autoSelectEnabled", event.currentTarget.checked)}
                />
                <span class="switch-track"><span class="switch-thumb" /></span>
              </span>
            </label>

            <Show when={settings().loadBalanceEnabled}>
              <div class="settings-inline-warning">
                <Icon name="alert" size={13} />
                <span>负载均衡已开启：代理启动时自动选路与故障切换会暂停；需要继续使用自动选路时，请先关闭负载均衡。</span>
              </div>
            </Show>

            <div class="settings-grid">
              <label>
                <span>选路间隔（分钟）</span>
                <input
                  type="number"
                  min={autoSelectIntervalRange.min}
                  max={autoSelectIntervalRange.max}
                  step="1"
                  value={settings().autoSelectIntervalMinutes}
                  onInput={(event) => update("autoSelectIntervalMinutes", Number(event.currentTarget.value))}
                />
                <small>允许范围 {autoSelectIntervalRange.min}-{autoSelectIntervalRange.max} 分钟，间隔越短切换越及时，测速开销也越大。</small>
              </label>
              <label>
                <span>切换容差（毫秒）</span>
                <input
                  type="number"
                  min={autoSelectToleranceRange.min}
                  max={autoSelectToleranceRange.max}
                  step="1"
                  value={settings().autoSelectToleranceMs}
                  onInput={(event) => update("autoSelectToleranceMs", Number(event.currentTarget.value))}
                />
                <small>允许范围 {autoSelectToleranceRange.min}-{autoSelectToleranceRange.max} 毫秒，新节点需要快出容差值才会切换，避免频繁抖动。</small>
              </label>
            </div>

            <label class="switch-row">
              <span>
                <strong>故障自动切换</strong>
                <small>代理健康检查失败时，自动切换到延迟最低的可用节点。</small>
              </span>
              <span classList={{ "switch": true, on: settings().failoverEnabled }}>
                <input
                  type="checkbox"
                  checked={settings().failoverEnabled}
                  onChange={(event) => update("failoverEnabled", event.currentTarget.checked)}
                />
                <span class="switch-track"><span class="switch-thumb" /></span>
              </span>
            </label>

            <p class="settings-note">
              <Icon name="alert" size={13} />
              <span>自动选路需要 URL 测速；TUN 模式开启时会暂停自动选路。保存设置后重启代理生效。</span>
            </p>

            <div class="setting-block">
              <div class="setting-label">
                <strong>负载均衡</strong>
                <small>把全部启用节点交给 Xray 的 balancer，每条连接按策略选择节点，而不是只使用一个活动节点。</small>
              </div>

              <label class="switch-row">
                <span>
                  <strong>启用负载均衡</strong>
                  <small>对全部启用节点做多出站负载均衡；需要至少 2 个启用节点，不足 2 个时自动回退单节点。开启时自动选路与故障切换暂停，TUN 模式暂回退单节点。</small>
                </span>
                <span classList={{ "switch": true, on: settings().loadBalanceEnabled }}>
                  <input
                    type="checkbox"
                    checked={settings().loadBalanceEnabled}
                    onChange={(event) => update("loadBalanceEnabled", event.currentTarget.checked)}
                  />
                  <span class="switch-track"><span class="switch-thumb" /></span>
                </span>
              </label>

              <div class="settings-grid compact">
                <label>
                  <span>负载均衡策略</span>
                  <select value={settings().loadBalanceMode} onChange={(event) => update("loadBalanceMode", event.currentTarget.value)}>
                    {loadBalanceModes.map((item) => <option value={item.value}>{item.label} · {item.hint}</option>)}
                  </select>
                  <small>仅支持 leastPing、leastLoad、roundRobin 三种策略；保存时后端会做同样的校验，前端会先拦截非法值。</small>
                </label>
              </div>

              <Show when={settings().loadBalanceEnabled}>
                <Show when={settings().autoSelectEnabled || settings().failoverEnabled}>
                  <div class="settings-inline-warning">
                    <Icon name="alert" size={13} />
                    <span>负载均衡与自动选路/故障切换互斥：保存并启用后，代理启动时会暂停自动选路和故障切换，节点选择完全交给负载均衡；关闭负载均衡即可恢复。</span>
                  </div>
                </Show>

                <Show when={settings().tunMode}>
                  <div class="settings-inline-warning">
                    <Icon name="alert" size={13} />
                    <span>当前已开启 TUN 模式：TUN 暂不支持多出站负载均衡，启动代理时会自动回退为单节点，并写入内核日志。</span>
                  </div>
                </Show>

                <p class="settings-note">
                  <Icon name="route" size={13} />
                  <span>负载均衡同样要求至少 2 个启用节点；需要保存设置并重启代理后生效，实际使用的策略和回退原因会写入内核日志。</span>
                </p>
              </Show>
            </div>
          </div>
        </details>

        <details
          class="panel settings-section"
          open={statsExpanded()}
          onToggle={(event) => {
            const open = event.currentTarget.open;
            setStatsExpanded(open);
            writeSectionState("fastv.settings.statsOpen", open);
          }}
        >
          <summary class="panel-heading settings-section-summary">
            <div class="heading-with-icon">
              <span class="heading-icon"><Icon name="speed" size={17} /></span>
              <div>
                <h3>流量统计</h3>
                <span>通过 Xray API 采集实时速率与累计流量</span>
              </div>
            </div>
            <span class="settings-section-meta">
              <span classList={{ "tag": true, success: settings().statsEnabled, neutral: !settings().statsEnabled }}>
                {settings().statsEnabled ? "统计已开启" : "统计已关闭"}
              </span>
              <span class="settings-section-chevron"><Icon name="chevron" size={14} /></span>
            </span>
          </summary>
          <div class="settings-section-body">
            <label class="switch-row">
              <span>
                <strong>启用流量统计</strong>
                <small>启动代理后通过 Xray API 的 StatsService 轮询上/下行流量，用于仪表盘实时速率与累计流量。</small>
              </span>
              <span classList={{ "switch": true, on: settings().statsEnabled }}>
                <input
                  type="checkbox"
                  checked={settings().statsEnabled}
                  onChange={(event) => update("statsEnabled", event.currentTarget.checked)}
                />
                <span class="switch-track"><span class="switch-thumb" /></span>
              </span>
            </label>

            <Show when={!settings().xrayApiEnabled}>
              <div class="settings-inline-warning">
                <Icon name="alert" size={13} />
                <span>流量统计需要先在内核设置中开启「Xray API 与本地统计」，当前 API 端口为 {settings().xrayApiPort}。</span>
              </div>
            </Show>

            <p class="settings-note">
              <Icon name="speed" size={13} />
              <span>Xray API 端口（当前 {settings().xrayApiPort}）用于统计采样；关闭统计后仪表盘不再显示实时流量，但不影响代理转发。</span>
            </p>
          </div>
        </details>

        <details
          class="panel settings-section"
          open={integrationExpanded()}
          onToggle={(event) => {
            const open = event.currentTarget.open;
            setIntegrationExpanded(open);
            writeSectionState("fastv.settings.integrationOpen", open);
          }}
        >
          <summary class="panel-heading settings-section-summary">
            <div class="heading-with-icon">
              <span class="heading-icon"><Icon name="maximize" size={17} /></span>
              <div>
                <h3>系统集成</h3>
                <span>开机自启、托盘行为与应用更新</span>
              </div>
            </div>
            <span class="settings-section-meta">
              <span classList={{ "tag": true, success: autostartEnabled(), neutral: !autostartEnabled() }}>
                {autostartEnabled() ? "开机自启已开启" : "开机自启已关闭"}
              </span>
              <span class="settings-section-chevron"><Icon name="chevron" size={14} /></span>
            </span>
          </summary>
          <div class="settings-section-body">
            <label class="switch-row">
              <span>
                <strong>开机自启</strong>
                <small>{autostartEnabled() ? "已在当前用户的开机启动项（HKCU Run）中注册 FastV。" : "FastV 不会随 Windows 登录自动启动。"}</small>
              </span>
              <span classList={{ "switch": true, on: autostartEnabled() }}>
                <input
                  type="checkbox"
                  checked={autostartEnabled()}
                  disabled={autostartBusy()}
                  onChange={(event) => { void toggleAutostart(event.currentTarget.checked); }}
                />
                <span class="switch-track"><span class="switch-thumb" /></span>
              </span>
            </label>

            <Show when={autostartBusy()}>
              <p class="settings-note"><Icon name="refresh" size={13} /><span>正在更新开机启动项...</span></p>
            </Show>

            <Show when={autostartError()}>
              <div class="alert error settings-inline-alert">
                <span class="alert-message"><Icon name="alert" size={15} /> {autostartError()}</span>
              </div>
            </Show>

            <p class="settings-note">
              <Icon name="external" size={13} />
              <span>开机自启按当前 FastV.exe 路径注册；移动或重命名程序后需要重新开启一次。</span>
            </p>

            <div class="setting-block">
              <div class="setting-label">
                <strong>窗口行为</strong>
                <small>控制标题栏关闭和最小化按钮的行为，保存后立即生效。</small>
              </div>
              <label class="switch-row">
                <span>
                  <strong>关闭窗口时最小化到托盘</strong>
                  <small>开启后点击窗口关闭按钮不会退出 FastV，而是隐藏到系统托盘；可在托盘菜单重新打开窗口或退出应用。</small>
                </span>
                <span classList={{ "switch": true, on: settings().closeToTray }}>
                  <input
                    type="checkbox"
                    checked={settings().closeToTray}
                    onChange={(event) => update("closeToTray", event.currentTarget.checked)}
                  />
                  <span class="switch-track"><span class="switch-thumb" /></span>
                </span>
              </label>
              <label class="switch-row">
                <span>
                  <strong>最小化时隐藏到托盘</strong>
                  <small>开启后最小化窗口会隐藏到系统托盘。Windows 没有可拦截的最小化事件，FastV 会在原生最小化后立即隐藏窗口，可能短暂看到最小化动画（best effort）。</small>
                </span>
                <span classList={{ "switch": true, on: settings().minimizeToTray }}>
                  <input
                    type="checkbox"
                    checked={settings().minimizeToTray}
                    onChange={(event) => update("minimizeToTray", event.currentTarget.checked)}
                  />
                  <span class="switch-track"><span class="switch-thumb" /></span>
                </span>
              </label>
              <p class="settings-note">
                <Icon name="alert" size={13} />
                <span>两个开关都依赖系统托盘；托盘不可用（例如资源管理器异常）时，关闭窗口仍会退出应用。</span>
              </p>
            </div>

            <div class="setting-block">
              <div class="setting-label">
                <strong>应用更新</strong>
                <small>通过 GitHub Release 检查 FastV 自身的新版本，需要网络连接；检查失败只提示，不影响代理使用。</small>
              </div>
              <label class="switch-row">
                <span>
                  <strong>启动时自动检查更新</strong>
                  <small>应用启动后在后台请求 GitHub 最新 Release，结果会广播到本页展示。</small>
                </span>
                <span classList={{ "switch": true, on: settings().autoCheckUpdate }}>
                  <input
                    type="checkbox"
                    checked={settings().autoCheckUpdate}
                    onChange={(event) => update("autoCheckUpdate", event.currentTarget.checked)}
                  />
                  <span class="switch-track"><span class="switch-thumb" /></span>
                </span>
              </label>

              <div class="core-actions">
                <button
                  class="btn secondary"
                  type="button"
                  onClick={checkUpdate}
                  disabled={updateBusy() || updateDownloadBusy() || updateInstallBusy()}
                >
                  <Icon name="refresh" size={14} /> {updateBusy() ? "检查中..." : "立即检查更新"}
                </button>
                <Show when={updateInfo()?.url}>
                  {(url) => (
                    <button class="btn ghost" type="button" onClick={() => { void openReleasePage(url()); }}>
                      <Icon name="external" size={14} /> 打开发布页面
                    </button>
                  )}
                </Show>
              </div>

              <Show when={updateError()}>
                <div class="alert error settings-inline-alert">
                  <span class="alert-message"><Icon name="alert" size={15} /> {updateError()}</span>
                </div>
              </Show>

              <Show when={updateNotice()}>
                <div class="alert success settings-inline-alert">
                  <span class="alert-message"><Icon name="check" size={15} /> {updateNotice()}</span>
                </div>
              </Show>

              <Show when={updateInfo()}>
                {(info) => (
                  <div class="backup-result">
                    <div class="backup-result-grid">
                      <div><span>当前版本</span><strong>{info().currentVersion || "未知"}</strong></div>
                      <div><span>最新版本</span><strong>{info().latestVersion || "未获取"}</strong></div>
                      <div><span>状态</span><strong>{updateStatusLabel(info())}</strong></div>
                    </div>
                    <Show when={info().notes}>
                      <p class="backup-result-hint" style="white-space: pre-wrap;">更新说明：{info().notes}</p>
                    </Show>
                    <Show when={info().url}>
                      <div class="core-path-row">
                        <span>发布页面</span>
                        <code>{info().url}</code>
                      </div>
                    </Show>

                    <Show when={info().available}>
                      <Show when={!info().assetUrl}>
                        <div class="settings-inline-warning">
                          <Icon name="alert" size={13} />
                          <span>该版本暂未提供 Windows 安装包，无法在应用内自动下载安装；请打开上方的发布页面手动下载。</span>
                        </div>
                      </Show>

                      <Show when={info().assetUrl && !info().downloadedPath}>
                        <p class="backup-result-hint">
                          安装包：{info().assetName || "未命名安装包"} · {formatAssetSize(info().assetSize)}。下载可能需要数十秒，期间请保持网络连接。
                        </p>
                        <Show when={updateDownloadBusy()}>
                          <p class="settings-note">
                            <Icon name="refresh" size={13} />
                            <span>正在从 GitHub 下载并解压安装包，可能需要数十秒，请勿关闭 FastV。</span>
                          </p>
                        </Show>
                        <div class="core-actions">
                          <button
                            class="btn primary"
                            type="button"
                            onClick={downloadUpdate}
                            disabled={updateDownloadBusy() || updateInstallBusy() || updateBusy()}
                          >
                            <Icon name="external" size={14} /> {updateDownloadBusy() ? "正在下载更新…" : "下载更新"}
                          </button>
                        </div>
                      </Show>

                      <Show when={info().downloadedPath}>
                        <div class="backup-result-head" style="margin-top: 10px;">
                          <Icon name="check" size={14} />
                          <strong>更新已下载</strong>
                        </div>
                        <p class="backup-result-hint">
                          安装包：{info().assetName || "FastV 安装包"} · {formatAssetSize(info().assetSize)}。点击「立即重启并安装」后 FastV 会退出并自动替换程序文件。
                        </p>
                        <div class="core-path-row">
                          <span>下载位置</span>
                          <code>{info().downloadedPath}</code>
                        </div>
                        <Show when={proxyRunning()}>
                          <div class="settings-inline-warning">
                            <Icon name="alert" size={13} />
                            <span>代理正在运行：安装更新会退出 FastV 并短暂中断网络，建议先停止代理再安装。</span>
                          </div>
                        </Show>
                        <div class="core-actions">
                          <button
                            class="btn primary"
                            type="button"
                            onClick={installUpdate}
                            disabled={updateInstallBusy() || updateDownloadBusy() || updateBusy()}
                          >
                            <Icon name="refresh" size={14} /> {updateInstallBusy() ? "正在重启并安装…" : "立即重启并安装"}
                          </button>
                        </div>
                        <Show when={updateInstallBusy()}>
                          <p class="settings-note">
                            <Icon name="refresh" size={13} />
                            <span>正在退出 FastV 并替换程序文件，请勿手动启动应用；安装完成后 FastV 会自动重新启动。</span>
                          </p>
                        </Show>
                      </Show>
                    </Show>

                    <p class="backup-result-hint">
                      上次检查：{formatCheckedAt(info().checkedAt)}
                      {updateHint(info())}
                    </p>
                  </div>
                )}
              </Show>
              <Show when={!updateInfo()}>
                <p class="settings-note">
                  <Icon name="refresh" size={13} />
                  <span>尚未获取更新检查结果；点击「立即检查更新」。</span>
                </p>
              </Show>
            </div>
          </div>
        </details>

        <details
          class="panel settings-section"
          open={backupExpanded()}
          onToggle={(event) => {
            const open = event.currentTarget.open;
            setBackupExpanded(open);
            writeSectionState("fastv.settings.backupOpen", open);
          }}
        >
          <summary class="panel-heading settings-section-summary">
            <div class="heading-with-icon">
              <span class="heading-icon"><Icon name="subscription" size={17} /></span>
              <div>
                <h3>备份与恢复</h3>
                <span>导出或导入完整配置（设置、订阅、节点与路由规则）</span>
              </div>
            </div>
            <span class="settings-section-chevron"><Icon name="chevron" size={14} /></span>
          </summary>
          <div class="settings-section-body">
            <div class="backup-actions">
              <button class="btn secondary" type="button" onClick={exportBackup} disabled={backupBusy() !== ""}>
                <Icon name="external" size={14} /> {backupBusy() === "export" ? "导出中..." : "导出备份"}
              </button>
              <button class="btn secondary" type="button" onClick={() => backupFileInput?.click()} disabled={backupBusy() !== ""}>
                <Icon name="plus" size={14} /> {backupBusy() === "import" ? "导入中..." : "导入备份"}
              </button>
              <input
                ref={(element) => { backupFileInput = element; }}
                class="backup-file-input"
                type="file"
                accept="application/json"
                onChange={(event) => {
                  const file = event.currentTarget.files?.[0] ?? null;
                  event.currentTarget.value = "";
                  void importBackup(file);
                }}
              />
            </div>

            <Show when={backupMessage()}>
              <div class="alert success settings-inline-alert">
                <span class="alert-message"><Icon name="check" size={15} /> {backupMessage()}</span>
              </div>
            </Show>
            <Show when={backupError()}>
              <div class="alert error settings-inline-alert">
                <span class="alert-message"><Icon name="alert" size={15} /> {backupError()}</span>
              </div>
            </Show>

            <Show when={importResult()}>
              {(result) => (
                <div class="backup-result">
                  <div class="backup-result-head">
                    <Icon name="check" size={14} />
                    <strong>备份已恢复</strong>
                  </div>
                  <div class="backup-result-grid">
                    <div><span>订阅</span><strong>{result().restoredSubscriptions}</strong></div>
                    <div><span>节点</span><strong>{result().restoredNodes}</strong></div>
                    <div><span>路由规则</span><strong>{result().restoredRules}</strong></div>
                  </div>
                  <p class="backup-result-hint">导入已覆盖当前配置；代理不会自动重启，请重启代理后生效。</p>
                </div>
              )}
            </Show>

            <p class="settings-note">
              <Icon name="alert" size={13} />
              <span>备份文件包含节点凭据（UUID、密码等），请妥善保管，不要上传到公开位置；导入会覆盖当前配置。</span>
            </p>
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
