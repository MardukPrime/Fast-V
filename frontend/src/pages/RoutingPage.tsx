import { createSignal, For, onMount, Show } from "solid-js";

import { ProxyService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/proxy";
import { Service as RoutingService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/routing";
import type { ProxyState, RoutingPreset, RoutingRule } from "../../bindings/github.com/MardukPrime/Fast-V/internal/model";
import Icon from "../components/Icon";
import { t } from "../i18n";

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

  onMount(load);

  const restartIfRunning = async (
    appliedMessage = t("route.msg.savedApplied"),
    pendingMessage = t("route.msg.savedPending"),
  ) => {
    if (props.proxyState.status === "running" || props.proxyState.status === "starting") {
      setMessage(t("route.msg.restarting"));
      await ProxyService.Restart();
      setMessage(appliedMessage);
    } else {
      setMessage(pendingMessage);
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

  const modeLabel = () => {
    switch (props.proxyMode) {
      case "global": return t("route.mode.global");
      case "direct": return t("route.mode.direct");
      default: return t("route.mode.rule");
    }
  };

  return (
    <div class="page-stack">
      <section class="mini-stats">
        <div class="mini-stat"><span>{t("route.stats.customRules")}</span><strong>{rules().length}</strong><small>{t("route.stats.customRulesHint")}</small></div>
        <div class="mini-stat"><span>{t("route.stats.enabled")}</span><strong>{rules().filter((rule) => rule.enabled).length}</strong><small>{t("route.stats.enabledHint")}</small></div>
        <div class="mini-stat"><span>{t("route.stats.mode")}</span><strong class="routing-mode-value">{modeLabel()}</strong><small>{t("route.stats.modeHint")}</small></div>
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
    </div>
  );
}