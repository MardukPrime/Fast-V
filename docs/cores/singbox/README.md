# sing-box 内核集成

FastV 从 1.9.0-pre.1 起支持把 sing-box 作为替代内核（设置 → 内核设置切换）。

## 安装与版本

- 内核目录：`<数据目录>/cores/singbox`；二进制 `sing-box` / `sing-box.exe`。
- 来源：GitHub `SagerNet/sing-box` 最新 release；Windows 使用 `sing-box-<ver>-windows-<arch>.zip`，macOS/Linux 使用 `sing-box-<ver>-<os>-<arch>.tar.gz`。
- 安装时保留压缩包内运行所需文件（如 `libcronet.dll`）；版本记录写入 `version.json`。
- 检查更新/下载/自动更新与 Xray 共用 CoreService，按当前设置的内核类型选择。

## 配置生成

`internal/proxy/builder_singbox.go` 从节点 `OutboundJSON`（或分享链接）转换生成：

| FastV / Xray | sing-box |
|---|---|
| 本地 SOCKS/HTTP 入站 | `mixed` 入站（同一端口） |
| VMess | `vmess` 出站（uuid/security/alter_id） |
| VLESS | `vless` 出站（uuid/flow） |
| Trojan | `trojan` 出站（password） |
| Shadowsocks | `shadowsocks` 出站（method/password） |
| TLS/REALITY | `tls`（server_name/insecure/alpn/utls/reality） |
| WebSocket/gRPC/HTTPUpgrade | `transport` |
| 路由规则（域名/IP/端口/网络/进程） | `route.rules` + `action: route/reject` |
| 代理模式 | `route.final = proxy` 或 `direct` |

启动前执行 `sing-box check -c <config>`；配置写入 `<数据目录>/runtime/sing-box-config.json`。

## 已知限制

- 不支持 TUN 模式与负载均衡（会退回单节点并记录警告）；实时流量统计通过 Clash API WebSocket (`/traffic`) 采集。
- 缺少 GeoIP/GeoSite 数据集，相关路由规则被跳过（记录警告）；Xray 的 GeoData 更新不适用于 sing-box。
- 节点「真实链路测速」仍启动临时 Xray 内核，因此需要同时安装 Xray-core。
- Android 预览版使用内嵌 Xray-core，不提供 sing-box 切换。

## 验证

- `go test ./internal/proxy/ -run SingBox`：配置生成、路由映射与 Clash API 流量采样。
- 实测：本地 sing-box 作为 Shadowsocks 服务端，FastV 选择 sing-box 启动代理，`curl --socks5-hostname 127.0.0.1:18080` 与 HTTP 代理均返回 200 / 1 MB。
