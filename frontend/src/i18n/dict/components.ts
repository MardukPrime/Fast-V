// Dictionary for shared components (QR modal, dialogs).
export const zh: Record<string, string> = {
  "qr.defaultSubtitle": "扫码后即可导入或分享",
  "qr.closeAria": "关闭二维码",
  "qr.imageAlt": "{title}二维码",
  "qr.retry": "重试",
  "qr.generating": "正在生成二维码...",
  "qr.copyLink": "复制链接",
  "qr.copied": "已复制",
  "qr.close": "关闭",
  "qr.error.noLink": "没有可复制的分享链接。",
  "qr.error.noClipboard": "当前环境不支持剪贴板，请手动选中链接复制。",
  "qr.error.copyFailed": "复制失败，请手动选中链接复制。",
  "qr.error.noImage": "后端没有返回二维码图片，请稍后重试。",
};

export const en: Record<string, string> = {
  "qr.defaultSubtitle": "Scan the code to import or share",
  "qr.closeAria": "Close the QR code dialog",
  "qr.imageAlt": "{title} QR code",
  "qr.retry": "Retry",
  "qr.generating": "Generating QR code...",
  "qr.copyLink": "Copy link",
  "qr.copied": "Copied",
  "qr.close": "Close",
  "qr.error.noLink": "No share link to copy.",
  "qr.error.noClipboard": "Clipboard is not available here. Select and copy the link manually.",
  "qr.error.copyFailed": "Copy failed. Select and copy the link manually.",
  "qr.error.noImage": "The backend returned no QR image. Please try again later.",
};
