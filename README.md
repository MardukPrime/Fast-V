<div align="center">

# FastV

**一个现代、轻量的代理客户端：桌面完整体验 + Android 预览版（内嵌 Xray 内核）**

Windows 10/11 · macOS 11+ · Linux (GTK3 / WebKitGTK 4.1) · Android 5.0+（预览） · Wails v3 · Xray-core · SolidJS

![Version](https://img.shields.io/badge/version-1.9.0--pre.1-2F6FED)
![Stable](https://img.shields.io/badge/stable-1.1.0-38A169)
[![Release](https://img.shields.io/github/v/release/MardukPrime/Fast-V?include_prereleases&label=release)](https://github.com/MardukPrime/Fast-V/releases)
![Platform](https://img.shields.io/badge/platform-Windows%20%7C%20macOS%20%7C%20Linux%20%7C%20Android-2F6FED)
![Core](https://img.shields.io/badge/core-Xray--core-38A169)
![License](https://img.shields.io/badge/license-GPL--3.0-718096)

</div>

---

FastV 是一款面向桌面端（并已提供 Android 预览版）的代理客户端。它把订阅管理、节点配置、Xray 内核、路由分流、本地代理、TUN、网络诊断和运行日志整合到一个界面里，尽量减少在多个工具之间来回切换。

桌面版使用外部 Xray-core 可执行文件；Android 版（预览）把 Xray-core 以 Go 库形式内嵌到应用内，可启动本地 SOCKS 代理，系统级 VPN/TUN 正在开发中（见 [Android Phase 2](docs/product/ANDROID_PHASE2.md)）。

FastV 不自行实现代理协议，而是使用 Xray-core 作为代理内核。

## 产品特色

### 订阅与节点管理

- 支持 Base64 订阅导入
- 订阅添加、更新、批量更新和删除
- 按订阅自动解析节点
- 节点搜索、协议筛选和订阅筛选
- 当前节点快速切换
- 节点启用 / 禁用
- 单节点 TCP 连通性测试
- 删除单个节点或整个订阅
- 订阅自动更新
- 自动解析 Subscription-Userinfo 套餐额度和已用流量
- 订阅更新时保留节点 ID 和启用状态

### 节点配置编辑

- 从 VMess、VLESS、Trojan、Shadowsocks 分享链接提取配置
- 表单编辑常用节点参数
- 修改服务器地址、端口、UUID、密码和加密方式
- 修改 WebSocket、gRPC、HTTPUpgrade、XHTTP 等传输参数
- 修改 TLS、REALITY、SNI、Fingerprint、ALPN
- 修改 Host、Path、gRPC Service Name
- 修改 REALITY Public Key、Short ID、SpiderX
- Xray outbound JSON 实时预览
- 高级 Xray JSON 直接编辑和覆盖
- 保存并应用
- 切换节点时自动重启正在运行的代理

### 内核管理（Xray-core / sing-box）

- 双内核可选：Xray-core（默认）或 sing-box；切换后独立检测、下载、更新与运行
- 自动检测本地 Xray-core / sing-box
- 检查 GitHub 最新版本
- 下载并安装 Xray-core
- SHA-256 校验
- 自动更新内核
- 自动生成 Xray 配置
- 启动前执行 `xray -test` 配置校验
- 启动、停止和重启内核
- Windows Job Object 进程树清理
- 应用退出时自动清理内核进程
- TUN 模式自动请求 UAC 管理员权限并重启
- GeoData 资源检测与更新（geoip.dat / geosite.dat）
- Xray DNS 服务器、DNS 查询策略、路由域名策略和 TUN MTU 配置
- FinalMask 传输伪装和 Sockopt 底层网络参数
- 本地 Xray API、StatsService 和统计策略配置
- sing-box 配置生成（mixed SOCKS/HTTP、VMess/VLESS/Trojan/Shadowsocks、TLS/REALITY、WS/gRPC/HTTPUpgrade）
- sing-box 启动前 `check` 校验、版本记录与自动更新；暂不支持 TUN/负载均衡/实时统计

### 本地代理与 TUN

- 本地 SOCKS/HTTP 入站
- 自定义本地端口
- 启动后显示实际入站地址
- 代理链路测试
- Windows Xray TUN 模式
- Wintun 适配器
- 管理员权限预检查
- TUN 地址、默认路由和代理服务器主机路由自动配置
- 停止或退出时自动清理路由
- TUN 模式与本地 SOCKS/HTTP 入站同时可用

TUN 模式需要：

- Windows 10/11
- 以管理员身份运行 FastV
- `xray.exe` 同目录存在 `wintun.dll`

### Android（预览版）

- 内嵌 Xray-core（Go library，随应用进程运行，无需下载外部内核）
- 本地 SOCKS 代理（默认 `127.0.0.1:10808`），可配合支持 SOCKS 的应用或开发调试使用
- 与桌面共用的订阅、节点、分组、路由规则、规则集、备份/快照/导入导出
- 移动端响应式布局：单列页面、底部图标导航、设置分区折叠与快捷导航
- 内嵌模式自动跳过缺少数据集的 GeoIP/GeoSite 路由条件并记录警告
- 限制：暂不支持系统级 VPN/TUN、实时流量统计与桌面专属能力（M3/M4 开发中）

### 代理模式

- 规则分流
- 全局代理
- 全部直连
- 自定义路由优先级
- 代理运行中修改配置后自动重启生效

### 路由规则

支持直接编辑 Xray 路由规则：

- 域名后缀
- 域名关键字
- 域名正则
- IP / CIDR
- GeoSite
- GeoIP
- 端口
- TCP / UDP

每个规则支持：

- 走代理
- 直连
- 拦截
- 启用 / 禁用
- 上移 / 下移调整优先级

未命中自定义规则的流量，按当前代理模式处理。

### 网络诊断

无需打开外部网站，直接在 FastV 内检测：

- 直连出口 IP
- 代理出口 IP
- IPv4 / IPv6
- 国家、地区、城市、时区
- ASN、运营商、ISP
- Cloudflare 边缘节点
- TLS 版本
- WARP 状态
- 平均延迟
- 抖动
- 5 MB 下载速度
- 直连出口与代理出口对比
- 出口 IP 是否切换

检测接口带有备用线路和自动重试，Cloudflare trace 不可用时会自动切换其他检测方式。

### 运行日志

- 实时捕获 Xray stdout / stderr
- 自动识别 debug、info、warning、error
- 关键词搜索
- 等级筛选
- 暂停 / 继续接收
- 自动滚动
- 复制当前筛选日志
- 清空日志
- 最多保留 1500 条，避免内存无限增长

### 界面

- 无边框窗口
- 自定义可拖动标题栏
- 液态玻璃视觉系统
- 深色主题
- 浅色主题
- 跟随系统主题
- 主题即时预览
- 可折叠设置分区
- 清晰的节点、路由和日志表格
- 全界面液态玻璃视觉系统
- 主流代理客户端风格仪表盘：连接状态、当前节点、套餐流量和快捷操作
- 液态玻璃底部快捷栏
- 侧边模式 / 底栏模式一键切换（默认底栏模式）
- 底栏玻璃透明度可调
- 响应式布局
- 首页实时流量波形：波形 / 柱状 / 极简三种样式（持久化），标注峰值/谷值；「最近 7 天流量」内嵌今日实时速率曲线
- 流量悬浮球（可选，默认关闭）：140px 玻璃球体 + 旋转活动环、实时速率与曲线、指针拖动、单击回到主界面
- 设置分区快捷导航（吸顶、展开定位、收起全部）与订阅行图标操作 + 更多菜单
- 首次运行三步引导；日志空状态引导；托盘状态颜色；全局快捷键录制
- 移动端布局：≤720px 单列页面、图标 Dock、设置分区折叠优化

## 支持的协议

| 协议 | 订阅解析 | 节点编辑 | Xray 配置 |
|---|---|---|---|
| VMess | 支持 | 支持 | 支持 |
| VLESS | 支持 | 支持 | 支持 |
| Trojan | 支持 | 支持 | 支持 |
| Shadowsocks | 支持 | 支持 | 支持 |

传输与安全：

- RAW / TCP
- WebSocket
- gRPC
- HTTPUpgrade
- XHTTP
- TLS
- REALITY
- Mux
- 流量嗅探

## 下载

最新预览版 **v1.9.0-pre.1**（多内核 + Android 内嵌内核）与稳定版 **v1.1.0** 均由 GitHub Actions 在 Windows / macOS / Linux 上自动构建，历史产物见 Releases 页面：

- Releases 页面：<https://github.com/MardukPrime/Fast-V/releases>
- **Windows x64**：`FastV-<version>-windows-amd64-setup.exe`（安装包，含桌面/开始菜单快捷方式与卸载程序）或 `FastV-<version>-windows-amd64.zip`（便携版）；要求 Windows 10/11 x64 + Microsoft Edge WebView2 Runtime
- **macOS（Apple Silicon / Intel）**：`FastV-<version>-macos-arm64.zip` / `-macos-amd64.zip`，解压得到 `FastV.app`；要求 macOS 11+，未签名构建首次启动需右键 → 打开
- **Linux（x64 / arm64）**：`FastV-<version>-linux-<arch>.tar.gz` 便携包、`FastV-<version>-linux-<arch>.deb` 安装包（安装到 `/opt/fastv`），x64 另有 `FastV-<version>-linux-amd64.AppImage`；要求 GTK3 与 WebKitGTK 4.1（如 Debian/Ubuntu 的 `libgtk-3-0`、`libwebkit2gtk-4.1-0`）
- **Android（arm64，预览）**：`FastV-<version>-android-arm64.apk`（侧载，可直接安装）与 `FastV-<version>-android-arm64.aab`（Play 上架包）；**内置 Xray 内核**，可启动本地 SOCKS 代理（默认 `127.0.0.1:10808`）；系统级 VPN/TUN 仍在开发中；未配置 keystore 时为 debug 签名
- 完整性校验：同页 `SHA256SUMS.txt`；平台说明见 `release-info.txt`
- 应用内自动更新当前仅支持 Windows；macOS/Linux/Android 请手动下载新版本
- 发布流程与标签约定见 [发布说明](docs/operations/release.md)

## 快速开始

### 运行要求

- Windows 10/11
- Microsoft Edge WebView2 Runtime
- Xray-core
- TUN 模式需要管理员权限和 Wintun

### 使用流程

1. 在订阅页面添加订阅地址
2. 等待订阅解析完成
3. 在服务器页面选择当前节点
4. 在设置页面选择代理模式
5. 回到概览页面启动代理
6. 使用测试代理检查本地代理链路
7. 在网络诊断页面检查出口 IP 和网络质量
8. 在日志页面查看 Xray 运行日志

默认本地 SOCKS/HTTP 入站：

```text
127.0.0.1:10808
```

端口可以在设置中修改。

## 从源码运行

### 环境

- Go 1.27+
- Node.js 20+
- pnpm 10+
- Wails v3 CLI
- WebView2 Runtime

Android 构建额外需要：Android SDK（API 36）、NDK（29.x）、JDK 21；构建命令与签名方式见 [发布说明](docs/operations/release.md)。

### 安装前端依赖

```bash
pnpm --dir frontend install
```

### 生成 Wails bindings

```bash
wails3 generate bindings -clean=true -ts -i
```

### 开发模式

```bash
wails3 dev
```

### 生产构建

推荐使用正式构建脚本：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/build-windows.ps1 -Version 0.1.0
```

等价的核心参数：

```bash
pnpm --dir frontend run build
go build -tags production -trimpath -ldflags "-s -w -H=windowsgui -X github.com/MardukPrime/Fast-V/internal/buildinfo.Version=0.1.0" -o bin/fastv.exe .
```

`-H=windowsgui` 必须保留。缺少该参数会生成控制台子系统程序，启动时出现终端窗口。

验证/开发内嵌 Xray 内核（Android 与桌面 `embeddedcore` 共用代码路径）：

```bash
# 内嵌内核 + 本地 SOCKS 的桌面验证测试
go test -tags embeddedcore -run TestEmbeddedCoreServesSocks -v ./internal/proxy/embedded/

# 以嵌入式内核模式运行桌面版（开发验证，不代表正式桌面构建）
go build -tags "production,embeddedcore" -trimpath -o bin/fastv-embedded.exe .
```

> 仓库只包含源码和文档。Wails 构建资源、平台资源、安装包和本地工具不纳入版本控制。

## 当前状态

### 平台支持矩阵

| 能力 | Windows | macOS | Linux | Android（预览） |
|---|---|---|---|---|
| 订阅 / 节点 / 分组 / 路由规则 / 规则集 / 备份 | ✅ | ✅ | ✅ | ✅ |
| 本地 SOCKS 入站 | ✅ | ✅ | ✅ | ✅（内嵌 Xray 内核） |
| 流量统计（实时速率 + 每日历史） | ✅ | ✅ | ✅ | ⏳ 内嵌模式暂不支持 |
| 网络诊断 / DNS 审计 / 网络救援 / 日志 / 诊断报告 | ✅ | ✅ | ✅ | ✅（依赖桌面系统能力项除外） |
| 系统代理 / PAC / TUN | ✅ | ❌ | ❌ | ❌（M3 开发中） |
| 托盘 / 全局快捷键 / 悬浮球 / 开机自启 / 应用内更新 | ✅ | ❌ | ❌ | ❌ |
| WebDAV / 本地备份 / 配置快照 / 导入导出 | ✅ | ✅ | ✅ | ✅ |
| 安装包 | 安装包 + 便携 zip | `.app` zip | deb / AppImage / tar.gz | APK + AAB |

> macOS / Linux 使用外部 Xray-core（Wails purego 后端）；Android 预览版使用内嵌 Xray-core（Go library）。

### Android 预览版（v1.8.0-pre.1）

- 已内置 Xray-core，无需下载外部内核；启动代理后在本机监听 SOCKS（默认 `127.0.0.1:10808`）。
- 同一条内嵌内核链路已在桌面 `embeddedcore` 构建完成真实验证（`curl --socks5` 返回 HTTP 200）；Android 真机验证待补充。
- 当前限制：无系统级 VPN/TUN 接管（M3）、内嵌模式无实时流量统计、缺少 GeoIP/GeoSite 数据集时相关路由条件会被跳过并记录警告、未配置正式 keystore 时为 debug 签名。
- 安装与测试步骤见 [用户手册](docs/USER_GUIDE.md)；里程碑见 [Android Phase 2](docs/product/ANDROID_PHASE2.md)。

### 已知限制

- macOS 构建未签名/未公证，首次启动需右键打开；macOS/Linux 暂不提供系统代理、TUN、托盘、全局快捷键与应用内更新。
- Android 预览版暂不支持系统级 VPN/TUN 与实时流量统计，Geo 规则条件会跳过；正式上架仍需 Play 测试轨道与签名配置。
- TUN 模式仅 Windows，且需要管理员权限与 `wintun.dll`。

## Roadmap

- **Android M3**：`VpnService` + JNI + tun2socks 基础 TCP，实现系统级代理
- **Android M4**：UDP/DNS、分应用代理、绕过局域网、GeoIP/GeoSite 数据与内嵌流量统计
- **Android M5**：正式签名 AAB、Play 内部/封闭测试轨道
- Hysteria2 / TUIC 协议
- 连接详情（连接级列表受 Xray API 限制，暂以流量/在线统计替代）
- macOS 签名/公证，Linux 包管理与应用自更新
- 性能、内存与启动速度优化

## 反馈

- 提交 Issue：请在仓库 Issues 中描述问题、复现步骤和系统版本
- 提交代码：欢迎提交 Pull Request
- 日志和截图请先移除订阅地址、节点 UUID、密码和私钥

## 文档

- [用户手册](docs/USER_GUIDE.md)
- [Android Phase 2 计划](docs/product/ANDROID_PHASE2.md)
- [sing-box 内核集成](docs/cores/singbox/README.md)
- [常见问题 FAQ](docs/product/FAQ.md)
- [节点配置编辑器](docs/cores/xray/node-editor.md)
- [网络诊断](docs/network-diagnostics.md)
- [Xray TUN](docs/cores/xray/tun.md)
- [GeoData](docs/cores/xray/geodata.md)
- [液态玻璃底栏](docs/ui/LIQUID_GLASS.md)
- [UI 覆盖与文字布局审查](docs/ui/UI_AUDIT.md)
- [同类产品社区调研与改进方向](docs/product/COMMUNITY_RESEARCH.md)
- [更新日志](CHANGELOG.md)
- [完整文档索引](docs/README.md)

## License

GPL-3.0. See [LICENSE](LICENSE).