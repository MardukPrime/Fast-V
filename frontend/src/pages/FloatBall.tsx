import { createEffect, createMemo, createSignal, onCleanup, onMount, Show } from "solid-js";

import { Events } from "@wailsio/runtime";

import { Service as FloatWindowService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/floatwin";
import { ProxyService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/proxy";
import type { ProxyState, TrafficStats } from "../../bindings/github.com/MardukPrime/Fast-V/internal/model";
import { t } from "../i18n";
import "./floatball.css";

const SAMPLES = 36;

function rateParts(value: number) {
  const rate = Number.isFinite(value) && value > 0 ? value : 0;
  if (rate >= 1024 ** 2) {
    return { value: (rate / 1024 ** 2).toFixed(rate >= 10 * 1024 ** 2 ? 0 : 1), unit: "M" };
  }
  if (rate >= 1024) {
    return { value: (rate / 1024).toFixed(rate >= 10 * 1024 ? 0 : 1), unit: "K" };
  }
  return { value: String(Math.round(rate)), unit: "B" };
}

// Small smooth sparkline for the inside of the ball; the viewBox is stretched to
// the orb width so the wave hugs the bottom edge.
function sparkPath(values: number[], maxValue: number) {
  const bottom = 28;
  const height = 20;
  const count = values.length;
  if (count === 0) return `M 0 ${bottom} L 100 ${bottom}`;
  const safeMax = Math.max(1, maxValue);
  const points = values.map((value, index) => {
    const x = count === 1 ? 50 : (index / (count - 1)) * 100;
    const ratio = Math.min(1, Math.max(0, value / safeMax));
    return [x, bottom - ratio * height] as [number, number];
  });
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
  return `${d} L ${last[0].toFixed(2)} ${last[1].toFixed(2)}`;
}

function sparkArea(values: number[], maxValue: number) {
  return `${sparkPath(values, maxValue)} L 100 30 L 0 30 Z`;
}

// FloatBall is rendered in its own frameless transparent window ("?float=1").
// It shows a polished glass ball with the live down/up speeds, a rotating
// activity ring, a sparkline and status. Drag is fully pointer-driven and
// forwarded to the float window service; a clean click brings FastV back.
export default function FloatBall() {
  const [traffic, setTraffic] = createSignal<TrafficStats | null>(null);
  const [state, setState] = createSignal<ProxyState | null>(null);
  const [downSamples, setDownSamples] = createSignal<number[]>([]);
  const [upSamples, setUpSamples] = createSignal<number[]>([]);
  const [dragging, setDragging] = createSignal(false);

  const running = () => state()?.status === "running";
  const transient = () => state()?.status === "starting" || state()?.status === "stopping";
  const failed = () => state()?.status === "error";
  const downRate = () => (running() ? traffic()?.downlinkRate ?? 0 : 0);
  const upRate = () => (running() ? traffic()?.uplinkRate ?? 0 : 0);
  const totalRate = () => downRate() + upRate();
  const waveMax = createMemo(() => Math.max(1024, ...downSamples(), ...upSamples()));
  const downParts = () => rateParts(downRate());
  const upParts = () => rateParts(upRate());
  const active = () => running() && totalRate() > 0;
  const highActivity = () => totalRate() >= 4 * 1024 ** 2;
  const midActivity = () => totalRate() >= 256 * 1024;

  const downShare = () => {
    const total = totalRate();
    if (total <= 0) return 50;
    return Math.min(95, Math.max(5, Math.round((downRate() / total) * 100)));
  };
  const spinDuration = () => {
    if (highActivity()) return "1.8s";
    if (midActivity()) return "3.2s";
    return "7s";
  };

  const modeLabel = () => {
    switch (state()?.proxyMode) {
      case "global":
        return t("float.modeGlobal");
      case "direct":
        return t("float.modeDirect");
      default:
        return t("float.modeRule");
    }
  };

  onMount(() => {
    document.documentElement.classList.add("float-mode");
    document.body.classList.add("float-mode");

    let alive = true;
    const poll = async () => {
      try {
        const nextState = await ProxyService.State();
        if (alive) setState(nextState);
      } catch {
        // Keep the last known state; the next tick retries.
      }
      try {
        const stats = await ProxyService.Traffic();
        if (alive) setTraffic(stats);
      } catch {
        // Traffic is only available while the core runs.
      }
    };
    void poll();
    const timer = window.setInterval(() => {
      void poll();
    }, 1000);
    const offTraffic = Events.On("proxy:traffic", (event) => {
      setTraffic(event.data as TrafficStats);
    });

    onCleanup(() => {
      alive = false;
      window.clearInterval(timer);
      offTraffic();
      document.documentElement.classList.remove("float-mode");
      document.body.classList.remove("float-mode");
    });
  });

  createEffect(() => {
    const isRunning = running();
    const stats = traffic();
    if (!isRunning) {
      setDownSamples([]);
      setUpSamples([]);
      return;
    }
    if (!stats) return;
    setDownSamples((prev) => [...prev, Math.max(0, stats.downlinkRate ?? 0)].slice(-SAMPLES));
    setUpSamples((prev) => [...prev, Math.max(0, stats.uplinkRate ?? 0)].slice(-SAMPLES));
  });

  // Pointer drag: a threshold distinguishes a click (open the main window)
  // from a drag, and the window service moves the native window by the cursor
  // delta so dragging stays precise at any DPI.
  let press: { x: number; y: number; dragging: boolean } | null = null;

  const onPointerDown = (event: PointerEvent) => {
    if (event.button !== 0) return;
    press = { x: event.screenX, y: event.screenY, dragging: false };
    (event.currentTarget as HTMLElement).setPointerCapture?.(event.pointerId);
    void FloatWindowService.DragStart(Math.round(event.screenX), Math.round(event.screenY));
  };

  const onPointerMove = (event: PointerEvent) => {
    if (!press) return;
    if (!press.dragging) {
      if (Math.abs(event.screenX - press.x) < 3 && Math.abs(event.screenY - press.y) < 3) return;
      press.dragging = true;
      setDragging(true);
    }
    void FloatWindowService.DragMove(Math.round(event.screenX), Math.round(event.screenY));
  };

  const finishPointer = (event: PointerEvent, cancelled: boolean) => {
    if (!press) return;
    const wasDragging = press.dragging;
    press = null;
    setDragging(false);
    (event.currentTarget as HTMLElement).releasePointerCapture?.(event.pointerId);
    void FloatWindowService.DragEnd();
    if (!wasDragging && !cancelled) {
      void FloatWindowService.OpenMain();
    }
  };

  return (
    <div
      classList={{
        "float-shell": true,
        running: running(),
        failed: failed(),
        transient: transient(),
        dragging: dragging(),
        active: active(),
        busy: highActivity(),
      }}
      style={{ "--ring-share": `${downShare()}%`, "--spin-duration": spinDuration() }}
      role="button"
      tabIndex={0}
      aria-label={running() ? t("float.ballRunningAria") : t("float.ballIdleAria")}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={(event) => finishPointer(event, false)}
      onPointerCancel={(event) => finishPointer(event, true)}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          void FloatWindowService.OpenMain();
        }
      }}
      onContextMenu={(event) => event.preventDefault()}
    >
      <span class="float-halo" aria-hidden="true" />
      <div class="float-ball">
        <span class="float-ring" aria-hidden="true" />
        <div class="float-orb">
          <svg class="float-spark" viewBox="0 0 100 30" preserveAspectRatio="none" aria-hidden="true">
            <defs>
              <linearGradient id="floatSparkArea" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stop-color="var(--accent)" stop-opacity="0.42" />
                <stop offset="100%" stop-color="var(--accent)" stop-opacity="0" />
              </linearGradient>
            </defs>
            <path class="float-spark-area" d={sparkArea(downSamples(), waveMax())} />
            <path class="float-spark-line down" d={sparkPath(downSamples(), waveMax())} />
            <path class="float-spark-line up" d={sparkPath(upSamples(), waveMax())} />
          </svg>

          <div class="float-head">
            <span class="float-dot" />
            <span class="float-status">
              {failed() ? t("float.error") : transient() ? t("float.connecting") : running() ? t("float.live") : t("float.idle")}
            </span>
            <Show when={running() || transient()}>
              <span class="float-mode">{modeLabel()}</span>
            </Show>
          </div>

          <Show
            when={running() || transient() || failed()}
            fallback={
              <div class="float-idle">
                <strong>{t("float.idle")}</strong>
                <span>{t("float.clickToOpen")}</span>
              </div>
            }
          >
            <div class="float-speeds">
              <div class="float-speed down">
                <span class="float-arrow">↓</span>
                <strong>{downParts().value}</strong>
                <em>{downParts().unit}/s</em>
              </div>
              <div class="float-speed up">
                <span class="float-arrow">↑</span>
                <strong>{upParts().value}</strong>
                <em>{upParts().unit}/s</em>
              </div>
            </div>
          </Show>

          <span class="float-gloss" aria-hidden="true" />
        </div>
      </div>
    </div>
  );
}
