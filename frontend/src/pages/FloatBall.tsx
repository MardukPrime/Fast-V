import { createEffect, createMemo, createSignal, onCleanup, onMount, Show } from "solid-js";

import { Events } from "@wailsio/runtime";

import { Service as FloatWindowService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/floatwin";
import { ProxyService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/proxy";
import type { ProxyState, TrafficStats } from "../../bindings/github.com/MardukPrime/Fast-V/internal/model";
import { t } from "../i18n";
import "./floatball.css";

const SAMPLES = 30;
const WAVE_HEIGHT = 34;
const WAVE_BOTTOM = 94;

function formatRate(value: number) {
  const rate = Number.isFinite(value) && value > 0 ? value : 0;
  if (rate >= 1024 ** 2) return `${(rate / 1024 ** 2).toFixed(1)}M`;
  if (rate >= 1024) return `${(rate / 1024).toFixed(0)}K`;
  return `${Math.round(rate)}B`;
}

function wavePath(values: number[], maxValue: number) {
  const count = values.length;
  if (count === 0) return `M 0 ${WAVE_BOTTOM} L 100 ${WAVE_BOTTOM}`;
  const safeMax = Math.max(1, maxValue);
  const points = values.map((value, index) => {
    const x = count === 1 ? 50 : (index / (count - 1)) * 100;
    const ratio = Math.min(1, Math.max(0, value / safeMax));
    return [x, WAVE_BOTTOM - ratio * WAVE_HEIGHT] as [number, number];
  });
  if (points.length === 1) {
    return `M 0 ${points[0][1].toFixed(2)} L 100 ${points[0][1].toFixed(2)}`;
  }
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

function waveAreaPath(values: number[], maxValue: number) {
  return `${wavePath(values, maxValue)} L 100 100 L 0 100 Z`;
}

// FloatBall is rendered in its own frameless transparent window ("?float=1").
// It shows a round, draggable ball with the live down/up traffic waveform; a
// single click brings the main window back to the front.
export default function FloatBall() {
  const [traffic, setTraffic] = createSignal<TrafficStats | null>(null);
  const [state, setState] = createSignal<ProxyState | null>(null);
  const [downSamples, setDownSamples] = createSignal<number[]>([]);
  const [upSamples, setUpSamples] = createSignal<number[]>([]);

  const running = () => state()?.status === "running";
  const transient = () => state()?.status === "starting" || state()?.status === "stopping";
  const failed = () => state()?.status === "error";
  const downRate = () => (running() ? traffic()?.downlinkRate ?? 0 : 0);
  const upRate = () => (running() ? traffic()?.uplinkRate ?? 0 : 0);
  const waveMax = createMemo(() => Math.max(1024, ...downSamples(), ...upSamples()));

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

  // Pointer handling: a short press (no movement) opens the main window, while
  // moving the pointer hands the window to the native move loop for dragging.
  let press: { x: number; y: number; dragging: boolean } | null = null;

  const onPointerDown = (event: PointerEvent) => {
    if (event.button !== 0) return;
    press = { x: event.screenX, y: event.screenY, dragging: false };
    (event.currentTarget as HTMLElement).setPointerCapture?.(event.pointerId);
  };

  const onPointerMove = (event: PointerEvent) => {
    if (!press || press.dragging) return;
    if (Math.hypot(event.screenX - press.x, event.screenY - press.y) > 4) {
      press.dragging = true;
      void FloatWindowService.StartDrag();
    }
  };

  const onPointerUp = (event: PointerEvent) => {
    if (press && !press.dragging) {
      void FloatWindowService.OpenMain();
    }
    press = null;
    (event.currentTarget as HTMLElement).releasePointerCapture?.(event.pointerId);
  };

  return (
    <div
      classList={{ "float-ball": true, running: running(), failed: failed(), transient: transient() }}
      role="button"
      tabIndex={0}
      aria-label={running() ? t("float.ballRunningAria") : t("float.ballIdleAria")}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={() => {
        press = null;
      }}
    >
      <svg class="float-ball-wave" viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
        <defs>
          <clipPath id="floatBallClip">
            <circle cx="50" cy="50" r="49" />
          </clipPath>
          <linearGradient id="floatBallArea" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stop-color="var(--accent)" stop-opacity="0.42" />
            <stop offset="100%" stop-color="var(--accent)" stop-opacity="0" />
          </linearGradient>
        </defs>
        <g clip-path="url(#floatBallClip)">
          <path class="float-ball-wave-area" d={waveAreaPath(downSamples(), waveMax())} />
          <path class="float-ball-wave-line down" d={wavePath(downSamples(), waveMax())} />
          <path class="float-ball-wave-line up" d={wavePath(upSamples(), waveMax())} />
        </g>
      </svg>

      <span class="float-ball-shine" aria-hidden="true" />
      <span class="float-ball-ring" aria-hidden="true" />

      <div class="float-ball-center">
        <Show
          when={running() || transient() || failed()}
          fallback={<span class="float-ball-idle">{t("float.idle")}</span>}
        >
          <span class="float-ball-status">
            <span class="float-ball-dot" />
            {failed() ? t("float.error") : transient() ? t("float.connecting") : t("float.live")}
          </span>
          <span class="float-ball-rate">
            <span class="float-ball-rate-down">↓ {formatRate(downRate())}</span>
            <span class="float-ball-rate-up">↑ {formatRate(upRate())}</span>
          </span>
        </Show>
      </div>
    </div>
  );
}
