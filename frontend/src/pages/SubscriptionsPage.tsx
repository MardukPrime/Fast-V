import { createSignal, For, onCleanup, onMount, Show } from "solid-js";

import { Events } from "@wailsio/runtime";

import { NodeService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/node";
import { ProxyService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/proxy";
import { Service as SubscriptionService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/subscription";
import type { NodeView, SubscriptionView } from "../../bindings/github.com/MardukPrime/Fast-V/internal/model";
import Icon from "../components/Icon";
import QRModal, { QRCodeIcon } from "../components/QRModal";

import "./subscriptions.css";

const DAY_MS = 24 * 60 * 60 * 1000;

function formatTime(value: string) {
  if (!value) return "从未更新";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

// Compact timestamp for the info column: "11/17 11:24".
function formatRecentTime(value: string) {
  if (!value) return "从未更新";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
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
  if (!value) return "未设置";
  const date = new Date(value * 1000);
  if (Number.isNaN(date.getTime())) return "未设置";
  return date.toLocaleDateString("zh-CN");
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
  if (days <= 0) return "已过期";
  return `剩余 ${days} 天`;
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
  const [qrTitle, setQRTitle] = createSignal("");
  const [qrImage, setQRImage] = createSignal("");
  const [qrLoading, setQrLoading] = createSignal(false);
  const [qrError, setQRError] = createSignal("");
  const [qrTarget, setQRTarget] = createSignal<SubscriptionView | null>(null);
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
      return ` · 最近 ${days} 天后到期`;
    }
    if (summary.expired > 0) return ` · ${summary.expired} 个已过期`;
    return "";
  };

  const focusAddForm = () => {
    const input = urlInput;
    if (!input) return;
    input.scrollIntoView({ behavior: "smooth", block: "center" });
    input.focus({ preventScroll: true });
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
      setError("请输入订阅地址。");
      return;
    }
    setAdding(true);
    try {
      await SubscriptionService.Add({ name: name().trim(), url: url().trim() });
      setName("");
      setURL("");
      setMessage("订阅已添加并完成首次更新。");
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
      setMessage("订阅已更新。");
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
      setMessage(`已更新 ${result.updated} 个订阅，失败 ${result.failed} 个。`);
      if ((result.errors ?? []).length > 0) {
        setError((result.errors ?? []).join("；"));
      }
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setUpdatingAll(false);
    }
  };

  const removeSubscription = async (sub: SubscriptionView) => {
    if (!window.confirm(`确定删除订阅「${sub.name}」及其 ${sub.nodeCount} 个节点吗？`)) {
      return;
    }
    setError("");
    setMessage("");
    try {
      await SubscriptionService.Delete(sub.id);
      setMessage("订阅已删除。");
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
      setMessage(`已将「${node.name}」设为当前节点。`);
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
        setQRError("后端没有返回二维码图片，请稍后重试。");
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

  return (
    <div class="page-stack">
      <section class="mini-stats subscription-stats">
        <div class="mini-stat"><span>订阅</span><strong>{subscriptions().length}</strong><small>{activeCount()} 个启用{nearestExpiryHint()}</small></div>
        <div class="mini-stat"><span>节点</span><strong>{totalNodes()}</strong><small>{failedCount() > 0 ? "部分订阅待修复" : "已解析"}</small></div>
        <div class="mini-stat"><span>套餐流量</span><strong>{formatBytes(usedTotal())}</strong><small>{quotaTotal() > 0 ? `共 ${formatBytes(quotaTotal())}` : "订阅端未提供"}</small></div>
        <div class="mini-stat"><span>异常</span><strong classList={{ "text-danger": failedCount() > 0 }}>{failedCount()}</strong><small>{failedCount() > 0 ? "需要处理" : "全部正常"}</small></div>
      </section>

      <section class="panel add-panel">
        <div class="panel-heading">
          <div class="heading-with-icon">
            <span class="heading-icon cyan"><Icon name="plus" size={17} /></span>
            <div>
              <h3>添加订阅</h3>
              <span>支持 Base64 订阅和常见分享链接</span>
            </div>
          </div>
          <button class="btn secondary" onClick={updateAll} disabled={updatingAll() || subscriptions().length === 0}>
            <Icon name="refresh" size={14} /> {updatingAll() ? "更新中..." : "全部更新"}
          </button>
        </div>
        <form class="subscription-form" onSubmit={addSubscription}>
          <label>
            <span>名称（可选）</span>
            <input value={name()} onInput={(event) => setName(event.currentTarget.value)} placeholder="例如：主力机场" />
          </label>
          <label class="grow">
            <span>订阅地址</span>
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
            <Icon name="plus" size={14} /> {adding() ? "添加中..." : "添加订阅"}
          </button>
        </form>
      </section>

      <Show when={message()}>
        <div class="alert success"><span class="alert-message"><Icon name="check" size={15} /> {message()}</span></div>
      </Show>
      <Show when={error()}>
        <div class="alert error"><span class="alert-message"><Icon name="alert" size={15} /> {error()}</span></div>
      </Show>

      <section class="panel">
        <div class="panel-heading">
          <div>
            <h3>订阅列表</h3>
            <span>{subscriptions().length} 个订阅 / {totalNodes()} 个节点</span>
          </div>
          <button class="btn ghost" onClick={load} disabled={loading()}>
            <Icon name="refresh" size={14} /> 刷新
          </button>
        </div>

        <Show when={!loading()} fallback={<div class="empty-state">正在读取订阅...</div>}>
          <Show when={subscriptions().length > 0} fallback={
            <div class="empty-state sub-empty">
              <div class="empty-icon"><Icon name="subscription" size={24} /></div>
              <strong>还没有订阅</strong>
              <span>添加订阅地址后，FastV 会自动解析节点，并同步套餐用量与到期时间。</span>
              <ul class="sub-empty-steps">
                <li>填写订阅地址</li>
                <li>自动解析节点</li>
                <li>同步用量与到期</li>
              </ul>
              <button class="btn secondary" type="button" onClick={focusAddForm}>
                <Icon name="plus" size={14} /> 去添加订阅
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
                              <span class="status-dot" /> {sub.lastError ? "异常" : "正常"}
                            </span>
                          </div>
                          <div class="sub-host" title={sub.host || undefined}>{sub.host || "—"}</div>
                          <div class="sub-chips">
                            <span class="sub-chip"><Icon name="nodes" size={12} /> {sub.nodeCount} 个节点</span>
                            <span class="sub-chip">
                              <span classList={{ "status-dot": true, online: sub.enabled }} />
                              {sub.enabled ? "已启用" : "已停用"}
                            </span>
                          </div>
                        </div>
                      </div>

                      <div class="sub-usage">
                        <Show
                          when={hasQuota}
                          fallback={
                            <div class="sub-usage-empty">
                              <span class="sub-usage-unknown">用量未知</span>
                              <Show when={used > 0}>
                                <span class="sub-usage-note">已用 {formatBytes(used)}</span>
                              </Show>
                              <Show when={sub.usageUpdatedAt}>
                                <span class="sub-usage-note">更新于 {formatRecentTime(sub.usageUpdatedAt)}</span>
                              </Show>
                            </div>
                          }
                        >
                          <div class="sub-usage-head">
                            <span class="sub-usage-label" title={`已用 ${formatBytes(used)} / ${formatBytes(sub.totalBytes)}`}>
                              已用 <strong>{formatBytes(used)}</strong> / {formatBytes(sub.totalBytes)}
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
                            aria-label={`${sub.name} 套餐用量`}
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
                            <span>上传 {formatBytes(sub.uploadBytes ?? 0)}</span>
                            <span>下载 {formatBytes(sub.downloadBytes ?? 0)}</span>
                            <Show when={sub.usageUpdatedAt}>
                              <span>用量更新 {formatRecentTime(sub.usageUpdatedAt)}</span>
                            </Show>
                          </div>
                        </Show>
                      </div>

                      <div class="sub-side">
                        <div class="sub-side-item">
                          <span class="sub-side-label">剩余天数</span>
                          <strong classList={{
                            "sub-side-days": true,
                            "is-muted": days === null,
                            "is-warning": expiringSoon,
                            "is-danger": expired,
                          }}>
                            {formatRemainingDays(days)}
                          </strong>
                          <Show when={(sub.expireAt ?? 0) > 0}>
                            <span class="sub-side-note" title={`到期时间 ${formatExpire(sub.expireAt ?? 0)}`}>
                              {formatExpire(sub.expireAt ?? 0)}
                            </span>
                          </Show>
                        </div>
                        <div class="sub-side-item">
                          <span class="sub-side-label">最近更新</span>
                          <span class="sub-side-value" title={formatTime(sub.updatedAt)}>{formatRecentTime(sub.updatedAt)}</span>
                        </div>
                      </div>

                      <div class="sub-actions">
                        <button
                          class="btn secondary"
                          type="button"
                          onClick={() => void openSubscriptionQRCode(sub)}
                          title="订阅二维码包含私有令牌，请谨慎分享"
                        >
                          <QRCodeIcon size={14} /> 二维码
                        </button>
                        <button class="btn secondary" type="button" onClick={() => openNodes(sub)}>
                          <Icon name="nodes" size={14} /> 节点
                        </button>
                        <button class="btn secondary" type="button" onClick={() => updateSubscription(sub.id)} disabled={updating()}>
                          <Icon name="refresh" size={14} /> {updating() ? "更新中" : "更新"}
                        </button>
                        <button
                          class="btn danger"
                          type="button"
                          onClick={() => removeSubscription(sub)}
                          aria-label={`删除 ${sub.name}`}
                          title="删除订阅"
                        >
                          <Icon name="trash" size={14} />
                        </button>
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
                <span>{(nodes() ?? []).length} 个节点</span>
              </div>
              <button class="icon-button" onClick={() => setNodes(null)} aria-label="关闭"><Icon name="close" size={18} /></button>
            </div>
            <div class="node-table-wrap">
              <Show when={(nodes() ?? []).length > 0} fallback={<div class="empty-state">没有解析到节点。</div>}>
                <table class="node-table">
                  <thead>
                    <tr><th>名称</th><th>协议</th><th>地址</th><th>端口</th><th>操作</th></tr>
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
                                {node.enabled ? "设为当前" : "已禁用"}
                              </button>
                            }>
                              <span class="status-label ok"><span class="status-dot" />当前</span>
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

      <QRModal
        open={qrOpen()}
        title={qrTitle() ? `订阅二维码 · ${qrTitle()}` : "订阅二维码"}
        subtitle="扫码可在其他设备或客户端中导入该订阅"
        image={qrImage()}
        loading={qrLoading()}
        error={qrError()}
        hint="订阅链接包含私有令牌；为保证安全，后端只提供二维码图片，无法在此显示或复制文本链接。"
        onClose={closeQRCode}
        onRetry={retryQRCode}
      />
    </div>
  );
}
