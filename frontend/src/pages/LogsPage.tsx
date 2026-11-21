import { createMemo, createSignal, For, onCleanup, onMount, Show } from "solid-js";

import { Events } from "@wailsio/runtime";

import { ProxyService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/proxy";
import type { LogEntry, ProxyState } from "../../bindings/github.com/MardukPrime/Fast-V/internal/model";
import Icon from "../components/Icon";
import { t } from "../i18n";

type LogsPageProps = {
  proxyState: ProxyState;
  onNavigate: (page: string) => void;
};

const levels = ["debug", "info", "warning", "error"];

function levelLabel(level: string) {
  switch (level) {
    case "debug": return "DEBUG";
    case "warning": return "WARN";
    case "error": return "ERROR";
    default: return "INFO";
  }
}

function formatTime(timestamp: string) {
  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return "--:--:--";
  return date.toLocaleTimeString("zh-CN", { hour12: false });
}

function formatLogLine(entry: LogEntry) {
  return `[${formatTime(entry.timestamp)}] [${levelLabel(entry.level)}] [${entry.source}] ${entry.message}`;
}

function formatExportStamp(date: Date) {
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}${pad(date.getMonth() + 1)}${pad(date.getDate())}-${pad(date.getHours())}${pad(date.getMinutes())}`;
}

export default function LogsPage(props: LogsPageProps) {
  const [logs, setLogs] = createSignal<LogEntry[]>([]);
  const [search, setSearch] = createSignal("");
  const [level, setLevel] = createSignal("");
  const [autoScroll, setAutoScroll] = createSignal(true);
  const [paused, setPaused] = createSignal(false);
  const [loading, setLoading] = createSignal(true);
  const [error, setError] = createSignal("");
  const [notice, setNotice] = createSignal("");
  let logView: HTMLDivElement | undefined;
  let noticeTimer: number | undefined;

  // Success feedback for copy/export actions is transient: show it for a moment
  // and only clear it when another notice has not replaced it in the meantime.
  const flashNotice = (text: string) => {
    setError("");
    setNotice(text);
    if (noticeTimer !== undefined) window.clearTimeout(noticeTimer);
    noticeTimer = window.setTimeout(() => {
      setNotice((current) => (current === text ? "" : current));
      noticeTimer = undefined;
    }, 2400);
  };

  onCleanup(() => {
    if (noticeTimer !== undefined) window.clearTimeout(noticeTimer);
  });

  const filtered = createMemo(() => {
    const keyword = search().trim().toLowerCase();
    return logs().filter((entry) => {
      if (level() && entry.level !== level()) return false;
      if (!keyword) return true;
      return `${entry.source} ${entry.message}`.toLowerCase().includes(keyword);
    });
  });

  const scrollToBottom = () => {
    if (!autoScroll() || !logView) return;
    requestAnimationFrame(() => {
      if (logView) logView.scrollTop = logView.scrollHeight;
    });
  };

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      setLogs((await ProxyService.Logs()) ?? []);
      scrollToBottom();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  onMount(() => {
    load();
    const off = Events.On("proxy:log", (event) => {
      if (paused()) return;
      const entry = event.data as LogEntry;
      setLogs((current) => [...current.slice(-1499), entry]);
      scrollToBottom();
    });
    onCleanup(off);
  });

  const clearLogs = async () => {
    setNotice("");
    try {
      await ProxyService.ClearLogs();
      setLogs([]);
      setNotice(t("logs.notice.cleared"));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const copyLogs = async () => {
    if (filtered().length === 0) return;
    const text = filtered().map(formatLogLine).join("\n");
    try {
      await navigator.clipboard.writeText(text);
      setNotice(t("logs.notice.copiedFiltered"));
    } catch {
      setError(t("logs.error.copyFiltered"));
    }
  };

  const copyAllLogs = async () => {
    if (logs().length === 0) {
      setNotice("");
      setError(t("logs.error.noLogsToCopy"));
      return;
    }
    setError("");
    try {
      await navigator.clipboard.writeText(logs().map(formatLogLine).join("\n"));
      flashNotice(t("logs.notice.copiedAll", { count: logs().length }));
    } catch {
      setError(t("logs.error.copyAll"));
    }
  };

  // Exports the current filtered view as a plain-text log file, never the raw
  // buffer: the search keyword and level filter decide what lands in the file.
  const exportLogs = () => {
    const entries = filtered();
    if (entries.length === 0) {
      setNotice("");
      setError(logs().length === 0 ? t("logs.error.noLogsToExport") : t("logs.error.noMatchToExport"));
      return;
    }
    setError("");
    const text = `${entries.map(formatLogLine).join("\n")}\n`;
    const blob = new Blob([text], { type: "text/plain;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `fastv-logs-${formatExportStamp(new Date())}.log`;
    document.body.appendChild(link);
    link.click();
    link.remove();
    window.setTimeout(() => URL.revokeObjectURL(url), 1000);
    flashNotice(t("logs.notice.exported", { count: entries.length, filename: link.download }));
  };

  const closeLogMoreMenu = (event: Event) => {
    const details = (event.currentTarget as HTMLElement).closest("details");
    details?.removeAttribute("open");
  };

  const proxyLabel = () => {
    switch (props.proxyState.status) {
      case "running": return t("logs.status.running", { addr: props.proxyState.localAddr });
      case "starting": return t("logs.status.starting");
      case "stopping": return t("logs.status.stopping");
      case "error": return t("logs.status.error");
      default: return t("logs.status.stopped");
    }
  };

  return (
    <div class="page-stack">
      <section class="log-toolbar panel">
        <div class="log-toolbar-left">
          <span classList={{ "status-dot": true, online: props.proxyState.status === "running", idle: props.proxyState.status !== "running" }} />
          <div><strong>{proxyLabel()}</strong><small>{t("logs.count", { total: logs().length, matched: filtered().length })}</small></div>
        </div>
        <div class="log-toolbar-right">
          <label class="log-search"><Icon name="search" size={14} /><input value={search()} onInput={(event) => setSearch(event.currentTarget.value)} placeholder={t("logs.searchPlaceholder")} /></label>
          <select value={level()} onChange={(event) => setLevel(event.currentTarget.value)}>
            <option value="">{t("logs.allLevels")}</option>
            <For each={levels}>{(item) => <option value={item}>{levelLabel(item)}</option>}</For>
          </select>
          <button classList={{ "btn": true, "secondary": true, active: autoScroll() }} type="button" onClick={() => setAutoScroll(!autoScroll())}><Icon name="chevron" size={13} class="rotate-down" /> {t("logs.autoScroll")}</button>
          <button classList={{ "btn": true, "secondary": true, active: paused() }} type="button" onClick={() => setPaused(!paused())}>{paused() ? t("logs.resume") : t("logs.pause")}</button>
          <button class="icon-button" type="button" onClick={load} disabled={loading()} aria-label={t("logs.refresh")}><Icon name="refresh" size={14} /></button>
          <details class="log-more">
            <summary class="btn ghost">{t("logs.more")}</summary>
            <div class="log-more-menu" role="menu">
              <button class="log-more-item" type="button" role="menuitem" disabled={filtered().length === 0} onClick={(event) => { closeLogMoreMenu(event); copyLogs(); }}>
                <Icon name="edit" size={14} /> {t("logs.copyFiltered")}
              </button>
              <button class="log-more-item" type="button" role="menuitem" onClick={(event) => { closeLogMoreMenu(event); copyAllLogs(); }}>
                <Icon name="edit" size={14} /> {t("logs.copyAll")}
              </button>
              <button class="log-more-item" type="button" role="menuitem" onClick={(event) => { closeLogMoreMenu(event); exportLogs(); }}>
                <Icon name="external" size={14} /> {t("logs.export")}
              </button>
              <button class="log-more-item danger" type="button" role="menuitem" disabled={logs().length === 0} onClick={(event) => { closeLogMoreMenu(event); clearLogs(); }}>
                <Icon name="trash" size={14} /> {t("logs.clear")}
              </button>
            </div>
          </details>
        </div>
      </section>

      <Show when={error()}>
        <div class="alert error"><span class="alert-message"><Icon name="alert" size={15} /> {error()}</span></div>
      </Show>
      <Show when={notice()}>
        <div class="alert success"><span class="alert-message"><Icon name="check" size={15} /> {notice()}</span></div>
      </Show>

      <section class="panel logs-panel">
        <div class="logs-head">
          <span>{t("logs.head.time")}</span>
          <span>{t("logs.head.level")}</span>
          <span>{t("logs.head.source")}</span>
          <span>{t("logs.head.message")}</span>
        </div>
        <div class="logs-view" ref={logView}>
          <Show when={!loading()} fallback={<div class="empty-state">{t("logs.loading")}</div>}>
            <Show when={filtered().length > 0} fallback={
              <div class="empty-state">
                <div class="empty-icon"><Icon name="logs" size={22} /></div>
                <strong>{logs().length === 0 ? t("logs.empty.noLogs") : t("logs.empty.noMatch")}</strong>
                <span>{logs().length === 0 ? t("logs.empty.noLogsHint") : t("logs.empty.noMatchHint")}</span>
                <div class="empty-actions">
                  <Show when={logs().length === 0} fallback={
                    <button class="btn ghost" type="button" onClick={() => setSearch("")}>{t("logs.empty.clearSearch")}</button>
                  }>
                    <button class="btn primary" type="button" onClick={() => props.onNavigate("dashboard")}>
                      <Icon name="dashboard" size={14} /> {t("logs.empty.goDashboard")}
                    </button>
                    <button class="btn ghost" type="button" onClick={() => props.onNavigate("settings")}>
                      <Icon name="settings" size={14} /> {t("logs.empty.openSettings")}
                    </button>
                  </Show>
                </div>
              </div>
            }>
              <For each={filtered()}>
                {(entry) => (
                  <div class="log-row">
                    <span class="log-time">{formatTime(entry.timestamp)}</span>
                    <span classList={{ "log-level": true, [entry.level]: true }}>{levelLabel(entry.level)}</span>
                    <span class="log-source">{entry.source}</span>
                    <span class="log-message">{entry.message}</span>
                  </div>
                )}
              </For>
            </Show>
          </Show>
        </div>
        <div class="logs-footer">
          <span>{paused() ? t("logs.footer.paused") : autoScroll() ? t("logs.footer.autoOn") : t("logs.footer.autoOff")}</span>
          <span>{t("logs.footer.limit")}</span>
        </div>
      </section>
    </div>
  );
}