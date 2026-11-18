import { createSignal } from "solid-js";

import * as components from "./dict/components";
import * as dashboard from "./dict/dashboard";
import * as logs from "./dict/logs";
import * as network from "./dict/network";
import * as routing from "./dict/routing";
import * as servers from "./dict/servers";
import * as settings from "./dict/settings";
import * as shell from "./dict/shell";
import * as subscriptions from "./dict/subscriptions";

export type Lang = "zh-CN" | "en-US";

export const LANGUAGES: { value: Lang; label: string }[] = [
  { value: "zh-CN", label: "简体中文" },
  { value: "en-US", label: "English" },
];

const zh: Record<string, string> = {
  ...shell.zh,
  ...dashboard.zh,
  ...servers.zh,
  ...subscriptions.zh,
  ...components.zh,
  ...routing.zh,
  ...network.zh,
  ...logs.zh,
  ...settings.zh,
};

const en: Record<string, string> = {
  ...shell.en,
  ...dashboard.en,
  ...servers.en,
  ...subscriptions.en,
  ...components.en,
  ...routing.en,
  ...network.en,
  ...logs.en,
  ...settings.en,
};

const dicts: Record<Lang, Record<string, string>> = {
  "zh-CN": zh,
  "en-US": en,
};

const [language, setLanguageSignal] = createSignal<Lang>("zh-CN");

export { language };

export function setLanguage(value: string | undefined): void {
  const next: Lang = value === "en-US" ? "en-US" : "zh-CN";
  setLanguageSignal(next);
  if (typeof document !== "undefined") {
    document.documentElement.lang = next;
  }
}

/**
 * Translate a UI string. Missing English entries fall back to Chinese and then
 * to the key itself, so an untranslated label never renders as blank.
 */
export function t(key: string, vars?: Record<string, string | number>): string {
  let text = dicts[language()][key];
  if (text === undefined) text = dicts["zh-CN"][key];
  if (text === undefined) return key;
  if (vars) {
    for (const [name, value] of Object.entries(vars)) {
      text = text.split(`{${name}}`).join(String(value));
    }
  }
  return text;
}
