package com.wails.app;

import android.content.Intent;
import android.net.VpnService;

/**
 * Phase 2 scaffold for the Android system-wide proxy mode.
 *
 * <p>The final implementation will:
 * <ul>
 *   <li>build a VpnService tun interface with split-tunnel / per-app rules,</li>
 *   <li>pass the tun file descriptor to the Go side through JNI,</li>
 *   <li>run an in-process Xray core plus a tun2socks bridge that forwards tun
 *       traffic to the embedded SOCKS inbound,</li>
 *   <li>run as a foreground service with a persistent notification.</li>
 * </ul>
 *
 * <p>Until that networking layer lands the service is intentionally inert: it is
 * registered in the manifest (so the permission/VPN declaration is in place) but
 * the app never starts it and it returns immediately.
 */
public class FastVVpnService extends VpnService {
    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        // Phase 2 will call startForeground(...) and bring the tunnel up here.
        return START_NOT_STICKY;
    }

    @Override
    public void onRevoke() {
        super.onRevoke();
        stopSelf();
    }
}
