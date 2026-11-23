/**
 * Android system-wide proxy bridge (Phase 2, mechanism M3).
 *
 * The tun interface belongs to the Android shell: only the platform can ask for
 * VPN consent and open a VpnService. Packet forwarding runs in Go, so this
 * module is the thin seam the UI uses to hand the local SOCKS port to the
 * service once the embedded core is listening.
 *
 * Every entry point is feature-detected. On desktop builds, the browser preview
 * and older packages these calls are inert, which keeps the desktop paths
 * untouched.
 */

/** Status reported by the Android shell. */
export type AndroidVpnStatus = {
  supported: boolean;
  running: boolean;
  error: string;
};

type AndroidWailsBridge = {
  vpnSupported?: () => boolean;
  vpnStatus?: () => string;
  vpnConnect?: (socksPort: number) => void;
  vpnDisconnect?: () => void;
};

type VpnHostWindow = {
  wails?: AndroidWailsBridge;
  __fastvVpnResult?: (ok: boolean, message: string | null) => void;
};

const EMPTY_STATUS: AndroidVpnStatus = { supported: false, running: false, error: "" };

function host(): VpnHostWindow | undefined {
  if (typeof window === "undefined") return undefined;
  return window as unknown as VpnHostWindow;
}

/** Whether this build can run the system-wide proxy. */
export function androidVpnSupported(): boolean {
  const candidate = host()?.wails;
  if (!candidate?.vpnSupported) return false;
  try {
    return candidate.vpnSupported() === true;
  } catch {
    return false;
  }
}

/** Current tunnel state; a parse failure is reported as "not running". */
export function androidVpnStatus(): AndroidVpnStatus {
  const candidate = host()?.wails;
  if (!candidate?.vpnStatus) return EMPTY_STATUS;
  try {
    const parsed = JSON.parse(candidate.vpnStatus()) as Partial<AndroidVpnStatus>;
    return {
      supported: parsed.supported === true,
      running: parsed.running === true,
      error: typeof parsed.error === "string" ? parsed.error : "",
    };
  } catch {
    return EMPTY_STATUS;
  }
}

/**
 * Ask Android for VPN consent and start the tunnel.
 *
 * Returns false when the bridge is missing; the consent dialog and the real
 * outcome arrive asynchronously through {@link onAndroidVpnResult}.
 */
export function androidVpnConnect(socksPort: number): boolean {
  const candidate = host()?.wails;
  if (!candidate?.vpnConnect || !Number.isInteger(socksPort) || socksPort <= 0) return false;
  try {
    candidate.vpnConnect(socksPort);
    return true;
  } catch {
    return false;
  }
}

/** Tear the tunnel down; safe to call when nothing is running. */
export function androidVpnDisconnect(): boolean {
  const candidate = host()?.wails;
  if (!candidate?.vpnDisconnect) return false;
  try {
    candidate.vpnDisconnect();
    return true;
  } catch {
    return false;
  }
}

/** Subscribe to the tunnel start/stop outcome. Returns an unsubscribe function. */
export function onAndroidVpnResult(handler: (ok: boolean, message: string) => void): () => void {
  const target = host();
  if (!target) return () => {};
  const listener = (ok: boolean, message: string | null) => handler(ok === true, message ?? "");
  target.__fastvVpnResult = listener;
  return () => {
    if (target.__fastvVpnResult === listener) delete target.__fastvVpnResult;
  };
}

/**
 * Extract the port from a proxy state address such as "127.0.0.1:10808".
 * Returns 0 when the address is empty or malformed.
 */
export function socksPortFromLocalAddr(localAddr: string): number {
  const match = /:(\d{1,5})$/.exec(localAddr ?? "");
  if (!match) return 0;
  const port = Number.parseInt(match[1], 10);
  return Number.isInteger(port) && port > 0 && port < 65536 ? port : 0;
}
