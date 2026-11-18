import { createMemo, createSignal, For, onMount, Show } from "solid-js";


import { DiagnosticsService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/diagnostics";
import { Service as HistoryService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/history";
import { Service as RescueService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/rescue";
import type { RescueReport, RescueResult } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/rescue";
import { Service as SelfCheckService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/selfcheck";
import type { CheckResult, LatencySample, NetworkProbe, ProxyState } from "../../bindings/github.com/MardukPrime/Fast-V/internal/model";
import Icon from "../components/Icon";
import { t } from "../i18n";

type NetworkPageProps = {
  proxyState: ProxyState;
};


function flagEmoji(countryCode: string) {
  const code = (countryCode || "").toUpperCase();
  if (code.length !== 2) return "🌐";
  return String.fromCodePoint(...code.split("").map((char) => 127397 + char.charCodeAt(0)));
}

function modeLabel(mode: string) {
  return mode === "proxy" ? t("net.mode.proxy") : t("net.mode.direct");
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
  if (status === "ok") return t("net.status.ok");
  if (status === "warn") return t("net.status.warn");
  return t("net.status.error");
}

// The rescue service reports ok | warning | error; the self-check styling uses
// ok | warn | error, so both share the same badge/item classes.
function rescueTone(severity: string) {
  if (severity === "ok") return "ok";
  if (severity === "warning") return "warn";
  return "error";
}

function rescueSeverityLabel(severity: string) {
  const tone = rescueTone(severity);
  if (tone === "ok") return t("net.rescue.severityOk");
  if (tone === "warn") return t("net.rescue.severityWarning");
  return t("net.rescue.severityError");
}

// Inspect degrades unsupported platform capabilities into an "ok" skip issue
// (stable id "system-proxy-skipped") instead of failing; surface that state in
// the summary so a skipped check never looks like "system proxy is off".
function rescueSystemProxyText(report: RescueReport) {
  const skipped = (report.issues ?? []).some((issue) => issue.id === "system-proxy-skipped");
  if (skipped) return t("net.rescue.systemProxyUnavailable");
  if (!report.systemProxyEnabled) return t("net.rescue.systemProxyOff");
  return report.systemProxyServer
    ? t("net.rescue.systemProxyOn", { server: report.systemProxyServer })
    : t("net.rescue.systemProxyOnNoServer");
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
        <button class="icon-button" onClick={props.onRefresh} disabled={props.loading || !props.available} aria-label={t("net.refreshMode", { mode: modeLabel(props.mode) })}>
          <Icon name="refresh" size={14} />
        </button>
      </header>

      <Show when={props.loading} fallback={
        <Show when={data()} fallback={
          <div class="network-empty">
            <span class="network-empty-icon"><Icon name={props.mode === "proxy" ? "route" : "speed"} size={20} /></span>
            <strong>{props.available ? t("net.probe.notChecked") : t("net.probe.proxyOffline")}</strong>
            <span>{props.available ? t("net.probe.notCheckedHint") : t("net.probe.proxyOfflineHint")}</span>
          </div>
        }>
          <div class="network-result">
            <div class="network-ip-row">
              <span class="network-flag">{flagEmoji(data()!.countryCode)}</span>
              <div>
                <strong>{data()!.ip || t("net.probe.unknownIp")}</strong>
                <span>{data()!.ipVersion || "IP"} · {[data()!.country, data()!.region, data()!.city].filter(Boolean).join(" / ") || t("net.probe.unknownLocation")}</span>
              </div>
            </div>

            <div class="network-facts">
              <div><span>{t("net.fact.asn")}</span><strong>{data()!.asn ? `AS${data()!.asn}` : "—"}</strong></div>
              <div><span>{t("net.fact.isp")}</span><strong>{data()!.isp || data()!.organization || "—"}</strong></div>
              <div><span>{t("net.fact.colo")}</span><strong>{data()!.colo || "—"}</strong></div>
              <div><span>{t("net.fact.tls")}</span><strong>{data()!.tls || "—"}</strong></div>
              <div><span>{t("net.fact.warp")}</span><strong>{data()!.warp || "—"}</strong></div>
              <div><span>{t("net.fact.latency")}</span><strong>{data()!.latencyMs ? `${data()!.latencyMs} ms` : "—"}</strong></div>
              <div><span>{t("net.fact.jitter")}</span><strong>{data()!.jitterMs ? `${data()!.jitterMs} ms` : "—"}</strong></div>
              <div><span>{t("net.fact.download")}</span><strong>{data()!.downloadMbps ? `${data()!.downloadMbps.toFixed(1)} Mbps` : t("net.fact.notTested")}</strong></div>
            </div>

            <Show when={data()!.geoError}>
              <div class="network-note warning"><Icon name="alert" size={12} /> {t("net.geoError", { error: data()!.geoError ?? "" })}</div>
            </Show>
            <Show when={data()!.downloadError}>
              <div class="network-note warning"><Icon name="alert" size={12} /> {t("net.downloadError", { error: data()!.downloadError ?? "" })}</div>
            </Show>
          </div>
        </Show>
      }>
        <div class="network-loading">
          <span class="network-pulse" />
          <strong>{t("net.probe.loading", { mode: modeLabel(props.mode) })}</strong>
          <span>{t("net.probe.loadingHint")}</span>
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
  const [rescueReport, setRescueReport] = createSignal<RescueReport | null>(null);
  const [rescueLoading, setRescueLoading] = createSignal(false);
  const [rescueError, setRescueError] = createSignal("");
  const [rescueCheckedAt, setRescueCheckedAt] = createSignal("");
  const [repairing, setRepairing] = createSignal(false);
  const [repairResult, setRepairResult] = createSignal<RescueResult | null>(null);
  const [repairError, setRepairError] = createSignal("");
  const [reportBusy, setReportBusy] = createSignal(false);
  const [reportPath, setReportPath] = createSignal("");
  const [reportError, setReportError] = createSignal("");

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
    const confirmed = window.confirm(t("net.clearConfirm"));
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
    if (total === 0) return t("net.selfCheck.noResults");
    if (error > 0) return t("net.selfCheck.verdictErrors", { count: error });
    if (warn > 0) return t("net.selfCheck.verdictWarnings", { count: warn });
    return t("net.selfCheck.verdictOk");
  };

  const rescueSummary = createMemo(() => {
    let ok = 0;
    let warning = 0;
    let error = 0;
    for (const issue of rescueReport()?.issues ?? []) {
      const tone = rescueTone(issue.severity);
      if (tone === "ok") ok += 1;
      else if (tone === "warn") warning += 1;
      else error += 1;
    }
    return { total: ok + warning + error, ok, warning, error };
  });

  const rescueVerdict = () => {
    const { total, warning, error } = rescueSummary();
    if (total === 0) return t("net.rescue.verdictEmpty");
    if (error > 0) return t("net.rescue.verdictErrors", { count: error });
    if (warning > 0) return t("net.rescue.verdictWarnings", { count: warning });
    return t("net.rescue.verdictOk");
  };

  // Inspect is best-effort on the backend: unsupported platform capabilities
  // become "ok" skip issues instead of failing, so an error here only means the
  // RPC itself failed. It must never block the existing diagnostics features.
  const inspectRescue = async () => {
    if (rescueLoading() || repairing()) return;
    setRescueLoading(true);
    setRescueError("");
    try {
      const report = await RescueService.Inspect();
      setRescueReport(report);
      setRescueCheckedAt(new Date().toLocaleTimeString());
    } catch (err) {
      setRescueError(t("net.rescue.error", { error: err instanceof Error ? err.message : String(err) }));
    } finally {
      setRescueLoading(false);
    }
  };

  const repairNetwork = async () => {
    if (repairing() || rescueLoading()) return;
    setRepairing(true);
    setRepairError("");
    setRepairResult(null);
    try {
      const result = await RescueService.Repair();
      setRepairResult(result);
      setRescueReport(result.report);
      setRescueCheckedAt(new Date().toLocaleTimeString());
    } catch (err) {
      setRepairError(t("net.rescue.repairError", { error: err instanceof Error ? err.message : String(err) }));
    } finally {
      setRepairing(false);
    }
  };

  const exportDiagnosticReport = async () => {
    if (reportBusy()) return;
    setReportBusy(true);
    setReportError("");
    setReportPath("");
    try {
      const path = await DiagnosticsService.ExportReport();
      setReportPath(path);
    } catch (err) {
      setReportError(t("net.report.error", { error: err instanceof Error ? err.message : String(err) }));
    } finally {
      setReportBusy(false);
    }
  };

  const proxyAvailable = () => props.proxyState.status === "running";

  const checkMode = async (mode: "direct" | "proxy") => {
    if (mode === "proxy" && !proxyAvailable()) {
      setError(t("net.error.proxyOffline"));
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

  // Network rescue runs one best-effort inspection on load so the panel is
  // populated without an extra click; the result is informational and never
  // affects the regular diagnostics probes.
  onMount(() => {
    void inspectRescue();
  });

  const sameExit = () => Boolean(direct()?.ip && proxied()?.ip && direct()!.ip === proxied()!.ip);
  const proxyCountry = () => proxied();

  return (
    <div class="page-stack">
      <section class="glass-panel network-hero">
        <div>
          <span class="network-eyebrow"><Icon name="speed" size={13} /> {t("net.eyebrow")}</span>
          <h2>{t("net.title")}</h2>
          <p>{t("net.subtitle")}</p>
        </div>
        <div class="network-actions">
          <label class="network-speed-toggle">
            <span classList={{ "switch": true, on: includeSpeed() }}>
              <input type="checkbox" checked={includeSpeed()} onChange={(event) => setIncludeSpeed(event.currentTarget.checked)} />
              <span class="switch-track"><span class="switch-thumb" /></span>
            </span>
            {t("net.includeSpeed")}
          </label>
          <button class="btn primary" onClick={runAll} disabled={directLoading() || proxyLoading()}>
            <Icon name="refresh" size={14} /> {directLoading() || proxyLoading() ? t("net.checking") : t("net.runAll")}
          </button>
        </div>
      </section>

      <Show when={error()}>
        <div class="alert error">
          <span class="alert-message"><Icon name="alert" size={15} /> {error()}</span>
          <button class="btn ghost" onClick={() => setError("")}>{t("net.close")}</button>
        </div>
      </Show>

      <div class="network-grid">
        <ProbeCard mode="direct" probe={direct()} loading={directLoading()} available onRefresh={() => checkMode("direct")} />
        <ProbeCard mode="proxy" probe={proxied()} loading={proxyLoading()} available={proxyAvailable()} onRefresh={() => checkMode("proxy")} />
      </div>

      <Show when={direct() && proxied()}>
        <section class="glass-panel network-compare">
          <div class="compare-exit">
            <span>{t("net.mode.direct")}</span>
            <strong>{direct()!.ip}</strong>
            <small>{flagEmoji(direct()!.countryCode)} {direct()!.country || direct()!.countryCode || t("net.compare.unknown")}</small>
          </div>
          <span class="compare-arrow">→</span>
          <div class="compare-exit">
            <span>{t("net.mode.proxy")}</span>
            <strong>{proxied()!.ip}</strong>
            <small>{flagEmoji(proxied()!.countryCode)} {proxied()!.country || proxied()!.countryCode || t("net.compare.unknown")}</small>
          </div>
          <span classList={{ "compare-badge": true, changed: !sameExit() }}>
            <span class="status-dot" /> {sameExit() ? t("net.compare.same") : t("net.compare.changed")}
          </span>
          <Show when={proxyCountry()?.colo}>
            <span class="compare-colo">Cloudflare {proxyCountry()!.colo}</span>
          </Show>
        </section>
      </Show>

      <section class="glass-panel selfcheck-panel">
        <div class="panel-heading">
          <div>
            <h3>{t("net.rescue.title")}</h3>
            <span>{t("net.rescue.subtitle")}</span>
          </div>
          <div class="selfcheck-actions">
            <Show when={rescueCheckedAt()}>
              <span class="selfcheck-timestamp">{t("net.rescue.lastAt", { time: rescueCheckedAt() })}</span>
            </Show>
            <button class="btn glass" onClick={() => { void inspectRescue(); }} disabled={rescueLoading() || repairing()}>
              <Icon name={rescueLoading() ? "refresh" : "search"} size={14} />
              {rescueLoading() ? t("net.rescue.checking") : t("net.rescue.check")}
            </button>
          </div>
        </div>

        <Show when={rescueError()}>
          <div class="alert error selfcheck-alert">
            <span class="alert-message"><Icon name="alert" size={15} /> {rescueError()}</span>
            <button class="btn ghost" onClick={() => { void inspectRescue(); }}>{t("net.retry")}</button>
          </div>
        </Show>

        <Show
          when={rescueLoading() && !rescueReport()}
          fallback={
            <Show
              when={rescueReport()}
              fallback={
                <div class="empty-state">
                  <div class="empty-icon"><Icon name="search" size={22} /></div>
                  <strong>{t("net.rescue.emptyTitle")}</strong>
                  <span>{t("net.rescue.emptyBody")}</span>
                </div>
              }
            >
              {(report) => (
                <>
                  <div class="network-facts" style="grid-template-columns: repeat(2, minmax(0, 1fr));">
                    <div><span>{t("net.rescue.systemProxy")}</span><strong>{rescueSystemProxyText(report())}</strong></div>
                    <div><span>{t("net.rescue.proxyRunning")}</span><strong>{report().proxyRunning ? t("net.rescue.running") : t("net.rescue.notRunning")}</strong></div>
                  </div>
                  <div class="selfcheck-summary">
                    <span class="selfcheck-chip ok"><strong>{rescueSummary().ok}</strong> {t("net.rescue.okSuffix")}</span>
                    <span class="selfcheck-chip warn"><strong>{rescueSummary().warning}</strong> {t("net.rescue.warningSuffix")}</span>
                    <span class="selfcheck-chip error"><strong>{rescueSummary().error}</strong> {t("net.rescue.errorSuffix")}</span>
                    <span class="selfcheck-verdict">{rescueVerdict()}</span>
                  </div>
                  <div class="selfcheck-list" role="list">
                    <For each={report().issues ?? []}>
                      {(issue) => (
                        <div
                          classList={{
                            "selfcheck-item": true,
                            ok: rescueTone(issue.severity) === "ok",
                            warn: rescueTone(issue.severity) === "warn",
                            error: rescueTone(issue.severity) === "error",
                          }}
                          role="listitem"
                        >
                          <span
                            classList={{
                              "selfcheck-badge": true,
                              ok: rescueTone(issue.severity) === "ok",
                              warn: rescueTone(issue.severity) === "warn",
                              error: rescueTone(issue.severity) === "error",
                            }}
                          >
                            <Icon name={rescueTone(issue.severity) === "ok" ? "check" : "alert"} size={12} />
                            {rescueSeverityLabel(issue.severity)}
                          </span>
                          <div class="selfcheck-body">
                            <strong class="selfcheck-name">{issue.title}</strong>
                            <Show when={issue.detail}>
                              <p class="selfcheck-detail">{issue.detail}</p>
                            </Show>
                          </div>
                        </div>
                      )}
                    </For>
                  </div>
                </>
              )}
            </Show>
          }
        >
          <div class="selfcheck-loading">
            <span class="network-pulse" />
            <strong>{t("net.rescue.loading")}</strong>
            <span>{t("net.rescue.loadingHint")}</span>
          </div>
        </Show>

        <Show when={repairError()}>
          <div class="alert error selfcheck-alert">
            <span class="alert-message"><Icon name="alert" size={15} /> {repairError()}</span>
          </div>
        </Show>

        <Show when={repairResult()}>
          {(result) => (
            <>
              <div classList={{ alert: true, success: (result().failed ?? []).length === 0, error: (result().failed ?? []).length > 0, "selfcheck-alert": true }}>
                <span class="alert-message">
                  <Icon name={(result().failed ?? []).length === 0 ? "check" : "alert"} size={15} />
                  {result().message || t("net.rescue.repairResult")}
                </span>
              </div>
              <Show when={(result().fixed ?? []).length > 0 || (result().failed ?? []).length > 0}>
                <div class="selfcheck-list" role="list">
                  <For each={result().fixed ?? []}>
                    {(name) => (
                      <div class="selfcheck-item ok" role="listitem">
                        <span class="selfcheck-badge ok"><Icon name="check" size={12} /> {t("net.rescue.fixed")}</span>
                        <div class="selfcheck-body"><strong class="selfcheck-name">{name}</strong></div>
                      </div>
                    )}
                  </For>
                  <For each={result().failed ?? []}>
                    {(name) => (
                      <div class="selfcheck-item error" role="listitem">
                        <span class="selfcheck-badge error"><Icon name="alert" size={12} /> {t("net.rescue.failed")}</span>
                        <div class="selfcheck-body"><strong class="selfcheck-name">{name}</strong></div>
                      </div>
                    )}
                  </For>
                </div>
              </Show>
            </>
          )}
        </Show>

        <div class="core-actions">
          <button class="btn primary" type="button" onClick={() => { void repairNetwork(); }} disabled={repairing() || rescueLoading()}>
            <Icon name="play" size={14} /> {repairing() ? t("net.rescue.repairing") : t("net.rescue.repair")}
          </button>
          <button class="btn secondary" type="button" onClick={() => { void exportDiagnosticReport(); }} disabled={reportBusy()}>
            <Icon name="external" size={14} /> {reportBusy() ? t("net.report.exporting") : t("net.report.export")}
          </button>
        </div>

        <Show when={reportError()}>
          <div class="alert error selfcheck-alert">
            <span class="alert-message"><Icon name="alert" size={15} /> {reportError()}</span>
          </div>
        </Show>

        <Show when={reportPath()}>
          <div class="alert success selfcheck-alert">
            <span class="alert-message"><Icon name="check" size={15} /> {t("net.report.success")}</span>
          </div>
          <div class="core-path-row">
            <span>{t("net.report.pathLabel")}</span>
            <code title={reportPath()}>{reportPath()}</code>
          </div>
          <p class="settings-note"><Icon name="check" size={13} /><span>{t("net.report.privacy")}</span></p>
        </Show>
      </section>

      <section class="glass-panel network-history-panel">
        <div class="panel-heading">
          <div>
            <h3>{t("net.history.title")}</h3>
            <span>
              {latencies().length > 0
                ? t("net.history.subtitle", { count: latencies().length })
                : t("net.history.subtitleEmpty")}
            </span>
          </div>
          <div class="network-history-actions">
            <button
              class="icon-button"
              onClick={() => { void loadLatencies(); }}
              disabled={latencyLoading()}
              aria-label={t("net.history.refresh")}
              title={t("net.history.refresh")}
            >
              <Icon name="refresh" size={14} />
            </button>
            <button
              class="btn danger"
              onClick={() => { void clearHistory(); }}
              disabled={clearingHistory() || latencyLoading() || latencies().length === 0}
            >
              <Icon name="trash" size={13} /> {clearingHistory() ? t("net.history.clearing") : t("net.history.clear")}
            </button>
          </div>
        </div>

        <Show when={latencyError()}>
          <div class="alert error network-history-alert">
            <span class="alert-message"><Icon name="alert" size={15} /> {t("net.history.error", { error: latencyError() })}</span>
            <button class="btn ghost" onClick={() => { void loadLatencies(); }}>{t("net.retry")}</button>
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
                  <strong>{t("net.history.emptyTitle")}</strong>
                  <span>{t("net.history.emptyBody")}</span>
                </div>
              }
            >
              <div class="network-history-list" role="list">
                <For each={latencies()}>
                  {(sample) => (
                    <div class="network-history-row" role="listitem">
                      <span class="network-history-time">{formatSampleTime(sample.checkedAt)}</span>
                      <span class="network-history-node">
                        <strong title={sample.nodeId || sample.nodeName}>{sample.nodeName || t("net.history.unknownNode")}</strong>
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
                          <span class="latency bad">{t("net.history.failed")}</span>
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
            <strong>{t("net.history.loading")}</strong>
          </div>
        </Show>
      </section>

      <section class="glass-panel selfcheck-panel">
        <div class="panel-heading">
          <div>
            <h3>{t("net.selfCheck.title")}</h3>
            <span>{t("net.selfCheck.subtitle")}</span>
          </div>
          <div class="selfcheck-actions">
            <Show when={selfCheckAt()}>
              <span class="selfcheck-timestamp">{t("net.selfCheck.lastAt", { time: selfCheckAt() })}</span>
            </Show>
            <button class="btn glass" onClick={() => { void runSelfCheck(); }} disabled={selfCheckBusy()}>
              <Icon name={selfCheckBusy() ? "refresh" : "check"} size={14} />
              {selfCheckBusy() ? t("net.selfCheck.busy") : selfCheckResults() ? t("net.selfCheck.rerun") : t("net.selfCheck.start")}
            </button>
          </div>
        </div>

        <Show when={selfCheckError()}>
          <div class="alert error selfcheck-alert">
            <span class="alert-message"><Icon name="alert" size={15} /> {t("net.selfCheck.error", { error: selfCheckError() })}</span>
            <button class="btn ghost" onClick={() => { void runSelfCheck(); }}>{t("net.retry")}</button>
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
                  <strong>{t("net.selfCheck.emptyTitle")}</strong>
                  <span>{t("net.selfCheck.emptyBody")}</span>
                </div>
              }
            >
              <div class="selfcheck-summary">
                <span class="selfcheck-chip ok"><strong>{selfCheckSummary().ok}</strong> {t("net.selfCheck.passedSuffix")}</span>
                <span class="selfcheck-chip warn"><strong>{selfCheckSummary().warn}</strong> {t("net.selfCheck.warnSuffix")}</span>
                <span class="selfcheck-chip error"><strong>{selfCheckSummary().error}</strong> {t("net.selfCheck.failedSuffix")}</span>
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
            <strong>{t("net.selfCheck.loading")}</strong>
            <span>{t("net.selfCheck.loadingHint")}</span>
          </div>
        </Show>
      </section>

      <section class="panel network-tools-panel">
        <div class="panel-heading">
          <div>
            <h3>{t("net.tools.title")}</h3>
            <span>{t("net.tools.subtitle")}</span>
          </div>
        </div>
        <div class="network-capability-grid">
          <div><span class="network-tool-icon"><Icon name="server" size={15} /></span><strong>{t("net.tools.exitIp")}</strong><small>{t("net.tools.exitIpHint")}</small></div>
          <div><span class="network-tool-icon"><Icon name="subscription" size={15} /></span><strong>{t("net.tools.location")}</strong><small>{t("net.tools.locationHint")}</small></div>
          <div><span class="network-tool-icon"><Icon name="nodes" size={15} /></span><strong>{t("net.tools.networkOwner")}</strong><small>{t("net.tools.networkOwnerHint")}</small></div>
          <div><span class="network-tool-icon"><Icon name="route" size={15} /></span><strong>{t("net.tools.cloudflareNode")}</strong><small>{t("net.tools.cloudflareNodeHint")}</small></div>
          <div><span class="network-tool-icon"><Icon name="speed" size={15} /></span><strong>{t("net.tools.quality")}</strong><small>{t("net.tools.qualityHint")}</small></div>
          <div><span class="network-tool-icon"><Icon name="check" size={15} /></span><strong>{t("net.tools.compare")}</strong><small>{t("net.tools.compareHint")}</small></div>
        </div>
        <p class="network-disclaimer">
          {t("net.tools.disclaimer")}
        </p>
      </section>
    </div>
  );
}
