import { createSignal, For, onMount, Show } from "solid-js";

import { NodeService, SubscriptionService } from "../../bindings/github.com/MardukPrime/Fast-V";
import type { NodeView, SubscriptionView } from "../../bindings/github.com/MardukPrime/Fast-V";
import Icon from "../components/Icon";

function formatTime(value: string) {
  if (!value) return "从未更新";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

export default function SubscriptionsPage() {
  const [subscriptions, setSubscriptions] = createSignal<SubscriptionView[]>([]);
  const [loading, setLoading] = createSignal(true);
  const [adding, setAdding] = createSignal(false);
  const [updatingId, setUpdatingId] = createSignal("");
  const [updatingAll, setUpdatingAll] = createSignal(false);
  const [name, setName] = createSignal("");
  const [url, setURL] = createSignal("");
  const [message, setMessage] = createSignal("");
  const [error, setError] = createSignal("");
  const [nodes, setNodes] = createSignal<NodeView[] | null>(null);
  const [nodesTitle, setNodesTitle] = createSignal("");

  const totalNodes = () => subscriptions().reduce((sum, sub) => sum + sub.nodeCount, 0);
  const activeCount = () => subscriptions().filter((sub) => sub.enabled).length;
  const failedCount = () => subscriptions().filter((sub) => Boolean(sub.lastError)).length;

  const load = async () => {
    setLoading(true);
    try {
      setSubscriptions((await SubscriptionService.List()) ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  onMount(load);

  const addSubscription = async (event: Event) => {
    event.preventDefault();
    setError("");
    setMessage("");
    if (!url().trim()) {
      setError("请输入订阅地址。");
      return;
    }
    setAdding(true);
    try {
      await SubscriptionService.Add({ name: name().trim(), url: url().trim() });
      setName("");
      setURL("");
      setMessage("订阅已添加并完成首次更新。");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setAdding(false);
    }
  };

  const updateSubscription = async (id: string) => {
    setError("");
    setMessage("");
    setUpdatingId(id);
    try {
      await SubscriptionService.Update(id);
      setMessage("订阅已更新。");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      await load();
    } finally {
      setUpdatingId("");
    }
  };

  const updateAll = async () => {
    setError("");
    setMessage("");
    setUpdatingAll(true);
    try {
      const result = await SubscriptionService.UpdateAll();
      setMessage(`已更新 ${result.updated} 个订阅，失败 ${result.failed} 个。`);
      if ((result.errors ?? []).length > 0) {
        setError((result.errors ?? []).join("；"));
      }
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setUpdatingAll(false);
    }
  };

  const removeSubscription = async (sub: SubscriptionView) => {
    if (!window.confirm(`确定删除订阅「${sub.name}」及其 ${sub.nodeCount} 个节点吗？`)) {
      return;
    }
    setError("");
    setMessage("");
    try {
      await SubscriptionService.Delete(sub.id);
      setMessage("订阅已删除。");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const setActiveNode = async (node: NodeView) => {
    setError("");
    try {
      await NodeService.SetActive(node.id);
      setNodes((current) => (current ?? []).map((item) => ({ ...item, active: item.id === node.id })));
      setMessage(`已将「${node.name}」设为当前节点。`);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const openNodes = async (sub: SubscriptionView) => {
    setError("");
    try {
      setNodes((await SubscriptionService.Nodes(sub.id)) ?? []);
      setNodesTitle(sub.name);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <div class="page-stack">
      <section class="mini-stats">
        <div class="mini-stat"><span>订阅</span><strong>{subscriptions().length}</strong><small>{activeCount()} 个启用</small></div>
        <div class="mini-stat"><span>节点</span><strong>{totalNodes()}</strong><small>已解析</small></div>
        <div class="mini-stat"><span>异常</span><strong classList={{ "text-danger": failedCount() > 0 }}>{failedCount()}</strong><small>需要处理</small></div>
      </section>

      <section class="panel add-panel">
        <div class="panel-heading">
          <div class="heading-with-icon">
            <span class="heading-icon cyan"><Icon name="plus" size={17} /></span>
            <div>
              <h3>添加订阅</h3>
              <span>支持 Base64 订阅和常见分享链接</span>
            </div>
          </div>
          <button class="btn secondary" onClick={updateAll} disabled={updatingAll() || subscriptions().length === 0}>
            <Icon name="refresh" size={14} /> {updatingAll() ? "更新中..." : "全部更新"}
          </button>
        </div>
        <form class="subscription-form" onSubmit={addSubscription}>
          <label>
            <span>名称（可选）</span>
            <input value={name()} onInput={(event) => setName(event.currentTarget.value)} placeholder="例如：主力机场" />
          </label>
          <label class="grow">
            <span>订阅地址</span>
            <input
              value={url()}
              onInput={(event) => setURL(event.currentTarget.value)}
              placeholder="https://example.com/subscribe?token=..."
              autocomplete="off"
            />
          </label>
          <button class="btn primary" type="submit" disabled={adding()}>
            <Icon name="plus" size={14} /> {adding() ? "添加中..." : "添加订阅"}
          </button>
        </form>
      </section>

      <Show when={message()}>
        <div class="alert success"><span class="alert-message"><Icon name="check" size={15} /> {message()}</span></div>
      </Show>
      <Show when={error()}>
        <div class="alert error"><span class="alert-message"><Icon name="alert" size={15} /> {error()}</span></div>
      </Show>

      <section class="panel">
        <div class="panel-heading">
          <div>
            <h3>订阅列表</h3>
            <span>{subscriptions().length} 个订阅 / {totalNodes()} 个节点</span>
          </div>
          <button class="btn ghost" onClick={load} disabled={loading()}>
            <Icon name="refresh" size={14} /> 刷新
          </button>
        </div>

        <Show when={!loading()} fallback={<div class="empty-state">正在读取订阅...</div>}>
          <Show when={subscriptions().length > 0} fallback={
            <div class="empty-state">
              <div class="empty-icon"><Icon name="subscription" size={24} /></div>
              <strong>还没有订阅</strong>
              <span>添加第一个订阅地址后，解析出的节点会显示在这里。</span>
            </div>
          }>
            <div class="subscription-list">
              <For each={subscriptions()}>
                {(sub) => (
                  <article class="subscription-card">
                    <div class="subscription-avatar"><Icon name="subscription" size={18} /></div>
                    <div class="subscription-main">
                      <div class="subscription-title">
                        <strong>{sub.name}</strong>
                        <span classList={{ "tag": true, danger: Boolean(sub.lastError), success: !sub.lastError }}>
                          {sub.lastError ? "异常" : "正常"}
                        </span>
                      </div>
                      <div class="subscription-host">{sub.host}</div>
                      <div class="subscription-meta">
                        <span><Icon name="nodes" size={12} /> {sub.nodeCount} 个节点</span>
                        <span><Icon name="refresh" size={12} /> {formatTime(sub.updatedAt)}</span>
                      </div>
                      <Show when={sub.lastError}>
                        <div class="inline-error"><Icon name="alert" size={12} /> {sub.lastError}</div>
                      </Show>
                    </div>
                    <div class="subscription-actions">
                      <button class="btn secondary" onClick={() => openNodes(sub)}>
                        <Icon name="nodes" size={14} /> 节点
                      </button>
                      <button class="btn secondary" onClick={() => updateSubscription(sub.id)} disabled={updatingId() === sub.id}>
                        <Icon name="refresh" size={14} /> {updatingId() === sub.id ? "更新中" : "更新"}
                      </button>
                      <button class="btn danger" onClick={() => removeSubscription(sub)} aria-label={`删除 ${sub.name}`}>
                        <Icon name="trash" size={14} />
                      </button>
                    </div>
                  </article>
                )}
              </For>
            </div>
          </Show>
        </Show>
      </section>

      <Show when={nodes() !== null}>
        <div class="modal-backdrop" onClick={(event) => { if (event.target === event.currentTarget) setNodes(null); }}>
          <div class="modal">
            <div class="modal-heading">
              <div>
                <h3>{nodesTitle()}</h3>
                <span>{(nodes() ?? []).length} 个节点</span>
              </div>
              <button class="icon-button" onClick={() => setNodes(null)} aria-label="关闭"><Icon name="close" size={18} /></button>
            </div>
            <div class="node-table-wrap">
              <Show when={(nodes() ?? []).length > 0} fallback={<div class="empty-state">没有解析到节点。</div>}>
                <table class="node-table">
                  <thead>
                    <tr><th>名称</th><th>协议</th><th>地址</th><th>端口</th><th>操作</th></tr>
                  </thead>
                  <tbody>
                    <For each={nodes() ?? []}>
                      {(node) => (
                        <tr>
                          <td>{node.name}</td>
                          <td><span class="tag neutral">{node.protocol}</span></td>
                          <td>{node.address}</td>
                          <td>{node.port}</td>
                          <td>
                            <Show when={node.active} fallback={
                              <button class="btn ghost compact" onClick={() => setActiveNode(node)} disabled={!node.enabled}>
                                {node.enabled ? "设为当前" : "已禁用"}
                              </button>
                            }>
                              <span class="status-label ok"><span class="status-dot" />当前</span>
                            </Show>
                          </td>
                        </tr>
                      )}
                    </For>
                  </tbody>
                </table>
              </Show>
            </div>
          </div>
        </div>
      </Show>
    </div>
  );
}
