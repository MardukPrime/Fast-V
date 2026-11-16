import { createEffect, createSignal, onCleanup, onMount, Show } from "solid-js";

import Icon from "./Icon";

/**
 * Generic share-link QR code dialog.
 *
 * The QR image is rendered on the Go side (NodeService.QRCode /
 * SubscriptionService.QRCode return a `data:image/png;base64,...` URL), so this
 * component only owns the presentation: image, optional link text, copy
 * feedback, loading and error states. It is shared by the servers page and the
 * subscriptions page.
 */
export type QRModalProps = {
  open: boolean;
  title: string;
  subtitle?: string;
  /** Share link shown below the QR image. Omitted when the backend cannot expose it as text. */
  link?: string;
  /** `data:image/png;base64,...` QR image returned by the backend. */
  image?: string;
  loading?: boolean;
  error?: string;
  /** Extra explanation shown under the QR code (e.g. why no link text is available). */
  hint?: string;
  onClose: () => void;
  onRetry?: () => void;
};

/** Small QR glyph reused by the per-row action buttons of both pages. */
export function QRCodeIcon(props: { size?: number }) {
  const size = () => props.size ?? 14;
  return (
    <svg
      width={size()}
      height={size()}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="1.8"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <rect x="3" y="3" width="7" height="7" rx="1.2" />
      <rect x="14" y="3" width="7" height="7" rx="1.2" />
      <rect x="3" y="14" width="7" height="7" rx="1.2" />
      <path d="M6 6h1v1H6z" />
      <path d="M17 6h1v1h-1z" />
      <path d="M6 17h1v1H6z" />
      <path d="M14 14h2v2h-2z" />
      <path d="M19 14h2v2h-2z" />
      <path d="M14 19h2v2h-2z" />
      <path d="M19 19h2v2h-2z" />
    </svg>
  );
}

function CopyIcon() {
  return (
    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <rect x="9" y="9" width="11" height="11" rx="2" />
      <path d="M5 15V6a2 2 0 0 1 2-2h9" />
    </svg>
  );
}

export default function QRModal(props: QRModalProps) {
  const [copied, setCopied] = createSignal(false);
  const [copyError, setCopyError] = createSignal("");
  let copyTimer: number | undefined;
  let alive = true;

  const clearCopyTimer = () => {
    if (copyTimer !== undefined) {
      window.clearTimeout(copyTimer);
      copyTimer = undefined;
    }
  };

  onCleanup(() => {
    alive = false;
    clearCopyTimer();
  });

  // Every new dialog/link starts from a clean copy state.
  createEffect(() => {
    props.open;
    props.link;
    clearCopyTimer();
    setCopied(false);
    setCopyError("");
  });

  const close = () => props.onClose();

  const copyLink = async () => {
    const link = props.link?.trim() ?? "";
    if (!link) {
      setCopyError("没有可复制的分享链接。");
      return;
    }
    if (!navigator.clipboard?.writeText) {
      setCopyError("当前环境不支持剪贴板，请手动选中链接复制。");
      return;
    }
    try {
      await navigator.clipboard.writeText(link);
      if (!alive) return;
      setCopyError("");
      setCopied(true);
      clearCopyTimer();
      copyTimer = window.setTimeout(() => {
        setCopied(false);
        copyTimer = undefined;
      }, 1600);
    } catch {
      if (!alive) return;
      setCopyError("复制失败，请手动选中链接复制。");
    }
  };

  onMount(() => {
    const handleKeydown = (event: KeyboardEvent) => {
      if (!props.open) return;
      if (event.key === "Escape") {
        event.stopPropagation();
        close();
      }
    };
    window.addEventListener("keydown", handleKeydown);
    onCleanup(() => window.removeEventListener("keydown", handleKeydown));
  });

  return (
    <Show when={props.open}>
      <div
        class="modal-backdrop"
        onClick={(event) => {
          if (event.target === event.currentTarget) close();
        }}
      >
        <div class="modal qr-modal" role="dialog" aria-modal="true" aria-label={props.title}>
          <div class="modal-heading">
            <div>
              <h3>{props.title}</h3>
              <span>{props.subtitle ?? "扫码后即可导入或分享"}</span>
            </div>
            <button class="icon-button" onClick={close} aria-label="关闭二维码">
              <Icon name="close" size={18} />
            </button>
          </div>

          <div class="qr-body">
            <Show
              when={props.loading}
              fallback={
                <Show
                  when={props.error}
                  fallback={
                    <Show when={props.image}>
                      {(image) => (
                        <div class="qr-image-wrap">
                          <img class="qr-image" src={image()} alt={`${props.title}二维码`} width="320" height="320" />
                        </div>
                      )}
                    </Show>
                  }
                >
                  <div class="alert error qr-error">
                    <span class="alert-message">
                      <Icon name="alert" size={14} /> {props.error}
                    </span>
                    <Show when={props.onRetry}>
                      <button class="btn ghost compact" type="button" onClick={() => props.onRetry?.()}>
                        <Icon name="refresh" size={13} /> 重试
                      </button>
                    </Show>
                  </div>
                </Show>
              }
            >
              <div class="qr-placeholder">
                <span class="qr-spinner" aria-hidden="true" />
                <span>正在生成二维码...</span>
              </div>
            </Show>

            <Show when={props.link}>
              {(link) => (
                <div class="qr-link">
                  <code class="qr-link-text" title={link()}>
                    {link()}
                  </code>
                  <button
                    classList={{ "btn": true, "secondary": true, "compact": true, "qr-copy": true, copied: copied() }}
                    type="button"
                    onClick={() => void copyLink()}
                  >
                    <Show when={copied()} fallback={<><CopyIcon /> 复制链接</>}>
                      <Icon name="check" size={13} /> 已复制
                    </Show>
                  </button>
                </div>
              )}
            </Show>

            <Show when={props.hint}>
              {(hint) => (
                <p class="qr-hint">
                  <Icon name="alert" size={13} />
                  <span>{hint()}</span>
                </p>
              )}
            </Show>

            <Show when={copyError()}>
              <div class="alert error qr-copy-error">
                <span class="alert-message">
                  <Icon name="alert" size={13} /> {copyError()}
                </span>
              </div>
            </Show>
          </div>

          <div class="modal-footer">
            <button class="btn ghost" type="button" onClick={close}>
              关闭
            </button>
          </div>
        </div>
      </div>
    </Show>
  );
}
