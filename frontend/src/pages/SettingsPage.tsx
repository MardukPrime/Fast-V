import { createSignal, For, onCleanup, onMount, Show } from "solid-js";

import { Browser, Events } from "@wailsio/runtime";

import { AppService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/app";
import { Service as BackupService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/backup";
import { CoreService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/core";
import { Service as EnvironmentService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/environment";
import type { CleanupResult, EnvironmentReport } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/environment";
import { ProxyService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/proxy";
import { SettingsService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/settings";
import type { BackupResult, CoreInfo, GeoDataInfo, ProxyState, Settings, SnapshotInfo, UpdateInfo } from "../../bindings/github.com/MardukPrime/Fast-V/internal/model";
import Icon from "../components/Icon";
import { LANGUAGES, t } from "../i18n";

type SettingsProps = {
  onSaved: (settings: Settings) => void;
  onPreviewTheme: (theme: string) => void;
  onPreviewAccent: (accent: string) => void;
  onPreviewLanguage: (language: string) => void;
  onPreviewDockTransparency: (value: number) => void;
};

const defaults: Settings = {
  theme: "light",
  language: "zh-CN",
  accentColor: "sky",
  layoutMode: "dock",
  systemProxyMode: "system",
  dockTransparency: 28,
  autoUpdate: false,
  subscriptionUpdateViaProxy: true,
  updateIntervalMinutes: 360,
  proxyMode: "rule",
  coreAutoUpdate: true,
  autoRestartCore: true,
  coreType: "xray",
  xrayLocalPort: 10808,
  xrayLogLevel: "warning",
  xrayMuxEnabled: false,
  xrayMuxConcurrency: 8,
  xraySniffing: true,
  xrayDnsStrategy: "UseIP",
  xrayDnsServers: "1.1.1.1,8.8.8.8",
  xrayDomainStrategy: "IPIfNonMatch",
  configOverride: "",
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
  hotkeysEnabled: true,
  hotkeyToggleProxy: "Ctrl+Shift+F",
  hotkeyCycleMode: "Ctrl+Shift+M",
  hotkeyNextNode: "Ctrl+Shift+N",
  autoCheckUpdate: true,
  statsEnabled: true,
  autoSelectEnabled: false,
  autoSelectIntervalMinutes: 10,
  autoSelectToleranceMs: 80,
  failoverEnabled: false,
  loadBalanceEnabled: false,
  loadBalanceMode: "leastPing",
  webdavUrl: "",
  webdavUsername: "",
  webdavPassword: "",
  webdavPath: "/FastV/fastv-backup.json",
  testUrl: "https://www.gstatic.com/generate_204",
  xrayApiEnabled: false,
  xrayApiPort: 10085,
};

const themes = () => [
  { value: "dark", label: t("set.themeDark"), hint: t("set.themeDarkHint") },
  { value: "light", label: t("set.themeLight"), hint: t("set.themeLightHint") },
  { value: "system", label: t("set.themeSystem"), hint: t("set.themeSystemHint") },
];

// Must stay in sync with the backend whitelist in internal/service/settings.
// The swatch palette itself is defined in styles.css for both page themes.
const accentColors = () => [
  { value: "sky", label: t("set.accentSky"), hint: t("set.accentSkyHint") },
  { value: "violet", label: t("set.accentViolet"), hint: t("set.accentVioletHint") },
  { value: "emerald", label: t("set.accentEmerald"), hint: t("set.accentEmeraldHint") },
  { value: "amber", label: t("set.accentAmber"), hint: t("set.accentAmberHint") },
  { value: "rose", label: t("set.accentRose"), hint: t("set.accentRoseHint") },
];

// Configurable global shortcuts. Keep the action order and the settings fields
// in sync with internal/hotkey/hotkey.go (settingsHotkeys): the order decides
// which row wins when two actions use the same combination.
type HotkeyField = "hotkeyToggleProxy" | "hotkeyCycleMode" | "hotkeyNextNode";

type HotkeyAction = {
  action: string;
  field: HotkeyField;
};

const hotkeyActions = (): HotkeyAction[] => [
  { action: t("set.hotkeyToggleProxy"), field: "hotkeyToggleProxy" },
  { action: t("set.hotkeyCycleMode"), field: "hotkeyCycleMode" },
  { action: t("set.hotkeyNextNode"), field: "hotkeyNextNode" },
];

const hotkeyModifierNames = ["Ctrl", "Alt", "Shift", "Win"] as const;
type HotkeyModifier = (typeof hotkeyModifierNames)[number];

const hotkeyMissingModifierError = () => t("set.hotkeyMissingModifier");
const hotkeyRecordingHint = () => t("set.hotkeyRecordingHint");

const createHotkeyErrors = (): Record<HotkeyField, string> => ({
  hotkeyToggleProxy: "",
  hotkeyCycleMode: "",
  hotkeyNextNode: "",
});

const parseHotkeyModifier = (part: string): HotkeyModifier | "" => {
  switch (part.toLowerCase()) {
    case "ctrl":
    case "control":
      return "Ctrl";
    case "alt":
      return "Alt";
    case "shift":
      return "Shift";
    case "win":
      return "Win";
    default:
      return "";
  }
};

// Mirrors internal/hotkey.parseMainKey: only the keys the recorder can produce
// are accepted (A-Z, 0-9 and F1-F24), always returned in upper case.
const normalizeHotkeyMainKey = (part: string): string => {
  const upper = part.trim().toUpperCase();
  if (/^[A-Z]$/.test(upper)) return upper;
  if (/^[0-9]$/.test(upper)) return upper;
  if (/^F(?:[1-9]|1[0-9]|2[0-4])$/.test(upper)) return upper;
  return "";
};

const formatHotkeyCombo = (modifiers: Set<HotkeyModifier>, mainKey: string): string => {
  const parts: string[] = [];
  for (const name of hotkeyModifierNames) {
    if (modifiers.has(name)) parts.push(name);
  }
  parts.push(mainKey);
  return parts.join("+");
};

// Parses one combination with the same rules as internal/hotkey.ParseCombo so a
// save can never be rejected by the backend after passing the frontend check.
// The canonical Ctrl+Alt+Shift+Win+Key form is returned on success.
const parseHotkeyCombo = (value: string): { combo: string; error: string } => {
  const trimmed = value.trim();
  if (!trimmed) return { combo: "", error: t("set.hotkeyEmpty") };
  const modifiers = new Set<HotkeyModifier>();
  let mainKey = "";
  for (const rawPart of trimmed.split("+")) {
    const part = rawPart.trim();
    if (!part) return { combo: "", error: t("set.hotkeyFormatError", { combo: trimmed }) };
    const modifier = parseHotkeyModifier(part);
    if (modifier) {
      if (modifiers.has(modifier)) return { combo: "", error: t("set.hotkeyDuplicateModifier", { modifier }) };
      modifiers.add(modifier);
      continue;
    }
    if (mainKey) return { combo: "", error: t("set.hotkeyOneMainKey", { combo: trimmed }) };
    const key = normalizeHotkeyMainKey(part);
    if (!key) return { combo: "", error: t("set.hotkeyUnsupportedKey", { part }) };
    mainKey = key;
  }
  if (!mainKey) return { combo: "", error: t("set.hotkeyMissingMainKey") };
  if (modifiers.size === 0) return { combo: "", error: hotkeyMissingModifierError() };
  return { combo: formatHotkeyCombo(modifiers, mainKey), error: "" };
};

// Equivalent combinations render identically even if the stored value is not
// canonical yet (the backend normalizes on save; old files may not be).
const hotkeyDisplayKeys = (value: string): string[] => {
  const parsed = parseHotkeyCombo(value);
  const display = parsed.combo || value.trim();
  return display ? display.split("+").filter(Boolean) : [];
};

// Maps a keydown event to a recordable main key. Physical key codes are used so
// Shift+1 records "1" instead of "!", and Numpad keys stay unsupported because
// the backend registration cannot distinguish them from the main row.
const hotkeyMainKeyFromEvent = (event: KeyboardEvent): string => {
  if (!event.code || event.code === "Unidentified") return normalizeHotkeyMainKey(event.key);
  const letter = /^Key([A-Z])$/.exec(event.code);
  if (letter) return letter[1];
  const digit = /^Digit([0-9])$/.exec(event.code);
  if (digit) return digit[1];
  const functionKey = /^F([1-9]|1[0-9]|2[0-4])$/.exec(event.code);
  if (functionKey) return `F${functionKey[1]}`;
  return "";
};

const isHotkeyModifierKey = (event: KeyboardEvent): boolean =>
  event.key === "Control" || event.key === "Alt" || event.key === "Shift" || event.key === "Meta" || event.key === "OS" || event.key === "Win";

const layoutModes = () => [
  { value: "sidebar", label: t("set.layoutSidebar"), hint: t("set.layoutSidebarHint") },
  { value: "dock", label: t("set.layoutDock"), hint: t("set.layoutDockHint") },
];

const proxyModes = () => [
  { value: "rule", label: t("set.proxyModeRule"), hint: t("set.proxyModeRuleHint") },
  { value: "global", label: t("set.proxyModeGlobal"), hint: t("set.proxyModeGlobalHint") },
  { value: "direct", label: t("set.proxyModeDirect"), hint: t("set.proxyModeDirectHint") },
];

// System proxy takeover modes. Values must stay in sync with the backend
// whitelist in internal/service/settings: "system" (fixed WinINet proxy),
// "off" (leave the registry untouched) and "pac" (Windows-only: FastV writes
// fastv.pac and points AutoConfigURL at it). The port is passed in because the
// hint mirrors the local inbound port shown by the rest of the section.
const systemProxyModes = (port: number) => [
  { value: "off", label: t("set.proxyMode.off"), hint: t("set.proxyMode.offHint") },
  { value: "system", label: t("set.proxyMode.system"), hint: t("set.systemProxyPointsTo", { port }) },
  { value: "pac", label: t("set.proxyMode.pac"), hint: t("set.proxyMode.pacHint") },
];

const coreTypes = () => [
  { value: "xray", label: "Xray-core", hint: t("set.coreXrayHint") },
  { value: "singbox", label: "sing-box", hint: t("set.coreSingboxHint") },
];

const sockoptDomainStrategies = () => [
  { value: "", label: t("set.defaultValue") },
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

const dnsStrategies = () => [
  { value: "UseIP", label: "UseIP", hint: t("set.dnsUseIpHint") },
  { value: "UseIPv4", label: "UseIPv4", hint: t("set.dnsUseIpv4Hint") },
  { value: "UseIPv6", label: "UseIPv6", hint: t("set.dnsUseIpv6Hint") },
  { value: "AsIs", label: "AsIs", hint: t("set.dnsAsIsHint") },
];

const domainStrategies = () => [
  { value: "IPIfNonMatch", label: "IPIfNonMatch", hint: t("set.domainIpIfNonMatchHint") },
  { value: "IPOnDemand", label: "IPOnDemand", hint: t("set.domainIpOnDemandHint") },
  { value: "AsIs", label: "AsIs", hint: t("set.domainAsIsHint") },
];

const logLevels = [
  { value: "warning", label: "warning" },
  { value: "info", label: "info" },
  { value: "debug", label: "debug" },
  { value: "error", label: "error" },
  { value: "none", label: "none" },
];

// Keep in sync with the backend validation in internal/service/settings.
const loadBalanceModes = () => [
  { value: "leastPing", label: t("set.loadBalanceLeastPing"), hint: t("set.loadBalanceLeastPingHint") },
  { value: "leastLoad", label: t("set.loadBalanceLeastLoad"), hint: t("set.loadBalanceLeastLoadHint") },
  { value: "roundRobin", label: t("set.loadBalanceRoundRobin"), hint: t("set.loadBalanceRoundRobinHint") },
];

const loadBalanceModeValues = ["leastPing", "leastLoad", "roundRobin"];

function coreStatusLabel(status: string) {
  switch (status) {
    case "ready": return t("set.coreStatusReady");
    case "up_to_date": return t("set.coreStatusUpToDate");
    case "update_available": return t("set.coreStatusUpdateAvailable");
    case "checking": return t("set.coreStatusChecking");
    case "downloading": return t("set.coreStatusDownloading");
    case "not_installed": return t("set.coreStatusNotInstalled");
    case "error": return t("set.coreStatusError");
    default: return status || t("set.coreStatusNotInstalled");
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
  if (!value) return t("set.unknown");
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

// ListSnapshots already returns the newest snapshot first; sorting again keeps
// the list stable if the backend order ever changes, and invalid timestamps are
// pushed to the end instead of breaking the comparison.
function snapshotTimeValue(value: string) {
  const time = Date.parse(value);
  return Number.isNaN(time) ? 0 : time;
}

function sortSnapshots(list: SnapshotInfo[]) {
  return [...list].sort((left, right) => snapshotTimeValue(right.createdAt) - snapshotTimeValue(left.createdAt));
}

// BackupService.ImportSnapshot rejects with this backend error message when the
// user closes the native file dialog; it is a cancellation, not a failure, so the
// UI must not render it as an error.
const SNAPSHOT_IMPORT_CANCELLED_ERROR = "已取消导入";

function isSnapshotImportCancelled(err: unknown) {
  return errorMessage(err).includes(SNAPSHOT_IMPORT_CANCELLED_ERROR);
}

// The self-update panel reports the release asset size in MB; the backend
// returns 0 when GitHub does not expose a size for the asset.
function formatAssetSize(size: number) {
  if (!Number.isFinite(size) || size <= 0) return t("set.sizeUnknown");
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}

function updateStatusLabel(info: UpdateInfo) {
  if (info.downloadedPath) return t("set.updateStatusDownloaded");
  if (info.error) return t("set.updateStatusFailed");
  if (info.available) return info.assetUrl ? t("set.updateStatusAvailable") : t("set.updateStatusAvailableManual");
  return t("set.updateStatusLatest");
}

function updateHint(info: UpdateInfo) {
  if (info.downloadedPath) return t("set.updateHintDownloaded");
  if (info.error) return "";
  if (!info.available) return t("set.updateHintNone");
  return info.assetUrl ? t("set.updateHintAuto") : t("set.updateHintManual");
}

const autoSelectIntervalRange = { min: 1, max: 1440 };
const autoSelectToleranceRange = { min: 0, max: 5000 };

// Mirrors internal/service/settings: the override is trimmed by the backend, the
// stored text may be empty, must parse as a JSON object and may not exceed
// 128 KB. Validating locally keeps the inline error instant and blocks a save
// before the request is sent.
const configOverrideMaxBytes = 128 * 1024;

function configOverrideByteLength(value: string) {
  if (typeof TextEncoder === "undefined") return value.length;
  return new TextEncoder().encode(value).length;
}

function validateConfigOverride(value: string) {
  const trimmed = value.trim();
  if (!trimmed) return "";
  if (configOverrideByteLength(trimmed) > configOverrideMaxBytes) {
    return t("set.configOverrideTooLarge");
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed);
  } catch {
    return t("set.configOverrideInvalidJson");
  }
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    return t("set.configOverrideNotObject");
  }
  return "";
}

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
  // The top bar can switch proxyMode while this page is open. Only overwrite the
  // stored proxy mode from this form when the user actually changed it here.
  const [proxyModeDirty, setProxyModeDirty] = createSignal(false);
  const [clientExpanded, setClientExpanded] = createSignal(readSectionState("fastv.settings.clientOpen", false));
  const [coreExpanded, setCoreExpanded] = createSignal(readSectionState("fastv.settings.coreOpen", false));
  const [autoSelectExpanded, setAutoSelectExpanded] = createSignal(readSectionState("fastv.settings.autoSelectOpen", false));
  const [statsExpanded, setStatsExpanded] = createSignal(readSectionState("fastv.settings.statsOpen", false));
  const [integrationExpanded, setIntegrationExpanded] = createSignal(readSectionState("fastv.settings.integrationOpen", false));
  const [backupExpanded, setBackupExpanded] = createSignal(readSectionState("fastv.settings.backupOpen", false));
  const [autostartEnabled, setAutostartEnabled] = createSignal(false);
  const [autostartBusy, setAutostartBusy] = createSignal(false);
  const [autostartError, setAutostartError] = createSignal("");
  // Environment cleanup: Cleanup already returns the refreshed report, so the
  // panel keeps the latest report plus the last cleanup result (and never runs
  // two environment operations at once).
  const [envBusy, setEnvBusy] = createSignal<"inspect" | "cleanup" | "">("");
  const [envReport, setEnvReport] = createSignal<EnvironmentReport | null>(null);
  const [envCleanup, setEnvCleanup] = createSignal<CleanupResult | null>(null);
  const [envError, setEnvError] = createSignal("");
  const envResiduals = () => envReport()?.residuals ?? [];
  const envRemoved = () => envCleanup()?.removed ?? [];
  const envFailed = () => envCleanup()?.failed ?? [];
  const [backupBusy, setBackupBusy] = createSignal("");
  const [backupMessage, setBackupMessage] = createSignal("");
  const [backupError, setBackupError] = createSignal("");
  const [importResult, setImportResult] = createSignal<BackupResult | null>(null);
  // WebDAV cloud backup: one shared busy flag ("test" | "upload" | "download")
  // because the operations must not overlap, plus per-operation result state.
  const [webdavBusy, setWebdavBusy] = createSignal("");
  const [webdavMessage, setWebdavMessage] = createSignal("");
  const [webdavError, setWebdavError] = createSignal("");
  const [webdavRemoteUrl, setWebdavRemoteUrl] = createSignal("");
  const [webdavRestoreResult, setWebdavRestoreResult] = createSignal<BackupResult | null>(null);
  // Local configuration snapshots: the list plus one shared busy flag
  // ("load" | "create" | "restore:<id>" | "delete:<id>") so two operations can
  // never race on the same snapshot directory.
  const [snapshots, setSnapshots] = createSignal<SnapshotInfo[]>([]);
  const [snapshotsBusy, setSnapshotsBusy] = createSignal("");
  const [snapshotsMessage, setSnapshotsMessage] = createSignal("");
  const [snapshotsError, setSnapshotsError] = createSignal("");
  const [updateBusy, setUpdateBusy] = createSignal(false);
  const [updateInfo, setUpdateInfo] = createSignal<UpdateInfo | null>(null);
  const [updateError, setUpdateError] = createSignal("");
  const [updateDownloadBusy, setUpdateDownloadBusy] = createSignal(false);
  const [updateInstallBusy, setUpdateInstallBusy] = createSignal(false);
  const [updateNotice, setUpdateNotice] = createSignal("");
  const [proxyRunning, setProxyRunning] = createSignal(false);
  // Global hotkey recorder state: which row (if any) is currently recording,
  // plus the per-row inline error shown below the action name.
  const [recordingHotkey, setRecordingHotkey] = createSignal<HotkeyField | "">("");
  const [hotkeyErrors, setHotkeyErrors] = createSignal<Record<HotkeyField, string>>(createHotkeyErrors());
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
        setAutostartError(t("set.autostartReadError", { error: errorMessage(err) }));
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

  const setHotkeyRowError = (field: HotkeyField, message: string) => {
    setHotkeyErrors((previous) => ({ ...previous, [field]: message }));
  };

  const cancelHotkeyRecording = () => {
    const field = recordingHotkey();
    if (!field) return;
    setHotkeyRowError(field, "");
    setRecordingHotkey("");
  };

  const startHotkeyRecording = (field: HotkeyField) => {
    if (!settings().hotkeysEnabled) return;
    const active = recordingHotkey();
    if (active && active !== field) setHotkeyRowError(active, "");
    setHotkeyRowError(field, "");
    setMessage("");
    setRecordingHotkey(field);
  };

  const clearHotkey = (field: HotkeyField) => {
    if (recordingHotkey() === field) setRecordingHotkey("");
    // Removing a combination can resolve a duplicate reported for another row.
    setHotkeyErrors(createHotkeyErrors());
    update(field, "");
  };

  // Recording handler. It is registered once on window in the capture phase so
  // the recorded combination cannot trigger page shortcuts (and Enter cannot
  // submit the form) before it is consumed; preventDefault also suppresses the
  // browser default for the pressed keys.
  const handleHotkeyKeydown = (event: KeyboardEvent) => {
    const field = recordingHotkey();
    if (!field) return;
    event.preventDefault();
    event.stopPropagation();
    if (event.repeat) return;
    if (event.key === "Escape") {
      cancelHotkeyRecording();
      return;
    }
    const mainKey = hotkeyMainKeyFromEvent(event);
    if (!mainKey) {
      // Typing a bare modifier is part of building a combination, not an error.
      if (isHotkeyModifierKey(event)) return;
      setHotkeyRowError(field, t("set.hotkeyRecordUnsupported", { key: event.key || event.code }));
      return;
    }
    const modifiers = new Set<HotkeyModifier>();
    if (event.ctrlKey) modifiers.add("Ctrl");
    if (event.altKey) modifiers.add("Alt");
    if (event.shiftKey) modifiers.add("Shift");
    if (event.metaKey) modifiers.add("Win");
    if (modifiers.size === 0) {
      setHotkeyRowError(field, hotkeyMissingModifierError());
      return;
    }
    update(field, formatHotkeyCombo(modifiers, mainKey));
    // Any change can resolve a duplicate reported for another row, so the
    // per-row hints are recomputed on the next save.
    setHotkeyErrors(createHotkeyErrors());
    setRecordingHotkey("");
  };

  onMount(() => {
    window.addEventListener("keydown", handleHotkeyKeydown, true);
    onCleanup(() => window.removeEventListener("keydown", handleHotkeyKeydown, true));
  });

  const validateAutoSelect = () => {
    const current = settings();
    const interval = current.autoSelectIntervalMinutes;
    const tolerance = current.autoSelectToleranceMs;
    if (!Number.isFinite(interval) || interval < autoSelectIntervalRange.min || interval > autoSelectIntervalRange.max) {
      return t("set.validationAutoSelectInterval", { min: autoSelectIntervalRange.min, max: autoSelectIntervalRange.max });
    }
    if (!Number.isFinite(tolerance) || tolerance < autoSelectToleranceRange.min || tolerance > autoSelectToleranceRange.max) {
      return t("set.validationTolerance", { min: autoSelectToleranceRange.min, max: autoSelectToleranceRange.max });
    }
    return "";
  };

  const validateInboundAuth = () => {
    const current = settings();
    if (!current.inboundAuthEnabled) return "";
    const username = current.inboundUsername;
    if (!username) return t("set.validationInboundUsernameRequired");
    if (/\s/.test(username)) return t("set.validationInboundUsernameWhitespace");
    const password = current.inboundPassword;
    if (!password) return t("set.validationInboundPasswordRequired");
    if ([...password].length < 6) return t("set.validationInboundPasswordLength");
    return "";
  };

  // The backend only accepts the three Xray balancer strategies; block anything
  // else before it reaches SettingsService.Save. An empty value keeps the
  // leastPing default, mirroring the backend behaviour.
  const validateLoadBalance = () => {
    const mode = settings().loadBalanceMode.trim();
    if (mode !== "" && !loadBalanceModeValues.includes(mode)) {
      return t("set.validationLoadBalanceMode");
    }
    return "";
  };

  // Re-evaluated on every keystroke: the inline error under the override editor
  // and the save-time guard share the same message.
  const configOverrideError = () => validateConfigOverride(settings().configOverride);

  const formatConfigOverride = () => {
    const raw = settings().configOverride.trim();
    if (!raw) return;
    let parsed: unknown;
    try {
      parsed = JSON.parse(raw);
    } catch {
      // The inline hint below the editor already explains the parse error.
      return;
    }
    update("configOverride", JSON.stringify(parsed, null, 2));
  };

  const clearConfigOverride = () => update("configOverride", "");

  // Mirrors internal/hotkey.ValidateSettingsCombos: every non-empty combination
  // must parse and no two actions may share the same normalized combination.
  // Empty values are allowed and simply disable that action. The per-row map is
  // used to highlight the offending row next to the page-level alert.
  const collectHotkeyErrors = () => {
    const current = settings();
    const actions = hotkeyActions();
    const rowErrors = createHotkeyErrors();
    const seen = new Map<string, HotkeyField>();
    let firstError = "";
    for (const item of actions) {
      const raw = current[item.field].trim();
      if (!raw) continue;
      const parsed = parseHotkeyCombo(raw);
      if (parsed.error) {
        rowErrors[item.field] = parsed.error;
        if (!firstError) firstError = t("set.hotkeyInvalid", { action: item.action, error: parsed.error });
        continue;
      }
      const previousField = seen.get(parsed.combo);
      if (previousField) {
        const previousAction = actions.find((candidate) => candidate.field === previousField)?.action ?? previousField;
        rowErrors[item.field] = t("set.hotkeyDuplicateRow", { action: previousAction, combo: parsed.combo });
        rowErrors[previousField] = rowErrors[previousField] || t("set.hotkeyDuplicateRow", { action: item.action, combo: parsed.combo });
        if (!firstError) firstError = t("set.hotkeyDuplicate", { first: previousAction, second: item.action, combo: parsed.combo });
        continue;
      }
      seen.set(parsed.combo, item.field);
    }
    return { firstError, rowErrors };
  };

  const save = async (event: Event) => {
    event.preventDefault();
    setRecordingHotkey("");
    const autoSelectError = validateAutoSelect();
    const inboundError = validateInboundAuth();
    const loadBalanceError = validateLoadBalance();
    const hotkeys = collectHotkeyErrors();
    const hotkeyError = hotkeys.firstError;
    const configOverrideValidation = configOverrideError();
    const validationError = autoSelectError || inboundError || loadBalanceError || hotkeyError || configOverrideValidation;
    if (validationError) {
      if (inboundError) {
        setClientExpanded(true);
        writeSectionState("fastv.settings.clientOpen", true);
      }
      if (autoSelectError || loadBalanceError) {
        setAutoSelectExpanded(true);
        writeSectionState("fastv.settings.autoSelectOpen", true);
      }
      if (hotkeyError) {
        setHotkeyErrors(hotkeys.rowErrors);
        setIntegrationExpanded(true);
        writeSectionState("fastv.settings.integrationOpen", true);
      }
      if (configOverrideValidation) {
        // The editor lives inside the collapsible core section; open it so the
        // inline error and the field are visible instead of only the page alert.
        setCoreExpanded(true);
        writeSectionState("fastv.settings.coreOpen", true);
      }
      setMessage("");
      setError(validationError);
      return;
    }
    setSaving(true);
    setError("");
    setMessage("");
    try {
      const input: Settings = { ...settings() };
      // Persist the canonical Ctrl+Alt+Shift+Win+Key form (the backend does the
      // same normalization); an empty value keeps the action disabled.
      for (const item of hotkeyActions()) {
        const raw = input[item.field].trim();
        input[item.field] = raw ? parseHotkeyCombo(raw).combo : "";
      }
      if (!proxyModeDirty()) {
        // Merge the freshest proxy mode so a top-bar switch made after this page
        // mounted is not reverted by an unrelated settings save.
        const latest = await SettingsService.Get().catch(() => null);
        if (latest) input.proxyMode = latest.proxyMode;
      }
      const saved = await SettingsService.Save(input);
      setSettings(saved);
      setProxyModeDirty(false);
      setHotkeyErrors(createHotkeyErrors());
      setRecordingHotkey("");
      props.onSaved(saved);
      setMessage(t("set.saved"));
      if (saved.tunMode && !elevated()) {
        setMessage(t("set.tunElevation"));
        await AppService.RestartAsAdmin();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const reset = async () => {
    if (!window.confirm(t("set.resetConfirm"))) return;
    setSaving(true);
    setError("");
    setMessage("");
    try {
      const resetSettings = await SettingsService.Reset();
      setSettings(resetSettings);
      setHotkeyErrors(createHotkeyErrors());
      setRecordingHotkey("");
      props.onSaved(resetSettings);
      setMessage(t("set.resetDone"));
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
      setMessage(enabled ? t("set.autostartEnabledMessage") : t("set.autostartDisabledMessage"));
    } catch (err) {
      setAutostartEnabled(previous);
      setAutostartError(t("set.autostartError", { error: errorMessage(err) }));
    } finally {
      setAutostartBusy(false);
    }
  };

  // Environment cleanup inspects (and removes) the OS leftovers of a previous
  // install: the fastv:// protocol registration, the autostart entry and the
  // generated PAC file. User data is never touched, so the confirm dialog only
  // explains what is removed and points at the data directory that stays.
  const inspectEnvironment = async () => {
    if (envBusy()) return;
    setEnvBusy("inspect");
    setEnvError("");
    setEnvCleanup(null);
    try {
      setEnvReport(await EnvironmentService.Inspect());
    } catch (err) {
      setEnvError(t("set.env.inspectError", { error: errorMessage(err) }));
    } finally {
      setEnvBusy("");
    }
  };

  const cleanupEnvironment = async () => {
    if (envBusy()) return;
    const dataDir = envReport()?.dataDir;
    const lines = [t("set.env.cleanupConfirm"), "", t("set.env.cleanupConfirmData")];
    if (dataDir) lines.push("", t("set.env.cleanupConfirmDir", { dataDir }));
    if (!window.confirm(lines.join("\n"))) return;
    setEnvBusy("cleanup");
    setEnvError("");
    setEnvCleanup(null);
    try {
      const result = await EnvironmentService.Cleanup();
      setEnvCleanup(result);
      // Cleanup returns the state after the cleanup, which keeps the status
      // card in sync without a second Inspect round-trip.
      setEnvReport(result.report);
    } catch (err) {
      setEnvError(t("set.env.cleanupError", { error: errorMessage(err) }));
    } finally {
      setEnvBusy("");
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
      setBackupMessage(t("set.exportDone", { file: `fastv-backup-${formatBackupTimestamp(new Date())}.json` }));
    } catch (err) {
      setBackupError(t("set.exportError", { error: errorMessage(err) }));
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
      setBackupMessage(t("set.importDone"));
      try {
        const restored = await SettingsService.Get();
        setSettings(restored);
        props.onSaved(restored);
      } catch {
        // The restore already succeeded; a stale form is better than a false error.
      }
    } catch (err) {
      setBackupError(t("set.importError", { error: errorMessage(err) }));
    } finally {
      setBackupBusy("");
    }
  };

  // Clearing the cloud result before every WebDAV operation keeps a stale
  // remote URL or restore summary from being mistaken for the new outcome.
  const clearWebdavResult = () => {
    setWebdavMessage("");
    setWebdavError("");
    setWebdavRemoteUrl("");
    setWebdavRestoreResult(null);
  };

  const testWebdav = async () => {
    if (webdavBusy()) return;
    setWebdavBusy("test");
    clearWebdavResult();
    try {
      await BackupService.WebDAVTest();
      setWebdavMessage(t("set.webdavTestOk"));
    } catch (err) {
      setWebdavError(t("set.webdavTestError", { error: errorMessage(err) }));
    } finally {
      setWebdavBusy("");
    }
  };

  const uploadWebdav = async () => {
    if (webdavBusy()) return;
    setWebdavBusy("upload");
    clearWebdavResult();
    try {
      const remoteUrl = await BackupService.WebDAVUpload();
      setWebdavRemoteUrl(remoteUrl);
      setWebdavMessage(t("set.webdavUploadOk"));
    } catch (err) {
      setWebdavError(t("set.webdavUploadError", { error: errorMessage(err) }));
    } finally {
      setWebdavBusy("");
    }
  };

  const restoreWebdav = async () => {
    if (webdavBusy()) return;
    const confirmed = window.confirm(t("set.webdavRestoreConfirm"));
    if (!confirmed) return;
    setWebdavBusy("download");
    clearWebdavResult();
    try {
      const result = await BackupService.WebDAVDownload();
      setWebdavRestoreResult(result);
      setWebdavMessage(t("set.webdavRestoreOk"));
      // The restore replaced the persisted settings; reload the form so it does
      // not keep showing pre-restore values.
      try {
        const restored = await SettingsService.Get();
        setSettings(restored);
        props.onSaved(restored);
      } catch {
        // The restore itself succeeded; a stale form is better than a false error.
      }
    } catch (err) {
      setWebdavError(t("set.webdavRestoreError", { error: errorMessage(err) }));
    } finally {
      setWebdavBusy("");
    }
  };

  // Snapshots are sorted newest first for display; the backend already returns
  // them in that order but sorting again keeps the UI deterministic.
  const snapshotRows = () => sortSnapshots(snapshots());

  // Fetch the snapshot list without touching the shared busy flag; the load
  // button and every operation that changes the list reuse it.
  const refreshSnapshots = async () => {
    const list = await BackupService.ListSnapshots();
    setSnapshots(list ?? []);
  };

  const loadSnapshots = async () => {
    if (snapshotsBusy()) return;
    setSnapshotsBusy("load");
    setSnapshotsError("");
    try {
      await refreshSnapshots();
    } catch (err) {
      setSnapshotsError(t("set.snapshots.loadError", { error: errorMessage(err) }));
    } finally {
      setSnapshotsBusy("");
    }
  };

  // A window.prompt keeps the creation flow minimal; an empty name is sent as
  // an empty string so the backend assigns its default label.
  const createSnapshot = async () => {
    if (snapshotsBusy()) return;
    const label = window.prompt(t("set.snapshots.createPrompt"), "");
    if (label === null) return;
    setSnapshotsBusy("create");
    setSnapshotsMessage("");
    setSnapshotsError("");
    try {
      await BackupService.CreateSnapshot(label.trim());
      await refreshSnapshots();
      setSnapshotsMessage(t("set.snapshots.createDone"));
    } catch (err) {
      setSnapshotsError(t("set.snapshots.createError", { error: errorMessage(err) }));
    } finally {
      setSnapshotsBusy("");
    }
  };

  const restoreSnapshot = async (snapshot: SnapshotInfo) => {
    if (snapshotsBusy()) return;
    if (!window.confirm(t("set.snapshots.restoreConfirm", { label: snapshot.label }))) return;
    setSnapshotsBusy(`restore:${snapshot.id}`);
    setSnapshotsMessage("");
    setSnapshotsError("");
    try {
      await BackupService.RestoreSnapshot(snapshot.id);
      await refreshSnapshots();
      setSnapshotsMessage(t("set.snapshots.restoreDone"));
      // The rollback replaced the persisted settings; reload the form exactly
      // like the import path does so it does not keep showing old values.
      try {
        const restored = await SettingsService.Get();
        setSettings(restored);
        setProxyModeDirty(false);
        props.onSaved(restored);
      } catch {
        // The rollback already succeeded; a stale form is better than a false error.
      }
    } catch (err) {
      setSnapshotsError(t("set.snapshots.restoreError", { error: errorMessage(err) }));
    } finally {
      setSnapshotsBusy("");
    }
  };

  const deleteSnapshot = async (snapshot: SnapshotInfo) => {
    if (snapshotsBusy()) return;
    if (!window.confirm(t("set.snapshots.deleteConfirm", { label: snapshot.label }))) return;
    setSnapshotsBusy(`delete:${snapshot.id}`);
    setSnapshotsMessage("");
    setSnapshotsError("");
    try {
      await BackupService.DeleteSnapshot(snapshot.id);
      await refreshSnapshots();
      setSnapshotsMessage(t("set.snapshots.deleteDone"));
    } catch (err) {
      setSnapshotsError(t("set.snapshots.deleteError", { error: errorMessage(err) }));
    } finally {
      setSnapshotsBusy("");
    }
  };

  // Opening the native save dialog shares the busy flag with the other snapshot
  // operations so two dialogs can never overlap. An empty path means the user
  // closed the dialog: that is a cancellation, not a failed export.
  const exportSnapshot = async (snapshot: SnapshotInfo) => {
    if (snapshotsBusy()) return;
    setSnapshotsBusy(`export:${snapshot.id}`);
    setSnapshotsMessage("");
    setSnapshotsError("");
    try {
      const path = await BackupService.ExportSnapshot(snapshot.id);
      await refreshSnapshots();
      setSnapshotsMessage(path ? t("set.snapshots.exported", { path }) : t("set.snapshots.exportCancelled"));
    } catch (err) {
      setSnapshotsError(t("set.snapshots.exportError", { error: errorMessage(err) }));
    } finally {
      setSnapshotsBusy("");
    }
  };

  // Importing stores a chosen backup file as a new local snapshot. The empty
  // label lets the backend apply its default name; a closed file dialog is
  // reported as a cancellation message instead of an error.
  const importSnapshot = async () => {
    if (snapshotsBusy()) return;
    setSnapshotsBusy("import");
    setSnapshotsMessage("");
    setSnapshotsError("");
    try {
      await BackupService.ImportSnapshot("");
      await refreshSnapshots();
      setSnapshotsMessage(t("set.snapshots.imported"));
    } catch (err) {
      if (isSnapshotImportCancelled(err)) {
        setSnapshotsMessage(t("set.snapshots.importCancelled"));
      } else {
        setSnapshotsError(t("set.snapshots.importFailed", { error: errorMessage(err) }));
      }
    } finally {
      setSnapshotsBusy("");
    }
  };

  // Snapshots live outside the settings document, so they are loaded once on
  // mount instead of as part of load(); reading the list is cheap.
  onMount(() => {
    void loadSnapshots();
  });

  const refreshGeoData = async () => {
    setGeoBusy("refresh");
    setError("");
    setMessage("");
    try {
      setGeoData(await CoreService.GeoData());
      setMessage(t("set.geoDataRefreshed"));
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
      setMessage(t("set.geoDataUpdated"));
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
        setMessage(info.available ? t("set.updateFound", { version: info.latestVersion }) : t("set.updateLatest"));
      }
    } catch (err) {
      setUpdateError(t("set.updateCheckError", { error: errorMessage(err) }));
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
        setUpdateError(t("set.updateDownloadError", { error: info.error }));
      } else {
        setUpdateNotice(t("set.updateDownloadNotice", { name: info.assetName || t("set.installerFallback"), size: formatAssetSize(info.assetSize) }));
      }
    } catch (err) {
      setUpdateError(t("set.updateDownloadError", { error: errorMessage(err) }));
    } finally {
      setUpdateDownloadBusy(false);
    }
  };

  const installUpdate = async () => {
    if (updateBusy() || updateDownloadBusy() || updateInstallBusy()) return;
    const info = updateInfo();
    if (!info?.downloadedPath) {
      setUpdateError(t("set.updateNoPackage"));
      return;
    }
    const lines = [
      t("set.installConfirm", { version: info.latestVersion || t("set.newVersion") }),
      "",
      t("set.installWillExit"),
    ];
    if (proxyRunning()) {
      lines.push("", t("set.installProxyWarning"));
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
      setUpdateNotice(t("set.installing"));
    } catch (err) {
      setUpdateError(t("set.installError", { error: errorMessage(err) }));
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
      <Show when={!loading()} fallback={<div class="empty-state">{t("set.loading")}</div>}>
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
                <h3>{t("set.clientSection")}</h3>
                <span>{t("set.clientSectionHint")}</span>
              </div>
            </div>
            <span class="settings-section-chevron"><Icon name="chevron" size={14} /></span>
          </summary>
          <div class="settings-section-body">
          <div class="setting-block">
            <div class="setting-label">
              <strong>{t("set.theme")}</strong>
              <small>{t("set.themeHint")}</small>
            </div>
            <div class="theme-options">
              {themes().map((item) => (
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
            <div class="accent-picker">
              <div class="setting-label">
                <strong>{t("set.accent")}</strong>
                <small>{t("set.accentHint")}</small>
              </div>
              <div class="accent-options" role="radiogroup" aria-label={t("set.accent")}>
                {accentColors().map((item) => (
                  <button
                    type="button"
                    role="radio"
                    aria-checked={settings().accentColor === item.value}
                    aria-label={item.label}
                    classList={{ "accent-option": true, active: settings().accentColor === item.value }}
                    onClick={() => {
                      update("accentColor", item.value);
                      props.onPreviewAccent(item.value);
                    }}
                  >
                    <span classList={{ "accent-swatch": true, [item.value]: true }} aria-hidden="true" />
                    <span class="accent-option-copy">
                      <strong>{item.label}</strong>
                      <small>{item.hint}</small>
                    </span>
                    <Show when={settings().accentColor === item.value}>
                      <span class="accent-check"><Icon name="check" size={11} /></span>
                    </Show>
                  </button>
                ))}
              </div>
            </div>
          </div>

          <div class="setting-block">
            <div class="setting-label">
              <strong>{t("set.layout")}</strong>
              <small>{t("set.layoutHint")}</small>
            </div>
            <div class="mode-options">
              {layoutModes().map((item) => (
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
                <strong>{t("set.dockTransparency")}</strong>
                <small>{t("set.dockTransparencyHint")}</small>
              </div>
              <div class="dock-transparency-row">
                <input type="range" min="5" max="90" value={settings().dockTransparency} onInput={(event) => { const value = Number(event.currentTarget.value); update("dockTransparency", value); props.onPreviewDockTransparency(value); }} />
                <strong>{settings().dockTransparency}%</strong>
              </div>
              <div class="dock-transparency-scale"><span>{t("set.dockMoreSolid")}</span><span>{t("set.dockMoreTransparent")}</span></div>
            </div>
          </Show>

          <div class="settings-grid compact">
            <label>
              <span>{t("set.language")}</span>
              <select value={settings().language} onChange={(event) => {
                update("language", event.currentTarget.value);
                props.onPreviewLanguage(event.currentTarget.value);
              }}>
                {LANGUAGES.map((item) => <option value={item.value}>{item.label}</option>)}
              </select>
              <small>{t("set.languageHint")}</small>
            </label>
          </div>

                    <div class="setting-block">
            <div class="setting-label">
              <strong>{t("set.systemProxy")}</strong>
              <small>{t("set.systemProxyHint")}</small>
            </div>
            <div class="mode-options">
              {systemProxyModes(settings().xrayLocalPort).map((item) => (
                <button
                  type="button"
                  classList={{ "mode-option": true, active: settings().systemProxyMode === item.value }}
                  onClick={() => update("systemProxyMode", item.value)}
                >
                  <strong>{item.label}</strong>
                  <small>{item.hint}</small>
                </button>
              ))}
            </div>
            <Show when={settings().systemProxyMode === "pac"}>
              <div class="settings-inline-warning">
                <Icon name="alert" size={13} />
                <span>{t("set.proxyMode.pacNote")}</span>
              </div>
            </Show>
            <Show when={settings().tunMode}>
              <p class="settings-note">
                <Icon name="alert" size={13} />
                <span>{t("set.systemProxySkippedTun")}</span>
              </p>
            </Show>
          </div>
<div class="setting-block">
            <div class="setting-label">
              <strong>{t("set.defaultProxyMode")}</strong>
              <small>{t("set.defaultProxyModeHint")}</small>
            </div>
            <div class="mode-options">
              {proxyModes().map((item) => (
                <button
                  type="button"
                  classList={{ "mode-option": true, active: settings().proxyMode === item.value }}
                  onClick={() => {
                    update("proxyMode", item.value);
                    setProxyModeDirty(true);
                  }}
                >
                  <strong>{item.label}</strong>
                  <small>{item.hint}</small>
                </button>
              ))}
            </div>
            <div class="settings-grid compact">
              <label>
                <span>{t("set.testUrl")}</span>
                <input
                  value={settings().testUrl}
                  onInput={(event) => update("testUrl", event.currentTarget.value)}
                  placeholder="https://www.gstatic.com/generate_204"
                />
                <small>{t("set.testUrlHint")}</small>
              </label>
            </div>
          </div>

          <div class="setting-block">
            <div class="setting-label">
              <strong>{t("set.networkTakeover")}</strong>
              <small>{t("set.networkTakeoverHint")}</small>
            </div>
            <label class="switch-row tun-mode-row">
              <span>
                <strong>{t("set.tunMode")}</strong>
                <small>{t("set.tunModeHint")}</small>
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
                <span>{t("set.tunWarning", { status: elevated() ? t("set.elevated") : t("set.notElevated") })}</span>
              </div>
            </Show>

            <label class="switch-row">
              <span>
                <strong>{t("set.allowLan")}</strong>
                <small>{settings().allowLan ? t("set.allowLanOn", { port: settings().xrayLocalPort }) : t("set.allowLanOff")}</small>
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
                <span>{t("set.allowLanWarning")}</span>
              </div>
            </Show>

            <label class="switch-row">
              <span>
                <strong>{t("set.inboundAuth")}</strong>
                <small>{t("set.inboundAuthHint")}</small>
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
                  <span>{t("set.inboundUsername")}</span>
                  <input
                    value={settings().inboundUsername}
                    onInput={(event) => update("inboundUsername", event.currentTarget.value)}
                    placeholder={t("set.inboundUsernamePlaceholder")}
                  />
                  <small>{t("set.inboundUsernameHint")}</small>
                </label>
                <label>
                  <span>{t("set.inboundPassword")}</span>
                  <input
                    type="password"
                    value={settings().inboundPassword}
                    onInput={(event) => update("inboundPassword", event.currentTarget.value)}
                    placeholder={t("set.inboundPasswordPlaceholder")}
                  />
                  <small>{t("set.inboundPasswordHint")}</small>
                </label>
              </div>
              <div class="settings-inline-warning">
                <Icon name="alert" size={13} />
                <span>{t("set.inboundAuthWarning")}</span>
              </div>
            </Show>
          </div>

          <div class="setting-block">
            <div class="setting-label">
              <strong>{t("set.subscriptionUpdate")}</strong>
              <small>{t("set.subscriptionUpdateHint")}</small>
            </div>
            <div class="settings-grid">
              <label>
                <span>{t("set.updateInterval")}</span>
                <input
                  type="number"
                  min="15"
                  max="1440"
                  value={settings().updateIntervalMinutes}
                  onInput={(event) => update("updateIntervalMinutes", Number(event.currentTarget.value))}
                />
                <small>{t("set.updateIntervalHint")}</small>
              </label>
            </div>
            <label class="switch-row">
              <span>
                <strong>{t("set.autoUpdateSubscriptions")}</strong>
                <small>{t("set.autoUpdateSubscriptionsHint")}</small>
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
            <label class="switch-row">
              <span>
                <strong>{t("set.subscriptionViaProxy")}</strong>
                <small>{t("set.subscriptionViaProxyHint")}</small>
              </span>
              <span classList={{ "switch": true, on: settings().subscriptionUpdateViaProxy }}>
                <input
                  type="checkbox"
                  checked={settings().subscriptionUpdateViaProxy}
                  onChange={(event) => update("subscriptionUpdateViaProxy", event.currentTarget.checked)}
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
                <h3>{t("set.coreSection")}</h3>
                <span>{t("set.coreSectionHint")}</span>
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
              <strong>{t("set.coreType")}</strong>
              <small>{t("set.coreTypeHint")}</small>
            </div>
            <div class="core-type-options">
              {coreTypes().map((item) => (
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
              <strong>{t("set.singboxNotReady")}</strong>
              <span>{t("set.singboxNotReadyHint")}</span>
            </div>
          }>
            <div class="core-summary">
              <div class="core-version">
                <span>{t("set.currentVersion")}</span>
                <strong>{core()?.version || t("set.notInstalled")}</strong>
              </div>
              <div class="core-version">
                <span>{t("set.latestVersion")}</span>
                <strong>{core()?.latestVersion || t("set.notChecked")}</strong>
              </div>
              <div class="core-version">
                <span>{t("set.updateMethod")}</span>
                <strong>{settings().coreAutoUpdate ? t("set.autoDetect") : t("set.manualCheck")}</strong>
              </div>
            </div>

            <p class="core-message">{core()?.message || t("set.coreMessageDefault")}</p>

            <div class="core-actions">
              <button class="btn secondary" type="button" onClick={checkCore} disabled={coreBusy() !== ""}>
                <Icon name="refresh" size={14} /> {coreBusy() === "checking" ? t("set.checking") : t("set.checkUpdate")}
              </button>
              <button class="btn primary" type="button" onClick={downloadCore} disabled={coreBusy() !== ""}>
                <Icon name="external" size={14} /> {coreBusy() === "downloading" ? t("set.downloading") : t("set.downloadLatest")}
              </button>
            </div>

            <div class="core-params">
              <div class="setting-label">
                <strong>{t("set.xrayRuntimeParams")}</strong>
                <small>{t("set.xrayRuntimeParamsHint")}</small>
              </div>
              <div class="core-param-grid">
                <label>
                  <span>{t("set.localPort")}</span>
                  <input
                    type="number"
                    min="1"
                    max="65535"
                    value={settings().xrayLocalPort}
                    onInput={(event) => update("xrayLocalPort", Number(event.currentTarget.value))}
                  />
                  <small>{t("set.localPortHint")}</small>
                </label>
                <label>
                  <span>{t("set.logLevel")}</span>
                  <select value={settings().xrayLogLevel} onChange={(event) => update("xrayLogLevel", event.currentTarget.value)}>
                    {logLevels.map((level) => <option value={level.value}>{level.label}</option>)}
                  </select>
                  <small>{t("set.logLevelHint")}</small>
                </label>
                <label>
                  <span>{t("set.muxConcurrency")}</span>
                  <input
                    type="number"
                    min="1"
                    max="128"
                    value={settings().xrayMuxConcurrency}
                    disabled={!settings().xrayMuxEnabled}
                    onInput={(event) => update("xrayMuxConcurrency", Number(event.currentTarget.value))}
                  />
                  <small>{t("set.muxConcurrencyHint")}</small>
                </label>
                <label>
                  <span>{t("set.apiPort")}</span>
                  <input
                    type="number"
                    min="1"
                    max="65535"
                    value={settings().xrayApiPort}
                    disabled={!settings().xrayApiEnabled}
                    onInput={(event) => update("xrayApiPort", Number(event.currentTarget.value))}
                  />
                  <small>{t("set.apiPortHint")}</small>
                </label>
              </div>

              <div class="core-switch-grid">
                <label class="switch-row">
                  <span>
                    <strong>{t("set.enableMux")}</strong>
                    <small>{t("set.enableMuxHint")}</small>
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
                    <strong>{t("set.enableSniffing")}</strong>
                    <small>{t("set.enableSniffingHint")}</small>
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
                    <strong>{t("set.enableXrayApi")}</strong>
                    <small>{t("set.enableXrayApiHint")}</small>
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
                <strong>{t("set.xrayAdvanced")}</strong>
                <small>{t("set.xrayAdvancedHint")}</small>
              </div>
              <div class="core-param-grid">
                <label>
                  <span>{t("set.dnsServers")}</span>
                  <input value={settings().xrayDnsServers} onInput={(event) => update("xrayDnsServers", event.currentTarget.value)} placeholder="1.1.1.1,8.8.8.8" />
                  <small>{t("set.dnsServersHint")}</small>
                </label>
                <label>
                  <span>{t("set.dnsStrategy")}</span>
                  <select value={settings().xrayDnsStrategy} onChange={(event) => update("xrayDnsStrategy", event.currentTarget.value)}>
                    {dnsStrategies().map((item) => <option value={item.value}>{item.label} · {item.hint}</option>)}
                  </select>
                  <small>{t("set.dnsStrategyHint")}</small>
                </label>
                <label>
                  <span>{t("set.domainStrategy")}</span>
                  <select value={settings().xrayDomainStrategy} onChange={(event) => update("xrayDomainStrategy", event.currentTarget.value)}>
                    {domainStrategies().map((item) => <option value={item.value}>{item.label} · {item.hint}</option>)}
                  </select>
                  <small>{t("set.domainStrategyHint")}</small>
                </label>
                <label>
                  <span>{t("set.tunMtu")}</span>
                  <input type="number" min="576" max="9000" value={settings().xrayTunMtu} onInput={(event) => update("xrayTunMtu", Number(event.currentTarget.value))} />
                  <small>{t("set.tunMtuHint")}</small>
                </label>
              </div>
              <div class="core-path-row">
                <span>{t("set.corePath")}</span>
                <code>{core()?.path || t("set.notInstalled")}</code>
              </div>
            </div>
            <div class="setting-block transport-advanced-block">
              <div class="setting-label">
                <strong>{t("set.transportAdvanced")}</strong>
                <small>{t("set.transportAdvancedHint")}</small>
              </div>

              <div class="core-switch-grid">
                <label class="switch-row">
                  <span><strong>TCP Fast Open</strong><small>{t("set.tfoHint")}</small></span>
                  <span classList={{ "switch": true, on: settings().xraySockoptTfo }}>
                    <input type="checkbox" checked={settings().xraySockoptTfo} onChange={(event) => update("xraySockoptTfo", event.currentTarget.checked)} />
                    <span class="switch-track"><span class="switch-thumb" /></span>
                  </span>
                </label>
                <label class="switch-row">
                  <span><strong>TCP MPTCP</strong><small>{t("set.mptcpHint")}</small></span>
                  <span classList={{ "switch": true, on: settings().xraySockoptMptcp }}>
                    <input type="checkbox" checked={settings().xraySockoptMptcp} onChange={(event) => update("xraySockoptMptcp", event.currentTarget.checked)} />
                    <span class="switch-track"><span class="switch-thumb" /></span>
                  </span>
                </label>
              </div>

              <div class="core-param-grid">
                <label>
                  <span>{t("set.keepAliveIdle")}</span>
                  <input type="number" min="0" value={settings().xraySockoptKeepIdle} onInput={(event) => update("xraySockoptKeepIdle", Number(event.currentTarget.value))} />
                  <small>{t("set.keepAliveHint")}</small>
                </label>
                <label>
                  <span>{t("set.keepAliveInterval")}</span>
                  <input type="number" min="0" value={settings().xraySockoptKeepIntvl} onInput={(event) => update("xraySockoptKeepIntvl", Number(event.currentTarget.value))} />
                  <small>{t("set.keepAliveHint")}</small>
                </label>
                <label>
                  <span>{t("set.userTimeout")}</span>
                  <input type="number" min="0" value={settings().xraySockoptUserTimeout} onInput={(event) => update("xraySockoptUserTimeout", Number(event.currentTarget.value))} />
                  <small>{t("set.userTimeoutHint")}</small>
                </label>
                <label>
                  <span>{t("set.congestion")}</span>
                  <input value={settings().xraySockoptCongestion} onInput={(event) => update("xraySockoptCongestion", event.currentTarget.value)} placeholder={t("set.congestionPlaceholder")} />
                  <small>{t("set.congestionHint")}</small>
                </label>
                <label>
                  <span>{t("set.sockoptDomainStrategy")}</span>
                  <select value={settings().xraySockoptDomainStrategy} onChange={(event) => update("xraySockoptDomainStrategy", event.currentTarget.value)}>
                    {sockoptDomainStrategies().map((item) => <option value={item.value}>{item.label}</option>)}
                  </select>
                  <small>{t("set.sockoptDomainStrategyHint")}</small>
                </label>
              </div>

              <div class="advanced-json-grid">
                <label>
                  <span>{t("set.sockoptJson")}</span>
                  <textarea value={settings().xraySockoptJson} onInput={(event) => update("xraySockoptJson", event.currentTarget.value)} spellcheck={false} placeholder={'{\n  "acceptProxyProtocol": false\n}'} />
                  <small>{t("set.sockoptJsonHint")}</small>
                </label>
                <label>
                  <span>{t("set.finalMaskJson")}</span>
                  <textarea value={settings().xrayFinalMaskJson} onInput={(event) => update("xrayFinalMaskJson", event.currentTarget.value)} spellcheck={false} placeholder={'{\n  "tcp": [\n    { "type": "fragment", "settings": { "packets": "tlshello" } }\n  ]\n}'} />
                  <small>{t("set.finalMaskJsonHint")}</small>
                </label>
              </div>

              <div class="core-actions">
                <button class="btn secondary" type="button" onClick={() => update("xrayFinalMaskJson", finalMaskTemplate)}>
                  <Icon name="plus" size={14} /> {t("set.insertFragment")}
                </button>
                <button class="btn ghost" type="button" onClick={() => update("xrayFinalMaskJson", "")}>
                  {t("set.clearFinalMask")}
                </button>
              </div>
            </div>
            <div class="setting-block config-override-block">
              <div class="setting-label">
                <strong>{t("set.configOverride")}</strong>
                <small>{t("set.configOverrideHint")}</small>
              </div>
              <label class="config-override-field">
                <textarea
                  classList={{
                    "config-override-input": true,
                    "is-invalid": configOverrideError() !== "",
                  }}
                  value={settings().configOverride}
                  onInput={(event) => update("configOverride", event.currentTarget.value)}
                  spellcheck={false}
                  rows={10}
                  placeholder={t("set.configOverridePlaceholder")}
                  aria-invalid={configOverrideError() !== ""}
                  aria-describedby="config-override-hint"
                />
                <Show when={configOverrideError()}>
                  <span class="config-override-error" role="alert">
                    <Icon name="alert" size={13} /> {configOverrideError()}
                  </span>
                </Show>
                <small id="config-override-hint" class="config-override-hint">
                  {t("set.configOverrideMaxSize")} · {t("set.configOverrideRestart")}
                </small>
              </label>
              <div class="core-actions">
                <button
                  class="btn secondary"
                  type="button"
                  onClick={formatConfigOverride}
                  disabled={!settings().configOverride.trim()}
                >
                  {t("set.configOverrideFormat")}
                </button>
                <button
                  class="btn ghost"
                  type="button"
                  onClick={clearConfigOverride}
                  disabled={!settings().configOverride}
                >
                  {t("set.configOverrideClear")}
                </button>
              </div>
            </div>
            <div class="geodata-block">
              <div class="setting-label">
                <strong>{t("set.geoData")}</strong>
                <small>{t("set.geoDataHint")}</small>
              </div>
              <div class="geodata-files">
                <div class="geodata-file">
                  <span>GeoIP</span>
                  <strong>{geoData()?.geoIp ? t("set.installed") : t("set.missing")}</strong>
                  <small>{geoData()?.geoIp ? `${formatBytes(geoData()!.geoIp!.size)} · ${geoData()!.geoIp!.name}` : t("set.geoIpFile")}</small>
                </div>
                <div class="geodata-file">
                  <span>{t("set.geoSite")}</span>
                  <strong>{geoData()?.geoSite ? t("set.installed") : t("set.missing")}</strong>
                  <small>{geoData()?.geoSite ? `${formatBytes(geoData()!.geoSite!.size)} · ${geoData()!.geoSite!.name}` : t("set.geoSiteFile")}</small>
                </div>
                <div class="geodata-file">
                  <span>{t("set.resourceFiles")}</span>
                  <strong>{geoData()?.files?.length ?? 0}</strong>
                  <small>{geoData()?.directory ?? t("set.coreDirectory")}</small>
                </div>
              </div>
              <div class="core-actions">
                <button class="btn secondary" type="button" onClick={refreshGeoData} disabled={geoBusy() !== ""}>
                  <Icon name="refresh" size={14} /> {geoBusy() === "refresh" ? t("set.refreshing") : t("set.refreshStatus")}
                </button>
                <button class="btn primary" type="button" onClick={updateGeoData} disabled={geoBusy() !== ""}>
                  <Icon name="external" size={14} /> {geoBusy() === "update" ? t("set.updating") : t("set.updateGeoData")}
                </button>
              </div>
              <p class="core-message">{t("set.geoDataNote")}</p>
            </div>
            <label class="switch-row">
              <span>
                <strong>{t("set.coreAutoUpdate")}</strong>
                <small>{t("set.coreAutoUpdateHint")}</small>
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
                <h3>{t("set.smartRouting")}</h3>
                <span>{t("set.smartRoutingHint")}</span>
              </div>
            </div>
            <span class="settings-section-meta">
              <Show when={settings().loadBalanceEnabled}>
                <span class="tag success">{t("set.loadBalanceOn")}</span>
              </Show>
              <span classList={{ "tag": true, success: settings().autoSelectEnabled, neutral: !settings().autoSelectEnabled }}>
                {settings().autoSelectEnabled ? t("set.autoSelectOn") : t("set.autoSelectOff")}
              </span>
              <span class="settings-section-chevron"><Icon name="chevron" size={14} /></span>
            </span>
          </summary>
          <div class="settings-section-body">
            <label class="switch-row">
              <span>
                <strong>{t("set.autoSelect")}</strong>
                <small>{t("set.autoSelectHint")}</small>
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
                <span>{t("set.loadBalancePauseWarning")}</span>
              </div>
            </Show>

            <div class="settings-grid">
              <label>
                <span>{t("set.selectInterval")}</span>
                <input
                  type="number"
                  min={autoSelectIntervalRange.min}
                  max={autoSelectIntervalRange.max}
                  step="1"
                  value={settings().autoSelectIntervalMinutes}
                  onInput={(event) => update("autoSelectIntervalMinutes", Number(event.currentTarget.value))}
                />
                <small>{t("set.selectIntervalHint", { min: autoSelectIntervalRange.min, max: autoSelectIntervalRange.max })}</small>
              </label>
              <label>
                <span>{t("set.switchTolerance")}</span>
                <input
                  type="number"
                  min={autoSelectToleranceRange.min}
                  max={autoSelectToleranceRange.max}
                  step="1"
                  value={settings().autoSelectToleranceMs}
                  onInput={(event) => update("autoSelectToleranceMs", Number(event.currentTarget.value))}
                />
                <small>{t("set.switchToleranceHint", { min: autoSelectToleranceRange.min, max: autoSelectToleranceRange.max })}</small>
              </label>
            </div>

            <label class="switch-row">
              <span>
                <strong>{t("set.failover")}</strong>
                <small>{t("set.failoverHint")}</small>
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
              <span>{t("set.autoSelectNote")}</span>
            </p>

            <div class="setting-block">
              <div class="setting-label">
                <strong>{t("set.loadBalance")}</strong>
                <small>{t("set.loadBalanceHint")}</small>
              </div>

              <label class="switch-row">
                <span>
                  <strong>{t("set.enableLoadBalance")}</strong>
                  <small>{t("set.enableLoadBalanceHint")}</small>
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
                  <span>{t("set.loadBalanceMode")}</span>
                  <select value={settings().loadBalanceMode} onChange={(event) => update("loadBalanceMode", event.currentTarget.value)}>
                    {loadBalanceModes().map((item) => <option value={item.value}>{item.label} · {item.hint}</option>)}
                  </select>
                  <small>{t("set.loadBalanceModeHint")}</small>
                </label>
              </div>

              <Show when={settings().loadBalanceEnabled}>
                <Show when={settings().autoSelectEnabled || settings().failoverEnabled}>
                  <div class="settings-inline-warning">
                    <Icon name="alert" size={13} />
                    <span>{t("set.loadBalanceConflictWarning")}</span>
                  </div>
                </Show>

                <Show when={settings().tunMode}>
                  <div class="settings-inline-warning">
                    <Icon name="alert" size={13} />
                    <span>{t("set.loadBalanceTunWarning")}</span>
                  </div>
                </Show>

                <p class="settings-note">
                  <Icon name="route" size={13} />
                  <span>{t("set.loadBalanceNote")}</span>
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
                <h3>{t("set.stats")}</h3>
                <span>{t("set.statsHint")}</span>
              </div>
            </div>
            <span class="settings-section-meta">
              <span classList={{ "tag": true, success: settings().statsEnabled, neutral: !settings().statsEnabled }}>
                {settings().statsEnabled ? t("set.statsOn") : t("set.statsOff")}
              </span>
              <span class="settings-section-chevron"><Icon name="chevron" size={14} /></span>
            </span>
          </summary>
          <div class="settings-section-body">
            <label class="switch-row">
              <span>
                <strong>{t("set.enableStats")}</strong>
                <small>{t("set.enableStatsHint")}</small>
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
                <span>{t("set.statsApiWarning", { port: settings().xrayApiPort })}</span>
              </div>
            </Show>

            <p class="settings-note">
              <Icon name="speed" size={13} />
              <span>{t("set.statsNote", { port: settings().xrayApiPort })}</span>
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
                <h3>{t("set.integration")}</h3>
                <span>{t("set.integrationHint")}</span>
              </div>
            </div>
            <span class="settings-section-meta">
              <span classList={{ "tag": true, success: autostartEnabled(), neutral: !autostartEnabled() }}>
                {autostartEnabled() ? t("set.autostartOn") : t("set.autostartOff")}
              </span>
              <span class="settings-section-chevron"><Icon name="chevron" size={14} /></span>
            </span>
          </summary>
          <div class="settings-section-body">
            <label class="switch-row">
              <span>
                <strong>{t("set.autostart")}</strong>
                <small>{autostartEnabled() ? t("set.autostartEnabledHint") : t("set.autostartDisabledHint")}</small>
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
              <p class="settings-note"><Icon name="refresh" size={13} /><span>{t("set.autostartBusy")}</span></p>
            </Show>

            <Show when={autostartError()}>
              <div class="alert error settings-inline-alert">
                <span class="alert-message"><Icon name="alert" size={15} /> {autostartError()}</span>
              </div>
            </Show>

            <p class="settings-note">
              <Icon name="external" size={13} />
              <span>{t("set.autostartNote")}</span>
            </p>

            <div class="setting-block">
              <div class="setting-label">
                <strong>{t("set.reliability")}</strong>
                <small>{t("set.reliabilityHint")}</small>
              </div>
              <label class="switch-row">
                <span>
                  <strong>{t("set.autoRestartCore")}</strong>
                  <small>{settings().autoRestartCore ? t("set.autoRestartCoreOnHint") : t("set.autoRestartCoreOffHint")}</small>
                </span>
                <span classList={{ "switch": true, on: settings().autoRestartCore }}>
                  <input
                    type="checkbox"
                    checked={settings().autoRestartCore}
                    onChange={(event) => update("autoRestartCore", event.currentTarget.checked)}
                  />
                  <span class="switch-track"><span class="switch-thumb" /></span>
                </span>
              </label>
            </div>

            <div class="setting-block">
              <div class="setting-label">
                <strong>{t("set.hotkeys")}</strong>
                <small>{t("set.hotkeysHint")}</small>
              </div>
              <label class="switch-row">
                <span>
                  <strong>{t("set.enableHotkeys")}</strong>
                  <small>{t("set.enableHotkeysHint")}</small>
                </span>
                <span classList={{ "switch": true, on: settings().hotkeysEnabled }}>
                  <input
                    type="checkbox"
                    checked={settings().hotkeysEnabled}
                    onChange={(event) => {
                      const enabled = event.currentTarget.checked;
                      update("hotkeysEnabled", enabled);
                      if (!enabled) {
                        setRecordingHotkey("");
                        setHotkeyErrors(createHotkeyErrors());
                      }
                    }}
                  />
                  <span class="switch-track"><span class="switch-thumb" /></span>
                </span>
              </label>

              <ul classList={{ "hotkey-list": true, disabled: !settings().hotkeysEnabled }} aria-label={t("set.hotkeyListAria")}>
                <For each={hotkeyActions()}>
                  {(item) => {
                    const keys = () => hotkeyDisplayKeys(settings()[item.field]);
                    const isRecording = () => recordingHotkey() === item.field;
                    const rowError = () => hotkeyErrors()[item.field];
                    return (
                      <li classList={{ "hotkey-row": true, recording: isRecording(), "has-error": !!rowError() }}>
                        <span class="hotkey-row-main">
                          <span class="hotkey-action">{item.action}</span>
                          <span classList={{ "hotkey-keys": true, empty: keys().length === 0 }}>
                            <Show when={keys().length > 0} fallback={<span class="hotkey-empty">{t("set.hotkeyNotSet")}</span>}>
                              <For each={keys()}>
                                {(key, index) => (
                                  <>
                                    <Show when={index() > 0}>
                                      <span class="hotkey-plus" aria-hidden="true">+</span>
                                    </Show>
                                    <kbd class="hotkey-key">{key}</kbd>
                                  </>
                                )}
                              </For>
                            </Show>
                          </span>
                        </span>
                        <span class="hotkey-row-controls">
                          <button
                            type="button"
                            classList={{ "btn": true, "hotkey-record-btn": true, recording: isRecording() }}
                            aria-pressed={isRecording()}
                            aria-label={isRecording() ? t("set.hotkeyRecordingAria", { action: item.action }) : t("set.hotkeyRecordAria", { action: item.action })}
                            disabled={!settings().hotkeysEnabled}
                            onClick={() => {
                              if (isRecording()) cancelHotkeyRecording();
                              else startHotkeyRecording(item.field);
                            }}
                          >
                            {isRecording() ? t("set.hotkeyPress") : t("set.hotkeyRecord")}
                          </button>
                          <button
                            type="button"
                            class="btn ghost compact hotkey-clear-btn"
                            aria-label={t("set.hotkeyClearAria", { action: item.action })}
                            disabled={!settings().hotkeysEnabled || (!settings()[item.field] && !isRecording())}
                            onClick={() => clearHotkey(item.field)}
                          >
                            {t("set.hotkeyClear")}
                          </button>
                        </span>
                        <Show when={isRecording()}>
                          <span class="hotkey-row-hint" role="status" aria-live="polite">{hotkeyRecordingHint()}</span>
                        </Show>
                        <Show when={rowError()}>
                          <span class="hotkey-row-error" role="alert">{rowError()}</span>
                        </Show>
                      </li>
                    );
                  }}
                </For>
              </ul>

              <p class="settings-note">
                <Icon name="alert" size={13} />
                <span>{t("set.hotkeyNote")}</span>
              </p>
            </div>

            <div class="setting-block">
              <div class="setting-label">
                <strong>{t("set.windowBehavior")}</strong>
                <small>{t("set.windowBehaviorHint")}</small>
              </div>
              <label class="switch-row">
                <span>
                  <strong>{t("set.closeToTray")}</strong>
                  <small>{t("set.closeToTrayHint")}</small>
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
                  <strong>{t("set.minimizeToTray")}</strong>
                  <small>{t("set.minimizeToTrayHint")}</small>
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
                <span>{t("set.trayNote")}</span>
              </p>
            </div>

            <div class="setting-block">
              <div class="setting-label">
                <strong>{t("set.appUpdate")}</strong>
                <small>{t("set.appUpdateHint")}</small>
              </div>
              <label class="switch-row">
                <span>
                  <strong>{t("set.autoCheckUpdate")}</strong>
                  <small>{t("set.autoCheckUpdateHint")}</small>
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
                  <Icon name="refresh" size={14} /> {updateBusy() ? t("set.checking") : t("set.checkUpdateNow")}
                </button>
                <Show when={updateInfo()?.url}>
                  {(url) => (
                    <button class="btn ghost" type="button" onClick={() => { void openReleasePage(url()); }}>
                      <Icon name="external" size={14} /> {t("set.openReleasePage")}
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
                      <div><span>{t("set.currentVersion")}</span><strong>{info().currentVersion || t("set.unknown")}</strong></div>
                      <div><span>{t("set.latestVersion")}</span><strong>{info().latestVersion || t("set.notFetched")}</strong></div>
                      <div><span>{t("set.status")}</span><strong>{updateStatusLabel(info())}</strong></div>
                    </div>
                    <Show when={info().notes}>
                      <p class="backup-result-hint" style="white-space: pre-wrap;">{t("set.updateNotes", { notes: info().notes ?? "" })}</p>
                    </Show>
                    <Show when={info().url}>
                      <div class="core-path-row">
                        <span>{t("set.releasePage")}</span>
                        <code>{info().url}</code>
                      </div>
                    </Show>

                    <Show when={info().available}>
                      <Show when={!info().assetUrl}>
                        <div class="settings-inline-warning">
                          <Icon name="alert" size={13} />
                          <span>{t("set.updateNoInstaller")}</span>
                        </div>
                      </Show>

                      <Show when={info().assetUrl && !info().downloadedPath}>
                        <p class="backup-result-hint">
                          {t("set.installerInfo", { name: info().assetName || t("set.unnamedInstaller"), size: formatAssetSize(info().assetSize) })}
                        </p>
                        <Show when={updateDownloadBusy()}>
                          <p class="settings-note">
                            <Icon name="refresh" size={13} />
                            <span>{t("set.updateDownloadingNote")}</span>
                          </p>
                        </Show>
                        <div class="core-actions">
                          <button
                            class="btn primary"
                            type="button"
                            onClick={downloadUpdate}
                            disabled={updateDownloadBusy() || updateInstallBusy() || updateBusy()}
                          >
                            <Icon name="external" size={14} /> {updateDownloadBusy() ? t("set.updateDownloading") : t("set.updateDownload")}
                          </button>
                        </div>
                      </Show>

                      <Show when={info().downloadedPath}>
                        <div class="backup-result-head" style="margin-top: 10px;">
                          <Icon name="check" size={14} />
                          <strong>{t("set.updateDownloaded")}</strong>
                        </div>
                        <p class="backup-result-hint">
                          {t("set.installerReady", { name: info().assetName || t("set.fastvInstaller"), size: formatAssetSize(info().assetSize) })}
                        </p>
                        <div class="core-path-row">
                          <span>{t("set.downloadLocation")}</span>
                          <code>{info().downloadedPath}</code>
                        </div>
                        <Show when={proxyRunning()}>
                          <div class="settings-inline-warning">
                            <Icon name="alert" size={13} />
                            <span>{t("set.updateProxyWarning")}</span>
                          </div>
                        </Show>
                        <div class="core-actions">
                          <button
                            class="btn primary"
                            type="button"
                            onClick={installUpdate}
                            disabled={updateInstallBusy() || updateDownloadBusy() || updateBusy()}
                          >
                            <Icon name="refresh" size={14} /> {updateInstallBusy() ? t("set.installingUpdate") : t("set.restartAndInstall")}
                          </button>
                        </div>
                        <Show when={updateInstallBusy()}>
                          <p class="settings-note">
                            <Icon name="refresh" size={13} />
                            <span>{t("set.updateInstallingNote")}</span>
                          </p>
                        </Show>
                      </Show>
                    </Show>

                    <p class="backup-result-hint">
                      {t("set.lastChecked", { time: formatCheckedAt(info().checkedAt) })}
                      {updateHint(info())}
                    </p>
                  </div>
                )}
              </Show>
              <Show when={!updateInfo()}>
                <p class="settings-note">
                  <Icon name="refresh" size={13} />
                  <span>{t("set.updateEmpty")}</span>
                </p>
              </Show>
            </div>

            <div class="setting-block">
              <div class="setting-label">
                <strong>{t("set.env.title")}</strong>
                <small>{t("set.env.hint")}</small>
              </div>

              <p class="settings-note">
                <Icon name="alert" size={13} />
                <span>{t("set.env.description")}</span>
              </p>
              <p class="settings-note">
                <Icon name="check" size={13} />
                <span>{t("set.env.keepData")}</span>
              </p>

              <div class="core-actions">
                <button
                  class="btn secondary"
                  type="button"
                  onClick={() => { void inspectEnvironment(); }}
                  disabled={envBusy() !== ""}
                >
                  <Icon name="search" size={14} /> {envBusy() === "inspect" ? t("set.env.checking") : t("set.env.check")}
                </button>
                <button
                  class="btn secondary"
                  type="button"
                  onClick={() => { void cleanupEnvironment(); }}
                  disabled={envBusy() !== ""}
                >
                  <Icon name="trash" size={14} /> {envBusy() === "cleanup" ? t("set.env.cleaning") : t("set.env.cleanup")}
                </button>
              </div>

              <Show when={envError()}>
                <div class="alert error settings-inline-alert">
                  <span class="alert-message"><Icon name="alert" size={15} /> {envError()}</span>
                </div>
              </Show>

              <Show when={envReport()}>
                {(report) => (
                  <div class="backup-result">
                    <div class="backup-result-grid">
                      <div>
                        <span>{t("set.env.protocol")}</span>
                        <span classList={{ "tag": true, success: report().protocolRegistered, neutral: !report().protocolRegistered }}>
                          {report().protocolRegistered ? t("set.env.yes") : t("set.env.no")}
                        </span>
                      </div>
                      <div>
                        <span>{t("set.env.autostart")}</span>
                        <span classList={{ "tag": true, success: report().autostartEnabled, neutral: !report().autostartEnabled }}>
                          {report().autostartEnabled ? t("set.env.yes") : t("set.env.no")}
                        </span>
                      </div>
                      <div>
                        <span>{t("set.env.pac")}</span>
                        <span classList={{ "tag": true, success: report().pacFilePresent, neutral: !report().pacFilePresent }}>
                          {report().pacFilePresent ? t("set.env.yes") : t("set.env.no")}
                        </span>
                      </div>
                    </div>

                    <Show
                      when={envResiduals().length > 0}
                      fallback={
                        <p class="settings-note">
                          <Icon name="check" size={13} />
                          <span>{t("set.env.residualsEmpty")}</span>
                        </p>
                      }
                    >
                      <div class="selfcheck-list" role="list" aria-label={t("set.env.residuals")}>
                        <For each={envResiduals()}>
                          {(item) => (
                            <div class="selfcheck-item warn" role="listitem">
                              <span class="selfcheck-badge warn"><Icon name="alert" size={12} /> {t("set.env.residual")}</span>
                              <div class="selfcheck-body"><strong class="selfcheck-name">{item}</strong></div>
                            </div>
                          )}
                        </For>
                      </div>
                    </Show>

                    <div class="core-path-row">
                      <span>{t("set.env.dataDir")}</span>
                      <code
                        style="white-space: normal; overflow: visible; overflow-wrap: anywhere; text-overflow: clip;"
                        title={report().dataDir}
                      >
                        {report().dataDir || t("set.unknown")}
                      </code>
                    </div>
                    <p class="backup-result-hint">{t("set.env.dataDirHint")}</p>
                    <p class="backup-result-hint">{t("set.env.checkedAt", { time: formatCheckedAt(report().checkedAt) })}</p>
                  </div>
                )}
              </Show>

              <Show when={envCleanup()}>
                {(result) => (
                  <>
                    <div classList={{ "alert": true, success: envFailed().length === 0, error: envFailed().length > 0, "settings-inline-alert": true }}>
                      <span class="alert-message">
                        <Icon name={envFailed().length === 0 ? "check" : "alert"} size={15} /> {result().message || t("set.env.cleanupDone")}
                      </span>
                    </div>

                    <Show when={envRemoved().length > 0}>
                      <div class="selfcheck-list" role="list" aria-label={t("set.env.removed")}>
                        <For each={envRemoved()}>
                          {(item) => (
                            <div class="selfcheck-item ok" role="listitem">
                              <span class="selfcheck-badge ok"><Icon name="check" size={12} /> {t("set.env.removed")}</span>
                              <div class="selfcheck-body"><strong class="selfcheck-name">{item}</strong></div>
                            </div>
                          )}
                        </For>
                      </div>
                    </Show>

                    <Show when={envFailed().length > 0}>
                      <div class="selfcheck-list" role="list" aria-label={t("set.env.failed")}>
                        <For each={envFailed()}>
                          {(item) => (
                            <div class="selfcheck-item error" role="listitem">
                              <span class="selfcheck-badge error"><Icon name="alert" size={12} /> {t("set.env.failed")}</span>
                              <div class="selfcheck-body"><strong class="selfcheck-name">{item}</strong></div>
                            </div>
                          )}
                        </For>
                      </div>
                      <p class="settings-note">
                        <Icon name="alert" size={13} />
                        <span>{t("set.env.cleanupFailedHint")}</span>
                      </p>
                    </Show>
                  </>
                )}
              </Show>

              <Show when={!envReport() && !envCleanup() && !envBusy()}>
                <p class="settings-note">
                  <Icon name="search" size={13} />
                  <span>{t("set.env.empty")}</span>
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
                <h3>{t("set.backup")}</h3>
                <span>{t("set.backupHint")}</span>
              </div>
            </div>
            <span class="settings-section-chevron"><Icon name="chevron" size={14} /></span>
          </summary>
          <div class="settings-section-body">
            <div class="backup-actions">
              <button class="btn secondary" type="button" onClick={exportBackup} disabled={backupBusy() !== ""}>
                <Icon name="external" size={14} /> {backupBusy() === "export" ? t("set.exporting") : t("set.exportBackup")}
              </button>
              <button class="btn secondary" type="button" onClick={() => backupFileInput?.click()} disabled={backupBusy() !== ""}>
                <Icon name="plus" size={14} /> {backupBusy() === "import" ? t("set.importing") : t("set.importBackup")}
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
                    <strong>{t("set.backupRestored")}</strong>
                  </div>
                  <div class="backup-result-grid">
                    <div><span>{t("set.subscriptions")}</span><strong>{result().restoredSubscriptions}</strong></div>
                    <div><span>{t("set.nodes")}</span><strong>{result().restoredNodes}</strong></div>
                    <div><span>{t("set.rules")}</span><strong>{result().restoredRules}</strong></div>
                  </div>
                  <p class="backup-result-hint">{t("set.importOverwriteHint")}</p>
                </div>
              )}
            </Show>

            <div class="webdav-backup">
              <div class="webdav-heading">
                <strong>{t("set.snapshots.title")}</strong>
                <small>{t("set.snapshots.hint")}</small>
              </div>

              <div class="backup-actions" style="margin-top: 12px;">
                <button class="btn secondary" type="button" onClick={() => void createSnapshot()} disabled={snapshotsBusy() !== ""}>
                  <Icon name="plus" size={14} /> {snapshotsBusy() === "create" ? t("set.snapshots.creating") : t("set.snapshots.create")}
                </button>
                <button class="btn secondary" type="button" onClick={() => void importSnapshot()} disabled={snapshotsBusy() !== ""}>
                  <Icon name="logs" size={14} /> {snapshotsBusy() === "import" ? t("set.snapshots.importing") : t("set.snapshots.import")}
                </button>
                <button class="btn ghost compact" type="button" onClick={() => void loadSnapshots()} disabled={snapshotsBusy() !== ""}>
                  <Icon name="refresh" size={13} /> {snapshotsBusy() === "load" ? t("set.snapshots.loading") : t("set.snapshots.refresh")}
                </button>
              </div>

              <Show when={snapshotsMessage()}>
                <div class="alert success settings-inline-alert">
                  <span class="alert-message"><Icon name="check" size={15} /> {snapshotsMessage()}</span>
                </div>
              </Show>
              <Show when={snapshotsError()}>
                <div class="alert error settings-inline-alert">
                  <span class="alert-message"><Icon name="alert" size={15} /> {snapshotsError()}</span>
                </div>
              </Show>

              <Show
                when={snapshotRows().length > 0}
                fallback={
                  <p class="settings-note">
                    <Icon name="subscription" size={13} />
                    <span>{t("set.snapshots.empty")}</span>
                  </p>
                }
              >
                <ul class="hotkey-list" aria-label={t("set.snapshots.title")}>
                  <For each={snapshotRows()}>
                    {(snapshot) => (
                      <li class="hotkey-row">
                        <span class="hotkey-row-main">
                          <span style="display: flex; flex-direction: column; gap: 2px; min-width: 0;">
                            <span style="display: flex; align-items: center; gap: 6px; min-width: 0;">
                              <strong style="overflow: hidden; color: var(--text); font-size: 11px; text-overflow: ellipsis; white-space: nowrap;">{snapshot.label}</strong>
                              <Show when={snapshot.automatic}>
                                <span class="tag neutral" style="flex-shrink: 0;">{t("set.snapshots.automatic")}</span>
                              </Show>
                            </span>
                            <small style="color: var(--faint); font-size: 10px; line-height: 1.5;">
                              {t("set.snapshots.createdAt")} {formatCheckedAt(snapshot.createdAt)} · {t("set.snapshots.size")} {formatBytes(snapshot.size)}
                            </small>
                          </span>
                        </span>
                        <span class="hotkey-row-controls">
                          <button class="btn secondary compact" type="button" onClick={() => void restoreSnapshot(snapshot)} disabled={snapshotsBusy() !== ""}>
                            <Icon name="refresh" size={13} /> {snapshotsBusy() === `restore:${snapshot.id}` ? t("set.snapshots.restoring") : t("set.snapshots.restore")}
                          </button>
                          <button class="btn ghost compact" type="button" onClick={() => void exportSnapshot(snapshot)} disabled={snapshotsBusy() !== ""}>
                            <Icon name="external" size={13} /> {snapshotsBusy() === `export:${snapshot.id}` ? t("set.snapshots.exporting") : t("set.snapshots.export")}
                          </button>
                          <button class="btn danger compact" type="button" onClick={() => void deleteSnapshot(snapshot)} disabled={snapshotsBusy() !== ""}>
                            <Icon name="trash" size={13} /> {snapshotsBusy() === `delete:${snapshot.id}` ? t("set.snapshots.deleting") : t("set.snapshots.delete")}
                          </button>
                        </span>
                      </li>
                    )}
                  </For>
                </ul>
              </Show>

              <p class="settings-note">
                <Icon name="alert" size={13} />
                <span>{t("set.snapshots.note")}</span>
              </p>
            </div>

            <div class="webdav-backup">
              <div class="webdav-heading">
                <strong>{t("set.webdav")}</strong>
                <small>{t("set.webdavHint")}</small>
              </div>

              <div class="settings-grid webdav-grid">
                <label>
                  <span>{t("set.webdavUrl")}</span>
                  <input
                    value={settings().webdavUrl}
                    onInput={(event) => update("webdavUrl", event.currentTarget.value)}
                    placeholder="https://dav.example.com/remote.php/dav/files/user/"
                    spellcheck={false}
                    autocomplete="off"
                  />
                  <small>{t("set.webdavUrlHint")}</small>
                </label>
                <label>
                  <span>{t("set.webdavUsername")}</span>
                  <input
                    value={settings().webdavUsername}
                    onInput={(event) => update("webdavUsername", event.currentTarget.value)}
                    placeholder={t("set.webdavUsernamePlaceholder")}
                    spellcheck={false}
                    autocomplete="off"
                  />
                  <small>{t("set.webdavUsernameHint")}</small>
                </label>
                <label>
                  <span>{t("set.webdavPassword")}</span>
                  <input
                    type="password"
                    value={settings().webdavPassword}
                    onInput={(event) => update("webdavPassword", event.currentTarget.value)}
                    placeholder={t("set.webdavPasswordPlaceholder")}
                    autocomplete="new-password"
                  />
                  <small>{t("set.webdavPasswordHint")}</small>
                </label>
                <label>
                  <span>{t("set.webdavPath")}</span>
                  <input
                    value={settings().webdavPath}
                    onInput={(event) => update("webdavPath", event.currentTarget.value)}
                    placeholder="/FastV/fastv-backup.json"
                    spellcheck={false}
                    autocomplete="off"
                  />
                  <small>{t("set.webdavPathHint")}</small>
                </label>
              </div>

              <div class="webdav-actions">
                <button class="btn secondary" type="button" onClick={testWebdav} disabled={webdavBusy() !== ""}>
                  <Icon name="refresh" size={14} /> {webdavBusy() === "test" ? t("set.testing") : t("set.testConnection")}
                </button>
                <button class="btn secondary" type="button" onClick={uploadWebdav} disabled={webdavBusy() !== ""}>
                  <Icon name="external" size={14} /> {webdavBusy() === "upload" ? t("set.uploading") : t("set.uploadCloud")}
                </button>
                <button class="btn secondary" type="button" onClick={restoreWebdav} disabled={webdavBusy() !== ""}>
                  <Icon name="subscription" size={14} /> {webdavBusy() === "download" ? t("set.restoring") : t("set.restoreCloud")}
                </button>
              </div>

              <Show when={webdavMessage()}>
                <div class="alert success settings-inline-alert">
                  <span class="alert-message"><Icon name="check" size={15} /> {webdavMessage()}</span>
                </div>
              </Show>
              <Show when={webdavError()}>
                <div class="alert error settings-inline-alert">
                  <span class="alert-message"><Icon name="alert" size={15} /> {webdavError()}</span>
                </div>
              </Show>
              <Show when={webdavRemoteUrl()}>
                <div class="webdav-remote-url">
                  <span>{t("set.webdavRemoteUrl")}</span>
                  <code>{webdavRemoteUrl()}</code>
                </div>
              </Show>
              <Show when={webdavRestoreResult()}>
                {(result) => (
                  <div class="backup-result webdav-restore-result">
                    <div class="backup-result-head">
                      <Icon name="check" size={14} />
                      <strong>{t("set.cloudRestored")}</strong>
                    </div>
                    <div class="backup-result-grid">
                      <div><span>{t("set.subscriptions")}</span><strong>{result().restoredSubscriptions}</strong></div>
                      <div><span>{t("set.nodes")}</span><strong>{result().restoredNodes}</strong></div>
                      <div><span>{t("set.rules")}</span><strong>{result().restoredRules}</strong></div>
                    </div>
                    <p class="backup-result-hint">{t("set.cloudRestoreHint")}</p>
                  </div>
                )}
              </Show>

              <p class="settings-note">
                <Icon name="alert" size={13} />
                <span>{t("set.webdavNote")}</span>
              </p>
            </div>

            <p class="settings-note">
              <Icon name="alert" size={13} />
              <span>{t("set.backupNote")}</span>
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
          <button class="btn ghost" type="button" onClick={reset} disabled={saving()}>{t("set.reset")}</button>
          <button class="btn primary" type="submit" disabled={saving()}>
            <Icon name="check" size={15} /> {saving() ? t("set.saving") : t("set.save")}
          </button>
        </div>

        <section class="panel about-panel">
          <div class="panel-heading">
            <div>
              <h3>{t("set.about")}</h3>
              <span>ABOUT</span>
            </div>
          </div>
          <div class="about-grid">
            <div><span>{t("set.aboutApp")}</span><strong>FastV</strong></div>
            <div><span>{t("set.aboutVersion")}</span><strong>0.1.0</strong></div>
            <div><span>{t("set.aboutUI")}</span><strong>Wails v3 + SolidJS</strong></div>
            <div><span>{t("set.aboutDataDir")}</span><strong>%APPDATA%\FastV</strong></div>
          </div>
        </section>
      </Show>
    </form>
  );
}
