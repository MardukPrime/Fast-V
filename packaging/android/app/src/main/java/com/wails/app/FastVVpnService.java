package com.wails.app;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Context;
import android.content.Intent;
import android.net.VpnService;
import android.os.Build;
import android.os.ParcelFileDescriptor;
import android.util.Log;

/**
 * Android VPN service (Phase 2, mechanism M3).
 *
 * <p>This service owns the VpnService tun interface and the foreground
 * notification. Packet forwarding itself happens in Go: after the interface is
 * established the raw descriptor is handed to the dataplane
 * ({@code internal/vpn}), which terminates the captured TCP/UDP flows and
 * relays them through the embedded core's loopback SOCKS5 inbound.</p>
 *
 * <p>Two rules matter for correctness and are easy to get wrong:</p>
 * <ul>
 *   <li>the app's own sockets are excluded with addDisallowedApplication, so the
 *       proxy's upstream connection does not get captured by its own tun
 *       (a routing loop);</li>
 *   <li>the descriptor is detached, not closed, before the handoff: Go takes
 *       ownership and closing it is what tears the interface down.</li>
 * </ul>
 */
public class FastVVpnService extends VpnService {
    private static final String TAG = "FastVVpnService";

    public static final String ACTION_START = "com.mardukprime.fastv.VPN_START";
    public static final String ACTION_STOP = "com.mardukprime.fastv.VPN_STOP";
    public static final String EXTRA_SOCKS_PORT = "socks_port";
    public static final String EXTRA_MTU = "mtu";

    /** Tun addresses; they must stay in sync with vpn.Config defaults. */
    private static final String TUN_IPV4 = "10.111.0.2";
    private static final int TUN_IPV4_PREFIX = 30;
    private static final String TUN_IPV6 = "fd00:1::2";
    private static final int TUN_IPV6_PREFIX = 64;
    private static final int DEFAULT_MTU = 1500;

    private static final String CHANNEL_ID = "fastv_vpn";
    private static final int NOTIFICATION_ID = 7301;

    private static volatile boolean running = false;
    private static volatile String lastError = "";

    private ParcelFileDescriptor tun;

    /** Whether the dataplane currently owns an established tun interface. */
    public static boolean isRunning() {
        return running;
    }

    /** Last dataplane start/stop failure, or an empty string. */
    public static String lastError() {
        return lastError;
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        String action = intent != null ? intent.getAction() : null;
        if (ACTION_STOP.equals(action)) {
            stopVpn();
            return START_NOT_STICKY;
        }
        // startForegroundService() obliges us to post a notification promptly, and
        // doing it before establishing the interface keeps the failure paths (bad
        // port, denied consent, missing dataplane) from tripping that contract.
        startForegroundNotification();
        startVpn(intent);
        // A restarted service arrives with a null intent, which cannot carry the
        // SOCKS port, so never ask the system to recreate it.
        return START_NOT_STICKY;
    }

    private void startVpn(Intent intent) {
        if (running) {
            return;
        }
        int mtu = intent != null ? intent.getIntExtra(EXTRA_MTU, DEFAULT_MTU) : DEFAULT_MTU;
        int socksPort = intent != null ? intent.getIntExtra(EXTRA_SOCKS_PORT, 0) : 0;
        if (socksPort <= 0) {
            fail("缺少本地 SOCKS 端口，无法建立数据面");
            return;
        }

        Builder builder = new Builder();
        builder.setSession("FastV");
        builder.addAddress(TUN_IPV4, TUN_IPV4_PREFIX);
        builder.addRoute("0.0.0.0", 0);
        builder.setMtu(mtu);
        builder.addDnsServer("1.1.1.1");
        builder.addDnsServer("8.8.8.8");
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.LOLLIPOP) {
            try {
                builder.addAddress(TUN_IPV6, TUN_IPV6_PREFIX);
                builder.addRoute("::", 0);
                builder.addDnsServer("2606:4700:4700::1111");
            } catch (Exception error) {
                Log.w(TAG, "IPv6 tun configuration skipped", error);
            }
        }
        // Keep the proxy's own upstream socket off the tun it just created.
        try {
            builder.addDisallowedApplication(getPackageName());
        } catch (Exception error) {
            Log.w(TAG, "Unable to exclude the app from the tunnel", error);
        }

