# Android Phase 2：VpnService + 内嵌内核

> 状态：**进行中（M1 脚手架已落地）**。当前 Android 预览版只能管理订阅/节点/规则，不能代理流量。本文记录把 Android 变成真正可用的系统级代理客户端所需的工作与里程碑。

## 目标

- 点击“启动”后申请 Android VPN 权限，通过 `VpnService` 接管系统流量；
- 内嵌 Xray-core（Go library，进程内运行，不再依赖外部 `xray` 可执行文件），生成配置复用现有 builder；
- tun 流量经 tun2socks（gVisor netstack 或 go-tun2socks）转发到内嵌 SOCKS/HTTP inbound；
- 前台服务 + 常驻通知，支持断开/切换节点；提供分应用代理、绕过局域网、DNS 处理；
- 与桌面版共用订阅/节点/路由/备份能力，UI 针对移动端简化。

## 架构

```
Android App (Java host, Wails)
 ├─ FastVVpnService (VpnService) ── tun fd ──┐
 ├─ Go runtime (libwails.so)                 │
 │   ├─ embeddedcore runner (xray-core lib)  │
 │   ├─ tun2socks bridge  ◀──────────────────┘
 │   ├─ VpnService JNI bridge (start/stop/status/routes)
 │   └─ existing services (subscriptions, routing, backup, …)
 └─ WebView UI（移动端连接页 + 既有管理页）
```

关键改造点：

1. `internal/proxy` 增加可插拔 `coreRunner`：桌面继续 exec 子进程；Android 使用 `xray-core` 的 `core.StartInstance`（Go library）在同一进程内运行。
2. `internal/core` 在 Android 上不下载外部内核，改为报告内嵌版本，并跳过 `GeoData` 下载。
3. Java 侧 `FastVVpnService`：`Builder` 配置地址/路由/DNS/分应用；建立 tun 后通过 JNI 将 fd 与 MTU 传给 Go。
4. Go 侧 tun2socks：读取 tun fd → 用户态 TCP/IP 栈 → 连接本地 SOCKS inbound；UDP/DNS 单独处理。
5. 前台服务 + 通知：“FastV 正在运行 / 当前节点 / 流量 / 断开”，Android 14+ 声明 `FOREGROUND_SERVICE_SPECIAL_USE` 或 `dataSync`（按 Play 政策选择）。
6. UI：Android 首页改为大号连接开关 + 当前节点 + 流量；桌面专属页（TUN 自检、系统代理、悬浮球、全局快捷键）在 Android 隐藏或改为“仅桌面可用”。

## 里程碑

| 里程碑 | 内容 | 验收 |
|---|---|---|
| M1（已完成） | VpnService manifest/权限声明、`FastVVpnService` 脚手架、Phase 2 文档 | APK 构建通过，权限列表正确 |
| M2 | 内嵌 Xray-core（Go library）+ 本地 SOCKS/HTTP inbound 在 Android 运行 | 真机/模拟器内 `curl --socks5 127.0.0.1:10808` 成功 |
| M3 | VpnService 建 tun + JNI 传 fd + tun2socks 基本 TCP | 手机浏览器全局代理可上网；通知可断开 |
| M4 | UDP/DNS、分应用代理、绕过局域网、IPv6、重连与看门狗 | 常用 App 直连/代理策略符合预期 |
| M5 | 正式签名 AAB、Play 声明与政策材料、真机回归 | 内部/封闭测试通过，可提交正式轨道 |

## 风险与注意事项

- **Play VPN 政策**：代理/VPN 类应用必须声明核心功能，不得用于绕过平台限制；需要隐私政策与 Data safety。
- **进程内核心体积**：xray-core 依赖较多，APK 预计增加 20–40 MB；可使用 `-trimpath` 与按需构建。
- **CGO/NDK**：Android 构建必须带 NDK 工具链；内嵌核心需与 Wails c-shared 链接兼容。
- **后台存活**：各厂商省电策略差异大，需要前台服务 + 用户引导（忽略电池优化）。
- **安全**：VpnService 与内嵌核心的 fd 传递需校验调用方，避免任意进程触发连接。

## 当前状态

- `FastVVpnService` 已注册在 `packaging/android/app/src/main/AndroidManifest.xml`，但应用不会启动它，也没有 VPN 权限弹窗；
- 现有 Android 包仍是**管理预览版**，请勿对外宣传为可用代理。
