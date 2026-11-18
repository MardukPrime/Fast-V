<div align="center">

# FastV

**一个现代、轻量、专注桌面体验的代理客户端**

Windows 10/11 · macOS 11+ · Linux (GTK3 / WebKitGTK 4.1) · Wails v3 · Xray-core · SolidJS

![Version](https://img.shields.io/badge/version-0.4.0--pre.1-5B8DEF)
[![Release](https://img.shields.io/github/v/release/MardukPrime/Fast-V?include_prereleases&label=release)](https://github.com/MardukPrime/Fast-V/releases)
![Platform](https://img.shields.io/badge/platform-Windows%20%7C%20macOS%20%7C%20Linux-2F6FED)
![Core](https://img.shields.io/badge/core-Xray--core-38A169)
![License](https://img.shields.io/badge/license-GPL--3.0-718096)

</div>

---

FastV 是一款面向桌面端的代理客户端。它把订阅管理、节点配置、Xray 内核、路由分流、本地代理、TUN、网络诊断和运行日志整合到一个界面里，尽量减少在多个工具之间来回切换。

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

### Xray 内核管理

- 自动检测本地 Xray-core
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

最新预发布版本由 GitHub Actions 在 **Windows / macOS / Linux** 上自动构建（多平台产物见 Releases 页面）：

- Releases 页面：<https://github.com/MardukPrime/Fast-V/releases>
- **Windows x64**：`FastV-<version>-windows-amd64.zip`，解压后运行 `Fast-V.exe`；要求 Windows 10/11 x64 + Microsoft Edge WebView2 Runtime
- **macOS（Apple Silicon / Intel）**：`FastV-<version>-macos-arm64.zip` / `-macos-amd64.zip`，解压得到 `FastV.app`；要求 macOS 11+，未签名构建首次启动需右键 → 打开
- **Linux（x64 / arm64）**：`FastV-<version>-linux-<arch>.tar.gz`，解压后运行 `FastV`；要求 GTK3 与 WebKitGTK 4.1（如 Debian/Ubuntu 的 `libgtk-3-0`、`libwebkit2gtk-4.1-0`）
- 完整性校验：同页 `SHA256SUMS.txt`；平台说明见 `release-info.txt`
- 应用内自动更新当前仅支持 Windows；macOS/Linux 请手动下载新版本
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

> 仓库只包含源码和文档。Wails 构建资源、平台资源、安装包和本地工具不纳入版本控制。

## 当前状态

已实现：

- 订阅和节点管理
- 订阅格式：分享链接 / Base64 / Clash YAML，节点与订阅二维码
- 节点分享链接导入 / 导出、手动节点、收藏、标签分组、排序与批量操作
- 节点配置提取与编辑
- TCP 连通性测速与真实链路 URL 延迟测速（批量临时内核），延迟历史记录
- 自动选择最优节点、故障自动切换、Xray 负载均衡（leastPing / leastLoad / roundRobin）
- Xray 配置生成与 `run -test` 校验、内核更新与 GeoData 更新
- 本地代理启动和停止
- 本地 SOCKS/HTTP 入站（局域网共享与入站认证，可选 Windows TUN、Windows 系统代理）
- 路由规则（域名 / IP / 端口 / 网络 / Windows 进程分流，内置规则预设与 JSON 导入导出）
- 实时流量统计与每日流量历史（Xray StatsService）
- 网络诊断与 10 项一键自检
- 运行日志（筛选、暂停、导出）
- 配置备份与恢复、开机自启、系统托盘（代理模式切换与重启代理）、关闭 / 最小化到托盘、应用更新（检查、下载、重启安装）
- 全局快捷键（设置页可录制自定义组合，默认 Ctrl+Shift+F 启停代理、Ctrl+Shift+M 切换模式、Ctrl+Shift+N 下一个节点）
- WebDAV 云备份与恢复（兼容 Basic Auth，测试连接 / 上传 / 恢复）
- 配置覆写：高级用户可用 JSON 深合并覆盖生成的 Xray 配置（对象递归合并、数组替换、null 删除字段，保存后重启代理生效）
- 订阅独立自动更新：每个订阅可单独开关自动更新、设置独立更新间隔与自定义 User-Agent（默认跟随全局设置）
- 节点分组：手动分组快速切换，或自动测速分组按间隔测速并自动选择延迟最低节点
- 规则集订阅：远程域名/IP 列表定时下载并展开为路由规则（兼容 hosts / Adblock / Clash 列表格式，失败保留上次规则）
- 深链接导入：注册 `fastv://` 协议（Windows），支持通过链接导入订阅或分享链接；命令行参数同样生效
- 深色 / 浅色 / 跟随系统主题（默认浅色）与 5 套强调色（天空蓝 / 紫罗兰 / 翡翠绿 / 琥珀橙 / 玫瑰红）

- 多平台构建：Windows x64、macOS（Apple Silicon / Intel，`FastV.app`）、Linux x64/arm64；Windows 专属能力（TUN、系统代理、开机自启、托盘菜单、应用内更新）在 macOS/Linux 上暂不提供，其余核心功能（订阅、节点、路由、流量统计、诊断、WebDAV）跨平台可用

当前仅支持 Xray-core。

## Roadmap

- sing-box 内核
- Hysteria2 / TUIC
- 连接详情（Xray API 无连接级列表，暂以流量 / 在线统计替代）
- 规则集订阅与远程规则管理
- 应用自更新安装
- macOS / Linux 适配
- 多语言界面
- SQLite 存储选项

## 反馈

- 提交 Issue：请在仓库 Issues 中描述问题、复现步骤和系统版本
- 提交代码：欢迎提交 Pull Request
- 日志和截图请先移除订阅地址、节点 UUID、密码和私钥

## 文档

- [节点配置编辑器](docs/cores/xray/node-editor.md)
- [网络诊断](docs/network-diagnostics.md)
- [Xray TUN](docs/cores/xray/tun.md)
- [GeoData](docs/cores/xray/geodata.md)
- [液态玻璃底栏](docs/ui/LIQUID_GLASS.md)
- [UI 覆盖与文字布局审查](docs/ui/UI_AUDIT.md)
- [更新日志](CHANGELOG.md)
- [完整文档索引](docs/README.md)

## License

GPL-3.0. See [LICENSE](LICENSE).