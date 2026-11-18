import { createSignal, For, onCleanup, onMount, Show } from "solid-js";

import { Events } from "@wailsio/runtime";

import { ProxyService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/proxy";
import { Service as RoutingService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/routing";
import { Service as RuleDebugService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/ruledebug";
import { Service as RuleSetService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/ruleset";
import type { ProxyState, RoutingPreset, RoutingRule, RuleSet } from "../../bindings/github.com/MardukPrime/Fast-V/internal/model";
import type { Result as RuleDebugResult } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/ruledebug";
import Icon from "../components/Icon";
import { language, t } from "../i18n";

type RoutingPageProps = {
  proxyState: ProxyState;
  proxyMode: string;
};

// Labels and hints live in the i18n dictionaries: the constants below only hold
// keys so switching the language re-renders every option through t().
const RULE_TYPES = [
  { value: "domain_suffix", labelKey: "route.type.domainSuffix", hintKey: "route.type.domainSuffixHint" },
  { value: "domain_keyword", labelKey: "route.type.domainKeyword", hintKey: "route.type.domainKeywordHint" },
  { value: "domain_regex", labelKey: "route.type.domainRegex", hintKey: "route.type.domainRegexHint" },
  { value: "ip", labelKey: "route.type.ip", hintKey: "route.type.ipHint" },
  { value: "geosite", labelKey: "route.type.geosite", hintKey: "route.type.geositeHint" },
  { value: "geoip", labelKey: "route.type.geoip", hintKey: "route.type.geoipHint" },
  { value: "port", labelKey: "route.type.port", hintKey: "route.type.portHint" },
  { value: "network", labelKey: "route.type.network", hintKey: "route.type.networkHint" },
  { value: "process", labelKey: "route.type.process", hintKey: "route.type.processHint" },
];

const ACTIONS = [
  { value: "proxy", labelKey: "route.actionLabel.proxy", hintKey: "route.actionLabel.proxyHint", tone: "accent" },
  { value: "direct", labelKey: "route.actionLabel.direct", hintKey: "route.actionLabel.directHint", tone: "success" },
  { value: "block", labelKey: "route.actionLabel.block", hintKey: "route.actionLabel.blockHint", tone: "danger" },
];

function ruleTypes() {
  return RULE_TYPES.map((item) => ({ value: item.value, label: t(item.labelKey), hint: t(item.hintKey) }));
}

function actions() {
  return ACTIONS.map((item) => ({ value: item.value, label: t(item.labelKey), hint: t(item.hintKey), tone: item.tone }));
}

function typeLabel(type: string) {
  const item = RULE_TYPES.find((entry) => entry.value === type);
  return item ? t(item.labelKey) : type;
}

function typeHint(type: string): string {
  const item = RULE_TYPES.find((entry) => entry.value === type);
  return item ? t(item.hintKey) : "";
}

function actionLabel(action: string) {
  const item = ACTIONS.find((entry) => entry.value === action);
  return item ? t(item.labelKey) : action;
}

// Rule debugger: destination kinds mirror the RuleDebugService binding. Label
// keys live in the i18n dictionary; the maps below only translate the backend
// values (outbound tag and rule source) so no diagnostic copy is hard coded.
const DEBUG_KINDS = [
  { value: "auto", labelKey: "route.debug.kind.auto" },
  { value: "domain", labelKey: "route.debug.kind.domain" },
  { value: "ip", labelKey: "route.debug.kind.ip" },
  { value: "process", labelKey: "route.debug.kind.process" },
  { value: "port", labelKey: "route.debug.kind.port" },
  { value: "network", labelKey: "route.debug.kind.network" },
];

const DEBUG_OUTBOUND_LABELS: Record<string, string> = {
  proxy: "route.debug.outbound.proxy",
  direct: "route.debug.outbound.direct",
  block: "route.debug.outbound.block",
};

const DEBUG_SOURCE_LABELS: Record<string, string> = {
  user: "route.debug.source.user",
  ruleset: "route.debug.source.ruleset",
  default: "route.debug.source.default",
};

function debugKinds() {
  return DEBUG_KINDS.map((item) => ({ value: item.value, label: t(item.labelKey) }));
}

function debugOutboundLabel(tag: string) {
  const key = DEBUG_OUTBOUND_LABELS[tag];
  return key ? t(key) : tag;
}

function debugSourceLabel(source: string) {
  const key = DEBUG_SOURCE_LABELS[source];
  return key ? t(key) : source;
}

// Rule set subscriptions: remote lists that expand into routing rules once they
// download successfully. Values mirror the backend validation and the interval
// options offered by the service binding (0 = default interval).
const RULE_SET_KINDS = [
  { value: "domain", labelKey: "route.ruleset.kind.domain" },
  { value: "ip", labelKey: "route.ruleset.kind.ip" },
  { value: "combined", labelKey: "route.ruleset.kind.combined" },
];

const RULE_SET_ACTIONS = [
  { value: "proxy", labelKey: "route.ruleset.action.proxy" },
  { value: "direct", labelKey: "route.ruleset.action.direct" },
  { value: "block", labelKey: "route.ruleset.action.block" },
];

const RULE_SET_INTERVAL_OPTIONS = [0, 15, 30, 60, 120, 360, 720, 1440];
// Mirrors the backend default interval applied when a rule set enables automatic
// updates without an interval of its own.
const DEFAULT_RULE_SET_INTERVAL_MINUTES = 360;

// The user rule table fixes seven columns, but the rule set table needs nine (it
// adds the enable switch and the auto-update column). Its header and rows reuse
// .routing-table-head/.routing-row and override only the grid template, so the
// existing border, hover and narrow-width scroll behavior are preserved.
const RULE_SET_TABLE_STYLE = {
  "grid-template-columns": "minmax(120px, 1.4fr) 44px 58px 66px 52px minmax(88px, 1fr) 78px 88px 88px",
};

// The debug candidate table reuses .routing-table/.routing-row but only needs
// four compact columns: name, type, value and source.
const DEBUG_CANDIDATE_GRID_STYLE = {
  "grid-template-columns": "minmax(120px, 1.3fr) 92px minmax(120px, 1.6fr) 88px",
};

function ruleSetKinds() {
  return RULE_SET_KINDS.map((item) => ({ value: item.value, label: t(item.labelKey) }));
}

function ruleSetActions() {
  return RULE_SET_ACTIONS.map((item) => ({ value: item.value, label: t(item.labelKey) }));
}

// The dropdown offers the documented intervals plus the stored value when a hand
// edited or future data file contains one that is not in the list, so editing
// never silently changes the configured interval to the first option.
function ruleSetIntervalOptions(current: number): number[] {
  if (current <= 0 || RULE_SET_INTERVAL_OPTIONS.includes(current)) return RULE_SET_INTERVAL_OPTIONS;
  return [...RULE_SET_INTERVAL_OPTIONS, current].sort((a, b) => a - b);
}

function ruleSetKindLabel(kind: string) {
  const item = RULE_SET_KINDS.find((entry) => entry.value === kind);
  return item ? t(item.labelKey) : kind;
}

function ruleSetActionLabel(action: string) {
  const item = RULE_SET_ACTIONS.find((entry) => entry.value === action);
  return item ? t(item.labelKey) : action;
}

function emptyRuleSet(): RuleSet {
  return {
    id: "",
    name: "",
    url: "",
    kind: "combined",
    action: "direct",
    enabled: true,
    autoUpdate: true,
    intervalMinutes: 0,
    updatedAt: "",
    ruleCount: 0,
    lastError: "",
  };
}

function ruleSetAutoUpdateLabel(set: RuleSet) {
  if (!set.autoUpdate) return t("route.ruleset.auto.off");
  if (set.intervalMinutes > 0) return t("route.ruleset.auto.every", { minutes: set.intervalMinutes });
  return t("route.ruleset.auto.default");
}

function ruleSetAutoUpdateHint(set: RuleSet) {
  if (!set.autoUpdate) return t("route.ruleset.auto.off");
  if (set.intervalMinutes > 0) return t("route.ruleset.auto.every", { minutes: set.intervalMinutes });
  return t("route.ruleset.auto.defaultHint", { minutes: DEFAULT_RULE_SET_INTERVAL_MINUTES });
}

// Compact "11/18 19:20" stamp for the last successful download; the raw RFC3339
// value stays available as the cell tooltip.
function formatRuleSetUpdated(value: string, locale: string): string {
  if (!value) return t("route.ruleset.never");
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString(locale, { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

function formatRuleSetCount(count: number, locale: string): string {
  return Math.max(0, Math.trunc(count)).toLocaleString(locale);
}

// A rule set is "ok" only after a successful download, "error" when the last
// download failed (the tooltip carries the backend message), "pending" when it
// was never downloaded and "updating" while a manual refresh is in flight.
function ruleSetStatusLabel(set: RuleSet, updating: boolean): string {
  if (updating) return t("route.ruleset.status.updating");
  if (set.lastError) return t("route.ruleset.status.error");
  if (set.updatedAt) return t("route.ruleset.status.ok");
  return t("route.ruleset.status.pending");
}

function ruleSetStatusClass(set: RuleSet, updating: boolean) {
  return {
    "status-label": true,
    ok: !updating && !set.lastError && Boolean(set.updatedAt),
    error: !updating && Boolean(set.lastError),
  };
}

function emptyRule(): RoutingRule {
  return { id: "", name: "", type: "domain_suffix", value: "", action: "direct", enabled: true };
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

function twoDigits(value: number): string {
  return String(value).padStart(2, "0");
}

function exportFileStamp(date: Date): string {
  return `${date.getFullYear()}${twoDigits(date.getMonth() + 1)}${twoDigits(date.getDate())}-${twoDigits(date.getHours())}${twoDigits(date.getMinutes())}`;
}

const knownTypes = new Set(RULE_TYPES.map((item) => item.value));
const knownActions = new Set(ACTIONS.map((item) => item.value));

// validateImportedRules parses an exported JSON rule array and rejects payloads
// that the backend would refuse, so the UI can show a precise localized error
// before replacing anything. The backend still validates the imported rules.
function validateImportedRules(text: string): RoutingRule[] {
  let raw: unknown;
  try {
    raw = JSON.parse(text);
  } catch {
    throw new Error(t("route.importErr.invalidJson"));
  }
  if (!Array.isArray(raw)) {
    throw new Error(t("route.importErr.notArray"));
  }
  if (raw.length === 0) {
    throw new Error(t("route.importErr.empty"));
  }
  return raw.map((item, index) => {
    const position = index + 1;
    if (typeof item !== "object" || item === null || Array.isArray(item)) {
      throw new Error(t("route.importErr.notObject", { position }));
    }
    const record = item as Record<string, unknown>;
    const type = typeof record.type === "string" ? record.type.trim().toLowerCase() : "";
    const value = typeof record.value === "string" ? record.value.trim() : "";
    if (!type) throw new Error(t("route.importErr.missingType", { position }));
    if (!value) throw new Error(t("route.importErr.missingValue", { position }));
    if (!knownTypes.has(type)) throw new Error(t("route.importErr.badType", { position, type }));
    const action = typeof record.action === "string" ? record.action.trim().toLowerCase() : "";
    if (action !== "" && !knownActions.has(action)) {
      throw new Error(t("route.importErr.badAction", { position, action }));
    }
    return {
      id: typeof record.id === "string" ? record.id : "",
      name: typeof record.name === "string" ? record.name.trim() : "",
      type,
      value,
      action,
      enabled: record.enabled !== false,
    };
  });
}

export default function RoutingPage(props: RoutingPageProps) {
  const [rules, setRules] = createSignal<RoutingRule[]>([]);
  const [loading, setLoading] = createSignal(true);
  const [error, setError] = createSignal("");
  const [message, setMessage] = createSignal("");
  const [editing, setEditing] = createSignal<RoutingRule | null>(null);
  const [busy, setBusy] = createSignal(false);
  const [presetOpen, setPresetOpen] = createSignal(false);
  const [presets, setPresets] = createSignal<RoutingPreset[]>([]);
  const [presetLoading, setPresetLoading] = createSignal(false);
  const [presetError, setPresetError] = createSignal("");
  const [applyingPreset, setApplyingPreset] = createSignal("");
  const [ioBusy, setIoBusy] = createSignal<"export" | "import" | "">("");
  const [ruleSets, setRuleSets] = createSignal<RuleSet[]>([]);
  const [ruleSetLoading, setRuleSetLoading] = createSignal(true);
  const [ruleSetError, setRuleSetError] = createSignal("");
  const [ruleSetMessage, setRuleSetMessage] = createSignal("");
  const [editingRuleSet, setEditingRuleSet] = createSignal<RuleSet | null>(null);
  const [ruleSetFormError, setRuleSetFormError] = createSignal("");
  const [ruleSetBusy, setRuleSetBusy] = createSignal(false);
  const [updatingRuleSetId, setUpdatingRuleSetId] = createSignal("");
  const [updatingAllRuleSets, setUpdatingAllRuleSets] = createSignal(false);
  const [debugInput, setDebugInput] = createSignal("");
  const [debugKind, setDebugKind] = createSignal("auto");
  const [debugBusy, setDebugBusy] = createSignal(false);
  const [debugError, setDebugError] = createSignal("");
  const [debugResult, setDebugResult] = createSignal<RuleDebugResult | null>(null);
  const [debugCandidatesOpen, setDebugCandidatesOpen] = createSignal(false);
  let importInput: HTMLInputElement | undefined;

  const load = async () => {
    setLoading(true);
    try {
      setRules((await RoutingService.List()) ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  const loadRuleSets = async () => {
    setRuleSetLoading(true);
    setRuleSetError("");
    try {
      setRuleSets((await RuleSetService.List()) ?? []);
    } catch (err) {
      setRuleSetError(t("route.ruleset.err.load", { error: errorMessage(err) }));
    } finally {
      setRuleSetLoading(false);
    }
  };

  onMount(() => {
    void load();
    void loadRuleSets();
    // Automatic downloads refresh rule sets in the background; reload so entry
    // counts, timestamps and error states never go stale.
    const off = Events.On("rulesets:changed", () => { void loadRuleSets(); });
    onCleanup(off);
  });

  // Rules and rule sets only reach the generated config through a proxy start,
  // so a successful mutation restarts a running proxy to apply it immediately.
  // `report` lets the rule set panel keep its own status line instead of writing
  // into the user-rule panel above it.
  const restartIfRunning = async (
    appliedMessage = t("route.msg.savedApplied"),
    pendingMessage = t("route.msg.savedPending"),
    report: (text: string) => void = setMessage,
  ) => {
    if (props.proxyState.status === "running" || props.proxyState.status === "starting") {
      report(t("route.msg.restarting"));
      await ProxyService.Restart();
      report(appliedMessage);
    } else {
      report(pendingMessage);
    }
  };

  const openNew = () => {
    setError("");
    setMessage("");
    setEditing(emptyRule());
  };

  const openEdit = (rule: RoutingRule) => {
    setError("");
    setMessage("");
    setEditing({ ...rule });
  };

  const updateEditing = <K extends keyof RoutingRule>(key: K, value: RoutingRule[K]) => {
    const current = editing();
    if (!current) return;
    setEditing({ ...current, [key]: value });
  };

  const saveRule = async (event: Event) => {
    event.preventDefault();
    const current = editing();
    if (!current) return;
    setBusy(true);
    setError("");
    setMessage("");
    try {
      await RoutingService.Save(current);
      setEditing(null);
      await load();
      await restartIfRunning();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const toggleRule = async (rule: RoutingRule) => {
    setBusy(true);
    setError("");
    try {
      await RoutingService.Save({ ...rule, enabled: !rule.enabled });
      await load();
      await restartIfRunning();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const deleteRule = async (rule: RoutingRule) => {
    if (!window.confirm(t("route.confirm.delete", { name: rule.name }))) return;
    setBusy(true);
    setError("");
    try {
      await RoutingService.Delete(rule.id);
      await load();
      await restartIfRunning();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const moveRule = async (index: number, direction: -1 | 1) => {
    const current = rules();
    const target = index + direction;
    if (target < 0 || target >= current.length) return;
    const next = [...current];
    [next[index], next[target]] = [next[target], next[index]];
    setBusy(true);
    setError("");
    try {
      await RoutingService.Reorder(next.map((rule) => rule.id));
      setRules(next);
      await restartIfRunning();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const resetRules = async () => {
    if (!window.confirm(t("route.confirm.reset"))) return;
    setBusy(true);
    setError("");
    try {
      setRules((await RoutingService.Reset()) ?? []);
      await restartIfRunning();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const openPresets = async () => {
    setPresetOpen(true);
    setPresetError("");
    setPresetLoading(true);
    try {
      setPresets((await RoutingService.Presets()) ?? []);
    } catch (err) {
      setPresetError(t("route.err.loadPresets", { error: errorMessage(err) }));
    } finally {
      setPresetLoading(false);
    }
  };

  const closePresets = () => {
    if (busy()) return;
    setPresetOpen(false);
  };

  const applyPreset = async (preset: RoutingPreset) => {
    if (busy() || applyingPreset()) return;
    if (!window.confirm(t("route.confirm.applyPreset", { name: preset.name }))) return;
    setBusy(true);
    setApplyingPreset(preset.id);
    setError("");
    setMessage("");
    setPresetError("");
    try {
      setRules((await RoutingService.ApplyPreset(preset.id)) ?? []);
      setPresetOpen(false);
      await load();
      await restartIfRunning(
        t("route.msg.presetAppliedRestarted", { name: preset.name }),
        t("route.msg.presetAppliedPending", { name: preset.name }),
      );
    } catch (err) {
      const reason = t("route.err.applyPreset", { error: errorMessage(err) });
      setPresetError(reason);
      if (!presetOpen()) setError(reason);
    } finally {
      setBusy(false);
      setApplyingPreset("");
    }
  };

  const exportRules = async () => {
    if (busy() || ioBusy()) return;
    setIoBusy("export");
    setError("");
    setMessage("");
    try {
      const list = (await RoutingService.List()) ?? [];
      if (list.length === 0) {
        setError(t("route.err.nothingToExport"));
        return;
      }
      const payload = list.map((rule) => ({
        id: rule.id,
        name: rule.name,
        type: rule.type,
        value: rule.value,
        action: rule.action,
        enabled: rule.enabled,
      }));
      const filename = `fastv-routing-${exportFileStamp(new Date())}.json`;
      const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = filename;
      document.body.appendChild(link);
      link.click();
      link.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 1000);
      setMessage(t("route.msg.exported", { count: payload.length, filename }));
    } catch (err) {
      setError(t("route.err.export", { error: errorMessage(err) }));
    } finally {
      setIoBusy("");
    }
  };

  const triggerImport = () => {
    if (busy() || ioBusy()) return;
    setError("");
    setMessage("");
    importInput?.click();
  };

  const importRuleFile = async (file: File | null) => {
    if (!file) return;
    setIoBusy("import");
    setError("");
    setMessage("");
    try {
      const imported = validateImportedRules(await file.text());
      if (!window.confirm(t("route.confirm.import", { count: imported.length }))) return;
      const applied = (await RoutingService.Import(imported)) ?? [];
      setRules(applied);
      await restartIfRunning(
        t("route.msg.importedRestarted", { count: applied.length }),
        t("route.msg.importedPending", { count: applied.length }),
      );
    } catch (err) {
      setError(t("route.err.import", { error: errorMessage(err) }));
    } finally {
      setIoBusy("");
    }
  };

  const enabledRuleSets = () => ruleSets().filter((set) => set.enabled);

  const openNewRuleSet = () => {
    setRuleSetError("");
    setRuleSetMessage("");
    setRuleSetFormError("");
    setEditingRuleSet(emptyRuleSet());
  };

  const openEditRuleSet = (set: RuleSet) => {
    setRuleSetError("");
    setRuleSetMessage("");
    setRuleSetFormError("");
    setEditingRuleSet({ ...set });
  };

  const updateEditingRuleSet = <K extends keyof RuleSet>(key: K, value: RuleSet[K]) => {
    const current = editingRuleSet();
    if (!current) return;
    setEditingRuleSet({ ...current, [key]: value });
  };

  const saveRuleSet = async (event: Event) => {
    event.preventDefault();
    const current = editingRuleSet();
    if (!current) return;
    const name = current.name.trim();
    const url = current.url.trim();
    if (!name) {
      setRuleSetFormError(t("route.ruleset.editor.nameRequired"));
      return;
    }
    if (!url) {
      setRuleSetFormError(t("route.ruleset.editor.urlRequired"));
      return;
    }
    if (!/^https?:\/\//i.test(url)) {
      setRuleSetFormError(t("route.ruleset.editor.urlScheme"));
      return;
    }
    setRuleSetFormError("");
    setRuleSetBusy(true);
    setRuleSetError("");
    setRuleSetMessage("");
    try {
      await RuleSetService.Save({ ...current, name, url });
      setEditingRuleSet(null);
      await loadRuleSets();
      await restartIfRunning(
        t("route.ruleset.msg.savedRestarted"),
        t("route.ruleset.msg.savedPending"),
        setRuleSetMessage,
      );
    } catch (err) {
      setRuleSetFormError(t("route.ruleset.editor.saveFailed", { error: errorMessage(err) }));
    } finally {
      setRuleSetBusy(false);
    }
  };

  const toggleRuleSet = async (set: RuleSet) => {
    if (ruleSetBusy()) return;
    setRuleSetBusy(true);
    setRuleSetError("");
    setRuleSetMessage("");
    try {
      await RuleSetService.Save({ ...set, enabled: !set.enabled });
      await loadRuleSets();
      await restartIfRunning(
        t("route.ruleset.msg.savedRestarted"),
        t("route.ruleset.msg.savedPending"),
        setRuleSetMessage,
      );
    } catch (err) {
      setRuleSetError(errorMessage(err));
    } finally {
      setRuleSetBusy(false);
    }
  };

  const updateRuleSet = async (set: RuleSet) => {
    if (ruleSetBusy()) return;
    setRuleSetBusy(true);
    setUpdatingRuleSetId(set.id);
    setRuleSetError("");
    setRuleSetMessage("");
    try {
      const updated = await RuleSetService.Update(set.id);
      await loadRuleSets();
      await restartIfRunning(
        t("route.ruleset.msg.updatedRestarted", { name: set.name, count: updated.ruleCount }),
        t("route.ruleset.msg.updatedPending", { name: set.name, count: updated.ruleCount }),
        setRuleSetMessage,
      );
    } catch (err) {
      // The backend records the failure on the rule set, so refresh to show the
      // error status and keep the last successful entry count visible.
      await loadRuleSets();
      setRuleSetError(t("route.ruleset.err.update", { name: set.name, error: errorMessage(err) }));
    } finally {
      setUpdatingRuleSetId("");
      setRuleSetBusy(false);
    }
  };

  const updateAllRuleSets = async () => {
    if (ruleSetBusy() || enabledRuleSets().length === 0) return;
    setRuleSetBusy(true);
    setUpdatingAllRuleSets(true);
    setRuleSetError("");
    setRuleSetMessage("");
    try {
      const updated = await RuleSetService.UpdateAll();
      await loadRuleSets();
      await restartIfRunning(
        t("route.ruleset.msg.updateAllRestarted", { updated }),
        t("route.ruleset.msg.updateAllPending", { updated }),
        setRuleSetMessage,
      );
    } catch (err) {
      await loadRuleSets();
      setRuleSetError(t("route.ruleset.err.updateAll", { error: errorMessage(err) }));
    } finally {
      setUpdatingAllRuleSets(false);
      setRuleSetBusy(false);
    }
  };

  const deleteRuleSet = async (set: RuleSet) => {
    if (!window.confirm(t("route.ruleset.confirm.delete", { name: set.name }))) return;
    setRuleSetBusy(true);
    setRuleSetError("");
    setRuleSetMessage("");
    try {
      await RuleSetService.Delete(set.id);
      await loadRuleSets();
      await restartIfRunning(
        t("route.ruleset.msg.deletedRestarted", { name: set.name }),
        t("route.ruleset.msg.deletedPending", { name: set.name }),
        setRuleSetMessage,
      );
    } catch (err) {
      setRuleSetError(t("route.ruleset.err.delete", { error: errorMessage(err) }));
    } finally {
      setRuleSetBusy(false);
    }
  };

  const modeLabel = () => {
    switch (props.proxyMode) {
      case "global": return t("route.mode.global");
      case "direct": return t("route.mode.direct");
      default: return t("route.mode.rule");
    }
  };

  // Rule debugger: asks the backend which rule decides one destination. The
  // backend normalizes the input and returns a Chinese validation error for
  // invalid values, which is shown as-is inside the localized error prefix.
  const runRuleDebug = async (event: Event) => {
    event.preventDefault();
    if (debugBusy()) return;
    setDebugBusy(true);
    setDebugError("");
    try {
      const result = await RuleDebugService.Debug(debugInput().trim(), debugKind());
      setDebugResult(result);
      setDebugCandidatesOpen(false);
    } catch (err) {
      setDebugResult(null);
      setDebugError(t("route.debug.err.failed", { error: errorMessage(err) }));
    } finally {
      setDebugBusy(false);
    }
  };

  return (
    <div class="page-stack">
      <section class="mini-stats">
        <div class="mini-stat"><span>{t("route.stats.customRules")}</span><strong>{rules().length}</strong><small>{t("route.stats.customRulesHint")}</small></div>
        <div class="mini-stat"><span>{t("route.stats.enabled")}</span><strong>{rules().filter((rule) => rule.enabled).length}</strong><small>{t("route.stats.enabledHint")}</small></div>
        <div class="mini-stat"><span>{t("route.stats.mode")}</span><strong class="routing-mode-value">{modeLabel()}</strong><small>{t("route.stats.modeHint")}</small></div>
      </section>

      {/* Rule debugger: explain which rule decides one destination without
          starting the core. It sits above the rule tables because it is the
          query tool users reach for when traffic takes the wrong outbound. */}
      <section class="panel routing-panel">
        <div class="panel-heading node-heading">
          <div class="heading-with-icon">
            <span class="heading-icon cyan"><Icon name="search" size={17} /></span>
            <div>
              <h3>{t("route.debug.panel.title")}</h3>
              <span>{t("route.debug.panel.subtitle")}</span>
            </div>
          </div>
        </div>

        <div class="routing-note">
          <Icon name="search" size={13} />
          <span>{t("route.debug.note.hint")}</span>
        </div>

        <form class="routing-io-bar" onSubmit={runRuleDebug}>
          <input
            value={debugInput()}
            onInput={(event) => setDebugInput(event.currentTarget.value)}
            placeholder={t("route.debug.input.placeholder")}
            aria-label={t("route.debug.input.label")}
            autocomplete="off"
            spellcheck={false}
            style={{ flex: "1 1 220px", "min-width": "0" }}
          />
          <div class="routing-io-group">
            <select
              value={debugKind()}
              onChange={(event) => setDebugKind(event.currentTarget.value)}
              aria-label={t("route.debug.kind.label")}
              style={{ width: "auto", "min-width": "112px" }}
            >
              <For each={debugKinds()}>{(item) => <option value={item.value}>{item.label}</option>}</For>
            </select>
            <button class="btn primary" type="submit" disabled={debugBusy()}>
              <Icon name="search" size={13} /> {debugBusy() ? t("route.debug.submitting") : t("route.debug.submit")}
            </button>
          </div>
        </form>

        <Show when={debugError()}>
          <div class="alert error" style={{ "margin-top": "10px" }}><span class="alert-message"><Icon name="alert" size={15} /> {debugError()}</span></div>
        </Show>

        <Show when={debugResult()}>
          {(result) => (
            <article class="routing-preset-card" style={{ "margin-top": "12px" }}>
              <div class="routing-preset-info">
                <div class="routing-preset-title">
                  <span classList={{ "routing-action": true, direct: result().outboundTag === "direct", block: result().outboundTag === "block" }}>
                    {debugOutboundLabel(result().outboundTag)}
                  </span>
                  <Show when={result().matched} fallback={<strong>{t("route.debug.result.unmatchedTitle")}</strong>}>
                    <span class="tag success">{t("route.debug.result.matched")}</span>
                    <strong>{result().ruleName}</strong>
                    <span class="tag neutral">{typeLabel(result().ruleType)}</span>
                  </Show>
                  <span class="tag neutral">{debugSourceLabel(result().source)}</span>
                </div>
                <Show when={result().matched} fallback={<p>{t("route.debug.result.unmatchedBody")}</p>}>
                  <Show when={result().ruleValue}>
                    <div class="routing-preset-preview">
                      <span class="routing-preset-chip" title={result().ruleValue}>{t("route.debug.field.value")}: {result().ruleValue}</span>
                    </div>
                  </Show>
                </Show>
                <p>{result().reason}</p>
                <For each={result().notes ?? []}>
                  {(note) => (
                    <div class="routing-editor-hint" style={{ "margin-top": "8px" }}>
                      <Icon name="alert" size={13} />
                      <span>{note}</span>
                    </div>
                  )}
                </For>
                <Show when={(result().candidates ?? []).length > 0}>
                  <button
                    class="btn ghost compact"
                    type="button"
                    style={{ "margin-top": "10px" }}
                    aria-expanded={debugCandidatesOpen() ? "true" : "false"}
                    onClick={() => setDebugCandidatesOpen(!debugCandidatesOpen())}
                  >
                    <span style={{ display: "inline-flex", transform: debugCandidatesOpen() ? "rotate(90deg)" : "none" }}>
                      <Icon name="chevron" size={13} />
                    </span>
                    {t("route.debug.candidates.toggle", { count: (result().candidates ?? []).length })}
                  </button>
                  <Show when={debugCandidatesOpen()}>
                    <div class="routing-table" style={{ "margin-top": "8px" }}>
                      <div class="routing-table-head" style={DEBUG_CANDIDATE_GRID_STYLE}>
                        <span>{t("route.debug.candidates.name")}</span>
                        <span>{t("route.debug.candidates.type")}</span>
                        <span>{t("route.debug.candidates.value")}</span>
                        <span>{t("route.debug.candidates.source")}</span>
                      </div>
                      <For each={result().candidates ?? []}>
                        {(candidate) => (
                          <div class="routing-row" style={DEBUG_CANDIDATE_GRID_STYLE}>
                            <div class="routing-name" title={candidate.reason}><strong>{candidate.ruleName}</strong><small>{t("route.debug.candidates.skipped")}</small></div>
                            <span class="tag neutral">{typeLabel(candidate.ruleType)}</span>
                            <span class="routing-value" title={candidate.ruleValue}>{candidate.ruleValue}</span>
                            <span class="tag neutral">{debugSourceLabel(candidate.source)}</span>
                          </div>
                        )}
                      </For>
                    </div>
                  </Show>
                </Show>
              </div>
            </article>
          )}
        </Show>
      </section>

      <section class="panel routing-panel">
        <div class="panel-heading node-heading">
          <div class="heading-with-icon">
            <span class="heading-icon cyan"><Icon name="route" size={17} /></span>
            <div>
              <h3>{t("route.panel.title")}</h3>
              <span>{t("route.panel.subtitle")}</span>
            </div>
          </div>
          <div class="routing-heading-actions">
            <button class="btn ghost" type="button" onClick={openPresets} disabled={busy()}>{t("route.buttons.presets")}</button>
            <button class="btn ghost" type="button" onClick={resetRules} disabled={busy()}>{t("route.buttons.restoreDefaults")}</button>
            <button class="btn primary" type="button" onClick={openNew} disabled={busy()}><Icon name="plus" size={14} /> {t("route.buttons.add")}</button>
          </div>
        </div>

        <div class="routing-note">
          <Icon name="route" size={13} />
          <span>{t("route.note.currentMode", { mode: modeLabel() })}</span>
        </div>

        <div class="routing-io-bar">
          <span class="routing-io-hint"><Icon name="alert" size={12} /> {t("route.io.hint")}</span>
          <div class="routing-io-group">
            <button class="btn ghost compact" type="button" onClick={exportRules} disabled={busy() || ioBusy() !== "" || rules().length === 0}>
              <Icon name="external" size={13} /> {ioBusy() === "export" ? t("route.io.exporting") : t("route.io.export")}
            </button>
            <button class="btn ghost compact" type="button" onClick={triggerImport} disabled={busy() || ioBusy() !== ""}>
              <Icon name="plus" size={13} /> {ioBusy() === "import" ? t("route.io.importing") : t("route.io.import")}
            </button>
            <input
              ref={(element) => { importInput = element; }}
              class="routing-file-input"
              type="file"
              accept="application/json,.json"
              onChange={(event) => {
                const file = event.currentTarget.files?.[0] ?? null;
                event.currentTarget.value = "";
                void importRuleFile(file);
              }}
            />
          </div>
        </div>

        <Show when={error()}>
          <div class="alert error"><span class="alert-message"><Icon name="alert" size={15} /> {error()}</span></div>
        </Show>
        <Show when={message()}>
          <div class="alert success"><span class="alert-message"><Icon name="check" size={15} /> {message()}</span></div>
        </Show>

        <Show when={!loading()} fallback={<div class="empty-state">{t("route.loading")}</div>}>
          <Show when={rules().length > 0} fallback={
            <div class="empty-state">
              <div class="empty-icon"><Icon name="route" size={22} /></div>
              <strong>{t("route.empty.title")}</strong>
              <span>{t("route.empty.body")}</span>
              <div class="routing-empty-actions">
                <button class="btn secondary" type="button" onClick={resetRules} disabled={busy()}>{t("route.empty.useDefaults")}</button>
                <button class="btn primary" type="button" onClick={openNew} disabled={busy()}><Icon name="plus" size={14} /> {t("route.buttons.add")}</button>
              </div>
            </div>
          }>
            <div class="routing-table">
              <div class="routing-table-head">
                <span>{t("route.table.order")}</span>
                <span>{t("route.table.rule")}</span>
                <span>{t("route.table.type")}</span>
                <span>{t("route.table.value")}</span>
                <span>{t("route.table.action")}</span>
                <span>{t("route.table.status")}</span>
                <span>{t("route.table.actions")}</span>
              </div>
              <For each={rules()}>
                {(rule, index) => (
                  <div classList={{ "routing-row": true, disabled: !rule.enabled }}>
                    <span class="routing-order">{index() + 1}</span>
                    <div class="routing-name"><strong>{rule.name}</strong><small>{rule.id === "" ? t("route.row.unsaved") : t("route.row.userRule")}</small></div>
                    <span class="tag neutral">{typeLabel(rule.type)}</span>
                    <span class="routing-value" title={rule.value}>{rule.value}</span>
                    <span classList={{ "routing-action": true, direct: rule.action === "direct", block: rule.action === "block" }}>{actionLabel(rule.action)}</span>
                    <button classList={{ "state-pill": true, on: rule.enabled }} type="button" onClick={() => toggleRule(rule)} disabled={busy()}>
                      <span class="status-dot" /> {rule.enabled ? t("route.row.enabled") : t("route.row.disabled")}
                    </button>
                    <div class="routing-actions">
                      <button class="icon-button" type="button" onClick={() => moveRule(index(), -1)} disabled={busy() || index() === 0} aria-label={t("route.row.moveUp")}><Icon name="chevron" size={13} class="rotate-up" /></button>
                      <button class="icon-button" type="button" onClick={() => moveRule(index(), 1)} disabled={busy() || index() === rules().length - 1} aria-label={t("route.row.moveDown")}><Icon name="chevron" size={13} class="rotate-down" /></button>
                      <button class="icon-button" type="button" onClick={() => openEdit(rule)} disabled={busy()} aria-label={t("route.row.edit", { name: rule.name })}><Icon name="edit" size={14} /></button>
                      <button class="icon-button danger" type="button" onClick={() => deleteRule(rule)} disabled={busy()} aria-label={t("route.row.delete", { name: rule.name })}><Icon name="trash" size={14} /></button>
                    </div>
                  </div>
                )}
              </For>
            </div>
          </Show>
        </Show>
      </section>

      <section class="panel routing-panel">
        <div class="panel-heading node-heading">
          <div class="heading-with-icon">
            <span class="heading-icon cyan"><Icon name="subscription" size={17} /></span>
            <div>
              <h3>{t("route.ruleset.panel.title")}</h3>
              <span>{t("route.ruleset.panel.subtitle")}</span>
            </div>
          </div>
          <div class="routing-heading-actions">
            <button
              class="btn ghost"
              type="button"
              onClick={() => void updateAllRuleSets()}
              disabled={ruleSetBusy() || enabledRuleSets().length === 0}
              title={enabledRuleSets().length === 0 ? t("route.ruleset.noEnabled") : undefined}
            >
              <Icon name="refresh" size={13} /> {updatingAllRuleSets() ? t("route.ruleset.buttons.updatingAll") : t("route.ruleset.buttons.updateAll")}
            </button>
            <button class="btn primary" type="button" onClick={openNewRuleSet} disabled={ruleSetBusy()}><Icon name="plus" size={14} /> {t("route.ruleset.buttons.add")}</button>
          </div>
        </div>

        <div class="routing-note">
          <Icon name="alert" size={13} />
          <span>{t("route.ruleset.note.hint")}</span>
        </div>

        <Show when={ruleSetError() || ruleSetMessage()}>
          <div style={{ display: "grid", gap: "8px", "margin-top": "10px" }}>
            <Show when={ruleSetError()}>
              <div class="alert error"><span class="alert-message"><Icon name="alert" size={15} /> {ruleSetError()}</span></div>
            </Show>
            <Show when={ruleSetMessage()}>
              <div class="alert success"><span class="alert-message"><Icon name="check" size={15} /> {ruleSetMessage()}</span></div>
            </Show>
          </div>
        </Show>

        <Show when={!ruleSetLoading()} fallback={<div class="empty-state">{t("route.ruleset.loading")}</div>}>
          <Show when={ruleSets().length > 0} fallback={
            <div class="empty-state">
              <div class="empty-icon"><Icon name="subscription" size={22} /></div>
              <strong>{t("route.ruleset.empty.title")}</strong>
              <span>{t("route.ruleset.empty.body")}</span>
              <div class="routing-empty-actions">
                <button class="btn primary" type="button" onClick={openNewRuleSet} disabled={ruleSetBusy()}><Icon name="plus" size={14} /> {t("route.ruleset.buttons.add")}</button>
              </div>
            </div>
          }>
            <div class="routing-table">
              <div class="routing-table-head" style={RULE_SET_TABLE_STYLE}>
                <span>{t("route.ruleset.table.name")}</span>
                <span>{t("route.ruleset.table.enabled")}</span>
                <span>{t("route.ruleset.table.kind")}</span>
                <span>{t("route.ruleset.table.action")}</span>
                <span>{t("route.ruleset.table.count")}</span>
                <span>{t("route.ruleset.table.updated")}</span>
                <span>{t("route.ruleset.table.status")}</span>
                <span>{t("route.ruleset.table.autoUpdate")}</span>
                <span>{t("route.ruleset.table.actions")}</span>
              </div>
              <For each={ruleSets()}>
                {(set) => (
                  <div classList={{ "routing-row": true, disabled: !set.enabled }} style={RULE_SET_TABLE_STYLE}>
                    <div class="routing-name" title={set.url}><strong>{set.name}</strong><small>{set.url}</small></div>
                    <label classList={{ switch: true, on: set.enabled }}>
                      <input
                        type="checkbox"
                        checked={set.enabled}
                        disabled={ruleSetBusy()}
                        aria-label={t("route.ruleset.row.toggle", { name: set.name })}
                        onChange={() => void toggleRuleSet(set)}
                      />
                      <span class="switch-track"><span class="switch-thumb" /></span>
                    </label>
                    <span class="tag neutral">{ruleSetKindLabel(set.kind)}</span>
                    <span classList={{ "routing-action": true, direct: set.action === "direct", block: set.action === "block" }}>{ruleSetActionLabel(set.action)}</span>
                    <span class="routing-value" title={t("route.ruleset.countTitle", { count: set.ruleCount })}>{formatRuleSetCount(set.ruleCount, language())}</span>
                    <span class="routing-value" title={set.updatedAt || undefined}>{formatRuleSetUpdated(set.updatedAt, language())}</span>
                    <span classList={ruleSetStatusClass(set, updatingRuleSetId() === set.id)} title={set.lastError || undefined}>
                      <span class="status-dot" /> {ruleSetStatusLabel(set, updatingRuleSetId() === set.id)}
                    </span>
                    <span class="routing-value" title={ruleSetAutoUpdateHint(set)}>{ruleSetAutoUpdateLabel(set)}</span>
                    <div class="routing-actions">
                      <button class="icon-button" type="button" onClick={() => void updateRuleSet(set)} disabled={ruleSetBusy()} aria-label={t("route.ruleset.row.update", { name: set.name })} title={t("route.ruleset.row.update", { name: set.name })}><Icon name="refresh" size={14} /></button>
                      <button class="icon-button" type="button" onClick={() => openEditRuleSet(set)} disabled={ruleSetBusy()} aria-label={t("route.ruleset.row.edit", { name: set.name })} title={t("route.ruleset.row.edit", { name: set.name })}><Icon name="edit" size={14} /></button>
                      <button class="icon-button danger" type="button" onClick={() => void deleteRuleSet(set)} disabled={ruleSetBusy()} aria-label={t("route.ruleset.row.delete", { name: set.name })} title={t("route.ruleset.row.delete", { name: set.name })}><Icon name="trash" size={14} /></button>
                    </div>
                  </div>
                )}
              </For>
            </div>
          </Show>
        </Show>
      </section>

      <Show when={presetOpen()}>
        <div class="modal-backdrop" onClick={(event) => { if (event.target === event.currentTarget && !busy()) setPresetOpen(false); }}>
          <div class="modal routing-preset-modal">
            <div class="modal-heading">
              <div><h3>{t("route.presets.title")}</h3><span>{t("route.presets.subtitle")}</span></div>
              <button class="icon-button" type="button" onClick={closePresets} disabled={busy()} aria-label={t("route.common.close")}><Icon name="close" size={18} /></button>
            </div>
            <div class="routing-preset-body">
              <Show when={presetError()}>
                <div class="alert error"><span class="alert-message"><Icon name="alert" size={15} /> {presetError()}</span></div>
              </Show>
              <Show when={!presetLoading()} fallback={<div class="empty-state">{t("route.presets.loading")}</div>}>
                <Show when={presets().length > 0} fallback={
                  <div class="empty-state">
                    <div class="empty-icon"><Icon name="route" size={22} /></div>
                    <strong>{t("route.presets.emptyTitle")}</strong>
                    <span>{t("route.presets.emptyBody")}</span>
                  </div>
                }>
                  <For each={presets()}>
                    {(preset) => (
                      <article class="routing-preset-card">
                        <div class="routing-preset-info">
                          <div class="routing-preset-title">
                            <strong>{preset.name}</strong>
                            <span class="tag neutral">{t("route.presets.ruleCount", { count: preset.rules?.length ?? 0 })}</span>
                          </div>
                          <p>{preset.description}</p>
                          <Show when={(preset.rules?.length ?? 0) > 0}>
                            <div class="routing-preset-preview">
                              <For each={(preset.rules ?? []).slice(0, 3)}>
                                {(rule) => <span class="routing-preset-chip" title={rule.value}>{typeLabel(rule.type)} · {rule.value}</span>}
                              </For>
                              <Show when={(preset.rules?.length ?? 0) > 3}>
                                <span class="routing-preset-more">{t("route.presets.more", { count: preset.rules?.length ?? 0 })}</span>
                              </Show>
                            </div>
                          </Show>
                        </div>
                        <button class="btn primary compact" type="button" onClick={() => applyPreset(preset)} disabled={busy()}>
                          {applyingPreset() === preset.id ? t("route.presets.applying") : t("route.presets.apply")}
                        </button>
                      </article>
                    )}
                  </For>
                </Show>
              </Show>
            </div>
            <div class="modal-footer">
              <span class="routing-preset-footnote">{t("route.presets.footnote")}</span>
              <button class="btn ghost" type="button" onClick={closePresets} disabled={busy()}>{t("route.common.close")}</button>
            </div>
          </div>
        </div>
      </Show>

      <Show when={editing()}>
        {(rule) => (
          <div class="modal-backdrop" onClick={(event) => { if (event.target === event.currentTarget && !busy()) setEditing(null); }}>
            <form class="modal routing-editor-modal" onSubmit={saveRule}>
              <div class="modal-heading">
                <div><h3>{rule().id ? t("route.editor.editTitle") : t("route.editor.addTitle")}</h3><span>{t("route.editor.subtitle")}</span></div>
                <button class="icon-button" type="button" onClick={() => setEditing(null)} disabled={busy()} aria-label={t("route.common.close")}><Icon name="close" size={18} /></button>
              </div>
              <div class="routing-editor-body">
                <label><span>{t("route.editor.name")}</span><input value={rule().name} onInput={(event) => updateEditing("name", event.currentTarget.value)} placeholder={t("route.editor.namePlaceholder")} /></label>
                <div class="routing-editor-grid">
                  <label><span>{t("route.editor.type")}</span><select value={rule().type} onChange={(event) => updateEditing("type", event.currentTarget.value)}>
                    <For each={ruleTypes()}>{(item) => <option value={item.value}>{item.label}</option>}</For>
                  </select></label>
                  <label><span>{t("route.editor.action")}</span><select value={rule().action} onChange={(event) => updateEditing("action", event.currentTarget.value)}>
                    <For each={actions()}>{(item) => <option value={item.value}>{item.label}</option>}</For>
                  </select></label>
                </div>
                <label><span>{t("route.editor.value")}</span><textarea value={rule().value} onInput={(event) => updateEditing("value", event.currentTarget.value)} placeholder={typeHint(rule().type)} spellcheck={false} /></label>
                <div class="routing-editor-hint">
                  <Icon name="external" size={13} /> {typeHint(rule().type)}
                </div>
                <label class="switch-row">
                  <span><strong>{t("route.editor.enable")}</strong><small>{t("route.editor.enableHint")}</small></span>
                  <span classList={{ "switch": true, on: rule().enabled }}>
                    <input type="checkbox" checked={rule().enabled} onChange={(event) => updateEditing("enabled", event.currentTarget.checked)} />
                    <span class="switch-track"><span class="switch-thumb" /></span>
                  </span>
                </label>
              </div>
              <div class="modal-footer">
                <button class="btn ghost" type="button" onClick={() => setEditing(null)} disabled={busy()}>{t("route.common.cancel")}</button>
                <button class="btn primary" type="submit" disabled={busy()}><Icon name="check" size={14} /> {busy() ? t("route.editor.saving") : t("route.editor.save")}</button>
              </div>
            </form>
          </div>
        )}
      </Show>

      <Show when={editingRuleSet()}>
        {(set) => (
          <div class="modal-backdrop" onClick={(event) => { if (event.target === event.currentTarget && !ruleSetBusy()) setEditingRuleSet(null); }}>
            <form class="modal routing-editor-modal" onSubmit={saveRuleSet}>
              <div class="modal-heading">
                <div><h3>{set().id ? t("route.ruleset.editor.editTitle") : t("route.ruleset.editor.addTitle")}</h3><span>{t("route.ruleset.editor.subtitle")}</span></div>
                <button class="icon-button" type="button" onClick={() => setEditingRuleSet(null)} disabled={ruleSetBusy()} aria-label={t("route.common.close")}><Icon name="close" size={18} /></button>
              </div>
              <div class="routing-editor-body">
                <Show when={ruleSetFormError()}>
                  <div class="alert error"><span class="alert-message"><Icon name="alert" size={15} /> {ruleSetFormError()}</span></div>
                </Show>
                <label><span>{t("route.ruleset.editor.name")}</span><input value={set().name} onInput={(event) => updateEditingRuleSet("name", event.currentTarget.value)} placeholder={t("route.ruleset.editor.namePlaceholder")} /></label>
                <label><span>{t("route.ruleset.editor.url")}</span><input value={set().url} onInput={(event) => updateEditingRuleSet("url", event.currentTarget.value)} placeholder={t("route.ruleset.editor.urlPlaceholder")} spellcheck={false} /></label>
                <div class="routing-editor-hint">
                  <Icon name="external" size={13} /> {t("route.ruleset.editor.urlHint")}
                </div>
                <div class="routing-editor-grid">
                  <label><span>{t("route.ruleset.editor.kind")}</span><select value={set().kind} onChange={(event) => updateEditingRuleSet("kind", event.currentTarget.value)}>
                    <For each={ruleSetKinds()}>{(item) => <option value={item.value}>{item.label}</option>}</For>
                  </select></label>
                  <label><span>{t("route.ruleset.editor.action")}</span><select value={set().action} onChange={(event) => updateEditingRuleSet("action", event.currentTarget.value)}>
                    <For each={ruleSetActions()}>{(item) => <option value={item.value}>{item.label}</option>}</For>
                  </select></label>
                </div>
                <label class="switch-row">
                  <span><strong>{t("route.ruleset.editor.autoUpdate")}</strong><small>{t("route.ruleset.editor.autoUpdateHint")}</small></span>
                  <span classList={{ "switch": true, on: set().autoUpdate }}>
                    <input type="checkbox" checked={set().autoUpdate} onChange={(event) => updateEditingRuleSet("autoUpdate", event.currentTarget.checked)} />
                    <span class="switch-track"><span class="switch-thumb" /></span>
                  </span>
                </label>
                <label><span>{t("route.ruleset.editor.interval")}</span><select value={String(set().intervalMinutes)} onChange={(event) => updateEditingRuleSet("intervalMinutes", Number(event.currentTarget.value))}>
                  <For each={ruleSetIntervalOptions(set().intervalMinutes)}>{(minutes) => (
                    <option value={String(minutes)}>{minutes === 0 ? t("route.ruleset.editor.intervalDefault") : t("route.ruleset.editor.intervalOption", { minutes })}</option>
                  )}</For>
                </select></label>
                <div class="routing-editor-hint">
                  <Icon name="refresh" size={13} /> {t("route.ruleset.editor.intervalHint", { minutes: DEFAULT_RULE_SET_INTERVAL_MINUTES })}
                </div>
                <label class="switch-row">
                  <span><strong>{t("route.ruleset.editor.enable")}</strong><small>{t("route.ruleset.editor.enableHint")}</small></span>
                  <span classList={{ "switch": true, on: set().enabled }}>
                    <input type="checkbox" checked={set().enabled} onChange={(event) => updateEditingRuleSet("enabled", event.currentTarget.checked)} />
                    <span class="switch-track"><span class="switch-thumb" /></span>
                  </span>
                </label>
              </div>
              <div class="modal-footer">
                <button class="btn ghost" type="button" onClick={() => setEditingRuleSet(null)} disabled={ruleSetBusy()}>{t("route.common.cancel")}</button>
                <button class="btn primary" type="submit" disabled={ruleSetBusy()}><Icon name="check" size={14} /> {ruleSetBusy() ? t("route.ruleset.editor.saving") : t("route.ruleset.editor.save")}</button>
              </div>
            </form>
          </div>
        )}
      </Show>
    </div>
  );
}