# Xray TUN on Windows

FastV uses the native Xray TUN inbound on Windows. TUN creates a virtual network adapter and forwards layer-3 traffic into Xray for proxying.

## Requirements

- Windows 10/11
- FastV running as administrator (automatic UAC elevation is supported)
- `wintun.dll` next to `xray.exe`
- Xray-core version with TUN inbound support
- Xray-core configured with the TUN inbound

The Xray release archive includes `wintun.dll`; FastV installs all files from the archive during core installation/update.

## Generated Inbound

```json
{
  "tag": "tun-in",
  "protocol": "tun",
  "settings": {
    "name": "FastV-TUN",
    "MTU": 1500
  },
  "sniffing": {
    "enabled": true,
    "destOverride": ["http", "tls", "quic"]
  }
}
```

The local SOCKS/HTTP inbound remains enabled alongside TUN so proxy testing and local clients still work.

## Windows Startup Sequence

1. Check administrator privileges.
2. Check that `wintun.dll` exists next to `xray.exe`.
3. Start Xray and wait until the local SOCKS/HTTP port is ready.
4. Wait for the `FastV-TUN` adapter to appear.
5. Assign an IPv4 address to the adapter.
6. Find the active physical default route and gateway.
7. Add a host route to the proxy server IP through the physical gateway.
8. Add the default route through `FastV-TUN`.
9. Set TUN interface metric and DNS servers.
10. Mark the proxy as running.

The proxy server host route is essential. Without it, Xray's own uplink can be routed back into the TUN interface and create a loop.

## Cleanup

On stop, crash or application exit FastV removes:

- Default route through `FastV-TUN`
- Proxy server host route
- TUN interface when the Xray process exits

The TUN adapter is owned by the Xray process. The Windows Job Object ensures the process tree is terminated when FastV exits.

## Current Limitations

- TUN route setup is implemented for Windows only.
- IPv4 route setup is implemented; IPv6 TUN route setup is not yet exposed.
- TUN requires administrator privileges.
- When TUN mode is enabled, FastV automatically requests UAC elevation and relaunches itself with administrator privileges.
- ICMP is not supported by the Xray TUN implementation.
- System proxy mode and TUN mode are separate; TUN does not set the Windows system proxy.

## Validation

Configuration shape can be validated without administrator privileges:

```bash
xray run -test -config runtime/xray-config.json
```

Actual adapter creation and route changes require an elevated process and should be tested on a disposable machine or with a known-good proxy endpoint.
## Automatic Elevation

FastV handles TUN elevation automatically:

1. TUN mode is enabled and settings are saved, or the proxy is started with TUN mode already enabled.
2. If the current process is not elevated, FastV launches a hidden helper through the Windows `runas` UAC prompt.
3. The helper waits for the current process to exit, then starts `fastv.exe` again with the elevated token.
4. FastV starts normally on the next launch because the saved TUN setting is still enabled.

If the user cancels UAC:

- Settings remain saved.
- FastV stays open with normal privileges.
- Starting the proxy reports that TUN mode requires administrator permission.

At startup, if TUN mode is already saved and FastV is not elevated, the same UAC request is made before the Wails window is created. This avoids creating two UI instances.