        try {
            tun = builder.establish();
        } catch (Exception error) {
            fail("建立 VPN 接口失败: " + error.getMessage());
            return;
        }
        if (tun == null) {
            // VpnService.prepare() was not granted by the activity.
            fail("VPN 授权未通过，请在弹出的系统对话框中允许");
            return;
        }

        int fd = tun.detachFd();
        tun = null;
        try {
            int result = WailsBridge.nativeVpnStart(fd, socksPort, mtu);
            if (result != 0) {
                fail("数据面启动失败 (code " + result + ")");
                return;
            }
        } catch (Throwable error) {
            // A missing/mismatched native export must not crash the app: report it
            // and drop the interface instead.
            Log.e(TAG, "nativeVpnStart failed", error);
            fail("数据面不可用: " + error);
            return;
        }

        running = true;
        lastError = "";
        Log.i(TAG, "VPN tunnel established (socks=" + socksPort + ", mtu=" + mtu + ")");
    }

    private void stopVpn() {
        if (running) {
            try {
                WailsBridge.nativeVpnStop();
            } catch (Throwable error) {
                Log.w(TAG, "nativeVpnStop failed", error);
            }
            running = false;
        }
        if (tun != null) {
            try {
                tun.close();
            } catch (Exception ignored) {
                // Best-effort close; the dataplane already owns the detached fd.
            }
            tun = null;
        }
        stopForegroundNotification();
        stopSelf();
    }

    private void fail(String message) {
        lastError = message;
        running = false;
        Log.e(TAG, message);
        stopForegroundNotification();
        stopSelf();
    }

    @Override
    public void onRevoke() {
        Log.i(TAG, "VPN permission revoked by the system; stopping");
        stopVpn();
        super.onRevoke();
    }

    @Override
    public void onDestroy() {
        stopVpn();
        super.onDestroy();
    }

    private void startForegroundNotification() {
        // The two-argument call means "use the type declared in the manifest",
        // which is what lets one code path serve every API level from 21 up.
        startForeground(NOTIFICATION_ID, buildNotification());
    }

    private void stopForegroundNotification() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) {
            stopForeground(STOP_FOREGROUND_REMOVE);
        } else {
            stopForeground(true);
        }
    }

    private Notification buildNotification() {
        NotificationManager manager = (NotificationManager) getSystemService(Context.NOTIFICATION_SERVICE);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O && manager != null) {
            NotificationChannel channel = new NotificationChannel(
                    CHANNEL_ID, "FastV VPN", NotificationManager.IMPORTANCE_LOW);
            channel.setShowBadge(false);
            manager.createNotificationChannel(channel);
        }

        Intent open = new Intent(this, MainActivity.class);
        open.setFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TOP);
        int flags = PendingIntent.FLAG_UPDATE_CURRENT;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
            flags |= PendingIntent.FLAG_IMMUTABLE;
        }
        PendingIntent content = PendingIntent.getActivity(this, 0, open, flags);

        Intent stop = new Intent(this, FastVVpnService.class);
        stop.setAction(ACTION_STOP);
        PendingIntent stopIntent = PendingIntent.getService(this, 1, stop, flags);

        Notification.Builder builder;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            builder = new Notification.Builder(this, CHANNEL_ID);
        } else {
            builder = new Notification.Builder(this);
        }
        // The deprecated int-icon constructor is the only one available on the
        // API 21/22 floor this app still supports.
        Notification.Action disconnect = new Notification.Action.Builder(
                android.R.drawable.ic_menu_close_clear_cancel, "断开", stopIntent).build();
        return builder
                .setContentTitle("FastV 代理已开启")
                .setContentText("系统流量正通过所选节点转发")
                .setSmallIcon(R.mipmap.ic_launcher)
                .setContentIntent(content)
                .addAction(disconnect)
                .setOngoing(true)
                .setPriority(Notification.PRIORITY_LOW)
                .build();
    }
}
