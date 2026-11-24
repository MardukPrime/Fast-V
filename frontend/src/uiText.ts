/**
 * Presentation helpers shared by the shell and the dashboard.
 *
 * The Go layer answers in English and Wails wraps every failed bound call as
 * "Binding call failed: Bound method returned an error: …". Showing that
 * verbatim (which the dashboard used to do) looks like a crash and buries the
 * one thing the user has to do, so messages are mapped to actionable text and
 * the raw string is kept as a collapsible detail.
 */
export type PresentedError = { text: string; detail: string };

const plumbingPrefixes = [
  /^binding call failed:\s*/i,
  /^bound method returned an error:\s*/i,
  /^error:\s*/i,
];

const knownErrors: Array<{ match: RegExp; key: string }> = [
  { match: /no active node/i, key: "dash.errNoNode" },
  { match: /proxy is not running/i, key: "dash.errNotRunning" },
  { match: /timeout waiting for local proxy port/i, key: "dash.errPortTimeout" },
  { match: /only xray-core and sing-box/i, key: "dash.errUnsupportedCore" },
  { match: /管理员|elevat|permission denied|access is denied/i, key: "dash.errPermission" },
  { match: /context deadline exceeded|timed out|i\/o timeout/i, key: "dash.errTimeout" },
];

/** Strip the runtime plumbing so only the real message survives. */
export function stripErrorPlumbing(raw: string): string {
  let text = (raw ?? "").trim();
  let changed = true;
  while (changed) {
    changed = false;
    for (const prefix of plumbingPrefixes) {
      if (prefix.test(text)) {
        text = text.replace(prefix, "").trim();
        changed = true;
      }
    }
  }
  return text;
}

/**
 * Map a backend failure to a headline the user can act on plus the untouched
 * original for the "技术详情" disclosure. Unknown messages keep their stripped
 * form as the headline instead of being hidden.
 */
export function presentError(raw: string, translate: (key: string) => string): PresentedError {
  const detail = stripErrorPlumbing(raw);
  if (!detail) return { text: "", detail: "" };
  for (const entry of knownErrors) {
    if (entry.match.test(detail)) return { text: translate(entry.key), detail };
  }
  return { text: detail, detail: "" };
}

/**
 * Human label for a core version. The embedded build reports "embedded-xray",
 * which is an internal identifier rather than something to show in the app bar.
 */
export function coreVersionLabel(version: string | undefined, translate: (key: string) => string): string {
  const value = (version ?? "").trim();
  if (!value) return "";
  if (value === "embedded-xray") return translate("shell.coreEmbedded");
  return value;
}
