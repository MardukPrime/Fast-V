import { createSignal, onMount, Show } from "solid-js";

import { Browser } from "@wailsio/runtime";

import { DiagnosticsService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/diagnostics";
import type { NetworkProbe, ProxyState } from "../../bindings/github.com/MardukPrime/Fast-V/internal/model";
import Icon from "../components/Icon";

type NetworkPageProps = {
  proxyState: ProxyState;
};

const externalTools = [
  { name: "IPCheck.ing", url: "https://ipcheck.ing/", description: "IPv4/IPv6、ASN、WebRTC、DNS 泄漏、网速" },
  { name: "ip.skk.moe", url: "https://ip.skk.moe/", description: "本机 IP、递归 DNS 出口、CDN 命中节点" },
  { name: "IPPure", url: "https://ippure.com/", description: "IP 纯净度、地理位置、运营商和风险检测" },
  { name: "IP看看", url: "https://ip.ping0.cc/", description: "公网 IP、ASN、原生 IP 和风险值" },
  { name: "Whoer.net", url: "https://whoer.net/", description: "匿名性、DNS、WebRTC 泄漏和代理状态" },
];

function flagEmoji(countryCode: string) {
  const code = (countryCode || "").toUpperCase();
  if (code.length !== 2) return "🌐";
  return String.fromCodePoint(...code.split("").map((char) => 127397 + char.charCodeAt(0)));
}

function modeLabel(mode: string) {
  return mode === "proxy" ? "代理出口" : "直连出口";
}
const tools = externalTools;

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

      <section class="panel network-tools-panel">
        <div class="panel-heading">
          <div>
            <h3>扩展检测工具</h3>
            <span>需要更深入的 WebRTC、DNS 泄漏、IP 纯净度或匿名性评分时，打开外部工具。</span>
          </div>
        </div>
        <div class="network-tool-grid">
          {tools.map((tool) => (
            <button class="network-tool" onClick={() => Browser.OpenURL(tool.url)}>
              <span class="network-tool-icon"><Icon name="external" size={15} /></span>
              <span><strong>{tool.name}</strong><small>{tool.description}</small></span>
              <Icon name="chevron" size={13} />
            </button>
          ))}
        </div>
        <p class="network-disclaimer">
          内置检测只请求 Cloudflare trace、ipwho.is 和 Cloudflare speed test，不发送节点凭证或订阅 URL。外部网站可能收集请求信息，请自行判断隐私风险。
        </p>
      </section>
    </div>
  );
}