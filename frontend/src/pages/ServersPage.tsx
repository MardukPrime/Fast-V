import { createMemo, createSignal, For, onMount, Show } from "solid-js";

import { NodeService } from "../../bindings/github.com/MardukPrime/Fast-V";
import type { NodeLatency, NodeView } from "../../bindings/github.com/MardukPrime/Fast-V";
import Icon from "../components/Icon";

type ServersProps = {
  onNavigate: (page: string) => void;
};

const protocolOptions = ["vmess", "vless", "trojan", "shadowsocks"];

export default function ServersPage(props: ServersProps) {
  const [nodes, setNodes] = createSignal<NodeView[]>([]);
  const [loading, setLoading] = createSignal(true);
  const [error, setError] = createSignal("");
  const [search, setSearch] = createSignal("");
  const [protocol, setProtocol] = createSignal("");
  const [subscriptionID, setSubscriptionID] = createSignal("");
  const [latencies, setLatencies] = createSignal<Record<string, NodeLatency>>({});
  const [testingID, setTestingID] = createSignal("");
  const [testingAll, setTestingAll] = createSignal(false);
  const [busyID, setBusyID] = createSignal("");

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      setNodes((await NodeService.List({ search: "", protocol: "", subscriptionId: "" })) ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  onMount(load);

  const subscriptions = createMemo(() => {
    const map = new Map<string, string>();
    for (const node of nodes()) {
      map.set(node.subscriptionId, node.subscriptionName || "未命名订阅");
    }
    return Array.from(map.entries()).map(([id, name]) => ({ id, name }));
  });

  const filteredNodes = createMemo(() => {
    const keyword = search().trim().toLowerCase();
    return nodes().filter((node) => {
      if (protocol() && node.protocol.toLowerCase() !== protocol()) return false;
      if (subscriptionID() && node.subscriptionId !== subscriptionID()) return false;
      if (!keyword) return true;
      const haystack = [node.name, node.protocol, node.address, node.subscriptionName].join(" ").toLowerCase();
      return haystack.includes(keyword);
    });
  });

  const activeCount = () => nodes().filter((node) => node.active).length;
  const enabledCount = () => nodes().filter((node) => node.enabled).length;

  const setActive = async (node: NodeView) => {
    if (!node.enabled) {
      setError("该节点已禁用，请先启用。");
      return;
    }
    setBusyID(node.id);
    setError("");
    try {
      await NodeService.SetActive(node.id);
      setNodes((current) => current.map((item) => ({ ...item, active: item.id === node.id })));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusyID("");
    }
  };

  const toggleEnabled = async (node: NodeView) => {
    setBusyID(node.id);
    setError("");
    try {
      const updated = await NodeService.SetEnabled(node.id, !node.enabled);
      setNodes((current) => current.map((item) => item.id === node.id ? updated : item));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusyID("");
    }
  };

  const removeNode = async (node: NodeView) => {
    if (!window.confirm(`确定删除节点「${node.name}」吗？`)) return;
    setBusyID(node.id);
    setError("");
    try {
      await NodeService.Delete(node.id);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusyID("");
    }
  };

  const applyLatencyResults = (results: NodeLatency[] | null) => {
    if (!results) return;
    setLatencies((current) => {
      const next = { ...current };
      for (const result of results) next[result.nodeId] = result;
      return next;
    });
  };

  const testNode = async (node: NodeView) => {
    setTestingID(node.id);
    setError("");
    try {
      applyLatencyResults(await NodeService.TestLatency([node.id]));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setTestingID("");
    }
  };

  const testAll = async () => {
    setTestingAll(true);
    setError("");
    try {
      applyLatencyResults(await NodeService.TestLatency(filteredNodes().map((node) => node.id)));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setTestingAll(false);
    }
  };

  const latencyLabel = (node: NodeView) => {
    const result = latencies()[node.id];
    if (!result) return <span class="latency unknown">未测试</span>;
    if (result.error) return <span class="latency bad" title={result.error}>失败</span>;
    return <span classList={{ "latency": true, good: result.latencyMs < 100, mid: result.latencyMs >= 100 && result.latencyMs < 300, bad: result.latencyMs >= 300 }}>{result.latencyMs} ms</span>;
  };

  return (
    <div class="page-stack">
      <section class="mini-stats">
        <div class="mini-stat"><span>全部节点</span><strong>{nodes().length}</strong><small>{enabledCount()} 个启用</small></div>
        <div class="mini-stat"><span>当前节点</span><strong>{activeCount()}</strong><small>已选择</small></div>
        <div class="mini-stat"><span>筛选结果</span><strong>{filteredNodes().length}</strong><small>当前列表</small></div>
      </section>

      <section class="panel node-panel">
        <div class="panel-heading node-heading">
          <div class="heading-with-icon">
            <span class="heading-icon cyan"><Icon name="server" size={17} /></span>
            <div>
              <h3>节点管理</h3>
              <span>选择当前节点、启用状态和 TCP 可达性测试</span>
            </div>
          </div>
          <button class="btn secondary" onClick={testAll} disabled={testingAll() || filteredNodes().length === 0}>
            <Icon name="refresh" size={14} /> {testingAll() ? "测试中..." : "测试当前列表"}
          </button>
        </div>

        <div class="node-toolbar">
          <label class="node-search">
            <Icon name="search" size={14} />
            <input
              value={search()}
              onInput={(event) => setSearch(event.currentTarget.value)}
              placeholder="搜索节点、地址或订阅"
            />
          </label>
          <select value={protocol()} onChange={(event) => setProtocol(event.currentTarget.value)}>
            <option value="">全部协议</option>
            <For each={protocolOptions}>{(item) => <option value={item}>{item}</option>}</For>
          </select>
          <select value={subscriptionID()} onChange={(event) => setSubscriptionID(event.currentTarget.value)}>
            <option value="">全部订阅</option>
            <For each={subscriptions()}>{(item) => <option value={item.id}>{item.name}</option>}</For>
          </select>
          <button class="btn ghost icon-only" onClick={load} disabled={loading()} aria-label="刷新节点">
            <Icon name="refresh" size={14} />
          </button>
        </div>

        <Show when={error()}>
          <div class="alert error"><span class="alert-message"><Icon name="alert" size={15} /> {error()}</span></div>
        </Show>

        <Show when={!loading()} fallback={<div class="empty-state">正在读取节点...</div>}>
          <Show when={filteredNodes().length > 0} fallback={
            <div class="empty-state">
              <div class="empty-icon"><Icon name="server" size={22} /></div>
              <strong>{nodes().length === 0 ? "还没有节点" : "没有匹配的节点"}</strong>
              <span>{nodes().length === 0 ? "添加订阅后，解析出的节点会显示在这里。" : "尝试调整搜索词或筛选条件。"}</span>
              <Show when={nodes().length === 0}>
                <button class="btn secondary" onClick={() => props.onNavigate("subscriptions")}>去添加订阅</button>
              </Show>
            </div>
          }>
            <div class="node-table">
              <div class="node-table-head">
                <span />
                <span>节点</span>
                <span>协议</span>
                <span>地址</span>
                <span>延迟</span>
                <span>状态</span>
                <span>操作</span>
              </div>
              <For each={filteredNodes()}>
                {(node) => (
                  <div classList={{ "node-row": true, active: node.active, disabled: !node.enabled }}>
                    <button
                      classList={{ "node-radio": true, selected: node.active }}
                      aria-label={node.active ? "当前节点" : `将 ${node.name} 设为当前节点`}
                      onClick={() => setActive(node)}
                      disabled={busyID() === node.id}
                    >
                      <span />
                    </button>
                    <div class="node-name">
                      <strong>{node.name}</strong>
                      <small>{node.subscriptionName}</small>
                    </div>
                    <span class="tag neutral">{node.protocol}</span>
                    <span class="node-address">{node.address}:{node.port}</span>
                    <span>{latencyLabel(node)}</span>
                    <button
                      classList={{ "state-pill": true, on: node.enabled }}
                      onClick={() => toggleEnabled(node)}
                      disabled={busyID() === node.id}
                    >
                      <span class="status-dot" />
                      {node.enabled ? "启用" : "禁用"}
                    </button>
                    <div class="node-actions">
                      <button class="btn secondary compact" onClick={() => testNode(node)} disabled={testingID() === node.id}>
                        <Icon name="speed" size={13} /> {testingID() === node.id ? "测试中" : "测速"}
                      </button>
                      <button class="icon-button danger" onClick={() => removeNode(node)} disabled={busyID() === node.id} aria-label={`删除 ${node.name}`}>
                        <Icon name="trash" size={14} />
                      </button>
                    </div>
                  </div>
                )}
              </For>
            </div>
          </Show>
        </Show>

        <p class="node-hint">
          延迟测试为直接 TCP 连通性测试，不等同于代理链路延迟。代理测试将在内核启动功能接入后提供。
        </p>
      </section>
    </div>
  );
}