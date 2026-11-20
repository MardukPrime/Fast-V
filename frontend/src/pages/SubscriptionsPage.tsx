import { createSignal, For, onCleanup, onMount, Show } from "solid-js";

import { Events } from "@wailsio/runtime";

import { NodeService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/node";
import { ProxyService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/proxy";
import { Service as SubscriptionService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/subscription";
import type { NodeView, SubscriptionView } from "../../bindings/github.com/MardukPrime/Fast-V/internal/model";
import Icon from "../components/Icon";
import QRModal, { QRCodeIcon } from "../components/QRModal";
import { language, t } from "../i18n";

import "./subscriptions.css";

const DAY_MS = 24 * 60 * 60 * 1000;

// Per-subscription auto-update intervals offered by the options dialog. Zero
// means "follow the global Settings.UpdateIntervalMinutes".
const subscriptionIntervalOptions: number[] = [0, 30, 60, 120, 360, 720, 1440];

function autoUpdateSummary(sub: SubscriptionView) {
  if (!sub.autoUpdate) return t("sub.options.statusOff");
  if (!sub.updateIntervalMinutes) return t("sub.options.statusGlobal");
  return t("sub.options.statusInterval", { minutes: sub.updateIntervalMinutes });
}

/** Gear glyph for the per-subscription options action. */
function GearIcon(props: { size?: number }) {
  const size = () => props.size ?? 14;
  return (
    <svg
      width={size()}
      height={size()}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="1.8"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
    </svg>
  );
}

function formatTime(value: string) {
  if (!value) return t("sub.time.never");
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString(language());
}

// Compact timestamp for the info column: "11/17 11:24".
function formatRecentTime(value: string) {
  if (!value) return t("sub.time.never");
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString(language(), { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

function formatBytes(value: number) {
  if (!value || value <= 0) return "0 B";
  if (value >= 1024 ** 4) return `${(value / 1024 ** 4).toFixed(2)} TB`;
  if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(2)} GB`;
  if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(1)} MB`;
  if (value >= 1024) return `${(value / 1024).toFixed(0)} KB`;
  return `${value} B`;
}

function formatExpire(value: number) {
  if (!value) return t("sub.time.notSet");
  const date = new Date(value * 1000);
  if (Number.isNaN(date.getTime())) return t("sub.time.notSet");
  return date.toLocaleDateString(language());
}

function usageOf(sub: SubscriptionView) {
  return (sub.uploadBytes ?? 0) + (sub.downloadBytes ?? 0);
}

function usagePercent(sub: SubscriptionView) {
  if (!sub.totalBytes || sub.totalBytes <= 0) return 0;
  return Math.min(100, Math.round((usageOf(sub) / sub.totalBytes) * 100));
}

// Whole days left until expireAt (unix seconds); null when no expiry is known.
// Values <= 0 mean the subscription already expired.
function remainingDays(expireAt: number): number | null {
  if (!expireAt || expireAt <= 0) return null;
  return Math.ceil((expireAt * 1000 - Date.now()) / DAY_MS);
}

function formatRemainingDays(days: number | null) {
  if (days === null) return "—";
  if (days <= 0) return t("sub.side.expired");
  return t("sub.side.remaining", { days });
}

type UsageLevel = "normal" | "warning" | "danger";

// <70% normal, 70-90% warning, >90% danger.
function usageLevel(percent: number): UsageLevel {
  if (percent > 90) return "danger";
  if (percent >= 70) return "warning";
  return "normal";
}

export default function SubscriptionsPage() {
  const [subscriptions, setSubscriptions] = createSignal<SubscriptionView[]>([]);
  const [loading, setLoading] = createSignal(true);
  const [adding, setAdding] = createSignal(false);
  const [updatingId, setUpdatingId] = createSignal("");
  const [updatingAll, setUpdatingAll] = createSignal(false);
  const [name, setName] = createSignal("");
  const [url, setURL] = createSignal("");
  const [message, setMessage] = createSignal("");
  const [error, setError] = createSignal("");
  const [nodes, setNodes] = createSignal<NodeView[] | null>(null);
  const [nodesTitle, setNodesTitle] = createSignal("");

  // Subscription QR dialog state. The subscription URL itself is never exposed
  // through the bindings (it contains the user's private token), so the dialog
  // shows the generated QR image only — no text link to copy.
  const [qrOpen, setQROpen] = createSignal(false);
  const [addOpen, setAddOpen] = createSignal(false);
  const [actionsMenuId, setActionsMenuId] = createSignal<string | null>(null);
  const [qrTitle, setQRTitle] = createSignal("");
  const [qrImage, setQRImage] = createSignal("");
  const [qrLoading, setQrLoading] = createSignal(false);
  const [qrError, setQRError] = createSignal("");
  const [qrTarget, setQRTarget] = createSignal<SubscriptionView | null>(null);
  // Per-subscription advanced options dialog. The card view seeds the form;
  // saving replaces that list item with the view returned by the backend.
  const [optionsTarget, setOptionsTarget] = createSignal<SubscriptionView | null>(null);
  const [optionsAutoUpdate, setOptionsAutoUpdate] = createSignal(true);
  const [optionsInterval, setOptionsInterval] = createSignal(0);
  const [optionsUserAgent, setOptionsUserAgent] = createSignal("");
  const [optionsBusy, setOptionsBusy] = createSignal(false);
  const [optionsError, setOptionsError] = createSignal("");
  let qrRequest = 0;
  let alive = true;
  let urlInput: HTMLInputElement | undefined;

  onCleanup(() => {
    alive = false;
  });

  const totalNodes = () => subscriptions().reduce((sum, sub) => sum + sub.nodeCount, 0);
  const activeCount = () => subscriptions().filter((sub) => sub.enabled).length;
  const failedCount = () => subscriptions().filter((sub) => Boolean(sub.lastError)).length;
  const usedTotal = () => subscriptions().reduce((sum, sub) => sum + usageOf(sub), 0);
  const quotaTotal = () => subscriptions().reduce((sum, sub) => sum + (sub.totalBytes ?? 0), 0);

  // Nearest upcoming expiry, plus how many subscriptions already expired.
  const expirySummary = () => {
    const now = Date.now();
    let next: number | null = null;
    let expired = 0;
    for (const sub of subscriptions()) {
      const expireAt = sub.expireAt ?? 0;
      if (expireAt <= 0) continue;
      const time = expireAt * 1000;
      if (time <= now) {
        expired += 1;
        continue;
      }
      if (next === null || time < next) next = time;
    }
    return { next, expired };
  };

  const nearestExpiryHint = () => {
    const summary = expirySummary();
    if (summary.next !== null) {
      const days = Math.max(1, Math.ceil((summary.next - Date.now()) / DAY_MS));
      return t("sub.stats.nextExpiry", { days });
    }
    if (summary.expired > 0) return t("sub.stats.expired", { count: summary.expired });
    return "";
  };

  const focusAddForm = () => {
    setAddOpen(true);
    requestAnimationFrame(() => {
      const input = urlInput;
      if (!input) return;
      input.scrollIntoView({ behavior: "smooth", block: "center" });
      input.focus({ preventScroll: true });
    });
  };

  const load = async () => {
    setLoading(true);
    try {
      setSubscriptions((await SubscriptionService.List()) ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  onMount(() => {
    load();
    const off = Events.On("subscriptions:changed", load);
    onCleanup(off);
  });

  const addSubscription = async (event: Event) => {
    event.preventDefault();
    setError("");
    setMessage("");
    if (!url().trim()) {
      setError(t("sub.error.urlRequired"));
      return;
    }
    setAdding(true);
    try {
      await SubscriptionService.Add({ name: name().trim(), url: url().trim() });
      setName("");
      setURL("");
      setMessage(t("sub.success.added"));
      setAddOpen(false);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setAdding(false);
    }
  };

  const updateSubscription = async (id: string) => {
    setError("");
    setMessage("");
    setUpdatingId(id);
    try {
      await SubscriptionService.Update(id);
      setMessage(t("sub.success.updated"));
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      await load();
    } finally {
      setUpdatingId("");
    }
  };

  const updateAll = async () => {
    setError("");
    setMessage("");
    setUpdatingAll(true);
    try {
      const result = await SubscriptionService.UpdateAll();
      setMessage(t("sub.success.updateAll", { updated: result.updated, failed: result.failed }));
      if ((result.errors ?? []).length > 0) {
        setError((result.errors ?? []).join(t("sub.error.join")));
      }
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setUpdatingAll(false);
    }
  };

  const removeSubscription = async (sub: SubscriptionView) => {
    if (!window.confirm(t("sub.confirm.delete", { name: sub.name, count: sub.nodeCount }))) {
      return;
    }
    setError("");
    setMessage("");
    try {
      await SubscriptionService.Delete(sub.id);
      setMessage(t("sub.success.deleted"));
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const setActiveNode = async (node: NodeView) => {
    setError("");
    try {
      await ProxyService.SwitchNode(node.id);
      setNodes((current) => (current ?? []).map((item) => ({ ...item, active: item.id === node.id })));
      setMessage(t("sub.success.active", { name: node.name }));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const openNodes = async (sub: SubscriptionView) => {
    setError("");
    try {
      setNodes((await SubscriptionService.Nodes(sub.id)) ?? []);
      setNodesTitle(sub.name);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const closeQRCode = () => {
    qrRequest += 1;
    setQROpen(false);
    setQrLoading(false);
  };

  const openSubscriptionQRCode = async (sub: SubscriptionView) => {
    setQRTarget(sub);
    setQRTitle(sub.name);
    setQRImage("");
    setQRError("");
    setQrLoading(true);
    setQROpen(true);
    const token = ++qrRequest;
    try {
      const image = await SubscriptionService.QRCode(sub.id);
      if (!alive || token !== qrRequest) return;
      if (!image) {
        setQRError(t("qr.error.noImage"));
        return;
      }
      setQRImage(image);
    } catch (err) {
      if (!alive || token !== qrRequest) return;
      setQRError(err instanceof Error ? err.message : String(err));
    } finally {
      if (alive && token === qrRequest) setQrLoading(false);
    }
  };

  const retryQRCode = () => {
    const sub = qrTarget();
    if (sub) void openSubscriptionQRCode(sub);
  };

  const openSubscriptionOptions = (sub: SubscriptionView) => {
    setOptionsTarget(sub);
    setOptionsAutoUpdate(Boolean(sub.autoUpdate));
    setOptionsInterval(sub.updateIntervalMinutes ?? 0);
    setOptionsUserAgent(sub.userAgent ?? "");
    setOptionsError("");
    setError("");
    setMessage("");
  };

  const closeSubscriptionOptions = () => {
    if (optionsBusy()) return;
    setOptionsTarget(null);
    setOptionsError("");
  };

  const saveSubscriptionOptions = async () => {
    const target = optionsTarget();
    if (!target || optionsBusy()) return;
    setOptionsBusy(true);
    setOptionsError("");
    setError("");
    setMessage("");
    try {
      const updated = await SubscriptionService.SetOptions(
        target.id,
        optionsAutoUpdate(),
        optionsInterval(),
        optionsUserAgent(),
      );
      // Update the row in place so the card's auto-update status refreshes
      // immediately after saving, without reloading the whole list.
      setSubscriptions((current) => current.map((item) => (item.id === updated.id ? updated : item)));
      setOptionsTarget(null);
      setMessage(t("sub.options.saved", { name: updated.name }));
    } catch (err) {
      // Validation failures come back from the backend as Chinese messages.
      setOptionsError(t("sub.options.saveFailed", { error: err instanceof Error ? err.message : String(err) }));
    } finally {
      setOptionsBusy(false);
    }
  };

  return (
    <div class="page-stack">
      <section class="mini-stats subscription-stats">
        <div class="mini-stat"><span>{t("sub.stats.subscriptions")}</span><strong>{subscriptions().length}</strong><small>{t("sub.stats.active", { count: activeCount() })}{nearestExpiryHint()}</small></div>
        <div class="mini-stat"><span>{t("sub.stats.nodes")}</span><strong>{totalNodes()}</strong><small>{failedCount() > 0 ? t("sub.stats.nodesBroken") : t("sub.stats.nodesHint")}</small></div>
        <div class="mini-stat"><span>{t("sub.stats.quota")}</span><strong>{formatBytes(usedTotal())}</strong><small>{quotaTotal() > 0 ? t("sub.stats.quotaTotal", { total: formatBytes(quotaTotal()) }) : t("sub.stats.quotaUnknown")}</small></div>
        <div class="mini-stat"><span>{t("sub.stats.errors")}</span><strong classList={{ "text-danger": failedCount() > 0 }}>{failedCount()}</strong><small>{failedCount() > 0 ? t("sub.stats.errorsHint") : t("sub.stats.errorsOk")}</small></div>
      </section>

      <Show when={addOpen()}>
      <section class="panel add-panel">
        <div class="panel-heading">
          <div class="heading-with-icon">
            <span class="heading-icon cyan"><Icon name="plus" size={17} /></span>
            <div>
              <h3>{t("sub.add.title")}</h3>
            </div>
          </div>
          <button class="icon-button" type="button" aria-label={t("sub.common.close")} onClick={() => setAddOpen(false)}>
            <Icon name="close" size={16} />
          </button>
        </div>
        <form class="subscription-form" onSubmit={addSubscription}>
          <label>
            <span>{t("sub.add.name")}</span>
            <input value={name()} onInput={(event) => setName(event.currentTarget.value)} placeholder={t("sub.add.namePlaceholder")} />
          </label>
          <label class="grow">
            <span>{t("sub.add.url")}</span>
            <input
              ref={(element) => {
                urlInput = element;
              }}
              value={url()}
              onInput={(event) => setURL(event.currentTarget.value)}
              placeholder="https://example.com/subscribe?token=..."
              autocomplete="off"
            />
          </label>
          <button class="btn primary" type="submit" disabled={adding()}>
            <Icon name="plus" size={14} /> {adding() ? t("sub.add.submitting") : t("sub.add.submit")}
          </button>
        </form>
      </section>
      </Show>

      <Show when={message()}>
        <div class="alert success"><span class="alert-message"><Icon name="check" size={15} /> {message()}</span></div>
      </Show>
      <Show when={error()}>
        <div class="alert error"><span class="alert-message"><Icon name="alert" size={15} /> {error()}</span></div>
      </Show>

      <section class="panel">
        <div class="panel-heading">
          <div>
            <h3>{t("sub.list.title")}</h3>
          </div>
          <div class="sub-list-actions">
            <button class="btn ghost" onClick={updateAll} disabled={updatingAll() || subscriptions().length === 0}>
              <Icon name="refresh" size={14} /> {updatingAll() ? t("sub.add.updatingAll") : t("sub.add.updateAll")}
            </button>
            <button class="btn ghost" onClick={load} disabled={loading()}>
              <Icon name="refresh" size={14} /> {t("sub.list.refresh")}
            </button>
            <button class="btn primary" type="button" onClick={() => setAddOpen(true)}>
              <Icon name="plus" size={14} /> {t("sub.add.title")}
            </button>
          </div>
        </div>

        <Show when={!loading()} fallback={<div class="empty-state">{t("sub.list.loading")}</div>}>
          <Show when={subscriptions().length > 0} fallback={
            <div class="empty-state sub-empty">
              <div class="empty-icon"><Icon name="subscription" size={24} /></div>
              <strong>{t("sub.empty.title")}</strong>
              <span>{t("sub.empty.hint")}</span>
              <ul class="sub-empty-steps">
                <li>{t("sub.empty.step1")}</li>
                <li>{t("sub.empty.step2")}</li>
                <li>{t("sub.empty.step3")}</li>
              </ul>
              <button class="btn secondary" type="button" onClick={focusAddForm}>
                <Icon name="plus" size={14} /> {t("sub.empty.action")}
              </button>
            </div>
          }>
            <div class="sub-list">
              <For each={subscriptions()}>
                {(sub) => {
                  const days = remainingDays(sub.expireAt ?? 0);
                  const expired = days !== null && days <= 0;
                  const expiringSoon = days !== null && days > 0 && days <= 7;
                  const percent = usagePercent(sub);
                  const level = usageLevel(percent);
                  const used = usageOf(sub);
                  const hasQuota = (sub.totalBytes ?? 0) > 0;
                  const updating = () => updatingId() === sub.id;
                  return (
                    <article
                      class="sub-card"
                      classList={{ "is-error": Boolean(sub.lastError), "is-busy": updating() }}
                      aria-busy={updating() ? "true" : "false"}
                    >
                      <div class="sub-identity">
                        <div class="sub-avatar"><Icon name="subscription" size={18} /></div>
                        <div class="sub-identity-copy">
                          <div class="sub-title-line">
                            <strong class="sub-name" title={sub.name}>{sub.name}</strong>
                            <span classList={{
                              "sub-badge": true,
                              "is-ok": !sub.lastError,
                              "is-error": Boolean(sub.lastError),
                            }}>
                              <span class="status-dot" /> {sub.lastError ? t("sub.card.error") : t("sub.card.ok")}
                            </span>
                          </div>
                          <div class="sub-host" title={sub.host || undefined}>{sub.host || "—"}</div>
                          <div class="sub-chips">
                            <span class="sub-chip"><Icon name="nodes" size={12} /> {t("sub.card.nodes", { count: sub.nodeCount })}</span>
                            <span class="sub-chip">
                              <span classList={{ "status-dot": true, online: sub.enabled }} />
                              {sub.enabled ? t("sub.card.enabled") : t("sub.card.disabled")}
                            </span>
                            <span classList={{ "sub-chip": true, "sub-update-chip": true, "is-off": !sub.autoUpdate }}>
                              <Icon name="refresh" size={12} /> {autoUpdateSummary(sub)}
                            </span>
                          </div>
                        </div>
                      </div>

                      <div class="sub-usage">
                        <Show
                          when={hasQuota}
                          fallback={
                            <div class="sub-usage-empty">
                              <span class="sub-usage-unknown">{t("sub.usage.unknown")}</span>
                              <Show when={used > 0}>
                                <span class="sub-usage-note">{t("sub.usage.usedNote", { value: formatBytes(used) })}</span>
                              </Show>
                              <Show when={sub.usageUpdatedAt}>
                                <span class="sub-usage-note">{t("sub.usage.updatedAt", { time: formatRecentTime(sub.usageUpdatedAt) })}</span>
                              </Show>
                            </div>
                          }
                        >
                          <div class="sub-usage-head">
                            <span class="sub-usage-label" title={t("sub.usage.title", { used: formatBytes(used), total: formatBytes(sub.totalBytes) })}>
                              {t("sub.usage.usedLabel")} <strong>{formatBytes(used)}</strong> / {formatBytes(sub.totalBytes)}
                            </span>
                            <span classList={{
                              "sub-usage-pct": true,
                              "is-warning": level === "warning",
                              "is-danger": level === "danger",
                            }}>
                              {percent}%
                            </span>
                          </div>
                          <div
                            class="sub-progress"
                            role="progressbar"
                            aria-label={t("sub.usage.aria", { name: sub.name })}
                            aria-valuemin="0"
                            aria-valuemax="100"
                            aria-valuenow={String(percent)}
                          >
                            <span
                              classList={{
                                "sub-progress-fill": true,
                                "is-warning": level === "warning",
                                "is-danger": level === "danger",
                              }}
                              style={{ width: `${percent}%` }}
                            />
                          </div>
                          <div class="sub-usage-meta">
                            <span>{t("sub.usage.upload", { value: formatBytes(sub.uploadBytes ?? 0) })}</span>
                            <span>{t("sub.usage.download", { value: formatBytes(sub.downloadBytes ?? 0) })}</span>
                            <Show when={sub.usageUpdatedAt}>
                              <span>{t("sub.usage.updatedLabel", { time: formatRecentTime(sub.usageUpdatedAt) })}</span>
                            </Show>
                          </div>
                        </Show>
                      </div>

                      <div class="sub-side">
                        <div class="sub-side-item">
                          <span class="sub-side-label">{t("sub.side.remainingDays")}</span>
                          <strong classList={{
                            "sub-side-days": true,
                            "is-muted": days === null,
                            "is-warning": expiringSoon,
                            "is-danger": expired,
                          }}>
                            {formatRemainingDays(days)}
                          </strong>
                          <Show when={(sub.expireAt ?? 0) > 0}>
                            <span class="sub-side-note" title={t("sub.side.expireTitle", { date: formatExpire(sub.expireAt ?? 0) })}>
                              {formatExpire(sub.expireAt ?? 0)}
                            </span>
                          </Show>
                        </div>
                        <div class="sub-side-item">
                          <span class="sub-side-label">{t("sub.side.recent")}</span>
                          <span class="sub-side-value" title={formatTime(sub.updatedAt)}>{formatRecentTime(sub.updatedAt)}</span>
                        </div>
                      </div>

                      <div class="sub-actions">
                        <button
                          class="btn secondary sub-icon-btn"
                          type="button"
                          onClick={() => updateSubscription(sub.id)}
                          disabled={updating()}
                          aria-label={t("sub.actions.update")}
                          title={updating() ? t("sub.actions.updating") : t("sub.actions.update")}
                        >
                          <Icon name="refresh" size={15} />
                        </button>
                        <button
                          class="btn secondary sub-icon-btn"
                          type="button"
                          onClick={() => void openSubscriptionQRCode(sub)}
                          aria-label={t("sub.actions.qr")}
                          title={t("sub.actions.qrTitle")}
                        >
                          <QRCodeIcon size={15} />
                        </button>
                        <button
                          class="btn secondary sub-icon-btn"
                          type="button"
                          onClick={() => openNodes(sub)}
                          aria-label={t("sub.actions.nodes")}
                          title={t("sub.actions.nodes")}
                        >
                          <Icon name="nodes" size={15} />
                        </button>
                        <div class="sub-more">
                          <button
                            class="btn secondary sub-icon-btn"
                            type="button"
                            aria-label={t("sub.actions.more")}
                            aria-expanded={actionsMenuId() === sub.id}
                            title={t("sub.actions.more")}
                            onClick={() => setActionsMenuId(actionsMenuId() === sub.id ? null : sub.id)}
                          >
                            <Icon name="more" size={15} />
                          </button>
                          <Show when={actionsMenuId() === sub.id}>
                            <div class="sub-menu-backdrop" onClick={() => setActionsMenuId(null)} />
                            <div class="sub-menu" role="menu">
                              <button
                                class="sub-menu-item"
                                type="button"
                                role="menuitem"
                                onClick={() => {
                                  setActionsMenuId(null);
                                  openSubscriptionOptions(sub);
                                }}
                              >
                                <GearIcon size={14} /> {t("sub.options.button")}
                              </button>
                              <button
                                class="sub-menu-item danger"
                                type="button"
                                role="menuitem"
                                onClick={() => {
                                  setActionsMenuId(null);
                                  void removeSubscription(sub);
                                }}
                              >
                                <Icon name="trash" size={14} /> {t("sub.actions.delete")}
                              </button>
                            </div>
                          </Show>
                        </div>
                      </div>

                      <Show when={sub.lastError}>
                        <div class="sub-error" title={sub.lastError}>
                          <Icon name="alert" size={13} /> <span>{sub.lastError}</span>
                        </div>
                      </Show>
                    </article>
                  );
                }}
              </For>
            </div>
          </Show>
        </Show>
      </section>

      <Show when={nodes() !== null}>
        <div class="modal-backdrop" onClick={(event) => { if (event.target === event.currentTarget) setNodes(null); }}>
          <div class="modal">
            <div class="modal-heading">
              <div>
                <h3>{nodesTitle()}</h3>
                <span>{t("sub.card.nodes", { count: (nodes() ?? []).length })}</span>
              </div>
              <button class="icon-button" onClick={() => setNodes(null)} aria-label={t("sub.common.close")}><Icon name="close" size={18} /></button>
            </div>
            <div class="node-table-wrap">
              <Show when={(nodes() ?? []).length > 0} fallback={<div class="empty-state">{t("sub.nodes.empty")}</div>}>
                <table class="node-table">
                  <thead>
                    <tr><th>{t("sub.nodes.col.name")}</th><th>{t("sub.nodes.col.protocol")}</th><th>{t("sub.nodes.col.address")}</th><th>{t("sub.nodes.col.port")}</th><th>{t("sub.nodes.col.actions")}</th></tr>
                  </thead>
                  <tbody>
                    <For each={nodes() ?? []}>
                      {(node) => (
                        <tr>
                          <td>{node.name}</td>
                          <td><span class="tag neutral">{node.protocol}</span></td>
                          <td>{node.address}</td>
                          <td>{node.port}</td>
                          <td>
                            <Show when={node.active} fallback={
                              <button class="btn ghost compact" onClick={() => setActiveNode(node)} disabled={!node.enabled}>
                                {node.enabled ? t("sub.nodes.setActive") : t("sub.nodes.disabled")}
                              </button>
                            }>
                              <span class="status-label ok"><span class="status-dot" />{t("sub.nodes.current")}</span>
                            </Show>
                          </td>
                        </tr>
                      )}
                    </For>
                  </tbody>
                </table>
              </Show>
            </div>
          </div>
        </div>
      </Show>

      <Show when={optionsTarget()}>
        {(target) => (
          <div
            class="modal-backdrop"
            onClick={(event) => {
              if (event.target === event.currentTarget) closeSubscriptionOptions();
            }}
          >
            <div
              class="modal sub-options-modal"
              role="dialog"
              aria-modal="true"
              aria-label={t("sub.options.titleWithName", { name: target().name })}
            >
              <div class="modal-heading">
                <div>
                  <h3>{t("sub.options.titleWithName", { name: target().name })}</h3>
                  <span>{t("sub.options.subtitle")}</span>
                </div>
                <button class="icon-button" onClick={closeSubscriptionOptions} aria-label={t("sub.common.close")}>
                  <Icon name="close" size={18} />
                </button>
              </div>
              <div class="sub-options-body">
                <label class="switch-row">
                  <span>
                    <strong>{t("sub.options.autoUpdate")}</strong>
                    <small>{t("sub.options.autoUpdateHint")}</small>
                  </span>
                  <span classList={{ "switch": true, on: optionsAutoUpdate() }}>
                    <input
                      type="checkbox"
                      checked={optionsAutoUpdate()}
                      disabled={optionsBusy()}
                      onChange={(event) => setOptionsAutoUpdate(event.currentTarget.checked)}
                    />
                    <span class="switch-track"><span class="switch-thumb" /></span>
                  </span>
                </label>
                <div class="settings-grid sub-options-grid">
                  <label>
                    <span>{t("sub.options.interval")}</span>
                    <select
                      value={String(optionsInterval())}
                      disabled={optionsBusy()}
                      onChange={(event) => setOptionsInterval(Number(event.currentTarget.value))}
                    >
                      <For each={subscriptionIntervalOptions}>
                        {(minutes) => (
                          <option value={String(minutes)}>
                            {minutes === 0
                              ? t("sub.options.intervalFollowGlobal")
                              : t("sub.options.intervalMinutes", { minutes })}
                          </option>
                        )}
                      </For>
                      <Show when={optionsInterval() !== 0 && !subscriptionIntervalOptions.includes(optionsInterval())}>
                        <option value={String(optionsInterval())}>
                          {t("sub.options.intervalMinutes", { minutes: optionsInterval() })}
                        </option>
                      </Show>
                    </select>
                    <small>{t("sub.options.intervalHint")}</small>
                  </label>
                  <label>
                    <span>{t("sub.options.userAgent")}</span>
                    <input
                      value={optionsUserAgent()}
                      onInput={(event) => setOptionsUserAgent(event.currentTarget.value)}
                      placeholder={t("sub.options.userAgentPlaceholder")}
                      spellcheck={false}
                      autocomplete="off"
                      maxlength={200}
                      disabled={optionsBusy()}
                    />
                    <small>{t("sub.options.userAgentHint")}</small>
                  </label>
                </div>
                <Show when={optionsError()}>
                  <div class="alert error sub-options-error">
                    <span class="alert-message"><Icon name="alert" size={14} /> {optionsError()}</span>
                  </div>
                </Show>
              </div>
              <div class="modal-footer">
                <button class="btn ghost" type="button" onClick={closeSubscriptionOptions} disabled={optionsBusy()}>
                  {t("sub.options.cancel")}
                </button>
                <button class="btn primary" type="button" onClick={() => void saveSubscriptionOptions()} disabled={optionsBusy()}>
                  <Icon name="check" size={14} /> {optionsBusy() ? t("sub.options.saving") : t("sub.options.save")}
                </button>
              </div>
            </div>
          </div>
        )}
      </Show>

      <QRModal
        open={qrOpen()}
        title={qrTitle() ? t("sub.qr.titleWithName", { name: qrTitle() }) : t("sub.qr.title")}
        subtitle={t("sub.qr.subtitle")}
        image={qrImage()}
        loading={qrLoading()}
        error={qrError()}
        hint={t("sub.qr.hint")}
        onClose={closeQRCode}
        onRetry={retryQRCode}
      />
    </div>
  );
}
