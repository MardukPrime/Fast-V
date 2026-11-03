import { createSignal, For, onMount, Show } from "solid-js";

import { ProxyService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/proxy";
import { Service as RoutingService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/routing";
import type { ProxyState, RoutingRule } from "../../bindings/github.com/MardukPrime/Fast-V/internal/model";
import Icon from "../components/Icon";

type RoutingPageProps = {
  proxyState: ProxyState;
  proxyMode: string;
};

const ruleTypes = [
  { value: "domain_suffix", label: "域名后缀", hint: "例如 example.com，匹配所有子域名" },
  { value: "domain_keyword", label: "域名关键字", hint: "匹配域名中的关键字" },
  { value: "domain_regex", label: "域名正则", hint: "使用 Xray regexp: 规则" },
  { value: "ip", label: "IP / CIDR", hint: "例如 10.0.0.0/8，可逗号分隔多个" },
  { value: "geosite", label: "GeoSite", hint: "例如 category-ads-all、cn" },
  { value: "geoip", label: "GeoIP", hint: "例如 private、cn" },
  { value: "port", label: "端口", hint: "例如 80,443,1000-2000" },
  { value: "network", label: "网络协议", hint: "tcp、udp 或 tcp,udp" },
];

const actions = [
  { value: "proxy", label: "走代理", hint: "proxy outbound", tone: "accent" },
  { value: "direct", label: "直连", hint: "direct outbound", tone: "success" },
  { value: "block", label: "拦截", hint: "block outbound", tone: "danger" },
];

function typeLabel(type: string) {
  return ruleTypes.find((item) => item.value === type)?.label ?? type;
}

function actionLabel(action: string) {
  return actions.find((item) => item.value === action)?.label ?? action;
}

function emptyRule(): RoutingRule {
  return { id: "", name: "", type: "domain_suffix", value: "", action: "direct", enabled: true };
}

export default function RoutingPage(props: RoutingPageProps) {
  const [rules, setRules] = createSignal<RoutingRule[]>([]);
  const [loading, setLoading] = createSignal(true);
  const [error, setError] = createSignal("");
  const [message, setMessage] = createSignal("");
  const [editing, setEditing] = createSignal<RoutingRule | null>(null);
  const [busy, setBusy] = createSignal(false);

  const load = async () => {
    setLoading(true);
    try {
      setRules((await RoutingService.List()) ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  onMount(load);

  const restartIfRunning = async () => {
    if (props.proxyState.status === "running" || props.proxyState.status === "starting") {
      setMessage("正在重启代理以应用路由规则...");
      await ProxyService.Restart();
      setMessage("路由规则已保存并应用。");
    } else {
      setMessage("路由规则已保存，将在代理启动时生效。");
    }
  };

  const openNew = () => {
    setError("");
    setMessage("");
    setEditing(emptyRule());
  };

  const openEdit = (rule: RoutingRule) => {
    setError("");
    setMessage("");
    setEditing({ ...rule });
  };

  const updateEditing = <K extends keyof RoutingRule>(key: K, value: RoutingRule[K]) => {
    const current = editing();
    if (!current) return;
    setEditing({ ...current, [key]: value });
  };

  const saveRule = async (event: Event) => {
    event.preventDefault();
    const current = editing();
    if (!current) return;
    setBusy(true);
    setError("");
    setMessage("");
    try {
      await RoutingService.Save(current);
      setEditing(null);
      await load();
      await restartIfRunning();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const toggleRule = async (rule: RoutingRule) => {
    setBusy(true);
    setError("");
    try {
      await RoutingService.Save({ ...rule, enabled: !rule.enabled });
      await load();
      await restartIfRunning();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const deleteRule = async (rule: RoutingRule) => {
    if (!window.confirm(`确定删除规则「${rule.name}」吗？`)) return;
    setBusy(true);
    setError("");
    try {
      await RoutingService.Delete(rule.id);
      await load();
      await restartIfRunning();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const moveRule = async (index: number, direction: -1 | 1) => {
    const current = rules();
    const target = index + direction;
    if (target < 0 || target >= current.length) return;
    const next = [...current];
    [next[index], next[target]] = [next[target], next[index]];
    setBusy(true);
    setError("");
    try {
      await RoutingService.Reorder(next.map((rule) => rule.id));
      setRules(next);
      await restartIfRunning();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const resetRules = async () => {
    if (!window.confirm("恢复默认路由规则？当前自定义规则会被替换。")) return;
    setBusy(true);
    setError("");
    try {
      setRules((await RoutingService.Reset()) ?? []);
      await restartIfRunning();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const modeLabel = () => {
    switch (props.proxyMode) {
      case "global": return "全局代理";
      case "direct": return "全部直连";
      default: return "规则分流";
    }
  };

  return (
    <div class="page-stack">
      <section class="mini-stats">
        <div class="mini-stat"><span>自定义规则</span><strong>{rules().length}</strong><small>按顺序匹配</small></div>
        <div class="mini-stat"><span>已启用</span><strong>{rules().filter((rule) => rule.enabled).length}</strong><small>生效中</small></div>
        <div class="mini-stat"><span>代理模式</span><strong class="routing-mode-value">{modeLabel()}</strong><small>未命中的流量</small></div>
      </section>

      <section class="panel routing-panel">
        <div class="panel-heading node-heading">
          <div class="heading-with-icon">
            <span class="heading-icon cyan"><Icon name="route" size={17} /></span>
            <div>
              <h3>路由规则</h3>
              <span>规则从上到下匹配，命中后停止；未命中流量按当前代理模式处理</span>
            </div>
          </div>
          <div class="routing-heading-actions">
            <button class="btn ghost" type="button" onClick={resetRules} disabled={busy()}>恢复默认</button>
            <button class="btn primary" type="button" onClick={openNew} disabled={busy()}><Icon name="plus" size={14} /> 添加规则</button>
          </div>
        </div>

        <div class="routing-note">
          <Icon name="route" size={13} />
          <span>当前代理模式：{modeLabel()}。代理运行中修改规则会自动重启内核，配置立即生效。</span>
        </div>

        <Show when={error()}>
          <div class="alert error"><span class="alert-message"><Icon name="alert" size={15} /> {error()}</span></div>
        </Show>
        <Show when={message()}>
          <div class="alert success"><span class="alert-message"><Icon name="check" size={15} /> {message()}</span></div>
        </Show>

        <Show when={!loading()} fallback={<div class="empty-state">正在读取路由规则...</div>}>
          <Show when={rules().length > 0} fallback={
            <div class="empty-state">
              <div class="empty-icon"><Icon name="route" size={22} /></div>
              <strong>还没有自定义路由规则</strong>
              <span>未设置时使用代理模式的默认路由。可以恢复默认规则或添加第一条规则。</span>
              <div class="routing-empty-actions">
                <button class="btn secondary" type="button" onClick={resetRules} disabled={busy()}>使用默认规则</button>
                <button class="btn primary" type="button" onClick={openNew} disabled={busy()}><Icon name="plus" size={14} /> 添加规则</button>
              </div>
            </div>
          }>
            <div class="routing-table">
              <div class="routing-table-head">
                <span>顺序</span>
                <span>规则</span>
                <span>类型</span>
                <span>内容</span>
                <span>动作</span>
                <span>状态</span>
                <span>操作</span>
              </div>
              <For each={rules()}>
                {(rule, index) => (
                  <div classList={{ "routing-row": true, disabled: !rule.enabled }}>
                    <span class="routing-order">{index() + 1}</span>
                    <div class="routing-name"><strong>{rule.name}</strong><small>{rule.id === "" ? "未保存" : "用户规则"}</small></div>
                    <span class="tag neutral">{typeLabel(rule.type)}</span>
                    <span class="routing-value" title={rule.value}>{rule.value}</span>
                    <span classList={{ "routing-action": true, direct: rule.action === "direct", block: rule.action === "block" }}>{actionLabel(rule.action)}</span>
                    <button classList={{ "state-pill": true, on: rule.enabled }} type="button" onClick={() => toggleRule(rule)} disabled={busy()}>
                      <span class="status-dot" /> {rule.enabled ? "启用" : "禁用"}
                    </button>
                    <div class="routing-actions">
                      <button class="icon-button" type="button" onClick={() => moveRule(index(), -1)} disabled={busy() || index() === 0} aria-label="上移"><Icon name="chevron" size={13} class="rotate-up" /></button>
                      <button class="icon-button" type="button" onClick={() => moveRule(index(), 1)} disabled={busy() || index() === rules().length - 1} aria-label="下移"><Icon name="chevron" size={13} class="rotate-down" /></button>
                      <button class="icon-button" type="button" onClick={() => openEdit(rule)} disabled={busy()} aria-label={`编辑 ${rule.name}`}><Icon name="edit" size={14} /></button>
                      <button class="icon-button danger" type="button" onClick={() => deleteRule(rule)} disabled={busy()} aria-label={`删除 ${rule.name}`}><Icon name="trash" size={14} /></button>
                    </div>
                  </div>
                )}
              </For>
            </div>
          </Show>
        </Show>
      </section>

      <Show when={editing()}>
        {(rule) => (
          <div class="modal-backdrop" onClick={(event) => { if (event.target === event.currentTarget && !busy()) setEditing(null); }}>
            <form class="modal routing-editor-modal" onSubmit={saveRule}>
              <div class="modal-heading">
                <div><h3>{rule().id ? "编辑路由规则" : "添加路由规则"}</h3><span>规则按列表顺序进入 Xray routing.rules</span></div>
                <button class="icon-button" type="button" onClick={() => setEditing(null)} disabled={busy()} aria-label="关闭"><Icon name="close" size={18} /></button>
              </div>
              <div class="routing-editor-body">
                <label><span>规则名称</span><input value={rule().name} onInput={(event) => updateEditing("name", event.currentTarget.value)} placeholder="例如：广告拦截" /></label>
                <div class="routing-editor-grid">
                  <label><span>规则类型</span><select value={rule().type} onChange={(event) => updateEditing("type", event.currentTarget.value)}>
                    <For each={ruleTypes}>{(item) => <option value={item.value}>{item.label}</option>}</For>
                  </select></label>
                  <label><span>动作</span><select value={rule().action} onChange={(event) => updateEditing("action", event.currentTarget.value)}>
                    <For each={actions}>{(item) => <option value={item.value}>{item.label}</option>}</For>
                  </select></label>
                </div>
                <label><span>规则内容</span><textarea value={rule().value} onInput={(event) => updateEditing("value", event.currentTarget.value)} placeholder={ruleTypes.find((item) => item.value === rule().type)?.hint} spellcheck={false} /></label>
                <div class="routing-editor-hint">
                  <Icon name="external" size={13} /> {ruleTypes.find((item) => item.value === rule().type)?.hint}
                </div>
                <label class="switch-row">
                  <span><strong>启用规则</strong><small>关闭后保留配置但不参与路由匹配。</small></span>
                  <span classList={{ "switch": true, on: rule().enabled }}>
                    <input type="checkbox" checked={rule().enabled} onChange={(event) => updateEditing("enabled", event.currentTarget.checked)} />
                    <span class="switch-track"><span class="switch-thumb" /></span>
                  </span>
                </label>
              </div>
              <div class="modal-footer">
                <button class="btn ghost" type="button" onClick={() => setEditing(null)} disabled={busy()}>取消</button>
                <button class="btn primary" type="submit" disabled={busy()}><Icon name="check" size={14} /> {busy() ? "保存中..." : "保存规则"}</button>
              </div>
            </form>
          </div>
        )}
      </Show>
    </div>
  );
}