import { createSignal, For, onMount, Show } from "solid-js";

import { DashboardService } from "../../bindings/github.com/MardukPrime/Fast-V";
import type { AppInfo, DashboardSummary, Settings } from "../../bindings/github.com/MardukPrime/Fast-V";

type DashboardProps = {
  info: AppInfo | null;
  settings: Settings | null;
  onNavigate: (page: string) => void;
};

function formatTime(value: string) {
  if (!value) return "尚未更新";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

export default function DashboardPage(props: DashboardProps) {
  const [summary, setSummary] = createSignal<DashboardSummary | null>(null);
  const [loading, setLoading] = createSignal(true);
  const [error, setError] = createSignal("");

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      setSummary(await DashboardService.Summary());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  onMount(load);

  return (
    <div class="page-stack">
      <section class="hero">
        <div>
          <span class="eyebrow">FASTV / DASHBOARD</span>
          <h2>准备开始配置</h2>
          <p>
            订阅和设置已经可以本地保存。下一步添加订阅地址，FastV 会自动拉取并解析节点信息。
          </p>
        </div>
        <div class="hero-badge">
          <span>{summary()?.nodeCount ?? 0}</span>
          <small>节点</small>
        </div>
      </section>

      <Show when={error()}>
        <div class="alert error">
          <span>{error()}</span>
          <button class="btn ghost" onClick={load}>重试</button>
        </div>
      </Show>

      <section class="cards">
        <article class="card">
          <span class="card-label">订阅数量</span>
          <strong>{loading() ? "..." : summary()?.subscriptionCount ?? 0}</strong>
          <small>{summary()?.activeSubscriptions ?? 0} 个启用</small>
        </article>
        <article class="card">
          <span class="card-label">节点数量</span>
          <strong>{loading() ? "..." : summary()?.nodeCount ?? 0}</strong>
          <small>来自所有订阅</small>
        </article>
        <article class="card">
          <span class="card-label">最近更新</span>
          <strong class="card-date">{formatTime(summary()?.lastUpdatedAt ?? "")}</strong>
          <small>订阅更新时间</small>
        </article>
        <article class="card">
          <span class="card-label">代理内核</span>
          <strong>{summary()?.coreStatus === "not_configured" ? "未配置" : summary()?.coreStatus ?? "未配置"}</strong>
          <small>{summary()?.coreVersion ?? "Xray-core 尚未接入"}</small>
        </article>
      </section>

      <section class="dashboard-grid">
        <div class="panel">
          <div class="panel-heading">
            <h3>快速操作</h3>
            <span>QUICK START</span>
          </div>
          <div class="quick-actions">
            <button class="quick-action" onClick={() => props.onNavigate("subscriptions")}>
              <span class="quick-icon">＋</span>
              <span>
                <strong>添加订阅</strong>
                <small>导入远程节点列表</small>
              </span>
            </button>
            <button class="quick-action" onClick={() => props.onNavigate("settings")}>
              <span class="quick-icon">⚙</span>
              <span>
                <strong>基础设置</strong>
                <small>调整主题和更新策略</small>
              </span>
            </button>
            <button class="quick-action" onClick={() => props.onNavigate("servers")}>
              <span class="quick-icon">◎</span>
              <span>
                <strong>查看节点</strong>
                <small>节点管理即将接入</small>
              </span>
            </button>
          </div>
        </div>

        <div class="panel">
          <div class="panel-heading">
            <h3>当前配置</h3>
            <span>SETTINGS</span>
          </div>
          <dl class="info-list">
            <div><dt>应用版本</dt><dd>{props.info?.version ?? "0.1.0"}</dd></div>
            <div><dt>运行平台</dt><dd>{props.info ? `${props.info.platform} / ${props.info.arch}` : "..."}</dd></div>
            <div><dt>界面主题</dt><dd>{props.settings?.theme ?? "dark"}</dd></div>
            <div><dt>代理模式</dt><dd>{props.settings?.proxyMode ?? "rule"}</dd></div>
          </dl>
        </div>
      </section>

      <section class="panel">
        <div class="panel-heading">
          <h3>最近订阅</h3>
          <button class="btn ghost" onClick={() => props.onNavigate("subscriptions")}>查看全部</button>
        </div>
        <Show when={(summary()?.recentSubscriptions ?? []).length > 0} fallback={<div class="empty-state">还没有订阅，先添加一个订阅地址。</div>}>
          <div class="recent-list">
            <For each={summary()?.recentSubscriptions ?? []}>
              {(sub) => (
                <div class="recent-item">
                  <div>
                    <strong>{sub.name}</strong>
                    <small>{sub.host}</small>
                  </div>
                  <span class="tag neutral">{sub.nodeCount} 节点</span>
                  <span classList={{ "tag": true, danger: Boolean(sub.lastError), neutral: !sub.lastError }}>
                    {sub.lastError ? "更新失败" : "正常"}
                  </span>
                </div>
              )}
            </For>
          </div>
        </Show>
      </section>
    </div>
  );
}