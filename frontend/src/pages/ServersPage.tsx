import { createEffect, createMemo, createSignal, For, onCleanup, onMount, Show } from "solid-js";

import { NodeService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/node";
import { ProxyService } from "../../bindings/github.com/MardukPrime/Fast-V/internal/service/proxy";
import type { BatchResult, NodeConfig, NodeConfigPreview, NodeImportResult, NodeLatency, NodeView } from "../../bindings/github.com/MardukPrime/Fast-V/internal/model";
import Icon from "../components/Icon";
import QRModal, { QRCodeIcon } from "../components/QRModal";
import "./servers.css";

type ServersProps = {
  onNavigate: (page: string) => void;
};

type EditorTab = "form" | "json";

type SortMode = "default" | "name" | "latency" | "favorite";

type BatchAction = "enable" | "disable" | "copy" | "delete" | "tag";

const sortOptions: { value: SortMode; label: string }[] = [
  { value: "default", label: "默认顺序" },
  { value: "name", label: "按名称" },
  { value: "latency", label: "按延迟" },
  { value: "favorite", label: "收藏优先" },
];

const isSortMode = (value: string): value is SortMode => sortOptions.some((option) => option.value === value);

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

// Mirrors store.NormalizeTag on the backend: a tag is trimmed, has at most 24
// characters (runes) and must not contain newlines or control characters.
const TAG_MAX_LENGTH = 24;

const tagCharCount = (value: string) => Array.from(value.trim()).length;

function validateNodeTag(value: string): string {
  for (const char of value) {
    const code = char.codePointAt(0) ?? 0;
    if (char === "\n" || char === "\r" || code < 0x20 || code === 0x7f) {
      return "标签不能包含换行或控制字符";
    }
  }
  if (tagCharCount(value) > TAG_MAX_LENGTH) {
    return `标签最多 ${TAG_MAX_LENGTH} 个字符`;
  }
  return "";
}

function CopyLinkIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <rect x="9" y="9" width="11" height="11" rx="2" />
      <path d="M5 15V6a2 2 0 0 1 2-2h9" />
    </svg>
  );
}

function PasteIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <rect x="9" y="4" width="11" height="16" rx="2" />
      <path d="M9 6H6a2 2 0 0 0-2 2v11a2 2 0 0 0 2 2h9" />
      <path d="M13 3v3" />
    </svg>
  );
}

function FavoriteStarIcon(props: { filled: boolean }) {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill={props.filled ? "currentColor" : "none"} stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <path d="M12 3.4l2.65 5.37 5.93.86-4.29 4.18 1.01 5.9L12 17.03l-5.3 2.68 1.01-5.9-4.29-4.18 5.93-.86z" />
    </svg>
  );
}

