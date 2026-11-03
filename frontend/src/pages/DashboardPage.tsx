import { createSignal, For, onMount, Show } from "solid-js";

import { DashboardService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/dashboard";
import type { AppInfo, DashboardSummary, Settings } from "../../bindings/github.com/MardukPrime/Fast-V/internal/model";
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

function coreStatusLabel(status: string) {
  switch (status) {
    case "ready": return "已就绪";
    case "up_to_date": return "已是最新";
    case "update_available": return "可更新";
    case "checking": return "检查中";
    case "downloading": return "下载中";
    case "not_installed": return "未安装";
    case "error": return "异常";
    default: return "未安装";
  }
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
      <section class="glass-panel workspace-hero">
        <div class="workspace-copy">
          <span class="status-inline"><span class="status-dot online" /> 本地工作区已就绪</span>
          <h2>工作区概览</h2>
          <p>
            订阅、节点和基础设置都保存在本机。添加订阅地址后，FastV 会自动解析节点并汇总到仪表盘。
          </p>
          <div class="workspace-actions">
            <button class="btn primary" onClick={() => props.onNavigate("subscriptions")}>
              <Icon name="plus" size={15} /> 添加订阅
            </button>
            <button class="btn glass" onClick={() => props.onNavigate("settings")}>
              <Icon name="settings" size={15} /> 打开设置
            </button>
          </div>
        </div>

        <div class="workspace-metrics">
          <div class="workspace-metric">
            <span>订阅</span>
            <strong>{loading() ? "—" : summary()?.subscriptionCount ?? 0}</strong>
            <small>{summary()?.activeSubscriptions ?? 0} 个启用</small>
          </div>
          <div class="workspace-metric">
            <span>节点</span>
            <strong>{loading() ? "—" : summary()?.nodeCount ?? 0}</strong>
            <small>已解析</small>
          </div>
          <div class="workspace-metric">
            <span>内核</span>
            <strong class="metric-text">{coreStatusLabel(summary()?.coreStatus ?? "")}</strong>
            <small>{summary()?.coreVersion || "Xray-core"}</small>
          </div>
        </div>
      </section>

      <Show when={error()}>
        <div class="alert error">
          <span class="alert-message"><Icon name="alert" size={15} /> {error()}</span>
          <button class="btn ghost" onClick={load}>重试</button>
        </div>
      </Show>

      <div class="dashboard-layout">
        <section class="glass-panel recent-panel">
          <div class="panel-heading">
            <div>
              <h3>最近订阅</h3>
              <span>{summary()?.lastUpdatedAt ? `最近更新 ${formatTime(summary()!.lastUpdatedAt)}` : "还没有更新记录"}</span>
            </div>
            <button class="btn ghost" onClick={() => props.onNavigate("subscriptions")}>查看全部</button>
          </div>

          <Show when={(summary()?.recentSubscriptions ?? []).length > 0} fallback={
            <div class="empty-state">
              <div class="empty-icon"><Icon name="subscription" size={22} /></div>
              <strong>还没有订阅</strong>
              <span>添加第一个订阅后，节点和更新状态会显示在这里。</span>
            </div>
          }>
            <div class="recent-list">
              <For each={summary()?.recentSubscriptions ?? []}>
                {(sub) => (
                  <div class="recent-row">
                    <div class="recent-avatar"><Icon name="subscription" size={16} /></div>
                    <div class="recent-copy">
                      <strong>{sub.name}</strong>
                      <small>{sub.host}</small>
                    </div>
                    <span class="recent-count">{sub.nodeCount} 节点</span>
                    <span classList={{ "status-label": true, error: Boolean(sub.lastError), ok: !sub.lastError }}>
                      <span class="status-dot" />
                      {sub.lastError ? "异常" : "正常"}
                    </span>
                  </div>
                )}
              </For>
            </div>
          </Show>
        </section>

        <aside class="dashboard-side">
          <section class="glass-panel side-card">
            <div class="panel-heading">
              <div>
                <h3>快捷操作</h3>
                <span>常用入口</span>
              </div>
            </div>
            <div class="quick-actions">
              <button class="quick-action" onClick={() => props.onNavigate("subscriptions")}>
                <span class="quick-icon"><Icon name="plus" size={16} /></span>
                <span class="quick-copy"><strong>添加订阅</strong><small>导入远程节点列表</small></span>
                <Icon name="chevron" size={14} class="quick-chevron" />
              </button>
              <button class="quick-action" onClick={() => props.onNavigate("settings")}>
                <span class="quick-icon"><Icon name="settings" size={16} /></span>
                <span class="quick-copy"><strong>基础设置</strong><small>主题、代理模式和更新间隔</small></span>
                <Icon name="chevron" size={14} class="quick-chevron" />
              </button>
              <button class="quick-action" onClick={() => props.onNavigate("servers")}>
                <span class="quick-icon"><Icon name="server" size={16} /></span>
                <span class="quick-copy"><strong>节点管理</strong><small>选择当前节点和测试连通性</small></span>
                <Icon name="chevron" size={14} class="quick-chevron" />
              </button>
            </div>
          </section>

          <section class="glass-panel side-card">
            <div class="panel-heading">
              <div>
                <h3>当前配置</h3>
                <span>本地状态</span>
              </div>
            </div>
            <div class="config-list">
              <div class="config-row"><span>当前节点</span><strong>{summary()?.activeNode?.name ?? "未选择"}</strong></div>
              <div class="config-row"><span>应用版本</span><strong>{props.info?.version ?? "0.1.0"}</strong></div>
              <div class="config-row"><span>运行平台</span><strong>{props.info ? `${props.info.platform} / ${props.info.arch}` : "..."}</strong></div>
              <div class="config-row"><span>界面主题</span><strong>{props.settings?.theme ?? "dark"}</strong></div>
              <div class="config-row"><span>代理模式</span><strong>{props.settings?.proxyMode ?? "rule"}</strong></div>
            </div>
          </section>
        </aside>
      </div>
    </div>
  );
}