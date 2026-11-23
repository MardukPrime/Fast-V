import { createSignal, For, Show } from "solid-js";

import { Service as CdnService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/cdnip";
import type { CdnScanHit, NodeView } from "../../bindings/github.com/MardukPrime/Fast-V/internal/model";
import { t } from "../i18n";
import Icon from "./Icon";

/**
 * Cloudflare edge picker for a CDN-fronted node.
 *
 * The scan runs entirely on the Go side (`internal/cdnip`): candidates come from
 * Cloudflare's anycast ranges, and a candidate only counts when it completes a
 * TLS handshake for this node's host *and* returns an HTTP response. Selecting one
 * stores it on the node, which keeps the domain for Host/SNI and only swaps the
 * destination IP — so the node survives subscription refreshes.
 */
export type CdnScanModalProps = {
  node: NodeView;
  onClose: () => void;
  /** Called with the updated node after the edge is applied or cleared. */
  onApplied: (node: NodeView) => void;
};

const sampleOptions = [60, 150, 300];

export default function CdnScanModal(props: CdnScanModalProps) {
  const [busy, setBusy] = createSignal(false);
  const [applyBusy, setApplyBusy] = createSignal(false);
  const [hits, setHits] = createSignal<CdnScanHit[]>([]);
  const [summary, setSummary] = createSignal("");
  const [error, setError] = createSignal("");
  const [sample, setSample] = createSignal(sampleOptions[1]);
  const [selected, setSelected] = createSignal("");

  const scan = async () => {
    setBusy(true);
    setError("");
    setSummary("");
    try {
      const result = await CdnService.Scan({
        nodeId: props.node.id,
        host: "",
        port: 0,
        customIps: "",
        sample: sample(),
        count: 12,
        concurrency: 12,
        timeoutMs: 2500,
      });
      setHits(result?.hits ?? []);
      const tested = result?.tested ?? 0;
      const reachable = result?.reachable ?? 0;
      setSummary(t("srv.cdn.summary", { tested: String(tested), reachable: String(reachable) }));
      if (result?.errors?.length) setError(result.errors.join("；"));
      if (!reachable) setError(t("srv.cdn.none"));
    } catch (scanError) {
      setError(scanError instanceof Error ? scanError.message : String(scanError));
    } finally {
      setBusy(false);
    }
  };

  const apply = async (ip: string) => {
    setApplyBusy(true);
    setError("");
    try {
      const view = await CdnService.Apply(props.node.id, ip);
      if (view) props.onApplied(view);
      setSelected(ip);
    } catch (applyError) {
      setError(applyError instanceof Error ? applyError.message : String(applyError));
    } finally {
      setApplyBusy(false);
    }
  };

  const clear = async () => {
    setApplyBusy(true);
    setError("");
    try {
      const view = await CdnService.Clear(props.node.id);
      if (view) props.onApplied(view);
      setSelected("");
    } catch (clearError) {
      setError(clearError instanceof Error ? clearError.message : String(clearError));
    } finally {
      setApplyBusy(false);
    }
  };

  const current = () => selected() || props.node.cdnIp || "";

  return (
    <div class="modal-backdrop" onClick={(event) => { if (event.target === event.currentTarget) props.onClose(); }}>
      <div class="modal cdn-modal" role="dialog" aria-modal="true" aria-label={t("srv.cdn.title")}>
        <div class="modal-heading">
          <div>
            <h3>{t("srv.cdn.title")}</h3>
            <span>{props.node.name} · {props.node.address}:{props.node.port}</span>
          </div>
          <button class="icon-btn" type="button" onClick={props.onClose} aria-label={t("srv.cdn.close")}>
            <Icon name="close" size={14} />
          </button>
        </div>

        <p class="cdn-hint">{t("srv.cdn.hint")}</p>

        <div class="cdn-toolbar">
          <div class="cdn-samples" role="group" aria-label={t("srv.cdn.sampleAria")}>
            <For each={sampleOptions}>
              {(option) => (
                <button
                  type="button"
                  classList={{ "cdn-sample": true, active: sample() === option }}
                  onClick={() => setSample(option)}
                  disabled={busy()}
                >
                  {option}
                </button>
              )}
            </For>
          </div>
          <button class="btn primary" type="button" onClick={() => void scan()} disabled={busy()}>
            <Icon name="refresh" size={14} /> {busy() ? t("srv.cdn.scanning") : t("srv.cdn.scan")}
          </button>
          <Show when={current()}>
            <button class="btn secondary" type="button" onClick={() => void clear()} disabled={applyBusy()}>
              {t("srv.cdn.clear")}
            </button>
          </Show>
        </div>

        <Show when={summary()}>
          <p class="cdn-summary">{summary()}</p>
        </Show>
        <Show when={error()}>
          <p class="cdn-error">{error()}</p>
        </Show>

        <Show when={hits().length > 0}>
          <div class="cdn-results">
            <For each={hits()}>
              {(hit) => (
                <button
                  type="button"
                  classList={{ "cdn-hit": true, active: current() === hit.ip }}
                  onClick={() => void apply(hit.ip)}
                  disabled={applyBusy()}
                >
                  <span class="cdn-hit-ip">{hit.ip}</span>
                  <span class="cdn-hit-latency">{hit.latencyMs} ms</span>
                  <span class="cdn-hit-status">HTTP {hit.status}</span>
                  <Show when={current() === hit.ip}>
                    <Icon name="check" size={13} />
                  </Show>
                </button>
              )}
            </For>
          </div>
        </Show>

        <Show when={!hits().length && !busy()}>
          <p class="cdn-empty">{t("srv.cdn.empty")}</p>
        </Show>
      </div>
    </div>
  );
}
