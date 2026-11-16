import { createEffect, createMemo, createSignal, For, onCleanup, onMount, Show } from "solid-js";

import { Events } from "@wailsio/runtime";

import { DashboardService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/dashboard";
import { Service as HistoryService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/history";
import { ProxyService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/proxy";
import type { AppInfo, DailyTraffic, DashboardSummary, ProxyState, ProxyTestResult, Settings, SubscriptionView, TrafficStats } from "../../bindings/github.com/MardukPrime/Fast-V/internal/model";
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

function formatBytes(value: number) {
  if (!value || value <= 0) return "0 B";
  if (value >= 1024 ** 4) return `${(value / 1024 ** 4).toFixed(2)} TB`;
  if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(2)} GB`;
  if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(1)} MB`;
  if (value >= 1024) return `${(value / 1024).toFixed(0)} KB`;
  return `${value} B`;
}

function formatRate(value: number) {
  const rate = Number.isFinite(value) && value > 0 ? value : 0;
  if (rate >= 1024 ** 2) return `${(rate / 1024 ** 2).toFixed(1)} MB/s`;
  if (rate >= 1024) return `${(rate / 1024).toFixed(1)} KB/s`;
  return `${rate.toFixed(1)} B/s`;
}

// The weekly chart always shows a rolling seven-day window, so the backend only
// has to provide the days that actually recorded traffic.
const TRAFFIC_HISTORY_DAYS = 7;
const TRAFFIC_HISTORY_REFRESH_MS = 30_000;

type TrafficDay = {
  date: string;
  label: string;
  uplinkBytes: number;
  downlinkBytes: number;
  totalBytes: number;
  downlinkPercent: number;
  uplinkPercent: number;
  today: boolean;
};

function localDateKey(date: Date) {
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${date.getFullYear()}-${month}-${day}`;
}

function buildTrafficWindow(entries: DailyTraffic[]): TrafficDay[] {
  const byDate = new Map<string, DailyTraffic>();
  for (const entry of entries) {
    if (entry?.date) byDate.set(entry.date, entry);
  }
  const now = new Date();
  const days: TrafficDay[] = [];
  for (let offset = TRAFFIC_HISTORY_DAYS - 1; offset >= 0; offset -= 1) {
    const date = new Date(now.getFullYear(), now.getMonth(), now.getDate() - offset);
    const key = localDateKey(date);
    const entry = byDate.get(key);
    const uplinkBytes = Math.max(0, entry?.uplinkBytes ?? 0);
    const downlinkBytes = Math.max(0, entry?.downlinkBytes ?? 0);
    days.push({
      date: key,
      label: key.slice(5),
      uplinkBytes,
      downlinkBytes,
      totalBytes: uplinkBytes + downlinkBytes,
      downlinkPercent: 0,
      uplinkPercent: 0,
      today: offset === 0,
    });
  }
  // Bar heights are relative to the busiest day so the chart always fills the
  // plot area; the two stacked segments together never exceed 100%.
  const maxTotal = days.reduce((max, day) => Math.max(max, day.totalBytes), 0);
  if (maxTotal > 0) {
    for (const day of days) {
      day.downlinkPercent = (day.downlinkBytes / maxTotal) * 100;
      day.uplinkPercent = (day.uplinkBytes / maxTotal) * 100;
    }
  }
  return days;
}

function sessionTotalBytes(stats: TrafficStats | null) {
  if (!stats) return 0;
  return Math.max(0, stats.totalUplinkBytes) + Math.max(0, stats.totalDownlinkBytes);
}

function usagePercent(upload: number, download: number, total: number) {
  if (!total || total <= 0) return 0;
  return Math.min(100, Math.round(((upload + download) / total) * 100));
}

function formatUptime(startedAt: string, now: number) {
  if (!startedAt) return "—";
  const start = new Date(startedAt).getTime();
  if (Number.isNaN(start)) return "—";
  const seconds = Math.max(0, Math.floor((now - start) / 1000));
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const secs = seconds % 60;
  return `${String(hours).padStart(2, "0")}:${String(minutes).padStart(2, "0")}:${String(secs).padStart(2, "0")}`;
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

function nodeUsagePercent(sub: SubscriptionView) {
  return usagePercent(sub.uploadBytes ?? 0, sub.downloadBytes ?? 0, sub.totalBytes ?? 0);
}

export default function DashboardPage(props: DashboardProps) {
  const [summary, setSummary] = createSignal<DashboardSummary | null>(null);
  const [loading, setLoading] = createSignal(true);
  const [error, setError] = createSignal("");
  const [clock, setClock] = createSignal(Date.now());
  const [traffic, setTraffic] = createSignal<TrafficStats | null>(null);
  const [trafficHistory, setTrafficHistory] = createSignal<DailyTraffic[]>([]);
  const [historyLoading, setHistoryLoading] = createSignal(true);
  const [historyError, setHistoryError] = createSignal("");

  // Traffic history failures are non-fatal: the live proxy keeps working and the
  // card keeps showing the last successfully loaded window.
  const loadTrafficHistory = async () => {
    try {
      const days = await HistoryService.Traffic(TRAFFIC_HISTORY_DAYS);
      setTrafficHistory(days ?? []);
      setHistoryError("");
    } catch (err) {
      setHistoryError(err instanceof Error ? err.message : String(err));
    } finally {
      setHistoryLoading(false);
    }
  };

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
  onMount(() => {
    const timer = window.setInterval(() => setClock(Date.now()), 1000);
    onCleanup(() => window.clearInterval(timer));
  });

  const activeNode = () => summary()?.activeNode;
  const usedBytes = () => (summary()?.uploadBytes ?? 0) + (summary()?.downloadBytes ?? 0);
  const totalBytes = () => summary()?.totalBytes ?? 0;
  const quotaPercent = () => usagePercent(summary()?.uploadBytes ?? 0, summary()?.downloadBytes ?? 0, totalBytes());
  const isRunning = () => props.proxyState.status === "running";
  const canStart = () => Boolean(activeNode()) && summary()?.coreStatus !== "not_installed" && summary()?.coreStatus !== "error";

  // Live traffic: subscribe to the "proxy:traffic" broadcast while the proxy runs,
  // and poll every second as a fallback because broadcasts can be missed while the
  // dashboard page is not mounted. Both are torn down when the proxy stops or the
  // page unmounts.
  createEffect(() => {
    if (!isRunning()) {
      setTraffic(null);
      return;
    }
    let alive = true;
    let inFlight = false;
    const offTraffic = Events.On("proxy:traffic", (event) => {
      setTraffic(event.data as TrafficStats);
    });
    const poll = async () => {
      if (!alive || inFlight) return;
      inFlight = true;
      try {
        const stats = await ProxyService.Traffic();
        if (alive) setTraffic(stats);
      } catch {
        // Keep the last good sample; the next tick retries.
      } finally {
        inFlight = false;
      }
    };
    void poll();
    const timer = window.setInterval(() => { void poll(); }, 1000);
    onCleanup(() => {
      alive = false;
      offTraffic();
      window.clearInterval(timer);
    });
  });

  // Persisted daily traffic: load once, then refresh every 30s while the proxy
  // runs because the backend keeps flushing live deltas into today's aggregate.
  // The proxy state itself is a dependency so the final flushed numbers show up
  // as soon as the proxy stops, without polling in the background.
  createEffect(() => {
    const running = isRunning();
    void loadTrafficHistory();
    if (!running) return;
    const timer = window.setInterval(() => { void loadTrafficHistory(); }, TRAFFIC_HISTORY_REFRESH_MS);
    onCleanup(() => window.clearInterval(timer));
  });

  const trafficWindow = createMemo(() => buildTrafficWindow(trafficHistory()));
  const trafficWindowTotal = createMemo(() => trafficWindow().reduce((sum, day) => sum + day.totalBytes, 0));
  const trafficHasData = createMemo(() => trafficWindowTotal() > 0);

  const totalSessionTraffic = () => sessionTotalBytes(traffic());
  const downlinkRateLabel = () => (isRunning() ? formatRate(traffic()?.downlinkRate ?? 0) : "—");
  const uplinkRateLabel = () => (isRunning() ? formatRate(traffic()?.uplinkRate ?? 0) : "—");
  const sessionTotalLabel = () => (isRunning() ? formatBytes(totalSessionTraffic()) : "0 B");
  const sessionRateLabel = () => (isRunning() ? `↓ ${formatRate(traffic()?.downlinkRate ?? 0)} · ↑ ${formatRate(traffic()?.uplinkRate ?? 0)}` : "↓ — · ↑ —");
  const downlinkShare = () => {
    const stats = traffic();
    const total = sessionTotalBytes(stats);
    if (!stats || total <= 0) return 0;
    return Math.min(100, Math.max(0, Math.round((stats.totalDownlinkBytes / total) * 100)));
  };

  return (
    <div class="page-stack dashboard-page">
      <section classList={{ "connection-hero": true, running: isRunning(), error: props.proxyState.status === "error" }}>
        <div class="connection-glow" />
        <div class="connection-main">
          <div classList={{ "connection-orb": true, running: isRunning() }}>
            <Icon name={isRunning() ? "route" : "play"} size={24} />
          </div>
          <div class="connection-copy">
            <div class="connection-eyebrow">
              <span classList={{ "status-dot": true, online: isRunning(), idle: !isRunning() }} />
              <span>{proxyStatusLabel(props.proxyState.status)}</span>
              <span class="tag neutral">{proxyModeLabel(props.settings?.proxyMode ?? "rule")}</span>
              <Show when={props.settings?.tunMode}><span class="tag warning">TUN</span></Show>
            </div>
            <h2>{isRunning() ? props.proxyState.nodeName : activeNode()?.name ?? "准备连接"}</h2>
            <p>
              {isRunning()
                ? `${props.proxyState.nodeProtocol.toUpperCase()} · ${props.proxyState.nodeAddress}:${props.proxyState.nodePort}`
                : activeNode()
                  ? `${activeNode()!.protocol.toUpperCase()} · ${activeNode()!.address}:${activeNode()!.port}`
                  : "还没有选择节点，先在服务器页面选择一个节点。"}
            </p>
            <div class="connection-meta">
              <span><Icon name="route" size={12} /> {isRunning() ? props.proxyState.localAddr : "本地 SOCKS/HTTP 未监听"}</span>
              <Show when={isRunning()}>
                <span><Icon name="speed" size={12} /> 运行 {formatUptime(props.proxyState.startedAt, clock())}</span>
              </Show>
              <span title="实时速率：下行 / 上行">↓ {downlinkRateLabel()} / ↑ {uplinkRateLabel()}</span>
              <Show when={summary()?.coreVersion}>
                <span><Icon name="server" size={12} /> Xray {summary()?.coreVersion}</span>
              </Show>
            </div>
          </div>
        </div>

        <div class="connection-actions">
          <Show when={isRunning()} fallback={
            <button class="btn primary connection-toggle" onClick={props.onStartProxy} disabled={props.proxyBusy || !canStart()}>
              <Icon name="play" size={16} /> {props.proxyBusy ? "启动中..." : "启动代理"}
            </button>
          }>
            <button class="btn primary danger-action connection-toggle" onClick={props.onStopProxy} disabled={props.proxyBusy}>
              <Icon name="stop" size={16} /> {props.proxyBusy ? "停止中..." : "停止代理"}
            </button>
          </Show>
          <button class="btn glass" onClick={props.onTestProxy} disabled={props.proxyBusy || !isRunning()}>
            <Icon name="speed" size={15} /> 测试
          </button>
          <button class="btn ghost" onClick={() => props.onNavigate("servers")}>
            <Icon name="server" size={15} /> 切换节点
          </button>
        </div>

        <Show when={props.proxyError}>
          <div class="proxy-inline-alert error connection-alert"><Icon name="alert" size={13} /> {props.proxyError}</div>
        </Show>
        <Show when={props.proxyTest}>
          <div classList={{ "proxy-inline-alert": true, "connection-alert": true, ok: props.proxyTest?.success, error: !props.proxyTest?.success }}>
            <Icon name={props.proxyTest?.success ? "check" : "alert"} size={13} />
            {props.proxyTest?.success ? `代理可用 · ${props.proxyTest?.latencyMs} ms · HTTP ${props.proxyTest?.statusCode}` : `代理测试失败${props.proxyTest?.error ? ` · ${props.proxyTest.error}` : ""}`}
          </div>
        </Show>
        <Show when={!activeNode()}>
          <div class="proxy-inline-alert muted connection-alert"><Icon name="alert" size={13} /> 请先在服务器页面选择一个当前节点。</div>
        </Show>
      </section>

      <section class="dashboard-stat-grid">
        <div class="glass-panel dashboard-stat">
          <span class="dashboard-stat-icon cyan"><Icon name="subscription" size={17} /></span>
          <div><span>订阅</span><strong>{loading() ? "—" : summary()?.subscriptionCount ?? 0}</strong><small>{summary()?.activeSubscriptions ?? 0} 个启用</small></div>
        </div>
        <div class="glass-panel dashboard-stat">
          <span class="dashboard-stat-icon indigo"><Icon name="server" size={17} /></span>
          <div><span>节点</span><strong>{loading() ? "—" : summary()?.nodeCount ?? 0}</strong><small>已解析节点</small></div>
        </div>
        <div class="glass-panel dashboard-stat traffic">
          <span class="dashboard-stat-icon green"><Icon name="speed" size={17} /></span>
          <div>
            <span>套餐流量</span>
            <strong>{formatBytes(usedBytes())}</strong>
            <small>{totalBytes() > 0 ? `${quotaPercent()}% · 共 ${formatBytes(totalBytes())}` : "订阅端未提供"}</small>
            <Show when={totalBytes() > 0}><div class="dashboard-progress"><span style={{ width: `${quotaPercent()}%` }} /></div></Show>
          </div>
        </div>
        <div class="glass-panel dashboard-stat">
          <span class="dashboard-stat-icon amber"><Icon name="server" size={17} /></span>
          <div><span>Xray 内核</span><strong>{coreStatusLabel(summary()?.coreStatus ?? "")}</strong><small>{summary()?.coreVersion || "未安装"}</small></div>
        </div>
        <div class="glass-panel dashboard-stat" style={{ "grid-column": "1 / -1" }}>
          <span class="dashboard-stat-icon green"><Icon name="speed" size={17} /></span>
          <div>
            <div style={{ display: "flex", "align-items": "baseline", "flex-wrap": "wrap", gap: "2px 16px" }}>
              <span>本次会话流量</span>
              <strong style={{ "margin-top": "0" }}>{sessionTotalLabel()}</strong>
              <small style={{ "margin-top": "0" }} title={traffic()?.error || "实时速率：下行 / 上行"}>{sessionRateLabel()}</small>
            </div>
            <Show when={isRunning() && totalSessionTraffic() > 0}>
              <div
                class="dashboard-progress"
                style={{ display: "flex" }}
                title={`下行 ${downlinkShare()}% · 上行 ${100 - downlinkShare()}%`}
              >
                <span style={{ width: `${downlinkShare()}%`, background: "var(--accent)" }} />
                <span style={{ width: `${100 - downlinkShare()}%`, background: "var(--success)" }} />
              </div>
            </Show>
          </div>
        </div>
      </section>

      <Show when={error()}>
        <div class="alert error"><span class="alert-message"><Icon name="alert" size={15} /> {error()}</span><button class="btn ghost" onClick={load}>重试</button></div>
      </Show>

      <div class="dashboard-main-grid">
        <section class="glass-panel node-overview">
          <div class="panel-heading">
            <div>
              <h3>当前节点</h3>
              <span>代理连接和本地入站信息</span>
            </div>
            <button class="btn secondary" onClick={() => props.onNavigate("servers")}>切换节点</button>
          </div>
          <Show when={activeNode()} fallback={
            <div class="empty-state dashboard-node-empty">
              <div class="empty-icon"><Icon name="server" size={22} /></div>
              <strong>还没有选择节点</strong>
              <span>从服务器页面选择一个节点后再启动代理。</span>
            </div>
          }>
            <div class="node-identity">
              <div class="node-identity-avatar"><Icon name="server" size={20} /></div>
              <div class="node-identity-copy">
                <strong>{isRunning() ? props.proxyState.nodeName : activeNode()!.name}</strong>
                <span>{isRunning() ? `${props.proxyState.nodeAddress}:${props.proxyState.nodePort}` : `${activeNode()!.address}:${activeNode()!.port}`}</span>
              </div>
              <span class="tag neutral">{(isRunning() ? props.proxyState.nodeProtocol : activeNode()!.protocol).toUpperCase()}</span>
            </div>
            <div class="node-detail-grid">
              <div><span>订阅</span><strong>{isRunning() ? props.proxyState.nodeSubscription || "手动节点" : activeNode()!.subscriptionName || "手动节点"}</strong></div>
              <div><span>本地入站</span><strong>{isRunning() ? props.proxyState.localAddr : "未监听"}</strong></div>
              <div><span>代理模式</span><strong>{proxyModeLabel(isRunning() ? props.proxyState.proxyMode : props.settings?.proxyMode ?? "rule")}</strong></div>
              <div><span>进程 PID</span><strong>{isRunning() ? props.proxyState.pid : "—"}</strong></div>
              <div><span>解析 IP</span><strong>{isRunning() ? props.proxyState.resolvedIp || "解析失败" : "启动后解析"}</strong></div>
              <div><span>运行时长</span><strong>{isRunning() ? formatUptime(props.proxyState.startedAt, clock()) : "—"}</strong></div>
            </div>
          </Show>
        </section>

        <aside class="dashboard-rail">
          <section class="glass-panel side-card">
            <div class="panel-heading"><div><h3>快捷操作</h3><span>常用入口</span></div></div>
            <div class="quick-actions">
              <button class="quick-action" onClick={() => props.onNavigate("subscriptions")}><span class="quick-icon"><Icon name="plus" size={16} /></span><span class="quick-copy"><strong>添加订阅</strong><small>导入远程节点列表</small></span><Icon name="chevron" size={14} class="quick-chevron" /></button>
              <button class="quick-action" onClick={() => props.onNavigate("routing")}><span class="quick-icon"><Icon name="route" size={16} /></span><span class="quick-copy"><strong>路由规则</strong><small>调整分流和直连策略</small></span><Icon name="chevron" size={14} class="quick-chevron" /></button>
              <button class="quick-action" onClick={() => props.onNavigate("logs")}><span class="quick-icon"><Icon name="logs" size={16} /></span><span class="quick-copy"><strong>运行日志</strong><small>查看 Xray 实时输出</small></span><Icon name="chevron" size={14} class="quick-chevron" /></button>
              <button class="quick-action" onClick={() => props.onNavigate("settings")}><span class="quick-icon"><Icon name="settings" size={16} /></span><span class="quick-copy"><strong>基础设置</strong><small>主题、代理模式和内核参数</small></span><Icon name="chevron" size={14} class="quick-chevron" /></button>
            </div>
          </section>

          <section class="glass-panel side-card">
            <div class="panel-heading"><div><h3>系统状态</h3><span>FastV 运行环境</span></div></div>
            <div class="system-status-list">
              <div><span>代理状态</span><strong>{isRunning() ? "运行中" : props.proxyState.status === "error" ? "异常" : "已停止"}</strong></div>
              <div><span>Xray API</span><strong>{props.settings?.xrayApiEnabled ? `127.0.0.1:${props.settings.xrayApiPort}` : "未启用"}</strong></div>
              <div><span>TUN 模式</span><strong>{props.settings?.tunMode ? "已启用" : "未启用"}</strong></div>
              <div><span>测试地址</span><strong class="config-url">{props.settings?.testUrl ?? "未设置"}</strong></div>
              <div><span>应用版本</span><strong>v{props.info?.version ?? "0.1.0"}</strong></div>
              <div><span>运行平台</span><strong>{props.info ? `${props.info.platform} / ${props.info.arch}` : "..."}</strong></div>
            </div>
          </section>
        </aside>
      </div>

      <section class="glass-panel traffic-history-panel">
        <div class="panel-heading">
          <div>
            <h3>最近 7 天流量</h3>
            <span>每日通过本地代理的上行 / 下行总量，历史保存在本地</span>
          </div>
          <div class="traffic-history-meta">
            <Show when={trafficHasData()}>
              <span class="traffic-history-total">
                7 天合计 <strong>{formatBytes(trafficWindowTotal())}</strong>
              </span>
            </Show>
            <div class="traffic-legend">
              <span class="traffic-legend-item"><i class="traffic-legend-dot downlink" /> 下行</span>
              <span class="traffic-legend-item"><i class="traffic-legend-dot uplink" /> 上行</span>
            </div>
          </div>
        </div>

        <Show when={historyError()}>
          <div class="alert error traffic-history-alert">
            <span class="alert-message"><Icon name="alert" size={15} /> 流量历史加载失败：{historyError()}</span>
            <button class="btn ghost" onClick={() => { void loadTrafficHistory(); }}>重试</button>
          </div>
        </Show>

        <Show
          when={historyLoading()}
          fallback={
            <Show
              when={trafficHasData()}
              fallback={
                <div class="empty-state">
                  <div class="empty-icon"><Icon name="speed" size={22} /></div>
                  <strong>还没有流量历史</strong>
                  <span>启动代理并产生流量后，这里会显示每天的上行与下行总量。</span>
                </div>
              }
            >
              <div class="traffic-chart" role="img" aria-label="最近 7 天每天的上行与下行流量柱状图">
                <For each={trafficWindow()}>
                  {(day) => (
                    <div classList={{ "traffic-day": true, today: day.today }}>
                      <span class="traffic-day-total">{formatBytes(day.totalBytes)}</span>
                      <div
                        class="traffic-day-plot"
                        title={`${day.date} · 下行 ${formatBytes(day.downlinkBytes)} · 上行 ${formatBytes(day.uplinkBytes)}`}
                      >
                        <Show when={day.totalBytes > 0} fallback={<span class="traffic-day-zero" />}>
                          <div class="traffic-day-stack">
                            <span class="traffic-day-bar downlink" style={{ height: `${day.downlinkPercent}%` }} />
                            <span class="traffic-day-bar uplink" style={{ height: `${day.uplinkPercent}%` }} />
                          </div>
                        </Show>
                      </div>
                      <span class="traffic-day-label">{day.label}</span>
                    </div>
                  )}
                </For>
              </div>
            </Show>
          }
        >
          <div class="traffic-history-loading">
            <span class="network-pulse" />
            <strong>正在读取流量历史…</strong>
          </div>
        </Show>
      </section>

      <section class="glass-panel recent-panel dashboard-recent">
        <div class="panel-heading">
          <div><h3>最近订阅</h3><span>{summary()?.lastUpdatedAt ? `最近更新 ${formatTime(summary()!.lastUpdatedAt)}` : "还没有更新记录"}</span></div>
          <button class="btn ghost" onClick={() => props.onNavigate("subscriptions")}>查看全部</button>
        </div>
        <Show when={(summary()?.recentSubscriptions ?? []).length > 0} fallback={
          <div class="empty-state"><div class="empty-icon"><Icon name="subscription" size={22} /></div><strong>还没有订阅</strong><span>添加第一个订阅后，节点和套餐用量会显示在这里。</span></div>
        }>
          <div class="recent-list">
            <For each={summary()?.recentSubscriptions ?? []}>
              {(sub) => (
                <div class="recent-row dashboard-recent-row">
                  <div class="recent-avatar"><Icon name="subscription" size={16} /></div>
                  <div class="recent-copy"><strong>{sub.name}</strong><small>{sub.host}</small></div>
                  <span class="recent-count">{sub.nodeCount} 节点</span>
                  <Show when={(sub.totalBytes ?? 0) > 0} fallback={<span class="recent-usage-empty">未提供用量</span>}>
                    <div class="recent-usage">
                      <span>{formatBytes((sub.uploadBytes ?? 0) + (sub.downloadBytes ?? 0))} / {formatBytes(sub.totalBytes ?? 0)}</span>
                      <div class="recent-usage-track"><span style={{ width: `${nodeUsagePercent(sub)}%` }} /></div>
                    </div>
                  </Show>
                  <span classList={{ "status-label": true, error: Boolean(sub.lastError), ok: !sub.lastError }}><span class="status-dot" />{sub.lastError ? "异常" : "正常"}</span>
                </div>
              )}
            </For>
          </div>
        </Show>
      </section>
    </div>
  );
}