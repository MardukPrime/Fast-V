import { For } from "solid-js";

export type IconName =
  | "dashboard"
  | "server"
  | "subscription"
  | "route"
  | "speed"
  | "logs"
  | "settings"
  | "plus"
  | "refresh"
  | "trash"
  | "nodes"
  | "chevron"
  | "check"
  | "alert"
  | "close"
  | "edit"
  | "external"
  | "minimize"
  | "maximize"
  | "search";

const iconPaths: Record<IconName, string[]> = {
  dashboard: ["M3 3h7v7H3z", "M14 3h7v7h-7z", "M3 14h7v7H3z", "M14 14h7v7h-7z"],
  server: ["M4 4h16v6H4z", "M4 14h16v6H4z", "M8 7h.01", "M8 17h.01"],
  subscription: ["M4 5h16", "M4 12h16", "M4 19h10"],
  route: ["M6 3v12a3 3 0 0 0 3 3h9", "M15 15l3 3-3 3", "M6 8h5a3 3 0 0 1 3 3v2"],
  speed: ["M12 14l4-4", "M5 19a9 9 0 1 1 14 0"],
  logs: ["M6 3h9l3 3v15H6z", "M9 8h6", "M9 12h6", "M9 16h4"],
  settings: ["M4 6h16", "M4 12h16", "M4 18h16", "M8 4v4", "M16 10v4", "M10 16v4"],
  plus: ["M12 5v14", "M5 12h14"],
  refresh: ["M20 12a8 8 0 1 1-2.3-5.7", "M20 4v5h-5"],
  trash: ["M4 7h16", "M9 7V5h6v2", "M7 7l1 14h8l1-14"],
  nodes: ["M12 3l9 5-9 5-9-5 9-5z", "M3 12l9 5 9-5", "M3 16l9 5 9-5"],
  chevron: ["M9 6l6 6-6 6"],
  check: ["M5 12l4 4L19 6"],
  alert: ["M12 3l9 16H3L12 3z", "M12 9v4", "M12 16h.01"],
  close: ["M6 6l12 12", "M18 6L6 18"],
  edit: ["M4 20h4L19 9l-4-4L4 16v4z"],
  external: ["M14 4h6v6", "M20 4l-9 9", "M18 14v5H5V6h5"],
  minimize: ["M5 12h14"],
  maximize: ["M6 6h12v12H6z"],
  search: ["M11 4a7 7 0 1 0 0 14 7 7 0 0 0 0-14z", "M16.5 16.5L21 21"],
};

type IconProps = {
  name: IconName;
  size?: number;
  class?: string;
};

export default function Icon(props: IconProps) {
  const size = () => props.size ?? 18;
  return (
    <svg
      class={props.class}
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
      <For each={iconPaths[props.name]}>{(path) => <path d={path} />}</For>
    </svg>
  );
}