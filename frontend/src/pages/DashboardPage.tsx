import { createEffect, createMemo, createSignal, For, onCleanup, onMount, Show } from "solid-js";

import { Events } from "@wailsio/runtime";

import { DashboardService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/dashboard";
import { Service as HistoryService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/history";
import { ProxyService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/proxy";
import type { AppInfo, DailyTraffic, DashboardSummary, ProxyState, ProxyTestResult, Settings, SubscriptionView, TrafficStats } from "../../bindings/github.com/MardukPrime/Fast-V/internal/model";
import Icon from "../components/Icon";
import { language, t } from "../i18n";
import "./dashboard.css";

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
  onLiveWaveStyleChange: (style: string) => void;
};

function proxyModeLabel(mode: string) {
  switch (mode) {
    case "global": return t("dash.proxyModeGlobal");
    case "direct": return t("dash.proxyModeDirect");
    default: return t("dash.proxyModeRule");
  }
}

function proxyStatusLabel(status: string) {
  switch (status) {
    case "running": return t("dash.proxyStatusRunning");
    case "starting": return t("dash.proxyStatusStarting");
    case "stopping": return t("dash.proxyStatusStopping");
    case "error": return t("dash.proxyStatusError");
    default: return t("dash.proxyStatusStopped");
  }
}

function formatTime(value: string) {
  if (!value) return t("dash.notUpdated");
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString(language());
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

// Subscription backends report expiry as a unix timestamp in seconds; 0 means
// "not provided". The plan card surfaces the nearest valid expiry because that
// is the subscription the user has to renew first.
function earliestExpireAt(subscriptions: SubscriptionView[] | null | undefined) {
  let earliest = 0;
  for (const sub of subscriptions ?? []) {
    const value = Number(sub.expireAt ?? 0);
    if (!Number.isFinite(value) || value <= 0) continue;
    if (earliest === 0 || value < earliest) earliest = value;
  }
  return earliest;
}

function planExpiryText(expireAtSeconds: number, now: number) {
  const target = expireAtSeconds * 1000;
  const date = new Date(target);
  if (!expireAtSeconds || !Number.isFinite(target) || Number.isNaN(date.getTime())) return "";
  const days = Math.ceil((target - now) / 86_400_000);
  if (!Number.isFinite(days)) return "";
  const dateLabel = `${date.getMonth() + 1}/${date.getDate()}`;
  if (days < 0) return t("dash.expired", { date: dateLabel });
  if (days === 0) return t("dash.expiresToday", { date: dateLabel });
  return t("dash.remainingDays", { days, date: dateLabel });
}

function planExpiryLevel(expireAtSeconds: number, now: number) {
  const target = expireAtSeconds * 1000;
  if (!expireAtSeconds || !Number.isFinite(target)) return "none";
  const days = Math.ceil((target - now) / 86_400_000);
  if (!Number.isFinite(days)) return "none";
  if (days < 0) return "expired";
  if (days <= 7) return "warning";
  return "ok";
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
    case "ready": return t("dash.coreReady");
    case "up_to_date": return t("dash.coreUpToDate");
    case "update_available": return t("dash.coreUpdateAvailable");
    case "checking": return t("dash.coreChecking");
    case "downloading": return t("dash.coreDownloading");
    case "not_installed": return t("dash.coreNotInstalled");
    case "error": return t("dash.coreError");
    default: return t("dash.coreNotInstalled");
  }
}

function nodeUsagePercent(sub: SubscriptionView) {
  return usagePercent(sub.uploadBytes ?? 0, sub.downloadBytes ?? 0, sub.totalBytes ?? 0);
}

const WAVE_SAMPLES = 40;

type WavePoint = [number, number];

function wavePoints(values: number[], maxValue: number): WavePoint[] {
  const height = 24;
  const bottom = 25;
  const safeMax = Math.max(1, maxValue);
  const count = values.length;
  if (count === 0) return [];
  return values.map((value, index) => {
    const x = count === 1 ? 50 : (index / (count - 1)) * 100;
    const ratio = Math.min(1, Math.max(0, value / safeMax));
    return [x, bottom - ratio * height];
  });
}

function wavePath(values: number[], maxValue: number) {
  const points = wavePoints(values, maxValue);
  if (points.length === 0) return "M 0 25 L 100 25";
  if (points.length === 1) return `M 0 ${points[0][1].toFixed(2)} L 100 ${points[0][1].toFixed(2)}`;
  let d = `M ${points[0][0].toFixed(2)} ${points[0][1].toFixed(2)}`;
  for (let i = 1; i < points.length - 1; i += 1) {
    const [x, y] = points[i];
    const next = points[i + 1];
    const midX = (x + next[0]) / 2;
    const midY = (y + next[1]) / 2;
    d += ` Q ${x.toFixed(2)} ${y.toFixed(2)} ${midX.toFixed(2)} ${midY.toFixed(2)}`;
  }
  const last = points[points.length - 1];
  d += ` L ${last[0].toFixed(2)} ${last[1].toFixed(2)}`;
  return d;
}

function waveAreaPath(values: number[], maxValue: number) {
  return `${wavePath(values, maxValue)} L 100 26 L 0 26 Z`;
}

const TODAY_SAMPLES = 180;

type WavePeak = { value: number; xPercent: number; yPercent: number };

// wavePeak finds the highest sample so the dashboard can annotate the peak
// value directly above the curve. Null means "no traffic recorded yet".
function wavePeak(values: number[], maxValue: number): WavePeak | null {
  if (values.length === 0) return null;
  let value = 0;
  let index = -1;
  values.forEach((sample, i) => {
    if (sample > value) {
      value = sample;
      index = i;
    }
  });
  if (index < 0 || value <= 0) return null;
  const count = values.length;
  const xPercent = count === 1 ? 50 : (index / (count - 1)) * 100;
  const ratio = Math.min(1, value / Math.max(1, maxValue));
  return { value, xPercent, yPercent: Math.max(2, Math.min(92, (1 - ratio) * 100)) };
}

// troughRate returns the smallest positive sample, ignoring idle gaps.
function troughRate(values: number[]) {
  let trough = 0;
  for (const value of values) {
    if (value > 0 && (trough === 0 || value < trough)) trough = value;
  }
  return trough;
}

// waveBars builds one down/up bar pair per sample for the "bars" style.
function waveBars(down: number[], up: number[], maxValue: number) {
  const count = Math.max(down.length, up.length);
  const safeMax = Math.max(1, maxValue);
  const slot = 100 / Math.max(1, count);
  const bars = [];
  for (let i = 0; i < count; i += 1) {
    const downHeight = (Math.min(1, Math.max(0, (down[i] ?? 0) / safeMax))) * 24;
    const upHeight = (Math.min(1, Math.max(0, (up[i] ?? 0) / safeMax))) * 24;
    bars.push({
      x: i * slot + slot * 0.12,
      width: slot * 0.46,
      downHeight,
      upX: i * slot + slot * 0.62,
      upWidth: slot * 0.26,
      upHeight,
    });
  }
  return bars;
}

function formatCompactRate(value: number) {
  const rate = Number.isFinite(value) && value > 0 ? value : 0;
  if (rate >= 1024 ** 2) return `${(rate / 1024 ** 2).toFixed(1)}M/s`;
  if (rate >= 1024) return `${(rate / 1024).toFixed(0)}K/s`;
  return `${Math.round(rate)}B/s`;
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
  const [waveDown, setWaveDown] = createSignal<number[]>([]);
  const [waveUp, setWaveUp] = createSignal<number[]>([]);
  const [todayDown, setTodayDown] = createSignal<number[]>([]);
  const [todayUp, setTodayUp] = createSignal<number[]>([]);

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
  // A running proxy always has a node, even if a summary reload failed: fall
  // back to the runtime proxy state so the hero never claims "no node" while
  // traffic is flowing.
  const heroHasNode = () => isRunning() || Boolean(activeNode());
  const heroNodeName = () => (isRunning() ? props.proxyState.nodeName : activeNode()?.name ?? t("dash.noNode"));

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


  // Live traffic waveform: keep a short rolling window of the rates reported by
  // the proxy so the dashboard hero can render a "breathing" sparkline while the
  // connection is up. The window resets as soon as the proxy stops.
  createEffect(() => {
    const running = isRunning();
    const stats = traffic();
    if (!running) {
      setWaveDown([]);
      setWaveUp([]);
      return;
    }
    if (!stats) return;
    const down = Math.max(0, stats.downlinkRate ?? 0);
    const up = Math.max(0, stats.uplinkRate ?? 0);
    setWaveDown((prev) => [...prev, down].slice(-WAVE_SAMPLES));
    setWaveUp((prev) => [...prev, up].slice(-WAVE_SAMPLES));
    // The "today" curve in the traffic panel keeps a longer window (about three
    // minutes at the one-second sampling cadence) and is not cleared on stop, so
    // the recent shape stays visible while the proxy is off.
    setTodayDown((prev) => [...prev, down].slice(-TODAY_SAMPLES));
    setTodayUp((prev) => [...prev, up].slice(-TODAY_SAMPLES));
  });

  const waveMax = createMemo(() => Math.max(1024, ...waveDown(), ...waveUp()));
  const todayMax = createMemo(() => Math.max(1024, ...todayDown(), ...todayUp()));
  const waveStyle = () => {
    const style = props.settings?.liveWaveStyle;
    return style === "bars" || style === "minimal" ? style : "wave";
  };
  const peakDown = createMemo(() => wavePeak(waveDown(), waveMax()));
  const peakUp = createMemo(() => wavePeak(waveUp(), waveMax()));
  const troughDown = createMemo(() => troughRate(waveDown()));
  const waveBarsData = createMemo(() => waveBars(waveDown(), waveUp(), waveMax()));
  const todayPeakDown = createMemo(() => wavePeak(todayDown(), todayMax()));

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
  const todayTotalBytes = createMemo(() => trafficWindow().find((day) => day.today)?.totalBytes ?? 0);

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

  // Plan quota card: nearest subscription expiry plus a small urgency level so
  // the card can warn before the subscription actually lapses.
  const planExpireAt = createMemo(() => earliestExpireAt(summary()?.recentSubscriptions));
  const planExpiry = createMemo(() => planExpiryText(planExpireAt(), clock()));
  const planExpiryTone = createMemo(() => planExpiryLevel(planExpireAt(), clock()));

  return (
    <div class="page-stack dashboard-page">
      <section classList={{ "connection-hero": true, "dash-hero": true, running: isRunning(), error: props.proxyState.status === "error" }}>
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
            <h2>{heroNodeName()}</h2>
            <Show when={heroHasNode()}>
              <p>
                {isRunning()
                  ? `${props.proxyState.nodeProtocol.toUpperCase()} · ${props.proxyState.nodeAddress}:${props.proxyState.nodePort}`
                  : `${activeNode()!.protocol.toUpperCase()} · ${activeNode()!.address}:${activeNode()!.port}`}
              </p>
            </Show>
            <div class="connection-meta">
              <span><Icon name="route" size={12} /> {isRunning() ? props.proxyState.localAddr : t("dash.localInboundNotListening")}</span>
              <Show when={isRunning()}>
                <span><Icon name="speed" size={12} /> {t("dash.runningFor", { duration: formatUptime(props.proxyState.startedAt, clock()) })}</span>
              </Show>
              <span
                class="dash-hero-traffic"
                title={t("dash.heroTrafficTitle", {
                  down: downlinkRateLabel(),
                  up: uplinkRateLabel(),
                  downTotal: formatBytes(traffic()?.totalDownlinkBytes ?? 0),
                  upTotal: formatBytes(traffic()?.totalUplinkBytes ?? 0),
                })}
              >
                <span class="dash-hero-rate">{sessionRateLabel()}</span>
                <span class="dash-hero-session">{t("dash.sessionTotal", { bytes: sessionTotalLabel() })}</span>
                <Show when={isRunning() && totalSessionTraffic() > 0}>
                  <span class="dash-hero-share" aria-hidden="true">
                    <span class="dash-hero-share-down" style={{ width: `${downlinkShare()}%` }} />
                    <span class="dash-hero-share-up" style={{ width: `${100 - downlinkShare()}%` }} />
                  </span>
                </Show>
              </span>
              <Show
                when={summary()?.coreVersion}
                fallback={
                  <span><Icon name="server" size={12} /> {props.settings?.coreType === "singbox" ? "sing-box" : "Xray"} {t("dash.notInstalled")}</span>
                }
              >
                <span><Icon name="server" size={12} /> {props.settings?.coreType === "singbox" ? "sing-box" : "Xray"} {summary()?.coreVersion}</span>
              </Show>
            </div>
          </div>
        </div>

        <div class="connection-actions dash-hero-actions">
          <Show when={heroHasNode()} fallback={
            <button class="btn primary connection-toggle dash-hero-cta" onClick={() => props.onNavigate("servers")}>
              <Icon name="server" size={16} /> {t("dash.selectNode")}
            </button>
          }>
            <Show when={isRunning()} fallback={
              <button class="btn primary connection-toggle" onClick={props.onStartProxy} disabled={props.proxyBusy || !canStart()}>
                <Icon name="play" size={16} /> {props.proxyBusy ? t("dash.starting") : t("dash.startProxy")}
              </button>
            }>
              <button class="btn primary danger-action connection-toggle" onClick={props.onStopProxy} disabled={props.proxyBusy}>
                <Icon name="stop" size={16} /> {props.proxyBusy ? t("dash.stopping") : t("dash.stopProxy")}
              </button>
            </Show>
            <button class="btn glass" onClick={props.onTestProxy} disabled={props.proxyBusy || !isRunning()}>
              <Icon name="speed" size={15} /> {t("dash.test")}
            </button>
            <button class="btn ghost" onClick={() => props.onNavigate("servers")}>
              <Icon name="server" size={15} /> {t("dash.switchNode")}
            </button>
          </Show>
        </div>

        <div classList={{ "dash-live-wave": true, active: isRunning(), minimal: waveStyle() === "minimal" }}>
          <div class="dash-live-wave-head">
            <span class="dash-live-dot" />
            <span>{isRunning() ? t("dash.liveTraffic") : t("dash.liveTrafficIdle")}</span>
            <div class="dash-wave-style-switch" role="group" aria-label={t("dash.waveStyleAria")}>
              <For each={[
                { value: "wave", label: t("dash.waveStyleWave") },
                { value: "bars", label: t("dash.waveStyleBars") },
                { value: "minimal", label: t("dash.waveStyleMinimal") },
              ]}>
                {(item) => (
                  <button
                    type="button"
                    classList={{ "dash-wave-style-btn": true, active: waveStyle() === item.value }}
                    aria-pressed={waveStyle() === item.value}
                    onClick={() => props.onLiveWaveStyleChange(item.value)}
                  >
                    {item.label}
                  </button>
                )}
              </For>
            </div>
            <span class="dash-live-wave-rates">{isRunning() ? `↓ ${downlinkRateLabel()} · ↑ ${uplinkRateLabel()}` : "—"}</span>
          </div>
          <div class="dash-live-wave-body">
            <Show when={waveStyle() === "bars"} fallback={
              <svg classList={{ "dash-live-wave-chart": true, minimal: waveStyle() === "minimal" }} viewBox="0 0 100 26" preserveAspectRatio="none">
                <Show when={waveStyle() === "wave"}>
                  <defs>
                    <linearGradient id="dashLiveWaveArea" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stop-color="var(--accent)" stop-opacity="0.26" />
                      <stop offset="100%" stop-color="var(--accent)" stop-opacity="0" />
                    </linearGradient>
                  </defs>
                  <path class="dash-wave-area" d={waveAreaPath(waveDown(), waveMax())} />
                </Show>
                <path class="dash-wave-line down" d={wavePath(waveDown(), waveMax())} />
                <path class="dash-wave-line up" d={wavePath(waveUp(), waveMax())} />
              </svg>
            }>
              <svg class="dash-live-wave-chart bars" viewBox="0 0 100 26" preserveAspectRatio="none">
                <For each={waveBarsData()}>
                  {(bar) => (
                    <>
                      <rect class="dash-wave-bar down" x={bar.x} y={25 - bar.downHeight} width={bar.width} height={bar.downHeight} rx="0.4" />
                      <rect class="dash-wave-bar up" x={bar.upX} y={25 - bar.upHeight} width={bar.upWidth} height={bar.upHeight} rx="0.3" />
                    </>
                  )}
                </For>
              </svg>
            </Show>
            <Show when={isRunning() && waveStyle() !== "minimal" && peakDown()}>
              <span class="dash-wave-peak-chip down" style={{ left: `${peakDown()!.xPercent}%`, top: `${peakDown()!.yPercent}%` }}>
                ↓ {formatCompactRate(peakDown()!.value)}
              </span>
            </Show>
            <Show when={isRunning() && waveStyle() !== "minimal" && peakUp() && peakUp()!.value >= Math.max(1024, waveMax() * 0.08)}>
              <span class="dash-wave-peak-chip up" style={{ left: `${peakUp()!.xPercent}%`, top: `${peakUp()!.yPercent}%` }}>
                ↑ {formatCompactRate(peakUp()!.value)}
              </span>
            </Show>
          </div>
          <div class="dash-wave-stats">
            <Show when={peakDown()} fallback={<span class="dash-wave-stat">{t("dash.liveWaveWaiting")}</span>}>
              <span class="dash-wave-stat down">{t("dash.peakDown", { rate: formatCompactRate(peakDown()!.value) })}</span>
            </Show>
            <Show when={peakUp() && peakUp()!.value > 0}>
              <span class="dash-wave-stat up">{t("dash.peakUp", { rate: formatCompactRate(peakUp()!.value) })}</span>
            </Show>
            <Show when={troughDown() > 0}>
              <span class="dash-wave-stat trough">{t("dash.troughDown", { rate: formatCompactRate(troughDown()) })}</span>
            </Show>
          </div>
        </div>

        <Show when={props.proxyError}>
          <div class="proxy-inline-alert error connection-alert"><Icon name="alert" size={13} /> {props.proxyError}</div>
        </Show>
        <Show when={props.proxyTest}>
          <div classList={{ "proxy-inline-alert": true, "connection-alert": true, ok: props.proxyTest?.success, error: !props.proxyTest?.success }}>
            <Icon name={props.proxyTest?.success ? "check" : "alert"} size={13} />
            {props.proxyTest?.success
              ? t("dash.proxyTestOk", { latency: props.proxyTest?.latencyMs ?? 0, status: props.proxyTest?.statusCode ?? 0 })
              : props.proxyTest?.error
                ? t("dash.proxyTestFailedDetail", { error: props.proxyTest.error })
                : t("dash.proxyTestFailed")}
          </div>
        </Show>
      </section>

      <section class="dashboard-stat-grid">
        <div class="glass-panel dashboard-stat">
          <span class="dashboard-stat-icon cyan"><Icon name="subscription" size={17} /></span>
          <div><span>{t("dash.statSubscriptions")}</span><strong>{loading() ? "—" : summary()?.subscriptionCount ?? 0}</strong><small>{t("dash.statEnabledSubscriptions", { count: summary()?.activeSubscriptions ?? 0 })}</small></div>
        </div>
        <div class="glass-panel dashboard-stat">
          <span class="dashboard-stat-icon indigo"><Icon name="server" size={17} /></span>
          <div><span>{t("dash.statNodes")}</span><strong>{loading() ? "—" : summary()?.nodeCount ?? 0}</strong><small>{t("dash.statParsedNodes")}</small></div>
        </div>
        <div class="glass-panel dashboard-stat traffic dash-stat-plan">
          <span class="dashboard-stat-icon green"><Icon name="speed" size={17} /></span>
          <div>
            <span>{t("dash.statPlanTraffic")}</span>
            <strong>{formatBytes(usedBytes())}</strong>
            <small>{totalBytes() > 0
              ? t("dash.statPlanQuota", { percent: quotaPercent(), total: formatBytes(totalBytes()) })
              : planExpiry() ? t("dash.usageNotProvided") : t("dash.usageFromSubMissing")}</small>
            <Show when={totalBytes() > 0}><div class="dashboard-progress"><span style={{ width: `${quotaPercent()}%` }} /></div></Show>
            <Show
              when={planExpiry()}
              fallback={
                <Show when={totalBytes() > 0}>
                  <small class="dash-plan-expiry">{t("dash.expiryNotProvided")}</small>
                </Show>
              }
            >
              <small
                classList={{
                  "dash-plan-expiry": true,
                  warning: planExpiryTone() === "warning",
                  expired: planExpiryTone() === "expired",
                }}
              >
                <Icon name={planExpiryTone() === "expired" ? "alert" : "subscription"} size={11} />
                {planExpiry()}
              </small>
            </Show>
          </div>
        </div>
        <div class="glass-panel dashboard-stat">
          <span class="dashboard-stat-icon amber"><Icon name="server" size={17} /></span>
          <div><span>{t("dash.statCore")}</span><strong>{coreStatusLabel(summary()?.coreStatus ?? "")}</strong><small>{summary()?.coreVersion || t("dash.notInstalled")}</small></div>
        </div>
      </section>

      <Show when={error()}>
        <div class="alert error"><span class="alert-message"><Icon name="alert" size={15} /> {error()}</span><button class="btn ghost" onClick={load}>{t("dash.retry")}</button></div>
      </Show>

      <div class="dashboard-main-grid">
        <div class="dashboard-left-column">
        <section class="glass-panel node-overview">
          <div class="panel-heading">
            <div>
              <h3>{t("dash.currentNode")}</h3>
              <span>{t("dash.nodeOverviewSubtitle")}</span>
            </div>
            <button class="btn secondary" onClick={() => props.onNavigate("servers")}>{t("dash.switchNode")}</button>
          </div>
          <Show when={activeNode()} fallback={
            <div class="empty-state dashboard-node-empty dash-empty">
              <div class="empty-icon"><Icon name="server" size={22} /></div>
              <strong>{t("dash.emptyNodeTitle")}</strong>
              <span>{t("dash.emptyNodeBody")}</span>
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
              <div><span>{t("dash.detailSubscription")}</span><strong>{isRunning() ? props.proxyState.nodeSubscription || t("dash.manualNode") : activeNode()!.subscriptionName || t("dash.manualNode")}</strong></div>
              <div><span>{t("dash.detailLocalInbound")}</span><strong>{isRunning() ? props.proxyState.localAddr : t("dash.notListening")}</strong></div>
              <div><span>{t("dash.detailProxyMode")}</span><strong>{proxyModeLabel(isRunning() ? props.proxyState.proxyMode : props.settings?.proxyMode ?? "rule")}</strong></div>
              <div><span>{t("dash.detailPid")}</span><strong>{isRunning() ? props.proxyState.pid : "—"}</strong></div>
              <div><span>{t("dash.detailResolvedIp")}</span><strong>{isRunning() ? props.proxyState.resolvedIp || t("dash.resolveFailed") : t("dash.resolveOnStart")}</strong></div>
              <div><span>{t("dash.detailUptime")}</span><strong>{isRunning() ? formatUptime(props.proxyState.startedAt, clock()) : "—"}</strong></div>
            </div>
          </Show>
        </section>

      <section class="glass-panel traffic-history-panel">
        <div class="panel-heading">
          <div>
            <h3>{t("dash.trafficHistoryTitle")}</h3>
            <span>{t("dash.trafficHistorySubtitle")}</span>
          </div>
          <div class="traffic-history-meta">
            <Show when={trafficHasData()}>
              <span class="traffic-history-total">
                {t("dash.trafficHistoryTotalLabel")} <strong>{formatBytes(trafficWindowTotal())}</strong>
              </span>
            </Show>
            <div class="traffic-legend">
              <span class="traffic-legend-item"><i class="traffic-legend-dot downlink" /> {t("dash.legendDownlink")}</span>
              <span class="traffic-legend-item"><i class="traffic-legend-dot uplink" /> {t("dash.legendUplink")}</span>
            </div>
          </div>
        </div>

        <div classList={{ "traffic-live": true, running: isRunning() }}>
          <div class="traffic-live-head">
            <div>
              <strong>{t("dash.todayLiveTitle")}</strong>
              <span>{t("dash.todayLiveHint")}</span>
            </div>
            <div class="traffic-live-meta">
              <span class="traffic-live-total">{t("dash.todayTotal", { bytes: formatBytes(todayTotalBytes()) })}</span>
              <Show when={todayPeakDown()}>
                <span class="traffic-live-peak">{t("dash.peakDown", { rate: formatCompactRate(todayPeakDown()!.value) })}</span>
              </Show>
            </div>
          </div>
          <div class="traffic-live-body">
            <svg class="traffic-live-chart" viewBox="0 0 100 26" preserveAspectRatio="none">
              <defs>
                <linearGradient id="trafficLiveArea" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stop-color="var(--accent)" stop-opacity="0.22" />
                  <stop offset="100%" stop-color="var(--accent)" stop-opacity="0" />
                </linearGradient>
              </defs>
              <path class="traffic-live-area" d={waveAreaPath(todayDown(), todayMax())} />
              <path class="traffic-live-line down" d={wavePath(todayDown(), todayMax())} />
              <path class="traffic-live-line up" d={wavePath(todayUp(), todayMax())} />
            </svg>
            <Show when={todayPeakDown()}>
              <span class="traffic-live-peak-chip" style={{ left: `${todayPeakDown()!.xPercent}%`, top: `${todayPeakDown()!.yPercent}%` }}>
                ↓ {formatCompactRate(todayPeakDown()!.value)}
              </span>
            </Show>
          </div>
        </div>

        <Show when={historyError()}>
          <div class="alert error traffic-history-alert">
            <span class="alert-message"><Icon name="alert" size={15} /> {t("dash.trafficHistoryLoadFailed", { error: historyError() })}</span>
            <button class="btn ghost" onClick={() => { void loadTrafficHistory(); }}>{t("dash.retry")}</button>
          </div>
        </Show>

        <Show
          when={historyLoading()}
          fallback={
            <Show
              when={trafficHasData()}
              fallback={
                <div class="empty-state dash-empty">
                  <div class="empty-icon"><Icon name="speed" size={22} /></div>
                  <strong>{t("dash.emptyTrafficTitle")}</strong>
                  <span>{t("dash.emptyTrafficBody")}</span>
                </div>
              }
            >
              <div class="traffic-chart" role="img" aria-label={t("dash.trafficChartAria")}>
                <For each={trafficWindow()}>
                  {(day) => (
                    <div classList={{ "traffic-day": true, today: day.today }}>
                      <span class="traffic-day-total">{formatBytes(day.totalBytes)}</span>
                      <div
                        class="traffic-day-plot"
                        title={t("dash.trafficDayTitle", { date: day.date, down: formatBytes(day.downlinkBytes), up: formatBytes(day.uplinkBytes) })}
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
            <strong>{t("dash.trafficHistoryLoading")}</strong>
          </div>
        </Show>
      </section>

        </div>

        <aside class="dashboard-rail">
          <section class="glass-panel side-card">
            <div class="panel-heading"><div><h3>{t("dash.quickActions")}</h3><span>{t("dash.quickActionsSubtitle")}</span></div></div>
            <div class="quick-actions">
              <button class="quick-action" onClick={() => props.onNavigate("subscriptions")}><span class="quick-icon"><Icon name="plus" size={16} /></span><span class="quick-copy"><strong>{t("dash.quickAddSubscription")}</strong><small>{t("dash.quickAddSubscriptionDesc")}</small></span><Icon name="chevron" size={14} class="quick-chevron" /></button>
              <button class="quick-action" onClick={() => props.onNavigate("routing")}><span class="quick-icon"><Icon name="route" size={16} /></span><span class="quick-copy"><strong>{t("dash.quickRouting")}</strong><small>{t("dash.quickRoutingDesc")}</small></span><Icon name="chevron" size={14} class="quick-chevron" /></button>
              <button class="quick-action" onClick={() => props.onNavigate("logs")}><span class="quick-icon"><Icon name="logs" size={16} /></span><span class="quick-copy"><strong>{t("dash.quickLogs")}</strong><small>{t("dash.quickLogsDesc")}</small></span><Icon name="chevron" size={14} class="quick-chevron" /></button>
              <button class="quick-action" onClick={() => props.onNavigate("settings")}><span class="quick-icon"><Icon name="settings" size={16} /></span><span class="quick-copy"><strong>{t("dash.quickSettings")}</strong><small>{t("dash.quickSettingsDesc")}</small></span><Icon name="chevron" size={14} class="quick-chevron" /></button>
            </div>
          </section>

          <section class="glass-panel side-card">
            <div class="panel-heading"><div><h3>{t("dash.systemStatus")}</h3><span>{t("dash.systemStatusSubtitle")}</span></div></div>
            <div class="system-status-list">
              <div><span>{t("dash.systemProxyStatus")}</span><strong>{isRunning() ? t("dash.systemRunning") : props.proxyState.status === "error" ? t("dash.systemError") : t("dash.systemStopped")}</strong></div>
              <div><span>{t("dash.systemXrayApi")}</span><strong>{props.settings?.xrayApiEnabled ? `127.0.0.1:${props.settings.xrayApiPort}` : t("dash.systemDisabled")}</strong></div>
              <div><span>{t("dash.systemTunMode")}</span><strong>{props.settings?.tunMode ? t("dash.systemEnabled") : t("dash.systemDisabled")}</strong></div>
              <div><span>{t("dash.systemTestUrl")}</span><strong class="config-url">{props.settings?.testUrl ?? t("dash.systemNotSet")}</strong></div>
              <div><span>{t("dash.systemAppVersion")}</span><strong>v{props.info?.version ?? "0.1.0"}</strong></div>
              <div><span>{t("dash.systemPlatform")}</span><strong>{props.info ? `${props.info.platform} / ${props.info.arch}` : "..."}</strong></div>
            </div>
          </section>
        </aside>
      </div>

      <section class="glass-panel recent-panel dashboard-recent">
        <div class="panel-heading">
          <div><h3>{t("dash.recentSubscriptions")}</h3><span>{summary()?.lastUpdatedAt ? t("dash.lastUpdated", { time: formatTime(summary()!.lastUpdatedAt) }) : t("dash.noUpdateRecord")}</span></div>
          <button class="btn ghost" onClick={() => props.onNavigate("subscriptions")}>{t("dash.viewAll")}</button>
        </div>
        <Show when={(summary()?.recentSubscriptions ?? []).length > 0} fallback={
          <div class="empty-state dash-empty"><div class="empty-icon"><Icon name="subscription" size={22} /></div><strong>{t("dash.emptySubscriptionsTitle")}</strong><span>{t("dash.emptySubscriptionsBody")}</span></div>
        }>
          <div class="recent-list">
            <For each={summary()?.recentSubscriptions ?? []}>
              {(sub) => (
                <div class="recent-row dashboard-recent-row">
                  <div class="recent-avatar"><Icon name="subscription" size={16} /></div>
                  <div class="recent-copy"><strong>{sub.name}</strong><small>{sub.host}</small></div>
                  <span class="recent-count">{t("dash.nodeCount", { count: sub.nodeCount })}</span>
                  <Show when={(sub.totalBytes ?? 0) > 0} fallback={<span class="recent-usage-empty">{t("dash.usageNotProvidedShort")}</span>}>
                    <div class="recent-usage">
                      <span>{formatBytes((sub.uploadBytes ?? 0) + (sub.downloadBytes ?? 0))} / {formatBytes(sub.totalBytes ?? 0)}</span>
                      <div class="recent-usage-track"><span style={{ width: `${nodeUsagePercent(sub)}%` }} /></div>
                    </div>
                  </Show>
                  <span classList={{ "status-label": true, error: Boolean(sub.lastError), ok: !sub.lastError }}><span class="status-dot" />{sub.lastError ? t("dash.statusAbnormal") : t("dash.statusNormal")}</span>
                </div>
              )}
            </For>
          </div>
        </Show>
      </section>
    </div>
  );
}