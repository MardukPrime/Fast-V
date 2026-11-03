import { createMemo, createSignal, For, onMount, Show } from "solid-js";

import { NodeService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/node";
import { ProxyService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/proxy";
import type { NodeConfig, NodeConfigPreview, NodeLatency, NodeView } from "../../bindings/github.com/MardukPrime/Fast-V/internal/model";
import Icon from "../components/Icon";

type ServersProps = {
  onNavigate: (page: string) => void;
};

type EditorTab = "form" | "json";

const protocolOptions = ["vmess", "vless", "trojan", "shadowsocks"];
const networkOptions = [
  { value: "raw", label: "RAW / TCP" },
  { value: "websocket", label: "WebSocket" },
  { value: "grpc", label: "gRPC" },
  { value: "httpupgrade", label: "HTTPUpgrade" },
  { value: "xhttp", label: "XHTTP" },
];
const tlsOptions = [
  { value: "none", label: "无 TLS" },
  { value: "tls", label: "TLS" },
  { value: "reality", label: "REALITY" },
];

function protocolLabel(protocol: string) {
  return protocol === "shadowsocks" ? "Shadowsocks" : protocol.toUpperCase();
}

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

  const [editorConfig, setEditorConfig] = createSignal<NodeConfig | null>(null);
  const [editorPreview, setEditorPreview] = createSignal<NodeConfigPreview | null>(null);
  const [editorTab, setEditorTab] = createSignal<EditorTab>("form");
  const [editorBusy, setEditorBusy] = createSignal(false);
  const [editorError, setEditorError] = createSignal("");
  const [editorMessage, setEditorMessage] = createSignal("");

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

  const activeNodeID = createMemo(() => nodes().find((node) => node.active)?.id ?? "");
  const activeCount = () => nodes().filter((node) => node.active).length;
  const enabledCount = () => nodes().filter((node) => node.enabled).length;

  const updateNodeFlags = (id: string) => {
    setNodes((current) => current.map((item) => ({ ...item, active: item.id === id })));
  };

  const switchActiveNode = async (id: string) => {
    if (!id) return;
    const node = nodes().find((item) => item.id === id);
    if (!node || !node.enabled) {
      setError("该节点已禁用，请先启用。");
      return;
    }
    setBusyID(id);
    setError("");
    try {
      await ProxyService.SwitchNode(id);
      updateNodeFlags(id);
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

  const openEditor = async (node: NodeView) => {
    setEditorError("");
    setEditorMessage("");
    setEditorTab("form");
    setEditorPreview(null);
    try {
      const config = await NodeService.Config(node.id);
      const preview = await NodeService.Preview(config);
      if (config.outboundJson.trim()) {
        setEditorPreview({ ...preview, outboundJson: config.outboundJson });
      } else {
        setEditorPreview(preview);
      }
      setEditorConfig(config);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const closeEditor = () => {
    if (editorBusy()) return;
    setEditorConfig(null);
    setEditorPreview(null);
    setEditorError("");
    setEditorMessage("");
  };

  const updateEditor = <K extends keyof NodeConfig>(key: K, value: NodeConfig[K]) => {
    const current = editorConfig();
    if (!current) return;
    setEditorConfig({ ...current, [key]: value });
    setEditorPreview(null);
  };

  const updateALPN = (value: string) => {
    updateEditor("alpn", value.split(",").map((item) => item.trim()).filter(Boolean));
  };

  const refreshPreview = async () => {
    const config = editorConfig();
    if (!config) return;
    setEditorError("");
    try {
      setEditorPreview(await NodeService.Preview(config));
    } catch (err) {
      setEditorError(err instanceof Error ? err.message : String(err));
    }
  };

  const switchTab = (tab: EditorTab) => {
    setEditorTab(tab);
    if (tab === "json") {
      const config = editorConfig();
      if (config && !config.outboundJson && editorPreview()) {
        updateEditor("outboundJson", editorPreview()!.outboundJson);
      }
    }
  };

  const generateJSON = async () => {
    await refreshPreview();
    const preview = editorPreview();
    if (preview) updateEditor("outboundJson", preview.outboundJson);
  };

  const saveEditor = async (apply: boolean) => {
    const config = editorConfig();
    if (!config) return;
    setEditorBusy(true);
    setEditorError("");
    setEditorMessage("");
    try {
      const input: NodeConfig = {
        ...config,
        outboundJson: editorTab() === "json" ? config.outboundJson.trim() : "",
      };
      const saved = await NodeService.SaveConfig(input);
      if (apply) {
        await ProxyService.SwitchNode(saved.id);
      }
      await load();
      setEditorConfig(null);
      setEditorPreview(null);
      setEditorMessage("");
    } catch (err) {
      setEditorError(err instanceof Error ? err.message : String(err));
    } finally {
      setEditorBusy(false);
    }
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
              <span>编辑节点配置、快速切换当前节点和测试连通性</span>
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
          <select class="quick-switch-select" value={activeNodeID()} onChange={(event) => switchActiveNode(event.currentTarget.value)} disabled={nodes().length === 0 || busyID() !== ""}>
            <option value="">快速切换节点</option>
            <For each={nodes().filter((node) => node.enabled)}>
              {(node) => <option value={node.id} selected={node.id === activeNodeID()}>{node.active ? "✓ " : ""}{node.name}</option>}
            </For>
          </select>
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
                      onClick={() => switchActiveNode(node.id)}
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
                      <button class="icon-button" onClick={() => openEditor(node)} disabled={busyID() === node.id} aria-label={`编辑 ${node.name}`}>
                        <Icon name="edit" size={14} />
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
          延迟测试为直接 TCP 连通性测试，不等同于代理链路延迟。切换节点时会自动重启正在运行的代理。
        </p>
      </section>

      <Show when={editorConfig()}>
        {(config) => (
          <div class="modal-backdrop" onClick={(event) => { if (event.target === event.currentTarget) closeEditor(); }}>
            <div class="modal node-editor-modal">
              <div class="modal-heading">
                <div>
                  <h3>编辑节点配置</h3>
                  <span>{protocolLabel(config().protocol)} · {config().address}:{config().port}</span>
                </div>
                <button class="icon-button" onClick={closeEditor} aria-label="关闭"><Icon name="close" size={18} /></button>
              </div>

              <div class="node-editor-layout">
                <div class="node-editor-main">
                  <div class="node-editor-tabs">
                    <button classList={{ "node-editor-tab": true, active: editorTab() === "form" }} onClick={() => switchTab("form")}>表单配置</button>
                    <button classList={{ "node-editor-tab": true, active: editorTab() === "json" }} onClick={() => switchTab("json")}>Xray JSON</button>
                  </div>

                  <Show when={editorTab() === "form"} fallback={
                    <div class="node-json-editor">
                      <div class="node-editor-note">
                        <Icon name="external" size={13} />
                        <span>高级 JSON 直接作为 outbound 使用。清空后保存会恢复为表单生成配置。</span>
                      </div>
                      <textarea
                        value={config().outboundJson}
                        onInput={(event) => updateEditor("outboundJson", event.currentTarget.value)}
                        spellcheck={false}
                        placeholder="留空时使用表单生成 Xray outbound"
                      />
                      <button class="btn secondary compact" onClick={generateJSON} type="button">
                        <Icon name="refresh" size={13} /> 从当前表单生成 JSON
                      </button>
                    </div>
                  }>
                    <div class="node-editor-form">
                      <div class="node-editor-section">
                        <div class="node-editor-section-title">基础信息</div>
                        <div class="node-editor-grid two">
                          <label><span>节点名称</span><input value={config().name} onInput={(event) => updateEditor("name", event.currentTarget.value)} /></label>
                          <label><span>协议</span><select value={config().protocol} onChange={(event) => updateEditor("protocol", event.currentTarget.value)}>
                            <For each={protocolOptions}>{(item) => <option value={item}>{protocolLabel(item)}</option>}</For>
                          </select></label>
                          <label><span>服务器地址</span><input value={config().address} onInput={(event) => updateEditor("address", event.currentTarget.value)} placeholder="example.com 或 1.2.3.4" /></label>
                          <label><span>端口</span><input type="number" min="1" max="65535" value={config().port} onInput={(event) => updateEditor("port", Number(event.currentTarget.value))} /></label>
                        </div>
                      </div>

                      <div class="node-editor-section">
                        <div class="node-editor-section-title">凭证</div>
                        <div class="node-editor-grid two">
                          <Show when={config().protocol === "vmess" || config().protocol === "vless"}>
                            <label><span>{config().protocol === "vmess" ? "UUID" : "用户 ID"}</span><input value={config().uuid} onInput={(event) => updateEditor("uuid", event.currentTarget.value)} /></label>
                          </Show>
                          <Show when={config().protocol === "vless"}>
                            <label><span>Flow</span><input value={config().flow} onInput={(event) => updateEditor("flow", event.currentTarget.value)} placeholder="xtls-rprx-vision" /></label>
                          </Show>
                          <Show when={config().protocol === "vmess"}>
                            <label><span>加密方式</span><input value={config().security} onInput={(event) => updateEditor("security", event.currentTarget.value)} placeholder="auto" /></label>
                          </Show>
                          <Show when={config().protocol === "trojan" || config().protocol === "shadowsocks"}>
                            <label><span>密码</span><input type="password" value={config().password} onInput={(event) => updateEditor("password", event.currentTarget.value)} /></label>
                          </Show>
                          <Show when={config().protocol === "shadowsocks"}>
                            <label><span>加密方式</span><input value={config().method} onInput={(event) => updateEditor("method", event.currentTarget.value)} placeholder="aes-256-gcm" /></label>
                          </Show>
                        </div>
                      </div>

                      <Show when={config().protocol !== "shadowsocks"}>
                        <div class="node-editor-section">
                          <div class="node-editor-section-title">传输与安全</div>
                          <div class="node-editor-grid two">
                            <label><span>传输方式</span><select value={config().network} onChange={(event) => updateEditor("network", event.currentTarget.value)}>
                              <For each={networkOptions}>{(item) => <option value={item.value}>{item.label}</option>}</For>
                            </select></label>
                            <label><span>安全层</span><select value={config().tls} onChange={(event) => updateEditor("tls", event.currentTarget.value)}>
                              <For each={tlsOptions}>{(item) => <option value={item.value}>{item.label}</option>}</For>
                            </select></label>
                            <label><span>SNI / Server Name</span><input value={config().serverName} onInput={(event) => updateEditor("serverName", event.currentTarget.value)} /></label>
                            <label><span>Fingerprint</span><input value={config().fingerprint} onInput={(event) => updateEditor("fingerprint", event.currentTarget.value)} placeholder="chrome / firefox" /></label>
                            <label><span>Host</span><input value={config().host} onInput={(event) => updateEditor("host", event.currentTarget.value)} /></label>
                            <label><span>Path</span><input value={config().path} onInput={(event) => updateEditor("path", event.currentTarget.value)} placeholder="/ws" /></label>
                            <label><span>gRPC Service Name</span><input value={config().serviceName} onInput={(event) => updateEditor("serviceName", event.currentTarget.value)} /></label>
                            <label><span>ALPN</span><input value={config().alpn?.join(", ") ?? ""} onInput={(event) => updateALPN(event.currentTarget.value)} placeholder="h2, http/1.1" /></label>
                          </div>
                          <Show when={config().tls === "reality"}>
                            <div class="node-editor-grid three">
                              <label><span>REALITY Public Key</span><input value={config().publicKey} onInput={(event) => updateEditor("publicKey", event.currentTarget.value)} /></label>
                              <label><span>Short ID</span><input value={config().shortId} onInput={(event) => updateEditor("shortId", event.currentTarget.value)} /></label>
                              <label><span>SpiderX</span><input value={config().spiderX} onInput={(event) => updateEditor("spiderX", event.currentTarget.value)} placeholder="/" /></label>
                            </div>
                          </Show>
                          <label class="switch-row node-editor-switch">
                            <span><strong>允许不安全 TLS</strong><small>仅用于测试自签名证书，不建议长期开启。</small></span>
                            <span classList={{ "switch": true, on: config().allowInsecure }}>
                              <input type="checkbox" checked={config().allowInsecure} onChange={(event) => updateEditor("allowInsecure", event.currentTarget.checked)} />
                              <span class="switch-track"><span class="switch-thumb" /></span>
                            </span>
                          </label>
                        </div>
                      </Show>
                    </div>
                  </Show>
                </div>

                <aside class="node-editor-side">
                  <div class="node-editor-preview-head">
                    <div><strong>Xray outbound 预览</strong><span>可直接参考 Xray 官方字段结构</span></div>
                    <button class="icon-button" onClick={refreshPreview} aria-label="刷新预览"><Icon name="refresh" size={14} /></button>
                  </div>
                  <pre class="node-config-preview">{editorPreview()?.outboundJson ?? config().outboundJson ?? "配置已修改，点击刷新预览。"}</pre>
                  <div class="node-editor-shortcut">
                    <Icon name="edit" size={13} />
                    <span>保存并应用会立即切换当前节点；代理运行中会自动重启。</span>
                  </div>
                </aside>
              </div>

              <Show when={editorError()}>
                <div class="alert error node-editor-alert"><span class="alert-message"><Icon name="alert" size={14} /> {editorError()}</span></div>
              </Show>
              <Show when={editorMessage()}>
                <div class="alert success node-editor-alert"><span class="alert-message"><Icon name="check" size={14} /> {editorMessage()}</span></div>
              </Show>

              <div class="modal-footer">
                <button class="btn ghost" type="button" onClick={closeEditor} disabled={editorBusy()}>取消</button>
                <button class="btn secondary" type="button" onClick={() => saveEditor(false)} disabled={editorBusy()}>
                  <Icon name="check" size={14} /> {editorBusy() ? "保存中..." : "保存"}
                </button>
                <button class="btn primary" type="button" onClick={() => saveEditor(true)} disabled={editorBusy()}>
                  <Icon name="play" size={14} /> 保存并应用
                </button>
              </div>
            </div>
          </div>
        )}
      </Show>
    </div>
  );
}