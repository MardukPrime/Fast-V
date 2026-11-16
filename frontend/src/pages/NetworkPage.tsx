import { createMemo, createSignal, For, onMount, Show } from "solid-js";


import { DiagnosticsService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/diagnostics";
import { Service as HistoryService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/history";
import { Service as SelfCheckService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/selfcheck";
import type { CheckResult, LatencySample, NetworkProbe, ProxyState } from "../../bindings/github.com/MardukPrime/Fast-V/internal/model";
import Icon from "../components/Icon";

type NetworkPageProps = {
  proxyState: ProxyState;
};


function flagEmoji(countryCode: string) {
  const code = (countryCode || "").toUpperCase();
  if (code.length !== 2) return "🌐";
  return String.fromCodePoint(...code.split("").map((char) => 127397 + char.charCodeAt(0)));
}

function modeLabel(mode: string) {
  return mode === "proxy" ? "代理出口" : "直连出口";
}

const LATENCY_HISTORY_LIMIT = 20;

function pad2(value: number) {
  return String(value).padStart(2, "0");
}

function formatSampleTime(value: string) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return `${pad2(date.getMonth() + 1)}-${pad2(date.getDate())} ${pad2(date.getHours())}:${pad2(date.getMinutes())}:${pad2(date.getSeconds())}`;
}

// Reuse the latency colour scale of the servers list: <100ms good, <300ms mid,
// anything slower (or failed) bad. A zero/NaN measurement is shown as unknown.
function latencyTone(latencyMs: number) {
  if (!Number.isFinite(latencyMs) || latencyMs <= 0) return "unknown";
  if (latencyMs < 100) return "good";
  if (latencyMs < 300) return "mid";
  return "bad";
}

function checkTone(status: string) {
  if (status === "ok") return "ok";
  if (status === "warn") return "warn";
  return "error";
}

function checkStatusLabel(status: string) {
  if (status === "ok") return "通过";
  if (status === "warn") return "警告";
  return "失败";
}

function ProbeCard(props: {
  mode: "direct" | "proxy";
  probe: NetworkProbe | null;
  loading: boolean;
  available: boolean;
  onRefresh: () => void;
}) {
  const data = () => props.probe;
  return (
    <section classList={{ "glass-panel network-card": true, unavailable: !props.available }}>
      <header class="network-card-head">
        <div>
          <span classList={{ "network-mode-tag": true, proxy: props.mode === "proxy" }}>
            {props.mode === "proxy" ? "PROXY" : "DIRECT"}
          </span>
          <h3>{modeLabel(props.mode)}</h3>
        </div>
        <button class="icon-button" onClick={props.onRefresh} disabled={props.loading || !props.available} aria-label={`刷新${modeLabel(props.mode)}`}>
          <Icon name="refresh" size={14} />
        </button>
      </header>

      <Show when={props.loading} fallback={
        <Show when={data()} fallback={
          <div class="network-empty">
            <span class="network-empty-icon"><Icon name={props.mode === "proxy" ? "route" : "speed"} size={20} /></span>
            <strong>{props.available ? "尚未检测" : "本地代理未运行"}</strong>
            <span>{props.available ? "点击右上角刷新，或运行一键检测。" : "启动本地代理后才能检测代理出口。"}</span>
          </div>
        }>
          <div class="network-result">
            <div class="network-ip-row">
              <span class="network-flag">{flagEmoji(data()!.countryCode)}</span>
              <div>
                <strong>{data()!.ip || "未知 IP"}</strong>
                <span>{data()!.ipVersion || "IP"} · {[data()!.country, data()!.region, data()!.city].filter(Boolean).join(" / ") || "归属地未知"}</span>
              </div>
            </div>

            <div class="network-facts">
              <div><span>ASN</span><strong>{data()!.asn ? `AS${data()!.asn}` : "—"}</strong></div>
              <div><span>运营商</span><strong>{data()!.isp || data()!.organization || "—"}</strong></div>
              <div><span>Cloudflare 节点</span><strong>{data()!.colo || "—"}</strong></div>
              <div><span>TLS</span><strong>{data()!.tls || "—"}</strong></div>
              <div><span>WARP</span><strong>{data()!.warp || "—"}</strong></div>
              <div><span>延迟</span><strong>{data()!.latencyMs ? `${data()!.latencyMs} ms` : "—"}</strong></div>
              <div><span>抖动</span><strong>{data()!.jitterMs ? `${data()!.jitterMs} ms` : "—"}</strong></div>
              <div><span>下载速度</span><strong>{data()!.downloadMbps ? `${data()!.downloadMbps.toFixed(1)} Mbps` : "未测"}</strong></div>
            </div>

            <Show when={data()!.geoError}>
              <div class="network-note warning"><Icon name="alert" size={12} /> 归属地信息获取失败：{data()!.geoError}</div>
            </Show>
            <Show when={data()!.downloadError}>
              <div class="network-note warning"><Icon name="alert" size={12} /> 下载测速失败：{data()!.downloadError}</div>
            </Show>
          </div>
        </Show>
      }>
        <div class="network-loading">
          <span class="network-pulse" />
          <strong>正在检测{modeLabel(props.mode)}</strong>
          <span>正在读取 IP、归属地和网络质量...</span>
        </div>
      </Show>
    </section>
  );
}

export default function NetworkPage(props: NetworkPageProps) {
  const [direct, setDirect] = createSignal<NetworkProbe | null>(null);
  const [proxied, setProxied] = createSignal<NetworkProbe | null>(null);
  const [directLoading, setDirectLoading] = createSignal(false);
  const [proxyLoading, setProxyLoading] = createSignal(false);
  const [includeSpeed, setIncludeSpeed] = createSignal(false);
  const [error, setError] = createSignal("");

  const [latencies, setLatencies] = createSignal<LatencySample[]>([]);
  const [latencyLoading, setLatencyLoading] = createSignal(true);
  const [latencyError, setLatencyError] = createSignal("");
  const [clearingHistory, setClearingHistory] = createSignal(false);
  const [selfCheckResults, setSelfCheckResults] = createSignal<CheckResult[] | null>(null);
  const [selfCheckBusy, setSelfCheckBusy] = createSignal(false);
  const [selfCheckError, setSelfCheckError] = createSignal("");
  const [selfCheckAt, setSelfCheckAt] = createSignal("");

  const loadLatencies = async () => {
    setLatencyLoading(true);
    setLatencyError("");
    try {
      const samples = await HistoryService.Latencies(LATENCY_HISTORY_LIMIT);
      // The backend returns samples oldest-first; the newest measurement is the
      // most interesting one, so the list is shown newest-first.
      setLatencies((samples ?? []).slice().reverse());
    } catch (err) {
      setLatencyError(err instanceof Error ? err.message : String(err));
    } finally {
      setLatencyLoading(false);
    }
  };

  const clearHistory = async () => {
    if (clearingHistory()) return;
    const confirmed = window.confirm(
      "确定要清除全部流量与延迟历史记录吗？\n\n该操作不可撤销，订阅、节点和设置不受影响。"
    );
    if (!confirmed) return;
    setClearingHistory(true);
    setLatencyError("");
    try {
      await HistoryService.Clear();
      await loadLatencies();
    } catch (err) {
      setLatencyError(err instanceof Error ? err.message : String(err));
    } finally {
      setClearingHistory(false);
    }
  };

  // The self-check can take several seconds (core validation, network probe), so
  // it only runs on demand; the last in-memory result is kept while the page is
  // mounted instead of re-running on every visit.
  const runSelfCheck = async () => {
    if (selfCheckBusy()) return;
    setSelfCheckBusy(true);
    setSelfCheckError("");
    try {
      const results = await SelfCheckService.Run();
      setSelfCheckResults(results ?? []);
      setSelfCheckAt(new Date().toLocaleTimeString());
    } catch (err) {
      setSelfCheckError(err instanceof Error ? err.message : String(err));
    } finally {
      setSelfCheckBusy(false);
    }
  };

  const selfCheckSummary = createMemo(() => {
    let ok = 0;
    let warn = 0;
    let error = 0;
    for (const result of selfCheckResults() ?? []) {
      const tone = checkTone(result.status);
      if (tone === "ok") ok += 1;
      else if (tone === "warn") warn += 1;
      else error += 1;
    }
    return { total: ok + warn + error, ok, warn, error };
  });

  const selfCheckVerdict = () => {
    const { total, warn, error } = selfCheckSummary();
    if (total === 0) return "自检没有返回结果";
    if (error > 0) return `发现 ${error} 项失败，按提示修复后可重新自检`;
    if (warn > 0) return `检查完成，${warn} 项需要关注`;
    return "全部检查通过";
  };

  const proxyAvailable = () => props.proxyState.status === "running";

  const checkMode = async (mode: "direct" | "proxy") => {
    if (mode === "proxy" && !proxyAvailable()) {
      setError("本地代理未运行，请先在概览页启动代理。");
      return;
    }
    setError("");
    if (mode === "direct") setDirectLoading(true); else setProxyLoading(true);
    try {
      const result = await DiagnosticsService.Check(mode, includeSpeed());
      if (mode === "direct") setDirect(result); else setProxied(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      if (mode === "direct") setDirectLoading(false); else setProxyLoading(false);
    }
  };

  const runAll = async () => {
    await checkMode("direct");
    if (proxyAvailable()) await checkMode("proxy");
  };

  onMount(() => {
    void checkMode("direct");
  });

  onMount(() => {
    void loadLatencies();
  });

  const sameExit = () => Boolean(direct()?.ip && proxied()?.ip && direct()!.ip === proxied()!.ip);
  const proxyCountry = () => proxied();

  return (
    <div class="page-stack">
      <section class="glass-panel network-hero">
        <div>
          <span class="network-eyebrow"><Icon name="speed" size={13} /> IP & NETWORK QUALITY</span>
          <h2>网络诊断</h2>
          <p>检测直连出口和本地代理出口的公网 IP、归属地、ASN、Cloudflare 节点、延迟与下载速度。</p>
        </div>
        <div class="network-actions">
          <label class="network-speed-toggle">
            <span classList={{ "switch": true, on: includeSpeed() }}>
              <input type="checkbox" checked={includeSpeed()} onChange={(event) => setIncludeSpeed(event.currentTarget.checked)} />
              <span class="switch-track"><span class="switch-thumb" /></span>
            </span>
            包含下载测速
          </label>
          <button class="btn primary" onClick={runAll} disabled={directLoading() || proxyLoading()}>
            <Icon name="refresh" size={14} /> {directLoading() || proxyLoading() ? "检测中..." : "一键检测"}
          </button>
        </div>
      </section>

      <Show when={error()}>
        <div class="alert error">
          <span class="alert-message"><Icon name="alert" size={15} /> {error()}</span>
          <button class="btn ghost" onClick={() => setError("")}>关闭</button>
        </div>
      </Show>

      <div class="network-grid">
        <ProbeCard mode="direct" probe={direct()} loading={directLoading()} available onRefresh={() => checkMode("direct")} />
        <ProbeCard mode="proxy" probe={proxied()} loading={proxyLoading()} available={proxyAvailable()} onRefresh={() => checkMode("proxy")} />
      </div>

      <Show when={direct() && proxied()}>
        <section class="glass-panel network-compare">
          <div class="compare-exit">
            <span>直连出口</span>
            <strong>{direct()!.ip}</strong>
            <small>{flagEmoji(direct()!.countryCode)} {direct()!.country || direct()!.countryCode || "未知"}</small>
          </div>
          <span class="compare-arrow">→</span>
          <div class="compare-exit">
            <span>代理出口</span>
            <strong>{proxied()!.ip}</strong>
            <small>{flagEmoji(proxied()!.countryCode)} {proxied()!.country || proxied()!.countryCode || "未知"}</small>
          </div>
          <span classList={{ "compare-badge": true, changed: !sameExit() }}>
            <span class="status-dot" /> {sameExit() ? "出口 IP 相同" : "出口 IP 已切换"}
          </span>
          <Show when={proxyCountry()?.colo}>
            <span class="compare-colo">Cloudflare {proxyCountry()!.colo}</span>
          </Show>
        </section>
      </Show>

      <section class="glass-panel network-history-panel">
        <div class="panel-heading">
          <div>
            <h3>延迟历史</h3>
            <span>
              {latencies().length > 0
                ? `最近 ${latencies().length} 次 URL 测速结果，按时间倒序`
                : "URL 测速结果会记录在这里"}
            </span>
          </div>
          <div class="network-history-actions">
            <button
              class="icon-button"
              onClick={() => { void loadLatencies(); }}
              disabled={latencyLoading()}
              aria-label="刷新延迟历史"
              title="刷新延迟历史"
            >
              <Icon name="refresh" size={14} />
            </button>
            <button
              class="btn danger"
              onClick={() => { void clearHistory(); }}
              disabled={clearingHistory() || latencyLoading() || latencies().length === 0}
            >
              <Icon name="trash" size={13} /> {clearingHistory() ? "清除中..." : "清除历史"}
            </button>
          </div>
        </div>

        <Show when={latencyError()}>
          <div class="alert error network-history-alert">
            <span class="alert-message"><Icon name="alert" size={15} /> 延迟历史操作失败：{latencyError()}</span>
            <button class="btn ghost" onClick={() => { void loadLatencies(); }}>重试</button>
          </div>
        </Show>

        <Show
          when={latencyLoading() && latencies().length === 0}
          fallback={
            <Show
              when={latencies().length > 0}
              fallback={
                <div class="empty-state">
                  <div class="empty-icon"><Icon name="speed" size={22} /></div>
                  <strong>还没有延迟历史</strong>
                  <span>在服务器页面执行 URL 测速后，每个节点的结果都会记录在这里。</span>
                </div>
              }
            >
              <div class="network-history-list" role="list">
                <For each={latencies()}>
                  {(sample) => (
                    <div class="network-history-row" role="listitem">
                      <span class="network-history-time">{formatSampleTime(sample.checkedAt)}</span>
                      <span class="network-history-node">
                        <strong title={sample.nodeId || sample.nodeName}>{sample.nodeName || "未知节点"}</strong>
                        <Show when={sample.error}>
                          <small class="network-history-error" title={sample.error}>{sample.error}</small>
                        </Show>
                      </span>
                      <span class="network-history-value">
                        <Show
                          when={sample.error}
                          fallback={
                            <Show
                              when={sample.latencyMs > 0}
                              fallback={<span class="latency unknown">—</span>}
                            >
                              <span class={`latency ${latencyTone(sample.latencyMs)}`}>{sample.latencyMs} ms</span>
                            </Show>
                          }
                        >
                          <span class="latency bad">失败</span>
                        </Show>
                      </span>
                    </div>
                  )}
                </For>
              </div>
            </Show>
          }
        >
          <div class="network-history-loading">
            <span class="network-pulse" />
            <strong>正在读取延迟历史…</strong>
          </div>
        </Show>
      </section>

      <section class="glass-panel selfcheck-panel">
        <div class="panel-heading">
          <div>
            <h3>一键自检</h3>
            <span>检查内核、配置、端口、权限、网络、Geo 数据、数据目录与订阅状态</span>
          </div>
          <div class="selfcheck-actions">
            <Show when={selfCheckAt()}>
              <span class="selfcheck-timestamp">上次自检 {selfCheckAt()}</span>
            </Show>
            <button class="btn glass" onClick={() => { void runSelfCheck(); }} disabled={selfCheckBusy()}>
              <Icon name={selfCheckBusy() ? "refresh" : "check"} size={14} />
              {selfCheckBusy() ? "自检中..." : selfCheckResults() ? "重新自检" : "开始自检"}
            </button>
          </div>
        </div>

        <Show when={selfCheckError()}>
          <div class="alert error selfcheck-alert">
            <span class="alert-message"><Icon name="alert" size={15} /> 自检执行失败：{selfCheckError()}</span>
            <button class="btn ghost" onClick={() => { void runSelfCheck(); }}>重试</button>
          </div>
        </Show>

        <Show
          when={selfCheckBusy()}
          fallback={
            <Show
              when={selfCheckResults()}
              fallback={
                <div class="empty-state">
                  <div class="empty-icon"><Icon name="check" size={22} /></div>
                  <strong>尚未运行自检</strong>
                  <span>点击「开始自检」检查当前配置和运行环境；自检可能需要数秒。</span>
                </div>
              }
            >
              <div class="selfcheck-summary">
                <span class="selfcheck-chip ok"><strong>{selfCheckSummary().ok}</strong> 项通过</span>
                <span class="selfcheck-chip warn"><strong>{selfCheckSummary().warn}</strong> 项警告</span>
                <span class="selfcheck-chip error"><strong>{selfCheckSummary().error}</strong> 项失败</span>
                <span class="selfcheck-verdict">{selfCheckVerdict()}</span>
              </div>
              <div class="selfcheck-list" role="list">
                <For each={selfCheckResults() ?? []}>
                  {(result) => (
                    <div
                      classList={{
                        "selfcheck-item": true,
                        ok: checkTone(result.status) === "ok",
                        warn: checkTone(result.status) === "warn",
                        error: checkTone(result.status) === "error",
                      }}
                      role="listitem"
                    >
                      <span
                        classList={{
                          "selfcheck-badge": true,
                          ok: checkTone(result.status) === "ok",
                          warn: checkTone(result.status) === "warn",
                          error: checkTone(result.status) === "error",
                        }}
                      >
                        <Icon name={checkTone(result.status) === "ok" ? "check" : "alert"} size={12} />
                        {checkStatusLabel(result.status)}
                      </span>
                      <div class="selfcheck-body">
                        <strong class="selfcheck-name">{result.name || result.id}</strong>
                        <Show when={result.detail}>
                          <p class="selfcheck-detail">{result.detail}</p>
                        </Show>
                        <Show when={result.hint}>
                          <p class="selfcheck-hint"><Icon name="alert" size={11} /> {result.hint}</p>
                        </Show>
                      </div>
                    </div>
                  )}
                </For>
              </div>
            </Show>
          }
        >
          <div class="selfcheck-loading">
            <span class="network-pulse" />
            <strong>正在执行自检…</strong>
            <span>逐步检查内核、配置、端口、权限与网络，最多需要约 30 秒，请稍候。</span>
          </div>
        </Show>
      </section>

      <section class="panel network-tools-panel">
        <div class="panel-heading">
          <div>
            <h3>内置检测范围</h3>
            <span>只保留可以直接集成进 FastV 的检测能力，不跳转外部网站。</span>
          </div>
        </div>
        <div class="network-capability-grid">
          <div><span class="network-tool-icon"><Icon name="server" size={15} /></span><strong>出口 IP</strong><small>IPv4 / IPv6、直连和代理出口</small></div>
          <div><span class="network-tool-icon"><Icon name="subscription" size={15} /></span><strong>归属地</strong><small>国家、地区、城市、时区</small></div>
          <div><span class="network-tool-icon"><Icon name="nodes" size={15} /></span><strong>网络归属</strong><small>ASN、运营商、ISP</small></div>
          <div><span class="network-tool-icon"><Icon name="route" size={15} /></span><strong>Cloudflare 节点</strong><small>边缘节点、TLS、WARP</small></div>
          <div><span class="network-tool-icon"><Icon name="speed" size={15} /></span><strong>网络质量</strong><small>延迟、抖动、下载速度</small></div>
          <div><span class="network-tool-icon"><Icon name="check" size={15} /></span><strong>代理对比</strong><small>直连出口与代理出口切换结果</small></div>
        </div>
        <p class="network-disclaimer">
          WebRTC 泄漏、DNS 泄漏、IP 纯净度和匿名性评分需要浏览器环境或专用服务接口，当前不集成，也不提供外部网站跳转。
        </p>
      </section>
    </div>
  );
}