export default function ServersPage(props: ServersProps) {
  const [nodes, setNodes] = createSignal<NodeView[]>([]);
  const [loading, setLoading] = createSignal(true);
  const [error, setError] = createSignal("");
  const [search, setSearch] = createSignal("");
  const [protocol, setProtocol] = createSignal("");
  const [subscriptionID, setSubscriptionID] = createSignal("");
  const [tagFilter, setTagFilter] = createSignal("");
  // Tag dictionary from NodeService.Tags(); refreshed with every node reload.
  const [tags, setTags] = createSignal<string[]>([]);
  const [latencies, setLatencies] = createSignal<Record<string, NodeLatency>>({});
  // Real proxy-chain HTTP latencies (NodeService.TestURL). They take priority
  // over the TCP reachability values above in the latency column.
  const [urlLatencies, setUrlLatencies] = createSignal<Record<string, NodeLatency>>({});
  const [testingID, setTestingID] = createSignal("");
  const [testingAll, setTestingAll] = createSignal(false);
  const [busyID, setBusyID] = createSignal("");
  const [sortMode, setSortMode] = createSignal<SortMode>("default");

  // Batch node selection and sharing QR dialog state.
  const [selectedIDs, setSelectedIDs] = createSignal<Set<string>>(new Set());
  const [batchBusy, setBatchBusy] = createSignal<BatchAction | "">("");
  const [qrOpen, setQROpen] = createSignal(false);
  const [qrTitle, setQRTitle] = createSignal("");
  const [qrSubtitle, setQRSubtitle] = createSignal("");
  const [qrLink, setQRLink] = createSignal("");
  const [qrImage, setQRImage] = createSignal("");
  const [qrHint, setQRHint] = createSignal("");
  const [qrLoading, setQRLoading] = createSignal(false);
  const [qrError, setQRError] = createSignal("");
  const [qrTarget, setQRTarget] = createSignal<NodeView | null>(null);

  let pageNoticeTimer: number | undefined;
  // Only the latest QR request is allowed to update the dialog (the user may
  // close it or open another node while the previous request is in flight).
  let qrRequest = 0;
  let selectAllRef: HTMLInputElement | undefined;

  const [importOpen, setImportOpen] = createSignal(false);
  const [importText, setImportText] = createSignal("");
  const [importBusy, setImportBusy] = createSignal(false);
  const [importError, setImportError] = createSignal("");
  const [importResult, setImportResult] = createSignal<NodeImportResult | null>(null);
  const [pageNotice, setPageNotice] = createSignal("");

  // Shared tag editor dialog: either one node (tagEditorNode) or the current
  // batch selection (tagEditorIDs). Both paths submit to the tag service.
  const [tagEditorOpen, setTagEditorOpen] = createSignal(false);
  const [tagEditorNode, setTagEditorNode] = createSignal<NodeView | null>(null);
  const [tagEditorIDs, setTagEditorIDs] = createSignal<string[]>([]);
  const [tagValue, setTagValue] = createSignal("");
  const [tagError, setTagError] = createSignal("");
  const [tagBusy, setTagBusy] = createSignal(false);

  // URL latency tests run a temporary Xray-core process and may take seconds.
  // Never touch signals after the page has been disposed.
  let alive = true;

  onCleanup(() => {
    alive = false;
    if (pageNoticeTimer !== undefined) window.clearTimeout(pageNoticeTimer);
  });

  const [editorConfig, setEditorConfig] = createSignal<NodeConfig | null>(null);
  const [editorPreview, setEditorPreview] = createSignal<NodeConfigPreview | null>(null);
  const [editorTab, setEditorTab] = createSignal<EditorTab>("form");
  const [editorBusy, setEditorBusy] = createSignal(false);
  const [editorError, setEditorError] = createSignal("");
  const [editorMessage, setEditorMessage] = createSignal("");

  // The tag dictionary is best-effort: a failed call must neither hide nodes nor
  // surface a page error, the next reload simply retries.
  const refreshTags = async () => {
    try {
      const list = await NodeService.Tags();
      if (!alive) return;
      const next = (list ?? []).filter((tag) => tag.trim() !== "");
      setTags(next);
      // Drop a filter whose tag no longer exists so the list is not stuck empty.
      setTagFilter((current) => current && !next.includes(current) ? "" : current);
    } catch {
      // Keep the previous options.
    }
  };

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      setNodes((await NodeService.List({ search: "", protocol: "", subscriptionId: "" })) ?? []);
      await refreshTags();
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
    const tag = tagFilter();
    return nodes().filter((node) => {
      if (protocol() && node.protocol.toLowerCase() !== protocol()) return false;
      if (subscriptionID() && node.subscriptionId !== subscriptionID()) return false;
      if (tag && node.tag !== tag) return false;
      if (!keyword) return true;
      const haystack = [node.name, node.tag, node.protocol, node.address, node.subscriptionName].join(" ").toLowerCase();
      return haystack.includes(keyword);
    });
  });

  // NodeService.List already returns nodes in the persisted order, so keep that
  // order stable for the "default" mode and only re-sort a copy for the others.
  const nodeOrder = createMemo(() => {
    const order = new Map<string, number>();
    nodes().forEach((node, index) => order.set(node.id, index));
    return order;
  });

  // Real proxy-chain (URL) latency wins over the TCP reachability fallback.
  const effectiveLatency = (node: NodeView): { result: NodeLatency; source: "url" | "tcp" } | null => {
    const urlResult = urlLatencies()[node.id];
    if (urlResult) return { result: urlResult, source: "url" };
    const tcpResult = latencies()[node.id];
    if (tcpResult) return { result: tcpResult, source: "tcp" };
    return null;
  };

  const latencyValue = (node: NodeView) => {
    const entry = effectiveLatency(node);
    if (!entry || entry.result.error) return Number.MAX_SAFE_INTEGER;
    return entry.result.latencyMs;
  };

  const sortedNodes = createMemo(() => {
    const list = filteredNodes();
    const mode = sortMode();
    if (mode === "default") return list;
    const order = nodeOrder();
    const rank = (node: NodeView) => order.get(node.id) ?? Number.MAX_SAFE_INTEGER;
    const sorted = [...list];
    switch (mode) {
      case "name":
        sorted.sort((a, b) => a.name.localeCompare(b.name, "zh-Hans-CN", { numeric: true }) || rank(a) - rank(b));
        break;
      case "latency":
        sorted.sort((a, b) => latencyValue(a) - latencyValue(b) || rank(a) - rank(b));
        break;
      case "favorite":
        sorted.sort((a, b) => Number(b.favorite) - Number(a.favorite) || rank(a) - rank(b));
        break;
    }
    return sorted;
  });

  const activeNodeID = createMemo(() => nodes().find((node) => node.active)?.id ?? "");
  const activeCount = () => nodes().filter((node) => node.active).length;
  const enabledCount = () => nodes().filter((node) => node.enabled).length;

  // ---- Batch selection ----------------------------------------------------
  const selectableIDs = createMemo(() => sortedNodes().map((node) => node.id));
  const selectedCount = () => selectedIDs().size;
  const allSelected = createMemo(() => selectableIDs().length > 0 && selectableIDs().every((id) => selectedIDs().has(id)));
  const someSelected = createMemo(() => selectedCount() > 0 && !allSelected());
  const batchBusyActive = () => batchBusy() !== "";

  // Keep the selection in sync with the visible list: a selected node that is
  // filtered out or removed never participates in a batch action.
  createEffect(() => {
    const visible = new Set(filteredNodes().map((node) => node.id));
    setSelectedIDs((current) => {
      if (current.size === 0) return current;
      const next = new Set<string>();
      for (const id of current) if (visible.has(id)) next.add(id);
      return next.size === current.size ? current : next;
    });
  });

  // The header checkbox shows the "partially selected" state too.
  createEffect(() => {
    if (selectAllRef) selectAllRef.indeterminate = someSelected();
  });

  const toggleSelected = (id: string) => {
    setSelectedIDs((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const toggleSelectAll = () => {
    if (selectableIDs().length === 0) return;
    setSelectedIDs((current) => (allSelected() ? new Set<string>() : new Set(selectableIDs())));
  };

  const invertSelection = () => {
    if (selectableIDs().length === 0) return;
    setSelectedIDs((current) => {
      const next = new Set<string>();
      for (const id of selectableIDs()) if (!current.has(id)) next.add(id);
      return next;
    });
  };

  const clearSelection = () => setSelectedIDs(new Set<string>());

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

  const toggleFavorite = async (node: NodeView) => {
    const favorite = !node.favorite;
    // Optimistic update: flip the star immediately, then reconcile with the
    // server result and roll back only when the call fails.
    setNodes((current) => current.map((item) => item.id === node.id ? { ...item, favorite } : item));
    setError("");
    try {
      const updated = await NodeService.SetFavorite(node.id, favorite);
      setNodes((current) => current.map((item) => item.id === node.id ? updated : item));
    } catch (err) {
      setNodes((current) => current.map((item) => item.id === node.id ? { ...item, favorite: node.favorite } : item));
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  // Real URL tests start a temporary Xray-core process, so they are serialized:
  // at most one single-node or batch test may be running at any time.
  const urlTestBusy = () => testingID() !== "" || testingAll();

  const urlTestFailure = (nodeId: string, message: string): NodeLatency => ({
    nodeId,
    latencyMs: 0,
    error: message,
    checkedAt: new Date().toISOString(),
  });

  const applyURLLatencyResults = (requestedIDs: string[], results: NodeLatency[] | null) => {
    if (!alive || !results) return;
    const byID = new Map<string, NodeLatency>();
    for (const result of results) {
      if (result?.nodeId) byID.set(result.nodeId, result);
    }
    setUrlLatencies((current) => {
      const next = { ...current };
      for (const [id, result] of byID) next[id] = result;
      // Never leave a requested node without a visible outcome: a missing
      // result is reported as a per-node failure instead of silently ignored.
      for (const id of requestedIDs) {
        if (!byID.has(id)) next[id] = urlTestFailure(id, "测速未返回结果");
      }
      return next;
    });
  };

  const testNode = async (node: NodeView) => {
    if (!alive || urlTestBusy()) return;
    setTestingID(node.id);
    setError("");
    try {
      const results = await NodeService.TestURL([node.id]);
      if (!alive) return;
      applyURLLatencyResults([node.id], results);
    } catch (err) {
      if (!alive) return;
      const message = err instanceof Error ? err.message : String(err);
      setError(message);
      setUrlLatencies((current) => ({ ...current, [node.id]: urlTestFailure(node.id, message) }));
    } finally {
      if (alive) setTestingID("");
    }
  };

  const testAll = async () => {
    if (!alive || urlTestBusy()) return;
    const ids = filteredNodes().map((node) => node.id);
    if (ids.length === 0) return;
    setTestingAll(true);
    setError("");
    try {
      const results = await NodeService.TestURL(ids);
      if (!alive) return;
      applyURLLatencyResults(ids, results);
    } catch (err) {
      if (!alive) return;
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      if (alive) setTestingAll(false);
    }
  };

  const showPageNotice = (message: string) => {
    if (pageNoticeTimer !== undefined) window.clearTimeout(pageNoticeTimer);
    setPageNotice(message);
    pageNoticeTimer = window.setTimeout(() => {
      setPageNotice("");
      pageNoticeTimer = undefined;
    }, 8000);
  };

  // ---- Batch actions ------------------------------------------------------
  const reportBatchResult = (action: string, result: BatchResult, requested: number) => {
    const errors = result.errors ?? [];
    if (result.affected > 0) {
      showPageNotice(requested > result.affected
        ? `${action} ${result.affected} / ${requested} 个节点，其余节点失败。`
        : `${action} ${result.affected} 个节点。`);
    } else if (errors.length === 0) {
      setError(`${action}失败：后端没有更新任何节点。`);
    }
    if (errors.length > 0) {
      const head = errors.slice(0, 2).join("；");
      setError(errors.length > 2 ? `${head}；等共 ${errors.length} 个错误。` : head);
    }
  };

  const runBatchSetEnabled = async (enabled: boolean) => {
    if (batchBusyActive()) return;
    const ids = [...selectedIDs()];
    if (ids.length === 0) return;
    setBatchBusy(enabled ? "enable" : "disable");
    setError("");
    try {
      const result = await NodeService.SetEnabledBatch(ids, enabled);
      await load();
      clearSelection();
      reportBatchResult(enabled ? "已启用" : "已禁用", result, ids.length);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBatchBusy("");
    }
  };

  const runBatchCopyLinks = async () => {
    if (batchBusyActive()) return;
    const ids = [...selectedIDs()];
    if (ids.length === 0) return;
    setBatchBusy("copy");
    setError("");
    try {
      const links = await NodeService.ExportLinks(ids);
      const texts = (links ?? []).map((item) => item.link.trim()).filter(Boolean);
      if (texts.length === 0) {
        setError("所选节点没有可导出的分享链接。");
        return;
      }
      if (!navigator.clipboard?.writeText) {
        setError("当前环境不支持剪贴板，无法复制分享链接。");
        return;
      }
      await navigator.clipboard.writeText(texts.join("\n"));
      showPageNotice(texts.length < ids.length
        ? `已复制 ${texts.length} 个节点的分享链接，${ids.length - texts.length} 个节点没有可导出的链接。`
        : `已复制 ${texts.length} 个节点的分享链接。`);
      clearSelection();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBatchBusy("");
    }
  };

  const runBatchDelete = async () => {
    if (batchBusyActive()) return;
    const ids = [...selectedIDs()];
    if (ids.length === 0) return;
    if (!window.confirm(`确定删除选中的 ${ids.length} 个节点吗？删除后无法恢复，该操作有风险。`)) return;
    setBatchBusy("delete");
    setError("");
    try {
      const result = await NodeService.DeleteBatch(ids);
      await load();
      clearSelection();
      reportBatchResult("已删除", result, ids.length);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBatchBusy("");
    }
  };

  // ---- Node tags ----------------------------------------------------------
  const openTagEditor = () => {
    setTagError("");
    setTagEditorOpen(true);
  };

  const openNodeTagEditor = (node: NodeView) => {
    setTagEditorNode(node);
    setTagEditorIDs([]);
    setTagValue(node.tag);
    openTagEditor();
  };

  const openBatchTagEditor = () => {
    if (batchBusyActive()) return;
    const ids = [...selectedIDs()];
    if (ids.length === 0) return;
    // Prefill only when every selected node already shares one tag; otherwise
    // leave the field empty so overwriting a mixed selection stays deliberate.
    const selected = nodes().filter((node) => ids.includes(node.id));
    const first = selected[0]?.tag ?? "";
    const common = first && selected.every((node) => node.tag === first) ? first : "";
    setTagEditorNode(null);
    setTagEditorIDs(ids);
    setTagValue(common);
    openTagEditor();
  };

  const closeTagEditor = () => {
    if (tagBusy()) return;
    setTagEditorOpen(false);
    setTagEditorNode(null);
    setTagEditorIDs([]);
    setTagValue("");
    setTagError("");
  };

  // Live validation keeps the save button honest: an invalid draft is never
  // sent to the backend (which would reject it anyway).
  const applyTagInput = (value: string) => {
    setTagValue(value);
    setTagError(validateNodeTag(value));
  };

  const clearTagInput = () => {
    setTagValue("");
    setTagError("");
  };

  const reportTagBatchResult = (tag: string, result: BatchResult, requested: number) => {
    const errors = result.errors ?? [];
    if (result.affected > 0) {
      const partial = requested > result.affected;
      if (tag) {
        showPageNotice(partial
          ? `已为 ${result.affected} / ${requested} 个节点设置标签「${tag}」，其余节点失败。`
          : `已为 ${result.affected} 个节点设置标签「${tag}」。`);
      } else {
        showPageNotice(partial
          ? `已清除 ${result.affected} / ${requested} 个节点的标签，其余节点失败。`
          : `已清除 ${result.affected} 个节点的标签。`);
      }
    } else if (errors.length === 0) {
      setError("设置标签失败：后端没有更新任何节点。");
    }
    if (errors.length > 0) {
      const head = errors.slice(0, 2).join("；");
      setError(errors.length > 2 ? `${head}；等共 ${errors.length} 个错误。` : head);
    }
  };

  const submitTag = async () => {
    if (tagBusy() || batchBusyActive()) return;
    const validation = validateNodeTag(tagValue());
    if (validation) {
      setTagError(validation);
      return;
    }
    // An empty (trimmed) tag is the documented way to clear it.
    const tag = tagValue().trim();
    const single = tagEditorNode();
    const ids = single ? [single.id] : tagEditorIDs();
    if (ids.length === 0) return;
    setTagBusy(true);
    setTagError("");
    setError("");
    try {
      if (single) {
        const updated = await NodeService.SetTag(single.id, tag);
        setNodes((current) => current.map((item) => item.id === single.id ? updated : item));
        showPageNotice(tag ? `已为「${single.name}」设置标签「${tag}」。` : `已清除「${single.name}」的标签。`);
        await refreshTags();
      } else {
        setBatchBusy("tag");
        const result = await NodeService.SetTagBatch(ids, tag);
        await load();
        clearSelection();
        reportTagBatchResult(tag, result, ids.length);
      }
      setTagEditorOpen(false);
      setTagEditorNode(null);
      setTagEditorIDs([]);
      setTagValue("");
    } catch (err) {
      setTagError(err instanceof Error ? err.message : String(err));
    } finally {
      setTagBusy(false);
      setBatchBusy("");
    }
  };

  // ---- Share QR codes -----------------------------------------------------
  const closeQRCode = () => {
    qrRequest += 1;
    setQROpen(false);
    setQRLoading(false);
  };

  const openNodeQRCode = async (node: NodeView) => {
    setQRTarget(node);
    setQRTitle(node.name);
    setQRSubtitle(`${protocolLabel(node.protocol)} · ${node.address}:${node.port}`);
    setQRLink("");
    setQRImage("");
    setQRHint("");
    setQRError("");
    setQRLoading(true);
    setQROpen(true);
    const token = ++qrRequest;
    try {
      const [image, links] = await Promise.all([
        NodeService.QRCode(node.id),
        NodeService.ExportLinks([node.id]).catch(() => null),
      ]);
      if (!alive || token !== qrRequest) return;
      if (!image) {
        setQRError("后端没有返回二维码图片，请稍后重试。");
        return;
      }
      const link = links?.[0]?.link ?? "";
      setQRImage(image);
      setQRLink(link);
      setQRHint(link
        ? "使用支持扫码导入的客户端（如 v2rayN、Xray）扫描二维码即可添加该节点。"
        : "该节点暂时无法导出文本分享链接，可以扫描二维码在客户端中导入。");
    } catch (err) {
      if (!alive || token !== qrRequest) return;
      setQRError(err instanceof Error ? err.message : String(err));
    } finally {
      if (alive && token === qrRequest) setQRLoading(false);
    }
  };

  const retryQRCode = () => {
    const node = qrTarget();
    if (node) void openNodeQRCode(node);
  };

  const openImport = () => {
    setImportError("");
    setImportResult(null);
    setImportOpen(true);
  };

  const closeImport = () => {
    if (importBusy()) return;
    setImportOpen(false);
    setImportText("");
    setImportError("");
    setImportResult(null);
  };

  const pasteFromClipboard = async () => {
    setImportError("");
    if (!navigator.clipboard?.readText) {
      setImportError("当前环境不支持读取剪贴板，请手动粘贴分享链接。");
      return;
    }
    try {
      const text = await navigator.clipboard.readText();
      if (!text.trim()) {
        setImportError("剪贴板为空，请先复制节点分享链接。");
        return;
      }
      setImportText((current) => current.trim() ? `${current.trim()}\n${text.trim()}` : text.trim());
    } catch {
      setImportError("无法读取剪贴板，请手动粘贴分享链接。");
    }
  };

  const importNodes = async () => {
    const text = importText().trim();
    if (!text) {
      setImportError("请先粘贴节点分享链接。");
      return;
    }
    setImportBusy(true);
    setImportError("");
    setImportResult(null);
    try {
      const result = await NodeService.ImportLinks(text);
      setImportResult(result);
      const errors = result.errors ?? [];
      if (result.imported > 0) {
        await load();
        showPageNotice(result.skipped > 0
          ? `已导入 ${result.imported} 条节点，跳过 ${result.skipped} 条重复或无效链接。`
          : `已导入 ${result.imported} 条节点。`);
      }
      if (errors.length > 0) {
        setImportError(`导入完成，但有 ${errors.length} 条链接未能处理，详见下方列表。`);
        return;
      }
      if (result.imported === 0) {
        setImportError(result.skipped > 0 ? "这些链接都已存在，没有新增节点。" : "没有识别到可导入的节点链接。");
        return;
      }
      setImportOpen(false);
      setImportText("");
      setImportResult(null);
    } catch (err) {
      setImportError(err instanceof Error ? err.message : String(err));
    } finally {
      setImportBusy(false);
    }
  };

  const latencyLabel = (node: NodeView) => {
    const entry = effectiveLatency(node);
    if (!entry) return <span class="latency unknown">未测试</span>;
    const sourceLabel = entry.source === "url" ? "真实链路延迟" : "TCP 连通性";
    if (entry.result.error) {
      return <span class="latency bad" title={`${sourceLabel}：${entry.result.error}`}>失败</span>;
    }
    return (
      <span
        classList={{ "latency": true, good: entry.result.latencyMs < 100, mid: entry.result.latencyMs >= 100 && entry.result.latencyMs < 300, bad: entry.result.latencyMs >= 300 }}
        title={`${sourceLabel}：${entry.result.latencyMs} ms`}
      >
        {entry.result.latencyMs} ms
      </span>
    );
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
    <div class="page-stack srv-page">
      <section class="mini-stats">
        <div class="mini-stat"><span>全部节点</span><strong>{nodes().length}</strong><small>{enabledCount()} 个启用</small></div>
        <div class="mini-stat"><span>当前节点</span><strong>{activeCount()}</strong><small>已选择</small></div>
        <div class="mini-stat"><span>筛选结果</span><strong>{filteredNodes().length}</strong><small>当前列表</small></div>
      </section>

      <section class="panel srv-panel">
        <div class="panel-heading srv-heading">
          <div class="heading-with-icon">
            <span class="heading-icon cyan"><Icon name="server" size={17} /></span>
            <div>
              <h3>节点管理</h3>
              <span>编辑节点配置、快速切换当前节点和测试连通性</span>
            </div>
          </div>
          <div class="srv-heading-actions">
            <select
              class="srv-quick-switch"
              value={activeNodeID()}
              onChange={(event) => switchActiveNode(event.currentTarget.value)}
              disabled={nodes().length === 0 || busyID() !== ""}
              aria-label="快速切换当前节点"
              title="快速切换当前节点"
            >
              <option value="">快速切换节点</option>
              <For each={nodes().filter((node) => node.enabled)}>
                {(node) => <option value={node.id} selected={node.id === activeNodeID()}>{node.active ? "✓ " : ""}{node.name}</option>}
              </For>
            </select>
            <button
              class="btn secondary srv-test-list"
              onClick={testAll}
              disabled={urlTestBusy() || filteredNodes().length === 0}
              title="真实链路 HTTP 延迟测速（会临时启动 Xray，可能耗时数秒）"
            >
              <Icon name="refresh" size={14} /> {testingAll() ? "测速中..." : "测试当前列表"}
            </button>
          </div>
        </div>

        <div class="srv-toolbar">
          <label class="srv-search">
            <Icon name="search" size={14} />
            <input
              value={search()}
              onInput={(event) => setSearch(event.currentTarget.value)}
              placeholder="搜索节点、标签、地址或订阅"
              aria-label="搜索节点"
            />
          </label>
          <div class="srv-filters" role="group" aria-label="筛选与排序">
            <span class="srv-filter-label" aria-hidden="true">筛选</span>
            <select
              class="srv-select srv-select-protocol"
              value={protocol()}
              onChange={(event) => setProtocol(event.currentTarget.value)}
              aria-label="按协议筛选"
              title="按协议筛选"
            >
              <option value="">全部协议</option>
              <For each={protocolOptions}>{(item) => <option value={item}>{protocolLabel(item)}</option>}</For>
            </select>
            <select
              class="srv-select srv-select-tag"
              value={tagFilter()}
              onChange={(event) => setTagFilter(event.currentTarget.value)}
              aria-label="按标签筛选节点"
              title="按标签筛选节点"
            >
              <option value="">全部标签</option>
              <For each={tags()}>{(tag) => <option value={tag}>{tag}</option>}</For>
            </select>
            <select
              class="srv-select srv-select-sub"
              value={subscriptionID()}
              onChange={(event) => setSubscriptionID(event.currentTarget.value)}
              aria-label="按订阅筛选"
              title="按订阅筛选"
            >
              <option value="">全部订阅</option>
              <For each={subscriptions()}>{(item) => <option value={item.id}>{item.name}</option>}</For>
            </select>
            <span class="srv-filter-sep" aria-hidden="true" />
            <select
              class="srv-select srv-select-sort"
              value={sortMode()}
              onChange={(event) => { const value = event.currentTarget.value; if (isSortMode(value)) setSortMode(value); }}
              aria-label="节点排序方式"
              title="节点排序方式"
            >
              <For each={sortOptions}>{(item) => <option value={item.value}>{item.label}</option>}</For>
            </select>
          </div>
          <div class="srv-tools">
            <button class="btn secondary srv-import" onClick={openImport} disabled={loading()}>
              <Icon name="plus" size={14} /> 导入节点
            </button>
            <button class="btn ghost icon-only srv-refresh" onClick={load} disabled={loading()} aria-label="刷新节点" title="刷新节点">
              <Icon name="refresh" size={14} />
            </button>
          </div>
        </div>

        <Show when={selectedCount() > 0}>
          <div class="srv-batch-bar" role="region" aria-label="批量操作">
            <span class="srv-batch-count">已选 <strong>{selectedCount()}</strong> 项</span>
            <div class="srv-batch-actions">
              <button class="btn ghost compact" type="button" onClick={toggleSelectAll} disabled={batchBusyActive() || selectableIDs().length === 0}>
                {allSelected() ? "取消全选" : "全选"}
              </button>
              <button class="btn ghost compact" type="button" onClick={invertSelection} disabled={batchBusyActive() || selectableIDs().length === 0}>
                反选
              </button>
              <button class="btn ghost compact" type="button" onClick={clearSelection} disabled={batchBusyActive()}>
                清空选择
              </button>
              <span class="srv-batch-divider" aria-hidden="true" />
              <button class="btn secondary compact" type="button" onClick={() => void runBatchSetEnabled(true)} disabled={batchBusyActive()}>
                <Icon name="check" size={13} /> {batchBusy() === "enable" ? "启用中..." : "批量启用"}
              </button>
              <button class="btn secondary compact" type="button" onClick={() => void runBatchSetEnabled(false)} disabled={batchBusyActive()}>
                <Icon name="stop" size={13} /> {batchBusy() === "disable" ? "禁用中..." : "批量禁用"}
              </button>
              <button class="btn secondary compact" type="button" onClick={() => void runBatchCopyLinks()} disabled={batchBusyActive()}>
                <CopyLinkIcon /> {batchBusy() === "copy" ? "复制中..." : "批量复制链接"}
              </button>
              <button class="btn secondary compact" type="button" onClick={openBatchTagEditor} disabled={batchBusyActive()}>
                <Icon name="edit" size={13} /> 设置标签
              </button>
              <button class="btn danger compact" type="button" onClick={() => void runBatchDelete()} disabled={batchBusyActive()}>
                <Icon name="trash" size={13} /> {batchBusy() === "delete" ? "删除中..." : "批量删除"}
              </button>
            </div>
          </div>
        </Show>

        <Show when={pageNotice()}>
          <div class="alert success srv-notice">
            <span class="alert-message"><Icon name="check" size={15} /> {pageNotice()}</span>
            <button class="icon-button" onClick={() => setPageNotice("")} aria-label="关闭导入提示">
              <Icon name="close" size={14} />
            </button>
          </div>
        </Show>

        <Show when={error()}>
          <div class="alert error srv-notice"><span class="alert-message"><Icon name="alert" size={15} /> {error()}</span></div>
        </Show>

        <Show when={!loading()} fallback={<div class="empty-state">正在读取节点...</div>}>
          <Show when={filteredNodes().length > 0} fallback={
            <div class="empty-state">
              <div class="empty-icon"><Icon name="server" size={22} /></div>
              <strong>{nodes().length === 0 ? "还没有节点" : "没有匹配的节点"}</strong>
              <span>{nodes().length === 0 ? "添加订阅后，解析出的节点会显示在这里。" : "尝试调整搜索词或筛选条件。"}</span>
              <Show when={nodes().length === 0}>
                <div class="node-empty-actions">
                  <button class="btn secondary" onClick={() => props.onNavigate("subscriptions")}>去添加订阅</button>
                  <button class="btn secondary" onClick={openImport}>导入节点</button>
                </div>
              </Show>
            </div>
          }>
            <div class="srv-table">
              <div class="srv-table-head">
                <label class="srv-select-all" title="全选当前列表">
                  <input
                    ref={(element) => { selectAllRef = element; }}
                    class="srv-check"
                    type="checkbox"
                    checked={allSelected()}
                    disabled={batchBusyActive() || selectableIDs().length === 0}
                    onChange={toggleSelectAll}
                    aria-label="全选当前列表"
                  />
                </label>
                <span class="srv-col-name">节点</span>
                <span class="srv-col-protocol">协议</span>
                <span class="srv-col-address">地址</span>
                <span class="srv-col-latency">延迟</span>
                <span class="srv-col-state">状态</span>
                <span class="srv-col-actions">操作</span>
              </div>
              <For each={sortedNodes()}>
                {(node) => (
                  <div classList={{ "srv-row": true, active: node.active, disabled: !node.enabled, selected: selectedIDs().has(node.id) }}>
                    <div class="srv-cell-select">
                      <label class="srv-check-label" title={selectedIDs().has(node.id) ? "取消选择该节点" : "选择该节点"}>
                        <input
                          class="srv-check"
                          type="checkbox"
                          checked={selectedIDs().has(node.id)}
                          disabled={batchBusyActive()}
                          onChange={() => toggleSelected(node.id)}
                          aria-label={`选择 ${node.name}`}
                        />
                      </label>
                      <button
                        classList={{ "srv-radio": true, selected: node.active }}
                        aria-label={node.active ? "当前节点" : `将 ${node.name} 设为当前节点`}
                        onClick={() => switchActiveNode(node.id)}
                        disabled={busyID() === node.id}
                      >
                        <span />
                      </button>
                    </div>
                    <div class="srv-node">
                      <div class="srv-node-main">
                        <strong class="srv-node-name" title={node.name}>{node.name}</strong>
                        <Show when={node.manual}>
                          <span class="srv-chip srv-chip-manual" title="通过分享链接手动导入的节点">手动</span>
                        </Show>
                        <Show when={node.tag}>
                          <button
                            type="button"
                            class="srv-chip srv-chip-tag"
                            onClick={() => openNodeTagEditor(node)}
                            disabled={batchBusyActive() || tagBusy()}
                            title={`标签：${node.tag}（点击编辑）`}
                            aria-label={`编辑 ${node.name} 的标签：${node.tag}`}
                          >
                            {node.tag}
                          </button>
                        </Show>
                      </div>
                      <small class="srv-node-sub">{node.subscriptionName || (node.manual ? "手动添加" : "")}</small>
                    </div>
                    <span class="srv-proto" title={protocolLabel(node.protocol)}>{protocolLabel(node.protocol)}</span>
                    <span class="srv-addr" title={`${node.address}:${node.port}`}>{node.address}:{node.port}</span>
                    <span class="srv-latency-cell">{latencyLabel(node)}</span>
                    <button
                      classList={{ "srv-state": true, on: node.enabled }}
                      onClick={() => toggleEnabled(node)}
                      disabled={busyID() === node.id}
                      aria-label={`${node.enabled ? "禁用" : "启用"} ${node.name}`}
                      title={node.enabled ? "点击禁用该节点" : "点击启用该节点"}
                    >
                      <span class="status-dot" />
                      {node.enabled ? "启用" : "禁用"}
                    </button>
                    <div class="srv-actions">
                      <button
                        classList={{ "srv-action": true, "srv-favorite": true, on: node.favorite }}
                        onClick={() => toggleFavorite(node)}
                        aria-pressed={node.favorite}
                        aria-label={node.favorite ? `取消收藏 ${node.name}` : `收藏 ${node.name}`}
                        title={node.favorite ? "取消收藏" : "收藏"}
                      >
                        <FavoriteStarIcon filled={node.favorite} />
                      </button>
                      <button
                        classList={{ "srv-action": true, "srv-speed": true, running: testingID() === node.id }}
                        onClick={() => testNode(node)}
                        disabled={urlTestBusy()}
                        aria-busy={testingID() === node.id}
                        aria-label={`测试 ${node.name} 的真实链路延迟`}
                        title={testingID() === node.id ? "测速中..." : "真实链路 HTTP 延迟测速（会临时启动 Xray）"}
                      >
                        <Icon name="speed" size={14} />
                      </button>
                      <button
                        class="srv-action srv-qr"
                        onClick={() => void openNodeQRCode(node)}
                        disabled={batchBusyActive()}
                        aria-label={`查看 ${node.name} 的分享二维码`}
                        title="分享二维码（弹窗内可复制链接）"
                      >
                        <QRCodeIcon />
                      </button>
                      <button
                        class="srv-action"
                        onClick={() => openEditor(node)}
                        disabled={busyID() === node.id}
                        aria-label={`编辑 ${node.name}`}
                        title="编辑节点配置"
                      >
                        <Icon name="edit" size={14} />
                      </button>
                      <button
                        class="srv-action srv-danger"
                        onClick={() => removeNode(node)}
                        disabled={busyID() === node.id}
                        aria-label={`删除 ${node.name}`}
                        title="删除节点"
                      >
                        <Icon name="trash" size={14} />
                      </button>
                    </div>
                  </div>
                )}
              </For>
            </div>
          </Show>
        </Show>

        <p class="srv-hint">
          「测速」为真实代理链路的 HTTP 延迟测试（会临时启动 Xray，可能耗时数秒）；没有真实链路结果时，延迟列回退显示 TCP 连通性结果。切换节点时会自动重启正在运行的代理。
        </p>
      </section>

      <Show when={importOpen()}>
        <div class="modal-backdrop" onClick={(event) => { if (event.target === event.currentTarget) closeImport(); }}>
          <div class="modal node-import-modal">
            <div class="modal-heading">
              <div>
                <h3>导入节点</h3>
                <span>粘贴 vmess / vless / trojan / ss 分享链接，或整段 base64 订阅内容</span>
              </div>
              <button class="icon-button" onClick={closeImport} disabled={importBusy()} aria-label="关闭">
                <Icon name="close" size={18} />
              </button>
            </div>

            <div class="node-import-body">
              <div class="node-import-toolbar">
                <button class="btn secondary compact" type="button" onClick={pasteFromClipboard} disabled={importBusy()}>
                  <PasteIcon /> 从剪贴板粘贴
                </button>
                <button class="btn ghost compact" type="button" onClick={() => setImportText("")} disabled={importBusy() || !importText().trim()}>
                  清空
                </button>
              </div>
              <textarea
                class="node-import-textarea"
                value={importText()}
                onInput={(event) => setImportText(event.currentTarget.value)}
                spellcheck={false}
                placeholder={"vmess://...\nvless://...\ntrojan://...\nss://..."}
                aria-label="节点分享链接"
              />
              <p class="node-import-hint">
                每行一条分享链接；也支持直接粘贴整段 base64 编码的订阅内容。已在列表中的链接会自动跳过。
              </p>

              <Show when={importError()}>
                <div class="alert error"><span class="alert-message"><Icon name="alert" size={14} /> {importError()}</span></div>
              </Show>

              <Show when={importResult()}>
                {(result) => (
                  <div class="node-import-result">
                    <div class="node-import-result-head">
                      <span class="tag success">成功 {result().imported} 条</span>
                      <span class="tag neutral">跳过 {result().skipped} 条</span>
                      <Show when={(result().errors?.length ?? 0) > 0}>
                        <span class="tag danger">错误 {result().errors?.length ?? 0} 条</span>
                      </Show>
                    </div>
                    <Show when={(result().errors?.length ?? 0) > 0}>
                      <ul class="node-import-errors">
                        <For each={result().errors ?? []}>{(item) => <li>{item}</li>}</For>
                      </ul>
                    </Show>
                  </div>
                )}
              </Show>
            </div>

            <div class="modal-footer">
              <button class="btn ghost" type="button" onClick={closeImport} disabled={importBusy()}>关闭</button>
              <button class="btn primary" type="button" onClick={importNodes} disabled={importBusy() || !importText().trim()}>
                <Icon name="plus" size={14} /> {importBusy() ? "导入中..." : "导入"}
              </button>
            </div>
          </div>
        </div>
      </Show>

      <Show when={tagEditorOpen()}>
        <div class="modal-backdrop" onClick={(event) => { if (event.target === event.currentTarget) closeTagEditor(); }}>
          <div class="modal node-tag-modal">
            <div class="modal-heading">
              <div>
                <h3>{tagEditorNode() ? "编辑节点标签" : "批量设置标签"}</h3>
                <span>{tagEditorNode()
                  ? `节点：${tagEditorNode()?.name ?? ""}`
                  : `已选 ${tagEditorIDs().length} 个节点，保存后覆盖原有标签`}</span>
              </div>
              <button class="icon-button" onClick={closeTagEditor} disabled={tagBusy()} aria-label="关闭">
                <Icon name="close" size={18} />
              </button>
            </div>

            <div class="node-tag-body">
              <label class="node-tag-field">
                <span>标签名称</span>
                <input
                  class="node-tag-input"
                  value={tagValue()}
                  placeholder="例如：香港、备用、低延迟；留空表示清除标签"
                  onInput={(event) => applyTagInput(event.currentTarget.value)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter") {
                      event.preventDefault();
                      void submitTag();
                    }
                  }}
                  disabled={tagBusy()}
                  aria-label="节点标签"
                />
              </label>
              <div class="node-tag-meta">
                <span classList={{ "node-tag-counter": true, over: tagCharCount(tagValue()) > TAG_MAX_LENGTH }}>{tagCharCount(tagValue())} / {TAG_MAX_LENGTH}</span>
                <button class="btn ghost compact node-tag-clear" type="button" onClick={clearTagInput} disabled={tagBusy() || !tagValue().trim()}>
                  <Icon name="close" size={12} /> 清除标签
                </button>
              </div>

              <Show when={tags().length > 0}>
                <div class="node-tag-presets">
                  <span class="node-tag-presets-label">已有标签</span>
                  <div class="node-tag-preset-list">
                    <For each={tags()}>
                      {(tag) => (
                        <button
                          classList={{ "node-tag-preset": true, active: tagValue().trim() === tag }}
                          type="button"
                          onClick={() => { setTagValue(tag); setTagError(""); }}
                          disabled={tagBusy()}
                          title={tag}
                        >
                          {tag}
                        </button>
                      )}
                    </For>
                  </div>
                </div>
              </Show>

              <p class="node-tag-hint">
                <Icon name="alert" size={12} />
                <span>标签最多 {TAG_MAX_LENGTH} 个字符，不能包含换行或控制字符；留空保存会清除标签。</span>
              </p>

              <Show when={tagError()}>
                <div class="alert error"><span class="alert-message"><Icon name="alert" size={14} /> {tagError()}</span></div>
              </Show>
            </div>

            <div class="modal-footer">
              <button class="btn ghost" type="button" onClick={closeTagEditor} disabled={tagBusy()}>取消</button>
              <button class="btn primary" type="button" onClick={() => void submitTag()} disabled={tagBusy() || validateNodeTag(tagValue()) !== "" || (tagEditorIDs().length === 0 && !tagEditorNode())}>
                <Icon name="check" size={14} /> {tagBusy() ? "保存中..." : tagValue().trim() ? "保存标签" : "清除标签"}
              </button>
            </div>
          </div>
        </div>
      </Show>

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
                            <label><span>VLESS 加密</span><input value={config().encryption} onInput={(event) => updateEditor("encryption", event.currentTarget.value)} placeholder="none 或 mlkem768x25519plus..." /></label>
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
                              <label><span>REALITY Verify (pqv)</span><input value={config().mldsa65Verify} onInput={(event) => updateEditor("mldsa65Verify", event.currentTarget.value)} placeholder="ML-DSA-65 verify key" /></label>
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

      <QRModal
        open={qrOpen()}
        title={qrTitle() ? `节点二维码 · ${qrTitle()}` : "节点二维码"}
        subtitle={qrSubtitle() || "扫码后即可在客户端中导入该节点"}
        link={qrLink()}
        image={qrImage()}
        loading={qrLoading()}
        error={qrError()}
        hint={qrHint()}
        onClose={closeQRCode}
        onRetry={retryQRCode}
      />
    </div>
  );
}
