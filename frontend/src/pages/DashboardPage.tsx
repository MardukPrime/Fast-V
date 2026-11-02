import { createSignal, For, onMount, Show } from "solid-js";

import { DashboardService } from "../../bindings/github.com/MardukPrime/Fast-V";
import type { AppInfo, DashboardSummary, Settings } from "../../bindings/github.com/MardukPrime/Fast-V";
import Icon from "../components/Icon";

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
      <section class="hero dashboard-hero">
        <div class="hero-content">
          <span class="eyebrow">FASTV / DASHBOARD</span>
          <h2>准备开始配置</h2>
          <p>
            订阅和设置已经可以本地保存。添加一个订阅地址，FastV 会自动拉取并解析节点信息。
          </p>
          <div class="hero-actions">
            <button class="btn primary" onClick={() => props.onNavigate("subscriptions")}>
              <Icon name="plus" size={15} /> 添加订阅
            </button>
            <button class="btn secondary" onClick={() => props.onNavigate("settings")}>
              <Icon name="settings" size={15} /> 基础设置
            </button>
          </div>
        </div>
        <div class="hero-metric">
          <div class="hero-metric-value">{summary()?.nodeCount ?? 0}</div>
          <div class="hero-metric-label">当前节点</div>
          <div class="hero-metric-line" />
          <div class="hero-metric-note">来自 {summary()?.subscriptionCount ?? 0} 个订阅</div>
        </div>
      </section>

      <Show when={error()}>
        <div class="alert error">
          <span class="alert-message"><Icon name="alert" size={15} /> {error()}</span>
          <button class="btn ghost" onClick={load}>重试</button>
        </div>
      </Show>

      <section class="stats-grid">
        <article class="stat-card">
          <div class="stat-icon cyan"><Icon name="subscription" size={18} /></div>
          <div class="stat-content">
            <span class="card-label">订阅数量</span>
            <strong>{loading() ? "..." : summary()?.subscriptionCount ?? 0}</strong>
            <small>{summary()?.activeSubscriptions ?? 0} 个启用</small>
          </div>
        </article>
        <article class="stat-card">
          <div class="stat-icon indigo"><Icon name="nodes" size={18} /></div>
          <div class="stat-content">
            <span class="card-label">节点数量</span>
            <strong>{loading() ? "..." : summary()?.nodeCount ?? 0}</strong>
            <small>来自所有订阅</small>
          </div>
        </article>
        <article class="stat-card">
          <div class="stat-icon green"><Icon name="refresh" size={18} /></div>
          <div class="stat-content">
            <span class="card-label">最近更新</span>
            <strong class="card-date">{formatTime(summary()?.lastUpdatedAt ?? "")}</strong>
            <small>订阅更新时间</small>
          </div>
        </article>
        <article class="stat-card">
          <div class="stat-icon amber"><Icon name="server" size={18} /></div>
          <div class="stat-content">
            <span class="card-label">代理内核</span>
            <strong>{summary()?.coreStatus === "not_configured" ? "未配置" : summary()?.coreStatus ?? "未配置"}</strong>
            <small>{summary()?.coreVersion ?? "Xray-core 尚未接入"}</small>
          </div>
        </article>
      </section>

      <section class="dashboard-grid">
        <div class="panel">
          <div class="panel-heading">
            <div>
              <h3>快速操作</h3>
              <span>QUICK START</span>
            </div>
          </div>
          <div class="quick-actions">
            <button class="quick-action" onClick={() => props.onNavigate("subscriptions")}>
              <span class="quick-icon cyan"><Icon name="plus" size={17} /></span>
              <span class="quick-copy">
                <strong>添加订阅</strong>
                <small>导入远程节点列表</small>
              </span>
              <Icon name="chevron" size={15} class="quick-chevron" />
            </button>
            <button class="quick-action" onClick={() => props.onNavigate("settings")}>
              <span class="quick-icon indigo"><Icon name="settings" size={17} /></span>
              <span class="quick-copy">
                <strong>基础设置</strong>
                <small>调整主题和更新策略</small>
              </span>
              <Icon name="chevron" size={15} class="quick-chevron" />
            </button>
            <button class="quick-action" onClick={() => props.onNavigate("servers")}>
              <span class="quick-icon green"><Icon name="server" size={17} /></span>
              <span class="quick-copy">
                <strong>查看节点</strong>
                <small>节点管理即将接入</small>
              </span>
              <Icon name="chevron" size={15} class="quick-chevron" />
            </button>
          </div>
        </div>

        <div class="panel">
          <div class="panel-heading">
            <div>
              <h3>当前配置</h3>
              <span>SETTINGS</span>
            </div>
          </div>
          <div class="config-list">
            <div class="config-row"><span>应用版本</span><strong>{props.info?.version ?? "0.1.0"}</strong></div>
            <div class="config-row"><span>运行平台</span><strong>{props.info ? `${props.info.platform} / ${props.info.arch}` : "..."}</strong></div>
            <div class="config-row"><span>界面主题</span><strong>{props.settings?.theme ?? "dark"}</strong></div>
            <div class="config-row"><span>代理模式</span><strong>{props.settings?.proxyMode ?? "rule"}</strong></div>
          </div>
        </div>
      </section>

      <section class="panel">
        <div class="panel-heading">
          <div>
            <h3>最近订阅</h3>
            <span>RECENT SUBSCRIPTIONS</span>
          </div>
          <button class="btn ghost" onClick={() => props.onNavigate("subscriptions")}>查看全部</button>
        </div>
        <Show when={(summary()?.recentSubscriptions ?? []).length > 0} fallback={
          <div class="empty-state">
            <div class="empty-icon"><Icon name="subscription" size={24} /></div>
            <strong>还没有订阅</strong>
            <span>先添加一个订阅地址，节点会自动出现在这里。</span>
          </div>
        }>
          <div class="recent-list">
            <For each={summary()?.recentSubscriptions ?? []}>
              {(sub) => (
                <div class="recent-item">
                  <div class="recent-avatar"><Icon name="subscription" size={16} /></div>
                  <div class="recent-copy">
                    <strong>{sub.name}</strong>
                    <small>{sub.host}</small>
                  </div>
                  <span class="tag neutral">{sub.nodeCount} 节点</span>
                  <span classList={{ "tag": true, danger: Boolean(sub.lastError), success: !sub.lastError }}>
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