import { createMemo, createSignal, For, onCleanup, onMount, Show } from "solid-js";

import { Events } from "@wailsio/runtime";

import { ProxyService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/proxy";
import type { LogEntry, ProxyState } from "../../bindings/github.com/MardukPrime/Fast-V/internal/model";
import Icon from "../components/Icon";

type LogsPageProps = {
  proxyState: ProxyState;
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
      setNotice("日志已清空。");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const copyLogs = async () => {
    if (filtered().length === 0) return;
    const text = filtered().map((entry) => `[${formatTime(entry.timestamp)}] [${levelLabel(entry.level)}] [${entry.source}] ${entry.message}`).join("\n");
    try {
      await navigator.clipboard.writeText(text);
      setNotice("当前筛选日志已复制。");
    } catch {
      setError("复制日志失败，请检查剪贴板权限。");
    }
  };

  const proxyLabel = () => {
    switch (props.proxyState.status) {
      case "running": return `代理运行中 · ${props.proxyState.localAddr}`;
      case "starting": return "代理启动中";
      case "stopping": return "代理停止中";
      case "error": return "代理异常";
      default: return "代理未启动";
    }
  };

  return (
    <div class="page-stack">
      <section class="log-toolbar panel">
        <div class="log-toolbar-left">
          <span classList={{ "status-dot": true, online: props.proxyState.status === "running", idle: props.proxyState.status !== "running" }} />
          <div><strong>{proxyLabel()}</strong><small>{logs().length} 条日志 · {filtered().length} 条匹配</small></div>
        </div>
        <div class="log-toolbar-right">
          <label class="log-search"><Icon name="search" size={14} /><input value={search()} onInput={(event) => setSearch(event.currentTarget.value)} placeholder="搜索日志内容" /></label>
          <select value={level()} onChange={(event) => setLevel(event.currentTarget.value)}>
            <option value="">全部等级</option>
            <For each={levels}>{(item) => <option value={item}>{levelLabel(item)}</option>}</For>
          </select>
          <button classList={{ "btn": true, "secondary": true, active: autoScroll() }} type="button" onClick={() => setAutoScroll(!autoScroll())}><Icon name="chevron" size={13} class="rotate-down" /> 自动滚动</button>
          <button classList={{ "btn": true, "secondary": true, active: paused() }} type="button" onClick={() => setPaused(!paused())}>{paused() ? "继续" : "暂停"}</button>
          <button class="icon-button" type="button" onClick={load} disabled={loading()} aria-label="刷新日志"><Icon name="refresh" size={14} /></button>
          <button class="icon-button" type="button" onClick={copyLogs} disabled={filtered().length === 0} aria-label="复制日志"><Icon name="edit" size={14} /></button>
          <button class="btn ghost" type="button" onClick={clearLogs} disabled={logs().length === 0}>清空</button>
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
          <span>时间</span>
          <span>等级</span>
          <span>来源</span>
          <span>日志内容</span>
        </div>
        <div class="logs-view" ref={logView}>
          <Show when={!loading()} fallback={<div class="empty-state">正在读取日志...</div>}>
            <Show when={filtered().length > 0} fallback={
              <div class="empty-state">
                <div class="empty-icon"><Icon name="logs" size={22} /></div>
                <strong>{logs().length === 0 ? "还没有内核日志" : "没有匹配的日志"}</strong>
                <span>{logs().length === 0 ? "启动代理后，Xray 的运行日志会显示在这里。" : "尝试调整搜索关键词或等级筛选。"}</span>
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
          <span>{paused() ? "已暂停接收新日志" : autoScroll() ? "自动滚动已开启" : "自动滚动已关闭"}</span>
          <span>最多保留 1500 条</span>
        </div>
      </section>
    </div>
  );
}