import { createSignal, For, onMount, Show } from "solid-js";

import { DashboardService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/dashboard";
import type { AppInfo, DashboardSummary, ProxyState, ProxyTestResult, Settings } from "../../bindings/github.com/MardukPrime/Fast-V/internal/model";
import Icon from "../components/Icon";

type DashboardProps = {
  info: AppInfo | null;
  settings: Settings | null;
  proxyState: ProxyState;
  proxyBusy: boolean;
  proxyError: string;
  proxyTest: ProxyTestResult | null;
  onStartProxy: () => void;
  onStopProxy: () => void;
  onTestProxy: () => void;
  onNavigate: (page: string) => void;
};

function proxyModeLabel(mode: string) {
  switch (mode) {
    case "global": return "全局代理";
    case "direct": return "直连";
    default: return "规则分流";
  }
}

function proxyStatusLabel(status: string) {
  switch (status) {
    case "running": return "本地代理运行中";
    case "starting": return "正在启动本地代理";
    case "stopping": return "正在停止本地代理";
    case "error": return "本地代理异常";
    default: return "本地代理未启动";
  }
}

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
          <span classList={{ "status-inline": true, error: props.proxyState.status === "error" }}>
            <span classList={{ "status-dot": true, online: props.proxyState.status === "running", idle: props.proxyState.status !== "running" }} />
            {proxyStatusLabel(props.proxyState.status)}
          </span>
          <Show when={props.proxyState.status === "running"}>
            <div class="proxy-runtime-line">
              <Icon name="route" size={13} />
              <span>SOCKS/HTTP {props.proxyState.localAddr}</span>
              <span class="proxy-runtime-node">{props.proxyState.nodeName}</span>
            </div>
          </Show>
          <h2>工作区概览</h2>
          <p>
            订阅、节点和基础设置都保存在本机。添加订阅地址后，FastV 会自动解析节点并汇总到仪表盘。
          </p>
          <div class="workspace-actions">
            <Show when={props.proxyState.status === "running"} fallback={
              <button class="btn primary" onClick={props.onStartProxy} disabled={props.proxyBusy || !summary()?.activeNode || summary()?.coreStatus === "not_installed" || summary()?.coreStatus === "error" || Boolean(props.settings?.tunMode && props.settings?.coreType !== "singbox")}>
                <Icon name="play" size={15} /> {props.proxyBusy ? "启动中..." : "启动代理"}
              </button>
            }>
              <button class="btn primary danger-action" onClick={props.onStopProxy} disabled={props.proxyBusy}>
                <Icon name="stop" size={15} /> {props.proxyBusy ? "停止中..." : "停止代理"}
              </button>
            </Show>
            <button class="btn glass" onClick={props.onTestProxy} disabled={props.proxyBusy || props.proxyState.status !== "running"}>
              <Icon name="speed" size={15} /> 测试代理
            </button>
            <button class="btn ghost" onClick={() => props.onNavigate("subscriptions")}>
              <Icon name="plus" size={15} /> 添加订阅
            </button>
            <button class="btn ghost" onClick={() => props.onNavigate("settings")}>
              <Icon name="settings" size={15} /> 设置
            </button>
          </div>
          <Show when={props.proxyError}>
            <div class="proxy-inline-alert error">
              <Icon name="alert" size={13} /> {props.proxyError}
            </div>
          </Show>
          <Show when={props.proxyTest}>
            <div classList={{ "proxy-inline-alert": true, ok: props.proxyTest?.success, error: !props.proxyTest?.success }}>
              <Icon name={props.proxyTest?.success ? "check" : "alert"} size={13} />
              {props.proxyTest?.success ? `代理可用 · ${props.proxyTest?.latencyMs} ms · HTTP ${props.proxyTest?.statusCode}` : `代理测试失败${props.proxyTest?.error ? ` · ${props.proxyTest.error}` : ""}`}
            </div>
          </Show>
          <Show when={!summary()?.activeNode}>
            <div class="proxy-inline-alert muted"><Icon name="alert" size={13} /> 请先在服务器页面选择一个当前节点。</div>
          </Show>
          <Show when={props.settings?.tunMode && props.settings?.coreType !== "singbox"}>
            <div class="proxy-inline-alert muted"><Icon name="alert" size={13} /> TUN 模式需要 sing-box 或独立 TUN 适配层，当前 Xray 内核下无法启动。</div>
          </Show>
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

      <Show when={props.proxyState.status === "running"}>
        <section class="glass-panel proxy-node-panel">
          <div class="proxy-node-head">
            <span class="heading-icon cyan"><Icon name="server" size={17} /></span>
            <div>
              <h3>运行节点</h3>
              <span>代理已启动，以下为当前实际使用的节点信息</span>
            </div>
            <span class="tag neutral">{props.proxyState.nodeProtocol}</span>
          </div>
          <div class="proxy-node-grid">
            <div><span>节点名称</span><strong>{props.proxyState.nodeName}</strong></div>
            <div><span>服务器地址</span><strong>{props.proxyState.nodeAddress}:{props.proxyState.nodePort}</strong></div>
            <div><span>解析 IP</span><strong>{props.proxyState.resolvedIp || "DNS 解析失败"}</strong></div>
            <div><span>协议</span><strong>{props.proxyState.nodeProtocol}</strong></div>
            <div><span>订阅</span><strong>{props.proxyState.nodeSubscription || "手动节点"}</strong></div>
            <div><span>本地入站</span><strong>{props.proxyState.localAddr}</strong></div>
            <div><span>代理模式</span><strong>{proxyModeLabel(props.proxyState.proxyMode)}</strong></div>
            <div><span>进程 PID</span><strong>{props.proxyState.pid}</strong></div>
          </div>
        </section>
      </Show>

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
              <div class="config-row"><span>代理状态</span><strong>{props.proxyState.status === "running" ? "运行中" : props.proxyState.status === "error" ? "异常" : "已停止"}</strong></div>
              <div class="config-row"><span>本地入站</span><strong>{props.proxyState.status === "running" ? props.proxyState.localAddr : "未监听"}</strong></div>
              <div class="config-row"><span>测试地址</span><strong class="config-url">{props.settings?.testUrl ?? "未设置"}</strong></div>
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